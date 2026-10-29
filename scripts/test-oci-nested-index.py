#!/usr/bin/env python3
"""Reproduce Docker 2026 OCI nested-index layout with content-addressed fixture."""
import gzip
import hashlib
import io
import json
import pathlib
import sys
import tarfile
import tempfile

ROOT = pathlib.Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "scripts"))
from importlib.machinery import SourceFileLoader
# Existing test helper filename contains a hyphen.
from importlib.util import spec_from_loader, module_from_spec
path = ROOT / "scripts" / "test-image-identity.py"
loader = SourceFileLoader("image_fixture", str(path))
spec = spec_from_loader("image_fixture", loader)
fixture = module_from_spec(spec)
loader.exec_module(fixture)

def compact(doc):
    return json.dumps(doc, separators=(",", ":"), sort_keys=True).encode()

def hashof(raw):
    return hashlib.sha256(raw).hexdigest()

with tempfile.TemporaryDirectory() as tmpname:
    tmp = pathlib.Path(tmpname)
    base = tmp / "base.tar"
    fixture.make_archive(base)
    with tarfile.open(base) as tf:
        raw = {m.name: tf.extractfile(m).read() for m in tf.getmembers() if m.isfile()}
    root_index = json.loads(raw["index.json"])
    tagged = root_index["manifests"][0]
    child = {
        "schemaVersion": 2,
        "mediaType": "application/vnd.oci.image.index.v1+json",
        "manifests": [{k: v for k, v in tagged.items() if k != "annotations"}],
    }
    child_raw = compact(child)
    child_digest = hashof(child_raw)
    raw["blobs/sha256/" + child_digest] = child_raw
    root_index["manifests"] = [{
        "mediaType": "application/vnd.oci.image.index.v1+json",
        "digest": "sha256:" + child_digest,
        "size": len(child_raw),
        "annotations": {"org.opencontainers.image.ref.name": "v0.4.6"},
    }]
    raw["index.json"] = compact(root_index)
    nested = tmp / "nested.tar"
    with tarfile.open(nested, "w") as tf:
        for name, blob in raw.items():
            info = tarfile.TarInfo(name)
            info.size = len(blob)
            info.mode = 0o600
            tf.addfile(info, io.BytesIO(blob))
    result = fixture.run("archive", str(nested), fixture.IMAGE_REF, check=False)
    assert result.returncode == 0, "nested OCI index rejected: " + result.stderr.strip()
    nested_fields = fixture.fields(result.stdout)
    assert nested_fields["image_index_digest"] == "sha256:" + child_digest
    assert fixture.run("verify-loaded-id", "sha256:" + child_digest,
                       nested_fields["image_config_digest"], nested_fields["image_manifest_digest"],
                       nested_fields["image_index_digest"]).returncode == 0
    for unknown_id in ("sha256:" + "0" * 64, "sha256:" + "f" * 64):
        rejected_id = fixture.run("verify-loaded-id", unknown_id,
                                  nested_fields["image_config_digest"], nested_fields["image_manifest_digest"],
                                  nested_fields["image_index_digest"], check=False)
        assert rejected_id.returncode != 0 and "approved archive content identity" in rejected_id.stderr
    direct = fixture.run("archive", str(base), fixture.IMAGE_REF)
    assert fixture.fields(result.stdout)["image_content_digest"] == fixture.fields(direct.stdout)["image_content_digest"]

    # Docker may include an attestation manifest alongside the image.
    attested = dict(raw)
    attestation_bytes = compact({
        "schemaVersion": 2,
        "mediaType": "application/vnd.oci.image.manifest.v1+json",
        "config": {},
        "layers": [],
    })
    attestation_hash = hashof(attestation_bytes)
    attested["blobs/sha256/" + attestation_hash] = attestation_bytes
    attested_child = json.loads(child_raw)
    attested_child["manifests"].append({
        "mediaType": "application/vnd.oci.image.manifest.v1+json",
        "digest": "sha256:" + attestation_hash,
        "size": len(attestation_bytes),
        "annotations": {"vnd.docker.reference.type": "attestation-manifest"},
    })
    attested_child_raw = compact(attested_child)
    attested_child_hash = hashof(attested_child_raw)
    attested["blobs/sha256/" + attested_child_hash] = attested_child_raw
    attested_root = json.loads(attested["index.json"])
    attested_root["manifests"][0]["digest"] = "sha256:" + attested_child_hash
    attested_root["manifests"][0]["size"] = len(attested_child_raw)
    attested["index.json"] = compact(attested_root)
    attested_path = tmp / "attested.tar"
    with tarfile.open(attested_path, "w") as tf:
        for name, blob in attested.items():
            info = tarfile.TarInfo(name)
            info.size = len(blob)
            tf.addfile(info, io.BytesIO(blob))
    attested_result = fixture.run("archive", str(attested_path), fixture.IMAGE_REF, check=False)
    assert attested_result.returncode == 0, attested_result.stderr
    assert fixture.fields(attested_result.stdout)["image_content_digest"] == fixture.fields(direct.stdout)["image_content_digest"]

    # The nested-index digest remains content-addressed: a changed parent
    # pointer must fail closed rather than silently selecting another blob.
    damaged = dict(raw)
    bad_index = json.loads(damaged["index.json"])
    bad_index["manifests"][0]["digest"] = "sha256:" + "0" * 64
    damaged["index.json"] = compact(bad_index)
    bad_path = tmp / "damaged-parent.tar"
    with tarfile.open(bad_path, "w") as tf:
        for name, blob in damaged.items():
            info = tarfile.TarInfo(name)
            info.size = len(blob)
            tf.addfile(info, io.BytesIO(blob))
    bad = fixture.run("archive", str(bad_path), fixture.IMAGE_REF, check=False)
    assert bad.returncode != 0 and "archive member is missing" in bad.stderr.lower(), bad.stderr

    # Multiple unmarked deployable manifest candidates must never be guessed.
    ambiguous = dict(raw)
    child["manifests"].append(dict(child["manifests"][0]))
    altered_child = compact(child)
    new_hash = hashof(altered_child)
    ambiguous["blobs/sha256/" + new_hash] = altered_child
    ambiguous_index = json.loads(ambiguous["index.json"])
    ambiguous_index["manifests"][0]["digest"] = "sha256:" + new_hash
    ambiguous_index["manifests"][0]["size"] = len(altered_child)
    ambiguous["index.json"] = compact(ambiguous_index)
    ambiguous_path = tmp / "ambiguous.tar"
    with tarfile.open(ambiguous_path, "w") as tf:
        for name, blob in ambiguous.items():
            info = tarfile.TarInfo(name)
            info.size = len(blob)
            tf.addfile(info, io.BytesIO(blob))
    rejected = fixture.run("archive", str(ambiguous_path), fixture.IMAGE_REF, check=False)
    assert rejected.returncode != 0 and "exactly one deployable image" in rejected.stderr
    # Docker 29 stores content-addressed compressed layers while rootfs
    # diff_ids identify the decompressed bytes.
    compressed = dict(raw)
    saved_manifest = json.loads(compressed["manifest.json"])
    original_layer_name = saved_manifest[0]["Layers"][0]
    original_layer = compressed.pop(original_layer_name)
    compressed_layer = gzip.compress(original_layer, mtime=0)
    compressed_layer_hash = hashof(compressed_layer)
    compressed_layer_name = "blobs/sha256/" + compressed_layer_hash
    compressed[compressed_layer_name] = compressed_layer
    saved_manifest[0]["Layers"][0] = compressed_layer_name
    compressed["manifest.json"] = compact(saved_manifest)
    image_descriptor = json.loads(child_raw)["manifests"][0]
    oci_manifest_name = "blobs/sha256/" + image_descriptor["digest"].split(":")[1]
    compressed_manifest = json.loads(compressed[oci_manifest_name])
    compressed_manifest["layers"][0] = {
        "mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
        "digest": "sha256:" + compressed_layer_hash,
        "size": len(compressed_layer),
    }
    compressed_manifest_bytes = compact(compressed_manifest)
    compressed_manifest_hash = hashof(compressed_manifest_bytes)
    compressed["blobs/sha256/" + compressed_manifest_hash] = compressed_manifest_bytes
    compressed_child = json.loads(child_raw)
    compressed_child["manifests"][0]["digest"] = "sha256:" + compressed_manifest_hash
    compressed_child["manifests"][0]["size"] = len(compressed_manifest_bytes)
    compressed_child_bytes = compact(compressed_child)
    compressed_child_hash = hashof(compressed_child_bytes)
    compressed["blobs/sha256/" + compressed_child_hash] = compressed_child_bytes
    compressed_root = json.loads(compressed["index.json"])
    compressed_root["manifests"][0]["digest"] = "sha256:" + compressed_child_hash
    compressed_root["manifests"][0]["size"] = len(compressed_child_bytes)
    compressed["index.json"] = compact(compressed_root)
    compressed_path = tmp / "nested-gzip.tar"
    with tarfile.open(compressed_path, "w") as tf:
        for name, blob in compressed.items():
            info = tarfile.TarInfo(name)
            info.size = len(blob)
            tf.addfile(info, io.BytesIO(blob))
    gzip_result = fixture.run("archive", str(compressed_path), fixture.IMAGE_REF, check=False)
    assert gzip_result.returncode == 0, gzip_result.stderr
    assert fixture.fields(gzip_result.stdout)["image_content_digest"] == fixture.fields(direct.stdout)["image_content_digest"]

    print("nested-oci-index-tests=PASS")
