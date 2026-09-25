#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
release="$root/deploy/kitsusync-deploy"
preview="$root/deploy/kitsusync-preview-deploy"
core="$root/deploy/kitsusync-deploy-transaction"

for file in "$release" "$preview" "$core"; do
  [[ -f "$file" ]] || { printf 'missing preview deployment component: %s\n' "$file" >&2; exit 1; }
  bash -n "$file"
done

grep -Fq 'KITSUSYNC_DEPLOY_POLICY=release' "$release"
grep -Fq 'KITSUSYNC_DEPLOY_POLICY=preview' "$preview"
grep -Fq 'PREVIEW' "$preview"
grep -Fq 'preview_identity_allows "${expected}"' "$preview"
grep -Fq '"${kind}" == candidate' "$preview"
grep -Fq -- '-z "${release_commit}"' "$preview"
grep -Fq -- '-z "${release_tag}"' "$preview"
grep -Fq 'kitsusync:ci-${expected}' "$preview"
grep -Fq 'preview:ready|preview:setup_required' "$core"
grep -Fq '127.0.0.1:8090' "$core"
grep -Fq 'PREVIEW / NON-RELEASE' "$core"
grep -Fq -- '--preview-system-status-dom' "$core"
grep -Fq 'preview System Status DOM contract mismatch: missing telemetry-line' "$core"
grep -Fq 'preview System Status DOM contract mismatch: obsolete marker present' "$core"
grep -Fq 'preview System Status DOM contract passed' "$core"

if grep -Eq '^[[:space:]]*(export[[:space:]]+)?KITSUSYNC_DEPLOY_POLICY=' "$core"; then
  printf 'shared transaction core must not select policy itself\n' >&2
  exit 1
fi

sha=0123456789abcdef0123456789abcdef01234567
image_id=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
run_identity() {
  local mode="$1" expected="$2" kind="$3" source="$4" source_id="$5" release_commit="$6" release_tag="$7" image="$8" id="$9" result="${10}"
  local selected_wrapper="$preview"
  [[ "$mode" != release-identity ]] || selected_wrapper="$release"
  if KITSUSYNC_DEPLOY_TEST_MODE="$mode" bash "$selected_wrapper" \
      "$expected" "$kind" "$source" "$source_id" "$release_commit" "$release_tag" "$image" "$id"; then
    [[ "$result" == pass ]] || { printf 'unexpectedly accepted %s identity\n' "$mode" >&2; exit 1; }
  else
    [[ "$result" == fail ]] || { printf 'unexpectedly rejected %s identity\n' "$mode" >&2; exit 1; }
  fi
}
run_identity preview-identity "$sha" candidate "$sha" "$sha" '' '' "kitsusync:ci-$sha" "$image_id" pass
run_identity preview-identity "$sha" candidate "${sha%?}0" "$sha" '' '' "kitsusync:ci-$sha" "$image_id" fail
run_identity preview-identity "$sha" candidate "$sha" "$sha" '' v0.4.9 "kitsusync:ci-$sha" "$image_id" fail
run_identity preview-identity "$sha" candidate "$sha" "$sha" '' '' kitsusync:v0.4.9 "$image_id" fail
run_identity preview-identity "$sha" release "$sha" "$sha" "$sha" v0.4.9 kitsusync:v0.4.9 "$image_id" fail
run_identity release-identity 0.4.9 release "$sha" "$sha" "$sha" v0.4.9 kitsusync:v0.4.9 "$image_id" pass
run_identity release-identity 0.4.9 candidate "$sha" "$sha" '' '' "kitsusync:ci-$sha" "$image_id" fail
run_readiness() {
  local state="$1" result="$2"
  if KITSUSYNC_DEPLOY_TEST_DOCKER_BIN=/usr/bin/true KITSUSYNC_DEPLOY_TEST_MODE=readiness bash "$core" preview "$state"; then
    [[ "$result" == pass ]] || { printf 'preview readiness unexpectedly accepted %s\n' "$state" >&2; exit 1; }
  else
    [[ "$result" == fail ]] || { printf 'preview readiness unexpectedly rejected %s\n' "$state" >&2; exit 1; }
  fi
}
run_readiness ready pass
run_readiness setup_required pass
run_readiness degraded fail

# Docker may report an OCI archive's manifest digest as the loaded image ID
# instead of the config digest. Keep the preview's preflight aligned with the
# shared transaction rule, which accepts either archive-bound identity.
run_image_id() {
  local actual="$1" config="$2" manifest="$3" result="$4"
  if KITSUSYNC_DEPLOY_TEST_DOCKER_BIN=/usr/bin/true KITSUSYNC_DEPLOY_TEST_MODE=image-id bash "$core" "$actual" "$config" "$manifest"; then
    [[ "$result" == pass ]] || { printf 'image ID unexpectedly accepted: %s\n' "$actual" >&2; exit 1; }
  else
    [[ "$result" == fail ]] || { printf 'image ID unexpectedly rejected: %s\n' "$actual" >&2; exit 1; }
  fi
}
config_digest=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
manifest_digest=sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
run_image_id "$config_digest" "$config_digest" "$manifest_digest" pass
run_image_id "$manifest_digest" "$config_digest" "$manifest_digest" pass
run_image_id sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc "$config_digest" "$manifest_digest" fail

if bash "$preview" "$sha" wrong >/dev/null 2>&1; then
  printf 'preview command accepted a missing PREVIEW confirmation marker\n' >&2
  exit 1
fi

printf 'preview-deploy-contract=PASS\n'
