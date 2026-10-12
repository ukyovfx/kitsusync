#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output="${1:?output directory is required}"
source_commit="$(git -C "${root}" rev-parse HEAD)"
[[ "${source_commit}" =~ ^[0-9a-f]{40}$ ]] || { printf 'invalid source identity\n' >&2; exit 1; }
[[ -z "${SOURCE_COMMIT:-}" || "${SOURCE_COMMIT}" == "${source_commit}" ]] || {
  printf 'requested source commit does not match checkout\n' >&2; exit 1;
}
baseline_commit="$(git -C "${root}" rev-parse 'v0.5.1^{commit}')"
[[ "${baseline_commit}" == f8a32bbba94d84068d9e96c18554ac4ab7b24540 ]] || {
  printf 'v0.5.1 baseline identity mismatch\n' >&2; exit 1;
}
[[ ! -e "${output}" && ! -L "${output}" ]] || { printf 'output already exists\n' >&2; exit 1; }
mkdir -m 0700 "${output}"
tmp="$(mktemp -d)"
trap 'rm -rf -- "${tmp}"' EXIT

python3 - "${root}" "${output}" "${source_commit}" <<'PY'
import hashlib, importlib.machinery, importlib.util, json, pathlib, subprocess, sys
root, output, source = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2]), sys.argv[3]
base = "f8a32bbba94d84068d9e96c18554ac4ab7b24540"
baseline = ("deploy/kitsusync-deploy", "deploy/kitsusync-runtime-state")
targets = {"kitsusync-deploy": "deploy/kitsusync-deploy",
           "kitsusync-runtime-state": "deploy/kitsusync-runtime-state"}

def blob(path):
    return subprocess.check_output(["git", "-C", str(root), "cat-file", "blob", "v0.5.1:" + path])

def digest(data):
    return hashlib.sha256(data).hexdigest()

old = {pathlib.Path(path).name: digest(blob(path)) for path in baseline}
module_path = root / "deploy" / "kitsusync-deployment-maintenance"
loader = importlib.machinery.SourceFileLoader("maintenance_builder", str(module_path))
spec = importlib.util.spec_from_loader(loader.name, loader)
module = importlib.util.module_from_spec(spec)
loader.exec_module(module)
payloads = {"INSTALL.txt": ("KitsuSync fixed deployment-helper maintenance artifact\n"
    f"Source commit: {source}\nBaseline: v0.5.1 at {base}\n\n"
    "Verify GitHub artifact digest and the archive_sha256 emitted by this builder before staging. "
    "Stage only these files in /root/kitsusync-deployment-maintenance with root ownership, "
    "directory mode 0700, regular payload/manifest mode 0600, and the two fixed entry points mode 0700. "
    "Then review provenance and run exactly one of install-deployment or rollback-deployment as root. "
    "Both commands take no arguments, serialize with deployment, use fixed paths, and do not alter sudoers, "
    "services, listeners, application data, or the v0.5.1 provenance file.\n").encode("ascii"),
    "install-deployment": module._entrypoint_bytes("install"),
    "rollback-deployment": module._entrypoint_bytes("rollback"),
    "deployment_maintenance.py": module_path.read_bytes()}
for name, source_path in targets.items():
    payloads[name] = (root / source_path).read_bytes()
files = {}
for name, data in payloads.items():
    path = output / name
    path.write_bytes(data)
    path.chmod(0o700 if name.startswith(("install-", "rollback-")) else 0o600)
    files[name] = digest(data)
manifest = {"schema_version": 1, "source_commit": source,
            "baseline_release": "v0.5.1", "baseline_source_commit": base,
            "baseline_tools": old, "files": files}
(output / "manifest.json").write_text(json.dumps(manifest, sort_keys=True,
    separators=(",", ":")) + "\n", encoding="ascii")
(output / "manifest.json").chmod(0o600)
PY

tar --sort=name --mtime='@0' --owner=0 --group=0 --numeric-owner --format=posix \
  -cf - -C "${output}" . | gzip -n >"${output}.tar.gz"
archive_sha="$(sha256sum "${output}.tar.gz" | cut -d' ' -f1)"
printf 'source_commit=%s\narchive_sha256=%s\nartifact=%s.tar.gz\n' \
  "${source_commit}" "${archive_sha}" "${output}"
