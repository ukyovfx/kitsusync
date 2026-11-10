"""Validate and extract the fixed normal-release bundle layout."""

import hashlib
import os
import re
import shutil
import stat
import tempfile
import zipfile
from pathlib import Path

REQUIRED = {
    "docker-compose.yml", "provenance.txt", "kitsusync-image.tar",
    "deployment-mode", "kitsusync-deploy", "kitsusync-inspect",
    "kitsusync-sqlite-backup", "kitsusync-bootstrap", "kitsusync-image-identity",
    "kitsusync-runtime-state", "kitsusync-restore-state", "kitsusync-preflight-collect",
    "kitsusync-release-apply", "release_zip.py", "release_registration.py",
}


def extract(archive_path, expected_sha256, destination):
    """Extract a verified archive into a new directory, never replacing an existing stage."""
    destination = Path(destination)
    if not re.fullmatch(r"[0-9a-f]{64}", expected_sha256):
        raise ValueError("invalid expected SHA-256")
    if destination.exists() or destination.is_symlink():
        raise ValueError("release stage already exists")
    created = False
    try:
        # Hash and extract the same private copy, even if the caller can modify
        # the original file while the operation is running.
        with open(archive_path, "rb") as original, tempfile.TemporaryFile(dir=destination.parent) as source:
            digest = hashlib.sha256()
            for block in iter(lambda: original.read(1024 * 1024), b""):
                digest.update(block)
                source.write(block)
            if digest.hexdigest() != expected_sha256:
                raise ValueError("ZIP SHA-256 mismatch")
            source.seek(0)
            with zipfile.ZipFile(source) as archive:
                entries = archive.infolist()
                names = [entry.filename for entry in entries]
                if len(names) != len(set(names)) or set(names) != REQUIRED:
                    raise ValueError("unexpected, missing, or duplicate ZIP entries")
                for entry in entries:
                    mode = entry.external_attr >> 16
                    if entry.is_dir() or (stat.S_IFMT(mode) not in (0, stat.S_IFREG)):
                        raise ValueError("ZIP entry is not a regular file")
                destination.mkdir(mode=0o700)
                created = True
                for entry in entries:
                    target = destination / entry.filename
                    with archive.open(entry) as source_file, open(target, "xb") as output:
                        os.chmod(target, 0o600)
                        shutil.copyfileobj(source_file, output, 1024 * 1024)
    except (OSError, zipfile.BadZipFile, RuntimeError, ValueError) as exc:
        if created:
            shutil.rmtree(destination)
        raise ValueError(f"release ZIP rejected: {exc}") from exc
