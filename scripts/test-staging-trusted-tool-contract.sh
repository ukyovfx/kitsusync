#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
helper="$root/deploy/kitsusync-staging-deploy"
tmp="$(mktemp -d)"
trap 'rm -rf -- "$tmp"' EXIT

image_sha=7698731a4f4b30bca140b700b6716afee6a7dd35ce73f84e3bf11d6676372d0b
sqlite_sha=6554ca0c07e171b27f039db513d8a929c974fbea1ca0df4efb810081ae11b960
[[ "$(sed 's/\r$//' "$root/deploy/kitsusync-image-identity" | sha256sum | cut -d' ' -f1)" == "$image_sha" ]]
[[ "$(sed 's/\r$//' "$root/deploy/kitsusync-sqlite-backup" | sha256sum | cut -d' ' -f1)" == "$sqlite_sha" ]]

sed -n '/^verify_trusted_candidate_tool() {$/,/^}$/p' "$helper" > "$tmp/function.sh"
[[ -s "$tmp/function.sh" ]]
cat > "$tmp/check.sh" <<'SH'
set -euo pipefail
source "$1"
verify_trusted_candidate_tool "$2" "$3"
SH

sed 's/\r$//' "$root/deploy/kitsusync-image-identity" > "$tmp/tool"
bash "$tmp/check.sh" "$tmp/function.sh" "$tmp/tool" "$image_sha"
printf '\n# tampered fixture\n' >> "$tmp/tool"
if bash "$tmp/check.sh" "$tmp/function.sh" "$tmp/tool" "$image_sha" 2>/dev/null; then
  echo 'tampered root-executed candidate tool was accepted' >&2
  exit 1
fi
printf 'staging-trusted-tool-contract=PASS\n'
