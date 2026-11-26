import datetime
import json
import os
from pathlib import Path
import shutil
import shlex
import subprocess
import tempfile
import unittest


class BuildMetadataTests(unittest.TestCase):
    def setUp(self):
        self.work = tempfile.TemporaryDirectory()
        self.addCleanup(self.work.cleanup)
        self.root = Path(self.work.name) / "repo"
        self.root.mkdir()
        (self.root / "scripts").mkdir()
        shutil.copy(Path(__file__).with_name("build.sh"), self.root / "scripts/build.sh")
        (self.root / "source.txt").write_text("original\n")
        self.git("init", "-q")
        self.git("config", "user.name", "Build fixture")
        self.git("config", "user.email", "fixture@example.com")
        self.git("add", ".")
        self.git("commit", "-qm", "fixture")
        self.sha = self.git("rev-parse", "HEAD").strip()
        self.bin = Path(self.work.name) / "bin"
        self.bin.mkdir()
        for name in ("go", "docker"):
            path = self.bin / name
            path.write_text("#!/usr/bin/env python3\nimport json,sys\nprint(json.dumps(sys.argv[1:]))\n")
            path.chmod(0o755)

    def git(self, *args):
        return subprocess.check_output(["git", *args], cwd=self.root, text=True)

    def build(self, *args, extra=None, check=True):
        env = {k: v for k, v in os.environ.items() if not k.startswith("JOBIFAI_BUILD_") and k != "JOBIFAI_SOURCE_STATE"}
        env["PATH"] = str(self.bin) + os.pathsep + env["PATH"]
        env.update(extra or {})
        return subprocess.run(["bash", str(self.root / "scripts/build.sh"), *args], cwd=self.root, env=env, text=True, capture_output=True, check=check)

    def metadata(self):
        return dict(line.split(": ", 1) for line in self.build("metadata").stdout.splitlines())

    def test_clean_and_dirty_builds_are_distinguishable(self):
        clean = self.metadata()
        self.assertEqual(self.sha, clean["Commit"])
        self.assertEqual("clean", clean["Source"])
        datetime.datetime.strptime(clean["Built (UTC)"], "%Y-%m-%dT%H:%M:%SZ")
        (self.root / "source.txt").write_text("modified\n")
        dirty = self.metadata()
        self.assertEqual(self.sha, dirty["Commit"])
        self.assertEqual("modified", dirty["Source"])
        self.assertEqual(clean["Version"] + "-dirty", dirty["Version"])

    def test_release_tags_identify_the_release(self):
        self.git("tag", "v1.2.3")
        self.assertEqual("v1.2.3", self.metadata()["Version"])

    def test_cloud_build_without_git_uses_the_provided_commit(self):
        (self.root / ".git").rename(Path(self.work.name) / "saved-git")
        result = self.build("metadata", extra={"JOBIFAI_BUILD_GIT_SHA": self.sha, "JOBIFAI_SOURCE_STATE": "clean"})
        metadata = dict(line.split(": ", 1) for line in result.stdout.splitlines())
        self.assertEqual(self.sha[:12], metadata["Version"])
        self.assertEqual(self.sha, metadata["Commit"])
        self.assertEqual("clean", metadata["Source"])

    def test_native_and_docker_receive_the_same_identity(self):
        extra = {"JOBIFAI_BUILD_TIME": "2026-11-26T01:00:00Z"}
        go_args = json.loads(self.build("go", "build", "-o", "binary", "./cmd/server", extra=extra).stdout)
        self.assertEqual("build", go_args[0])
        assignments = {}
        tokens = shlex.split(go_args[go_args.index("-ldflags") + 1])
        for i, token in enumerate(tokens):
            if token == "-X":
                key, value = tokens[i + 1].split("=", 1)
                assignments[key.rsplit(".", 1)[1]] = value
        for mode in ("docker", "buildx"):
            args = json.loads(self.build(mode, "-t", "fixture:test", ".", extra=extra).stdout)
            docker_values = dict(args[i + 1].split("=", 1) for i, arg in enumerate(args) if arg == "--build-arg")
            self.assertEqual(assignments["Version"], docker_values["BUILD_VERSION"])
            self.assertEqual(self.sha, docker_values["BUILD_GIT_SHA"])
            self.assertEqual(assignments["BuildTime"], docker_values["BUILD_TIMESTAMP"])
            self.assertEqual(assignments["SourceState"], docker_values["BUILD_SOURCE_STATE"])

    def test_linker_values_cannot_split_into_extra_arguments(self):
        result = self.build("go", "build", extra={"JOBIFAI_BUILD_VERSION": "version with spaces"}, check=False)
        self.assertNotEqual(0, result.returncode)
        self.assertIn("Invalid build metadata", result.stderr)


if __name__ == "__main__":
    unittest.main()
