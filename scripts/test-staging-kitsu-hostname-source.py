#!/usr/bin/env python3
from pathlib import Path
import subprocess
import sys
import tempfile

root = Path(__file__).resolve().parents[1]
helper = (root / "deploy/kitsusync-staging-deploy").read_text(encoding="utf-8")
start_marker = "<<'PY'\n"
start = helper.index(start_marker, helper.index('staging_kitsu_hostname="$(')) + len(start_marker)
end = helper.index("\nPY\n", start)
parser = helper[start:end] + "\n"

cases = [
    ("KITSU_HOSTNAME=https://Kitsu.Example.test/studio///\nDISCORD_BOT_TOKEN=never-print-this\n", 0, "https://kitsu.example.test/studio/"),
    ("KITSU_HOSTNAME=http://kitsu.example.test/\nKITSU_HOSTNAME=http://other.example.test/\n", 1, ""),
    ("KITSU_HOSTNAME=\n", 1, ""),
    ("KITSU_HOSTNAME=javascript://kitsu.example.test/\n", 1, ""),
    ("KITSU_HOSTNAME=https://user:password@kitsu.example.test/\n", 1, ""),
    ("KITSU_HOSTNAME=https://kitsu.example.test/?token=x\n", 1, ""),
    ("KITSU_HOSTNAME=https://kitsu.example.test/$value/\n", 1, ""),
]

with tempfile.TemporaryDirectory(prefix="staging-kitsu-hostname-") as directory:
    source = Path(directory) / ".env.local"
    for contents, expected_status, expected_value in cases:
        source.write_text(contents, encoding="utf-8")
        result = subprocess.run(
            [sys.executable, "-", str(source)],
            input=parser,
            text=True,
            capture_output=True,
            check=False,
        )
        if result.returncode != expected_status:
            raise SystemExit(f"hostname parser status mismatch: expected={expected_status}")
        if expected_status == 0:
            if result.stdout.strip() != expected_value or result.stderr:
                raise SystemExit("hostname normalization mismatch")
        elif result.stdout or result.stderr.strip() != "STAGING_KITSU_HOSTNAME_SOURCE_INVALID":
            raise SystemExit("hostname parser did not fail closed with a safe marker")
        if "never-print-this" in result.stdout or "password" in result.stdout:
            raise SystemExit("hostname parser leaked unrelated operator environment values")

print("staging-kitsu-hostname-source=PASS")
