#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
resolver="${root}/scripts/resolve-release-identity.sh"
generator="${root}/scripts/generate-provenance.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

a=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
b=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
image=sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc

GITHUB_EVENT_NAME=pull_request GITHUB_REF=refs/pull/159/merge GITHUB_SHA="${a}" PR_HEAD_SHA="${b}" GITHUB_OUTPUT="${tmp}/pr" bash "${resolver}"
grep -Fxq 'artifact_kind=candidate' "${tmp}/pr"
grep -Fxq "merge_test_commit=${a}" "${tmp}/pr"
grep -Fxq "source_commit=${b}" "${tmp}/pr"
grep -Fxq 'version=0.4.6' "${tmp}/pr"

GITHUB_EVENT_NAME=push GITHUB_REF=refs/heads/master GITHUB_SHA="${a}" GITHUB_OUTPUT="${tmp}/master" bash "${resolver}"
grep -Fxq 'artifact_kind=nonrelease' "${tmp}/master"
grep -Fxq 'release_commit=' "${tmp}/master"
! grep -Fq 'version=master' "${tmp}/master"

GITHUB_EVENT_NAME=push GITHUB_REF=refs/tags/v0.4.6 GITHUB_SHA="${a}" GITHUB_OUTPUT="${tmp}/release" bash "${resolver}"
grep -Fxq 'artifact_kind=release' "${tmp}/release"
grep -Fxq "release_commit=${a}" "${tmp}/release"

if GITHUB_EVENT_NAME=pull_request GITHUB_REF=refs/pull/159/merge GITHUB_SHA="${a}" PR_HEAD_SHA="${b}" EXPECTED_SOURCE_COMMIT="${a}" GITHUB_OUTPUT="${tmp}/bad" bash "${resolver}" >/dev/null 2>&1; then
  printf 'expected source mismatch was accepted\n' >&2
  exit 1
fi

ARTIFACT_KIND=candidate SOURCE_COMMIT="${b}" SOURCE_ID="${b}" MERGE_TEST_COMMIT="${a}" RELEASE_VERSION=0.4.6 IMAGE_ID="${image}" PROVENANCE_OUTPUT="${tmp}/manifest" bash "${generator}"
grep -Fxq "source_commit=${b}" "${tmp}/manifest"
grep -Fxq "build_daemon_image_id=${image}" "${tmp}/manifest"

digest=dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
portable=sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
ARTIFACT_KIND=release SOURCE_COMMIT="${a}" SOURCE_ID="${a}" RELEASE_COMMIT="${a}" RELEASE_VERSION=0.4.6 RELEASE_TAG=v0.4.6 IMAGE_ID="${image}" IMAGE_REF=kitsusync:v0.4.6 IMAGE_ARCHIVE_SHA256="${digest}" IMAGE_CONFIG_DIGEST="${portable}" IMAGE_MANIFEST_DIGEST="${portable}" IMAGE_CONTENT_DIGEST="${portable}" COMPOSE_SHA256="${digest}" DEPLOYMENT_TOOL_SHA256="${digest}" INSPECTION_TOOL_SHA256="${digest}" SQLITE_BACKUP_TOOL_SHA256="${digest}" BOOTSTRAP_TOOL_SHA256="${digest}" IMAGE_IDENTITY_TOOL_SHA256="${digest}" RUNTIME_STATE_TOOL_SHA256="${digest}" RESTORE_STATE_TOOL_SHA256="${digest}" PROVENANCE_OUTPUT="${tmp}/release-manifest" bash "${generator}"
grep -Fxq "runtime_state_tool_sha256=${digest}" "${tmp}/release-manifest"
grep -Fxq "restore_state_tool_sha256=${digest}" "${tmp}/release-manifest"
grep -Fxq 'release_tag=v0.4.6' "${tmp}/release-manifest"
grep -Fxq 'image_ref=kitsusync:v0.4.6' "${tmp}/release-manifest"
grep -Fxq "image_archive_sha256=${digest}" "${tmp}/release-manifest"
grep -Fxq "image_config_digest=${portable}" "${tmp}/release-manifest"
grep -Fxq "image_manifest_digest=${portable}" "${tmp}/release-manifest"
grep -Fxq "image_content_digest=${portable}" "${tmp}/release-manifest"
grep -Fxq "image_identity_tool_sha256=${digest}" "${tmp}/release-manifest"

printf 'release-identity-tests=PASS\n'
