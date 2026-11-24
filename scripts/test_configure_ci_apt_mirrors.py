from pathlib import Path
import tempfile
import unittest

from configure_ci_apt_mirrors import configure_sources, official_https_sources


class UbuntuMirrorTests(unittest.TestCase):
    def test_deb822_regional_mirror_keeps_suites_and_signature(self):
        source = (
            "Types: deb\n"
            "URIs: http://us-east-2.ec2.archive.ubuntu.com/ubuntu/\n"
            "Suites: noble noble-updates noble-backports\n"
            "Components: main restricted universe multiverse\n"
            "Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg\n"
        )
        converted = official_https_sources(source)
        self.assertIn("URIs: https://archive.ubuntu.com/ubuntu/\n", converted)
        self.assertIn("Suites: noble noble-updates noble-backports\n", converted)
        self.assertIn("Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg\n", converted)
        self.assertEqual(converted, official_https_sources(converted))

    def test_legacy_security_and_other_repositories(self):
        source = (
            "deb http://security.ubuntu.com/ubuntu noble-security main\n"
            "deb https://archive.ubuntu.com/ubuntu noble main\n"
            "deb http://packages.example.org/ubuntu noble main\n"
        )
        converted = official_https_sources(source)
        self.assertIn("deb https://security.ubuntu.com/ubuntu/ noble-security main", converted)
        self.assertIn("deb https://archive.ubuntu.com/ubuntu noble main", converted)
        self.assertIn("deb http://packages.example.org/ubuntu noble main", converted)

    def test_changes_only_supported_source_files_in_supplied_directory(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            directory = root / "sources.list.d"
            directory.mkdir()
            entry = directory / "ubuntu.sources"
            entry.write_text("URIs: http://us-east-2.ec2.archive.ubuntu.com/ubuntu/\n")
            ignored = directory / "notes.txt"
            ignored.write_text(entry.read_text())
            self.assertEqual(1, configure_sources(root))
            self.assertEqual(0, configure_sources(root))
            self.assertTrue(entry.read_text().startswith("URIs: https://archive.ubuntu.com/"))
            self.assertTrue(ignored.read_text().startswith("URIs: http://us-east-2.ec2."))


if __name__ == "__main__":
    unittest.main()
