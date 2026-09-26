#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
deploy="${root}/deploy/kitsusync-deploy"
core="${root}/deploy/kitsusync-deploy-transaction"
bootstrap="${root}/deploy/kitsusync-bootstrap"
bundle="${root}/scripts/build-deployment-bundle.sh"
provenance="${root}/scripts/generate-provenance.sh"

KITSUSYNC_DEPLOY_TEST_DOCKER_BIN="$(type -P true)" KITSUSYNC_DEPLOY_TEST_MODE=readiness bash "${deploy}" fresh-install setup_required
if KITSUSYNC_DEPLOY_TEST_DOCKER_BIN="$(type -P true)" KITSUSYNC_DEPLOY_TEST_MODE=readiness bash "${deploy}" fresh-install ready; then
  printf 'fresh-install incorrectly accepted ready instead of setup_required\n' >&2
  exit 1
fi

grep -Fq 'fresh-install' "${bootstrap}"
grep -Fq 'fresh-install' "${core}"
grep -Fq 'fresh-install.pending' "${core}"
grep -Fq 'fresh install requires no prior KitsuSync container' "${core}"
grep -Fq 'fresh install requires an empty runtime data directory' "${core}"
grep -Fq 'fresh deployment validation failed; cleanup follows' "${core}"
grep -Fq 'VALIDATION_STAGE=kitsu_connectivity' "${core}"
grep -Fq 'exec "${id}" /usr/bin/curl' "${core}"
grep -Fq 'path.rstrip("/") + "/api/"' "${core}"
grep -Fq 'compose_fresh down --remove-orphans' "${core}"
grep -Fq "printf 'normal\\n'" "${core}"
grep -Fq 'fresh install has already completed' "${bootstrap}"
grep -Fq '/usr/bin/install -d -o root -g root -m 0755 /usr/local/libexec' "${bootstrap}"
grep -Fq '/usr/bin/install -d -o root -g root -m 0700 "${runtime_tmp}/data"' "${bootstrap}"
grep -Fq '/usr/bin/chown 10001:10001 "${runtime_tmp}/data"' "${bootstrap}"

if grep -Fq '/usr/bin/install -d -o 10001 -g 10001' "${bootstrap}"; then
  printf 'fresh bootstrap must not require passwd/group entries for UID/GID 10001\n' >&2
  exit 1
fi
grep -Fq 'fresh_conf_sha256' "${provenance}"
grep -Fq 'fresh_templates_manifest_sha256' "${provenance}"
grep -Fq 'APP_SOURCE_ROOT' "${bundle}"

if grep -Eiq '(token|password|secret|webhook|guild)[[:space:]]*=[[:space:]]*[^[:space:]#]+' "${root}/deploy/fresh-env.local"; then
  printf 'fresh environment seed contains credential-like material\n' >&2
  exit 1
fi
if grep -Eiq '(REPLACE_ME|YOUR_|example-token|[0-9]{17,20})' "${root}/deploy/fresh-conf.toml" "${root}/deploy/fresh-env.local"; then
  printf 'fresh runtime seed contains a placeholder that may be treated as configured\n' >&2
  exit 1
fi

printf 'fresh-install-contract=PASS\n'
