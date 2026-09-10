#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
wrapper="$root/deploy/kitsusync-deploy"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

[[ ! -e "$root/deploy/docker-compose.yml" ]] || {
  printf 'retired production Compose path was reintroduced\n' >&2
  exit 1
}

require_wrapper_doc() {
  local file="$1"
  grep -Fq '/usr/local/sbin/kitsusync-deploy' "$root/$file" || {
    printf 'production wrapper is not documented in %s\n' "$file" >&2
    exit 1
  }
}

for file in README.md docs/ENVIRONMENTS.md docs/QUICK_START.md docs/SETUP_FOR_STUDIOS.md docs/TROUBLESHOOTING.md; do
  require_wrapper_doc "$file"
done

# Remove explicitly marked local-development snippets and historical evidence
# before checking active guidance. This intentionally does not reject valid
# development/test Compose examples.
for file in README.md docs/QUICK_START.md docs/SETUP_FOR_STUDIOS.md docs/TROUBLESHOOTING.md .env.example; do
  awk '
    /<!-- LOCAL DEVELOPMENT ONLY -->|^# LOCAL DEVELOPMENT ONLY/ { local_only=1; next }
    /<!-- END LOCAL DEVELOPMENT ONLY -->|^# END LOCAL DEVELOPMENT ONLY/ { local_only=0; next }
    /^### Historical retired deployment procedure/ { historical=1 }
    local_only || historical { next }
    { print }
  ' "$root/$file" > "$tmp/$(basename "$file")"
  if grep -nE 'docker compose (up|build|restart|pull|down)' "$tmp/$(basename "$file")"; then
    printf 'active production-like Compose command remains in %s\n' "$file" >&2
    exit 1
  fi
done

grep -Fq 'ENV_FILE=${CONTROL_DIR}/.env.local' "$wrapper"
grep -Fq 'PROJECT_NAME=kitsusync' "$wrapper"
grep -Fq -- '--project-name "${PROJECT_NAME}"' "$wrapper"
grep -Fq 'APP_ENV: production' "$wrapper"
grep -Fq 'APP_ENV=development' "$root/docker-compose.yml"
if grep -Fq 'deploy/docker-compose.yml' "$root/.env.example"; then
  printf 'stale retired Compose reference remains in .env.example\n' >&2
  exit 1
fi
if grep -Fq 'PUBLIC_HOST=' "$root/.env.example" || grep -Fq 'ALIAS=' "$root/.env.example"; then
  printf 'obsolete Traefik-only variables remain in .env.example\n' >&2
  exit 1
fi

printf 'production-path-contract=PASS\n'
