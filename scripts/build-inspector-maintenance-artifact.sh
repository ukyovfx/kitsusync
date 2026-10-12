#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output="${1:?output directory is required}"
source_commit="$(git -C "${root}" rev-parse HEAD)"
[[ "${source_commit}" =~ ^[0-9a-f]{40}$ ]] || { printf 'invalid source commit\n' >&2; exit 1; }
[[ -z "${SOURCE_COMMIT:-}" || "${SOURCE_COMMIT}" == "${source_commit}" ]] || {
  printf 'requested source commit does not match checkout\n' >&2; exit 1;
}
baseline_commit="$(git -C "${root}" rev-parse 'v0.5.1^{commit}')"
[[ "${baseline_commit}" == f8a32bbba94d84068d9e96c18554ac4ab7b24540 ]] || {
  printf 'v0.5.1 baseline identity mismatch\n' >&2; exit 1;
}
git -C "${root}" cat-file -e 'v0.5.1:deploy/kitsusync-inspect'
if git -C "${root}" cat-file -e 'v0.5.1:deploy/kitsusync-preflight-collect' 2>/dev/null; then
  printf 'v0.5.1 unexpectedly contains the preflight collector\n' >&2; exit 1
fi
[[ ! -e "${output}" && ! -L "${output}" ]] || { printf 'output already exists\n' >&2; exit 1; }
mkdir -m 0700 "${output}"
tmp="$(mktemp -d)"
trap 'rm -rf -- "${tmp}"' EXIT

install -m 0700 "${root}/deploy/kitsusync-inspector-maintenance" "${output}/install-inspection"
install -m 0700 "${root}/deploy/kitsusync-inspector-maintenance" "${output}/rollback-inspection"
install -m 0600 "${root}/deploy/kitsusync-inspect" "${output}/kitsusync-inspect"
install -m 0600 "${root}/deploy/kitsusync-preflight-collect" "${output}/kitsusync-preflight-collect"
git -C "${root}" cat-file blob 'v0.5.1:deploy/kitsusync-inspect' >"${tmp}/prior-inspect"
prior_inspect_sha="$(sha256sum "${tmp}/prior-inspect" | cut -d' ' -f1)"

cat >"${output}/INSTALL.txt" <<EOF
KitsuSync one-time inspector maintenance artifact
Source commit: ${source_commit}
Released predecessor: v0.5.1 at ${baseline_commit}

Before staging, verify the downloaded GitHub Actions artifact digest against
the run metadata, then verify this tar.gz SHA-256 against the artifact_sha256
line printed by the build step. Stage the exact file set under
/root/kitsusync-inspection-maintenance as root:root mode 0700; regular files
mode 0600 and install/rollback entry points mode 0700. Review the source commit
and archive digest before the one-time administrator action. These entry points
accept no arguments and do not alter sudoers, services, or network listeners.
On success, installation records the helper source and exact hashes in the
root-only inspection-maintenance.json state file; rollback restores or removes
that record with the helper pair.
EOF
chmod 0600 "${output}/INSTALL.txt"

python3 - "${output}" "${source_commit}" "${prior_inspect_sha}" <<'PY'
import hashlib, json, pathlib, sys
root = pathlib.Path(sys.argv[1])
source, old_inspect = sys.argv[2:]
names = ("install-inspection", "rollback-inspection", "kitsusync-inspect",
         "kitsusync-preflight-collect", "INSTALL.txt")
files = {name: hashlib.sha256((root / name).read_bytes()).hexdigest() for name in names}
manifest = {"schema_version": 1, "source_commit": source,
            "baseline_release": "v0.5.1",
            "expected_previous_inspect_sha256": old_inspect,
            "expected_previous_collector": "absent", "files": files}
(root / "manifest.json").write_text(json.dumps(manifest, sort_keys=True,
    separators=(",", ":")) + "\n", encoding="ascii")
(root / "manifest.json").chmod(0o600)
PY

tar --sort=name --mtime='@0' --owner=0 --group=0 --numeric-owner --format=posix \
  -cf - -C "${output}" . | gzip -n >"${output}.tar.gz"
sha="$(sha256sum "${output}.tar.gz" | cut -d' ' -f1)"
printf 'source_commit=%s\nartifact_sha256=%s\nartifact=%s\n' \
  "${source_commit}" "${sha}" "${output}.tar.gz"
