#!/usr/bin/env bash
set -euo pipefail

kind="${ARTIFACT_KIND:?ARTIFACT_KIND is required}"
source_commit="${SOURCE_COMMIT:?SOURCE_COMMIT is required}"
source_id="${SOURCE_ID:?SOURCE_ID is required}"
version="${RELEASE_VERSION:?RELEASE_VERSION is required}"
image_id="${IMAGE_ID:?IMAGE_ID is required}"
merge_test="${MERGE_TEST_COMMIT:-}"
release_commit="${RELEASE_COMMIT:-}"
output="${PROVENANCE_OUTPUT:-provenance.txt}"

[[ "${kind}" == candidate || "${kind}" == nonrelease || "${kind}" == release ]] || { printf 'invalid artifact kind\n' >&2; exit 1; }
[[ "${source_commit}" =~ ^[0-9a-f]{40}$ && "${source_id}" == "${source_commit}" ]] || { printf 'invalid source identity\n' >&2; exit 1; }
[[ "${version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { printf 'invalid release version\n' >&2; exit 1; }
[[ "${image_id}" =~ ^sha256:[0-9a-f]{64}$ ]] || { printf 'invalid immutable image identity\n' >&2; exit 1; }
[[ -z "${merge_test}" || "${merge_test}" =~ ^[0-9a-f]{40}$ ]] || { printf 'invalid merge-test commit\n' >&2; exit 1; }
if [[ "${kind}" == release ]]; then
  [[ "${release_commit}" == "${source_commit}" ]] || { printf 'release commit does not match source\n' >&2; exit 1; }
else
  [[ -z "${release_commit}" ]] || { printf 'non-release artifact has release commit\n' >&2; exit 1; }
fi

{
  printf 'artifact_kind=%s\n' "${kind}"
  printf 'merge_test_commit=%s\n' "${merge_test}"
  printf 'source_commit=%s\n' "${source_commit}"
  printf 'source_id=%s\n' "${source_id}"
  printf 'release_commit=%s\n' "${release_commit}"
  printf 'version=%s\n' "${version}"
  printf 'image_id=%s\n' "${image_id}"
} > "${output}"
