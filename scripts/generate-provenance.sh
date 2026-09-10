#!/usr/bin/env bash
set -euo pipefail

kind="${ARTIFACT_KIND:?ARTIFACT_KIND is required}"
source_commit="${SOURCE_COMMIT:?SOURCE_COMMIT is required}"
source_id="${SOURCE_ID:?SOURCE_ID is required}"
version="${RELEASE_VERSION:?RELEASE_VERSION is required}"
image_id="${IMAGE_ID:?IMAGE_ID is required}"
release_tag="${RELEASE_TAG:-}"
image_ref="${IMAGE_REF:-}"
archive_sha="${IMAGE_ARCHIVE_SHA256:-}"
compose_sha="${COMPOSE_SHA256:-}"
deploy_sha="${DEPLOYMENT_TOOL_SHA256:-}"
inspect_sha="${INSPECTION_TOOL_SHA256:-}"
backup_sha="${SQLITE_BACKUP_TOOL_SHA256:-}"
bootstrap_sha="${BOOTSTRAP_TOOL_SHA256:-}"
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
  [[ "${release_tag}" == "v${version}" ]] || { printf 'release tag does not match version\n' >&2; exit 1; }
else
  [[ -z "${release_commit}" ]] || { printf 'non-release artifact has release commit\n' >&2; exit 1; }
fi
if [[ -n "${archive_sha}${compose_sha}${deploy_sha}${inspect_sha}${backup_sha}${bootstrap_sha}${image_ref}" ]]; then
  [[ "${image_ref}" =~ ^kitsusync:(v?[a-zA-Z0-9][a-zA-Z0-9._-]{0,127})$ ]] || { printf 'invalid image reference\n' >&2; exit 1; }
  for digest in "${archive_sha}" "${compose_sha}" "${deploy_sha}" "${inspect_sha}" "${backup_sha}" "${bootstrap_sha}"; do
    [[ "${digest}" =~ ^[0-9a-f]{64}$ ]] || { printf 'invalid bundle digest\n' >&2; exit 1; }
  done
fi

{
  printf 'artifact_kind=%s\n' "${kind}"
  printf 'release_tag=%s\n' "${release_tag}"
  printf 'merge_test_commit=%s\n' "${merge_test}"
  printf 'source_commit=%s\n' "${source_commit}"
  printf 'source_id=%s\n' "${source_id}"
  printf 'release_commit=%s\n' "${release_commit}"
  printf 'version=%s\n' "${version}"
  printf 'image_id=%s\n' "${image_id}"
  printf 'image_ref=%s\n' "${image_ref}"
  printf 'image_archive_sha256=%s\n' "${archive_sha}"
  printf 'compose_sha256=%s\n' "${compose_sha}"
  printf 'deployment_tool_sha256=%s\n' "${deploy_sha}"
  printf 'inspection_tool_sha256=%s\n' "${inspect_sha}"
  printf 'sqlite_backup_tool_sha256=%s\n' "${backup_sha}"
  printf 'bootstrap_tool_sha256=%s\n' "${bootstrap_sha}"
} > "${output}"
