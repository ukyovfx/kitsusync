#!/bin/bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
inspect="${root}/deploy/kitsusync-inspect"
bash -n "${inspect}"
for contract in \
  'docker compose --project-name kitsusync' \
  'compose_ids' \
  'deployment_mode.*legacy-migration' \
  'docker ps -q --no-trunc --filter label=com.docker.compose.project=kitsusync --filter label=com.docker.compose.service=app --filter status=running' \
  'legacy migration requires exactly one running prior container' \
  'candidate_name.*kitsusync-app-1' \
  'candidate_project.*kitsusync' \
  'candidate_service.*app' \
  'candidate_running.*true' \
  'candidate_image.*sha256:\[0-9a-f\]\{64\}' \
  'candidate_ready.*404' \
  'kitsusync-runtime-state plan' \
  'kitsusync-runtime-state validate' \
  'duplicate retained service containers require operator preflight'; do
  grep -Eq -- "${contract}" "${inspect}" || { printf 'missing inspect detector contract: %s\n' "${contract}" >&2; exit 1; }
done
grep -Eq 'deployment_mode.*normal \|\|' "${inspect}"
grep -Eq 'deployment_mode.*recovery \|\|' "${inspect}"
printf 'inspect-legacy-detector-contract=PASS\n'
