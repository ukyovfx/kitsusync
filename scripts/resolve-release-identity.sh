#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version="$(tr -d '\r\n' < "${root}/VERSION")"
event="${GITHUB_EVENT_NAME:-}"
ref="${GITHUB_REF:-}"
checkout_sha="${GITHUB_SHA:-}"
pr_head_sha="${PR_HEAD_SHA:-}"
expected_source="${EXPECTED_SOURCE_COMMIT:-}"
output="${GITHUB_OUTPUT:-/dev/stdout}"

[[ "${version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { printf 'VERSION is not strict semver\n' >&2; exit 1; }
[[ "${checkout_sha}" =~ ^[0-9a-f]{40}$ ]] || { printf 'GITHUB_SHA is not an immutable commit\n' >&2; exit 1; }

artifact_kind=nonrelease
source_commit="${checkout_sha}"
merge_test_commit=""
release_commit=""

case "${event}" in
  pull_request)
    [[ "${pr_head_sha}" =~ ^[0-9a-f]{40}$ ]] || { printf 'PR_HEAD_SHA is not an immutable commit\n' >&2; exit 1; }
    artifact_kind=candidate
    source_commit="${pr_head_sha}"
    merge_test_commit="${checkout_sha}"
    ;;
  push)
    case "${ref}" in
      refs/tags/v*)
        [[ "${ref#refs/tags/}" == "v${version}" ]] || { printf 'release tag does not match VERSION\n' >&2; exit 1; }
        artifact_kind=release
        release_commit="${source_commit}"
        ;;
      refs/heads/master) ;;
      *) printf 'unsupported push ref: %s\n' "${ref}" >&2; exit 1 ;;
    esac
    ;;
  *) printf 'unsupported GitHub event: %s\n' "${event}" >&2; exit 1 ;;
esac

[[ "${source_commit}" =~ ^[0-9a-f]{40}$ ]] || { printf 'source commit is not immutable\n' >&2; exit 1; }
[[ -z "${expected_source}" || "${expected_source}" == "${source_commit}" ]] || { printf 'expected source commit mismatch\n' >&2; exit 1; }

{
  printf 'artifact_kind=%s\n' "${artifact_kind}"
  printf 'version=%s\n' "${version}"
  printf 'source_commit=%s\n' "${source_commit}"
  printf 'source_id=%s\n' "${source_commit}"
  printf 'merge_test_commit=%s\n' "${merge_test_commit}"
  printf 'release_commit=%s\n' "${release_commit}"
} >> "${output}"
