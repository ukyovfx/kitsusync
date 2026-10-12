#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf -- "${tmp}"' EXIT
bash "${root}/scripts/build-deployment-maintenance-artifact.sh" "${tmp}/artifact"
archive="${tmp}/artifact.tar.gz"
python3 - "${archive}" "${root}" <<'PY'
import hashlib, json, pathlib, subprocess, sys, tarfile
archive, root = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2])
expected = {"INSTALL.txt", "install-deployment", "rollback-deployment", "deployment_maintenance.py",
            "kitsusync-deploy", "kitsusync-runtime-state", "manifest.json"}
with tarfile.open(archive, "r:gz") as bundle:
    members = bundle.getmembers()
    names = [m.name[2:] if m.name.startswith("./") else m.name for m in members]
    files = [m for m in members if m.isfile()]
    dirs = [m for m in members if m.isdir()]
    if (len(files) != len(expected) or set(names) != expected | {""}
            or len(dirs) != 1 or names[members.index(dirs[0])] not in {"", "."}):
        raise SystemExit("deployment maintenance artifact inventory mismatch")
    by_name = {name: member for name, member in zip(names, members) if member.isfile()}
    manifest = json.load(bundle.extractfile(by_name["manifest.json"]))
    if (manifest.get("baseline_release") != "v0.5.1"
            or manifest.get("baseline_source_commit") != "f8a32bbba94d84068d9e96c18554ac4ab7b24540"
            or set(manifest.get("files", {})) != expected - {"manifest.json"}
            or set(manifest.get("baseline_tools", {})) != {"kitsusync-deploy", "kitsusync-runtime-state"}):
        raise SystemExit("deployment maintenance artifact baseline mismatch")
    for name, digest in manifest["files"].items():
        if hashlib.sha256(bundle.extractfile(by_name[name]).read()).hexdigest() != digest:
            raise SystemExit("deployment maintenance artifact digest mismatch")
    expected_modes = {name: 0o700 if name.startswith(("install-", "rollback-")) else 0o600
                      for name in expected - {"manifest.json"}}
    expected_modes["manifest.json"] = 0o600
    for name, mode in expected_modes.items():
        if by_name[name].mode & 0o777 != mode:
            raise SystemExit("deployment maintenance artifact mode mismatch")
    for name, path in (("kitsusync-deploy", "deploy/kitsusync-deploy"),
                       ("kitsusync-runtime-state", "deploy/kitsusync-runtime-state")):
        prior = subprocess.check_output(["git", "-C", str(root), "cat-file", "blob", "v0.5.1:" + path])
        if hashlib.sha256(prior).hexdigest() != manifest["baseline_tools"][name]:
            raise SystemExit("deployment maintenance predecessor digest mismatch")
    head = subprocess.check_output(["git", "-C", str(root), "rev-parse", "HEAD"], text=True).strip()
    if manifest.get("source_commit") != head:
        raise SystemExit("deployment maintenance source identity mismatch")
print("deployment-maintenance-artifact=PASS")
PY
