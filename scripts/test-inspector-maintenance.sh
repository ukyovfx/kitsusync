#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf -- "${tmp}"' EXIT
bash "${root}/scripts/build-inspector-maintenance-artifact.sh" "${tmp}/artifact"
archive="${tmp}/artifact.tar.gz"
[[ -f "${archive}" && ! -L "${archive}" ]]
python3 - "${archive}" "${root}" <<'PY'
import hashlib, io, json, pathlib, subprocess, sys, tarfile
archive, root = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2])
with tarfile.open(archive, "r:gz") as bundle:
    members = bundle.getmembers()
    expected = {"INSTALL.txt", "install-inspection", "rollback-inspection",
                "kitsusync-inspect", "kitsusync-preflight-collect", "manifest.json"}
    normalized = [item.name[2:] if item.name.startswith("./") else item.name for item in members]
    files = [item for item in members if item.isfile()]
    directories = [item for item in members if item.isdir()]
    if (set(normalized_item for normalized_item, item in zip(normalized, members) if item.isfile()) != expected
            or len(files) != len(expected) or len(members) != len(expected) + 1
            or any(name not in expected and name not in {"", "."} for name in normalized)
            or len(directories) != 1 or normalized[members.index(directories[0])] not in {"", "."}):
        raise SystemExit("maintenance artifact inventory mismatch")
    by_name = {name: item for name, item in zip(normalized, members) if item.isfile()}
    manifest = json.load(bundle.extractfile(by_name["manifest.json"]))
    if (manifest.get("baseline_release") != "v0.5.1"
            or set(manifest.get("files", {})) != expected - {"manifest.json"}
            or len(manifest.get("source_commit", "")) != 40):
        raise SystemExit("maintenance artifact baseline mismatch")
    modes = {"install-inspection": 0o700, "rollback-inspection": 0o700,
             "INSTALL.txt": 0o600, "kitsusync-inspect": 0o600,
             "kitsusync-preflight-collect": 0o600, "manifest.json": 0o600}
    for name, expected_sha in manifest.get("files", {}).items():
        payload = bundle.extractfile(by_name[name]).read()
        if hashlib.sha256(payload).hexdigest() != expected_sha:
            raise SystemExit("maintenance artifact file digest mismatch")
    for name, expected_mode in modes.items():
        if by_name[name].mode & 0o777 != expected_mode:
            raise SystemExit("maintenance artifact mode mismatch")
    prior = subprocess.check_output(
        ["git", "-C", str(root), "cat-file", "blob", "v0.5.1:deploy/kitsusync-inspect"])
    if hashlib.sha256(prior).hexdigest() != manifest.get("expected_previous_inspect_sha256"):
        raise SystemExit("maintenance artifact predecessor digest mismatch")
    commit = subprocess.check_output(["git", "-C", str(root), "rev-parse", "HEAD"], text=True).strip()
    if manifest.get("source_commit") != commit:
        raise SystemExit("maintenance artifact source identity mismatch")
print("inspection-maintenance-artifact=PASS")
PY
