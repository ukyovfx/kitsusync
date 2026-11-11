#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
export KITSUSYNC_DEPLOY_TEST_MODE=lock-contract
export KITSUSYNC_DEPLOY_TEST_DOCKER_BIN=/usr/bin/true
wrapper="$root/deploy/kitsusync-deploy"

# Ordinary invocation acquires and releases the lock.
bash "$wrapper" "$tmp/lock"

# A coordinator transfers the same open file description to deploy, keeping
# the lock held; another invocation without that descriptor must fail.
exec 9>"$tmp/lock"
/usr/bin/flock -n 9
bash "$wrapper" "$tmp/lock"
if bash -c 'exec 9>&-; bash "$1" "$2"' _ "$wrapper" "$tmp/lock"; then
  printf 'competing deployment unexpectedly acquired the lock\n' >&2
  exit 1
fi
exec 9>&-
bash "$wrapper" "$tmp/lock"
printf 'release-deploy-lock=PASS\n'
