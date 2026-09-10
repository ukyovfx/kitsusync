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
grep -Fxq 'version=0.4.5' "${tmp}/pr"

GITHUB_EVENT_NAME=push GITHUB_REF=refs/heads/master GITHUB_SHA="${a}" GITHUB_OUTPUT="${tmp}/master" bash "${resolver}"
grep -Fxq 'artifact_kind=nonrelease' "${tmp}/master"
grep -Fxq 'release_commit=' "${tmp}/master"
! grep -Fq 'version=master' "${tmp}/master"

GITHUB_EVENT_NAME=push GITHUB_REF=refs/tags/v0.4.5 GITHUB_SHA="${a}" GITHUB_OUTPUT="${tmp}/release" bash "${resolver}"
grep -Fxq 'artifact_kind=release' "${tmp}/release"
grep -Fxq "release_commit=${a}" "${tmp}/release"

if GITHUB_EVENT_NAME=pull_request GITHUB_REF=refs/pull/159/merge GITHUB_SHA="${a}" PR_HEAD_SHA="${b}" EXPECTED_SOURCE_COMMIT="${a}" GITHUB_OUTPUT="${tmp}/bad" bash "${resolver}" >/dev/null 2>&1; then
  printf 'expected source mismatch was accepted\n' >&2
  exit 1
fi

ARTIFACT_KIND=candidate SOURCE_COMMIT="${b}" SOURCE_ID="${b}" MERGE_TEST_COMMIT="${a}" RELEASE_VERSION=0.4.5 IMAGE_ID="${image}" PROVENANCE_OUTPUT="${tmp}/manifest" bash "${generator}"
grep -Fxq "source_commit=${b}" "${tmp}/manifest"
grep -Fxq "image_id=${image}" "${tmp}/manifest"

printf 'release-identity-tests=PASS\n'
