"""Use official HTTPS Ubuntu mirrors on disposable CI runners."""

from pathlib import Path
import re


def official_https_sources(source: str) -> str:
    source = re.sub(
        r"http://(?:[A-Za-z0-9-]+\.)*archive\.ubuntu\.com/ubuntu/?",
        "https://archive.ubuntu.com/ubuntu/",
        source,
    )
    return re.sub(
        r"http://security\.ubuntu\.com/ubuntu/?",
        "https://security.ubuntu.com/ubuntu/",
        source,
    )


def configure_sources(root: Path) -> int:
    files = [root / "sources.list"]
    directory = root / "sources.list.d"
    files.extend(directory.glob("*.list"))
    files.extend(directory.glob("*.sources"))
    changed = 0
    for file in files:
        if not file.is_file():
            continue
        source = file.read_text()
        replacement = official_https_sources(source)
        if replacement != source:
            file.write_text(replacement)
            changed += 1
    return changed


if __name__ == "__main__":
    print(f"Updated {configure_sources(Path('/etc/apt'))} Ubuntu source files")
