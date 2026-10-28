#!/usr/bin/env python3
"""Emit only safe descriptor *shape* from a Docker-save OCI index.

No image config, labels, environment, paths, digests, or layer bytes are logged.
This is diagnostic only; it does not change accepted identity verification.
"""

import json
import re
import sys
import tarfile

SAFE_MEDIA = {
    "application/vnd.oci.image.manifest.v1+json",
    "application/vnd.docker.distribution.manifest.v2+json",
    "application/vnd.oci.image.index.v1+json",
    "application/vnd.docker.distribution.manifest.list.v2+json",
}
DIGEST = re.compile(r"sha256:[0-9a-f]{64}\Z")


def main(path: str) -> None:
    with tarfile.open(path, "r:*") as archive:
        member = archive.getmember("index.json")
        if not member.isfile() or member.size > 1024 * 1024:
            raise ValueError("index metadata is missing or oversized")
        stream = archive.extractfile(member)
        if stream is None:
            raise ValueError("index metadata is unreadable")
        index = json.load(stream)
        descriptors = index.get("manifests")
        if not isinstance(descriptors, list):
            raise ValueError("index descriptors are invalid")
        print(f"oci_index_descriptor_count={len(descriptors)}")
        for position, descriptor in enumerate(descriptors):
            if not isinstance(descriptor, dict):
                raise ValueError("non-object OCI descriptor")
            media_type = descriptor.get("mediaType")
            media_class = media_type if media_type in SAFE_MEDIA else "missing-or-unrecognized"
            digest = descriptor.get("digest")
            digest_valid = isinstance(digest, str) and DIGEST.fullmatch(digest) is not None
            print(f"oci_descriptor_{position}_media_type={media_class}")
            print(f"oci_descriptor_{position}_sha256_syntax_valid={str(digest_valid).lower()}")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit("usage: diagnose-oci-index.py <image-tar>")
    main(sys.argv[1])
