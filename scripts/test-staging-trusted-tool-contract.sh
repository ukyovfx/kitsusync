#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
helper="$root/deploy/kitsusync-staging-deploy"
tmp="$(mktemp -d)"
trap 'rm -rf -- "$tmp"' EXIT

image_sha=8ccc4279655004942b6c2c19c532d3ee25d8e47ea7fca41843da3310a63ae5e7
sqlite_sha=18803d1f00883be6c2ee4d9c91c801679b89e31d47349253ee1654ec01df4844
[[ "$(sha256sum "$root/deploy/kitsusync-image-identity" | cut -d' ' -f1)" == "$image_sha" ]]
[[ "$(sha256sum "$root/deploy/kitsusync-sqlite-backup" | cut -d' ' -f1)" == "$sqlite_sha" ]]

sed -n '/^verify_trusted_candidate_tool() {$/,/^}$/p' "$helper" > "$tmp/function.sh"
[[ -s "$tmp/function.sh" ]]
cat > "$tmp/check.sh" <<'SH'
set -euo pipefail
source "$1"
verify_trusted_candidate_tool "$2" "$3"
SH

cp "$root/deploy/kitsusync-image-identity" "$tmp/tool"
bash "$tmp/check.sh" "$tmp/function.sh" "$tmp/tool" "$image_sha"
printf '\n# tampered fixture\n' >> "$tmp/tool"
if bash "$tmp/check.sh" "$tmp/function.sh" "$tmp/tool" "$image_sha" 2>/dev/null; then
  echo 'tampered root-executed candidate tool was accepted' >&2
  exit 1
fi
printf 'staging-trusted-tool-contract=PASS\n'
