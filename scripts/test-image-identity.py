#!/usr/bin/env python3
"""Regression tests for portable Docker archive image identity."""

from __future__ import annotations

import hashlib
import io
import json
import pathlib
import subprocess
import sys
import tarfile
import tempfile


ROOT = pathlib.Path(__file__).resolve().parents[1]
HELPER = ROOT / "deploy" / "kitsusync-image-identity"
IMAGE_REF = "kitsusync:v0.4.6"
LABELS = {
    "org.opencontainers.image.revision": "b7b30157cb90c4500e8b00d3c26ac7038f5c8c10",
    "org.opencontainers.image.source-id": "b7b30157cb90c4500e8b00d3c26ac7038f5c8c10",
    "org.opencontainers.image.version": "0.4.6",
}


def sha(value: bytes) -> str:
    return hashlib.sha256(value).hexdigest()


def add_bytes(archive: tarfile.TarFile, name: str, value: bytes) -> None:
    info = tarfile.TarInfo(name)
    info.size = len(value)
    info.mode = 0o600
    archive.addfile(info, io.BytesIO(value))


def make_archive(path: pathlib.Path, *, layer: bytes = b"approved-rootfs", command: list[str] | None = None,
                 stored_layer: bytes | None = None) -> tuple[dict[str, object], str, str]:
    layer_digest = sha(layer)
    config = {
        "architecture": "amd64",
        "os": "linux",
        "rootfs": {"type": "layers", "diff_ids": [f"sha256:{layer_digest}"]},
        "config": {
            "User": "10001:10001",
            "Env": ["PATH=/usr/bin:/bin"],
            "Cmd": command or ["./kitsu-discord"],
            "WorkingDir": "/app",
            "Labels": LABELS,
            "ArgsEscaped": True,
        },
    }
    config_bytes = json.dumps(config, separators=(",", ":"), sort_keys=True).encode()
    config_digest = sha(config_bytes)
    oci_manifest = {
        "schemaVersion": 2,
        "mediaType": "application/vnd.oci.image.manifest.v1+json",
        "config": {
            "mediaType": "application/vnd.oci.image.config.v1+json",
            "digest": f"sha256:{config_digest}",
            "size": len(config_bytes),
        },
        "layers": [{
            "mediaType": "application/vnd.oci.image.layer.v1.tar",
            "digest": f"sha256:{layer_digest}",
            "size": len(layer if stored_layer is None else stored_layer),
        }],
    }
    oci_manifest_bytes = json.dumps(oci_manifest, separators=(",", ":")).encode()
    oci_manifest_digest = sha(oci_manifest_bytes)
    oci_index = {
        "schemaVersion": 2,
        "mediaType": "application/vnd.oci.image.index.v1+json",
        "manifests": [{
            "mediaType": "application/vnd.oci.image.manifest.v1+json",
            "digest": f"sha256:{oci_manifest_digest}",
            "size": len(oci_manifest_bytes),
            "annotations": {"org.opencontainers.image.ref.name": "v0.4.6"},
        }],
    }
    manifest = [{
        "Config": f"blobs/sha256/{config_digest}",
        "RepoTags": [IMAGE_REF],
        "Layers": [f"blobs/sha256/{layer_digest}"],
    }]
    with tarfile.open(path, "w") as archive:
        add_bytes(archive, "manifest.json", json.dumps(manifest, separators=(",", ":")).encode())
        add_bytes(archive, "index.json", json.dumps(oci_index, separators=(",", ":")).encode())
        add_bytes(archive, f"blobs/sha256/{oci_manifest_digest}", oci_manifest_bytes)
        add_bytes(archive, f"blobs/sha256/{config_digest}", config_bytes)
        add_bytes(archive, f"blobs/sha256/{layer_digest}", layer if stored_layer is None else stored_layer)
    inspect = {
        "Id": "sha256:" + "f" * 64,
        "Architecture": "amd64",
        "Os": "linux",
        "RootFS": {"Type": "layers", "Layers": [f"sha256:{layer_digest}"]},
        "Config": config["config"],
    }
    return inspect, f"sha256:{config_digest}", f"sha256:{oci_manifest_digest}"


def run(*args: str, stdin: dict[str, object] | None = None, check: bool = True) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        [sys.executable, str(HELPER), *args],
        input=None if stdin is None else json.dumps(stdin),
        text=True,
        capture_output=True,
        check=check,
    )


def fields(output: str) -> dict[str, str]:
    return dict(line.split("=", 1) for line in output.splitlines() if "=" in line)


def main() -> None:
    with tempfile.TemporaryDirectory() as directory:
        tmp = pathlib.Path(directory)
        approved = tmp / "approved.tar"
        inspect, config_digest, manifest_digest = make_archive(approved)
        archive_fields = fields(run("archive", str(approved), IMAGE_REF).stdout)
        assert archive_fields["image_config_digest"] == config_digest
        assert archive_fields["image_manifest_digest"] == manifest_digest

        # Docker daemon-local .Id is deliberately excluded. The portable
        # identity remains equal when the exact approved Linux content has
        # another Id and the daemon omits the legacy Windows ArgsEscaped key.
        inspect["Id"] = manifest_digest
        inspect["Config"].pop("ArgsEscaped")
        inspect_fields = fields(run("inspect", stdin=inspect).stdout)
        assert inspect_fields["image_content_digest"] == archive_fields["image_content_digest"]

        # Labels copied onto different filesystem content cannot impersonate
        # the approved archive.
        copied_label_archive = tmp / "copied-labels.tar"
        copied_inspect, copied_config_digest, copied_manifest_digest = make_archive(copied_label_archive, layer=b"attacker-rootfs")
        assert copied_inspect["Id"] not in {config_digest, manifest_digest}
        assert copied_config_digest not in {config_digest, manifest_digest}
        assert copied_manifest_digest not in {config_digest, manifest_digest}
        copied_fields = fields(run("inspect", stdin=copied_inspect).stdout)
        assert copied_fields["image_content_digest"] != archive_fields["image_content_digest"]

        # A runtime config change is identity-significant.
        wrong_config_archive = tmp / "wrong-config.tar"
        wrong_config_inspect, _, _ = make_archive(wrong_config_archive, command=["/bin/sh"])
        wrong_config_fields = fields(run("inspect", stdin=wrong_config_inspect).stdout)
        assert wrong_config_fields["image_content_digest"] != archive_fields["image_content_digest"]

        # A layer whose bytes do not match its content-addressed descriptor is
        # rejected before Docker load.
        corrupt_archive = tmp / "corrupt-layer.tar"
        make_archive(corrupt_archive, stored_layer=b"different-bytes")
        corrupt = run("archive", str(corrupt_archive), IMAGE_REF, check=False)
        assert corrupt.returncode != 0
        assert "layer content digest" in corrupt.stderr

        wrong_ref = run("archive", str(approved), "kitsusync:v9.9.9", check=False)
        assert wrong_ref.returncode != 0
        assert "repository/tag mapping" in wrong_ref.stderr

    print("image-identity-tests=PASS")


if __name__ == "__main__":
    main()
