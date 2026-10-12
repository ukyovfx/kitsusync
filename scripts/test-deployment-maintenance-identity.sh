#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'sudo rm -rf -- "$tmp"' EXIT
chmod 0755 "$tmp"
record="$tmp/deployment-maintenance.json"
provenance="$tmp/provenance"
old="$(printf 'v051-wrapper' | sha256sum | cut -d' ' -f1)"
target="$(sha256sum "$root/deploy/kitsusync-deploy" | cut -d' ' -f1)"
runtime_old="$(printf 'v051-runtime-state' | sha256sum | cut -d' ' -f1)"
runtime_target="$(sha256sum "$root/deploy/kitsusync-runtime-state" | cut -d' ' -f1)"
base=f8a32bbba94d84068d9e96c18554ac4ab7b24540
printf 'release_tag=v0.5.1\nversion=0.5.1\nsource_commit=%s\nrelease_commit=%s\ndeployment_tool_sha256=%s\nruntime_state_tool_sha256=%s\n' \
  "$base" "$base" "$old" "$runtime_old" >"$provenance"
python3 - "$record" "$provenance" "$base" "$old" "$target" "$runtime_old" "$runtime_target" <<'PY'
import hashlib, json, pathlib, sys
record_path, provenance_path = map(pathlib.Path, sys.argv[1:3])
base, old, target, runtime_old, runtime_target = sys.argv[3:]
record = {
    "schema_version": 1,
    "baseline_release": "v0.5.1",
    "baseline_source_commit": base,
    "source_commit": "a" * 40,
    "previous_provenance_sha256": hashlib.sha256(provenance_path.read_bytes()).hexdigest(),
    "artifact_manifest_sha256": "b" * 64,
    "baseline_deployment_tool_sha256": old,
    "baseline_runtime_state_tool_sha256": runtime_old,
    "target_deployment_tool_sha256": target,
    "target_runtime_state_tool_sha256": runtime_target,
}
record_path.write_text(json.dumps(record, sort_keys=True, separators=(",", ":")) + "\n")
PY
sudo chown root:root "$record" "$provenance"
sudo chmod 0600 "$record" "$provenance"

run_check() {
  KITSUSYNC_DEPLOY_TEST_MODE=maintenance-identity \
  KITSUSYNC_DEPLOY_TEST_MAINTENANCE_IDENTITY="$record" \
  KITSUSYNC_DEPLOY_TEST_PROVENANCE="$provenance" \
    bash "$root/deploy/kitsusync-deploy" "$@"
}

run_check deployment_tool_sha256 "$old" "$target"
run_check runtime_state_tool_sha256 "$runtime_old" "$runtime_target"
run_check deployment_tool_sha256 "$target" "$target"

sudo python3 - "$record" <<'PY'
import json, pathlib, sys
path = pathlib.Path(sys.argv[1])
record = json.loads(path.read_text())
record["previous_provenance_sha256"] = "c" * 64
path.write_text(json.dumps(record, sort_keys=True, separators=(",", ":")) + "\n")
PY
sudo chmod 0600 "$record"
if run_check deployment_tool_sha256 "$old" "$target" >/dev/null 2>&1; then
  printf 'tampered prior-provenance identity was accepted\n' >&2
  exit 1
fi

sudo python3 - "$record" <<'PY'
import hashlib, json, pathlib, sys
path = pathlib.Path(sys.argv[1])
record = json.loads(path.read_text())
record["previous_provenance_sha256"] = hashlib.sha256(pathlib.Path(path.parent / "provenance").read_bytes()).hexdigest()
path.write_text(json.dumps(record, sort_keys=True, separators=(",", ":")) + "\n")
PY
sudo chmod 0600 "$record"
mv "$record" "$record.real"
ln -s "$record.real" "$record"
if run_check deployment_tool_sha256 "$old" "$target" >/dev/null 2>&1; then
  printf 'symlinked maintenance identity was accepted\n' >&2
  exit 1
fi

printf 'deployment-maintenance-identity=PASS\n'
