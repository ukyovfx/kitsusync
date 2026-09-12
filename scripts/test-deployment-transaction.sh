#!/usr/bin/env bash
set -euo pipefail

# This integration test installs the actual root wrapper at its fixed paths.
# It is exclusively for a fresh, disposable GitHub-hosted Ubuntu runner.
[[ "${GITHUB_ACTIONS:-}" == true && "${RUNNER_ENVIRONMENT:-}" == github-hosted && "${RUNNER_OS:-}" == Linux && "${EUID}" -ne 0 ]] || {
  printf 'deployment transaction test requires an unprivileged GitHub-hosted Linux runner\n' >&2; exit 1;
}
[[ "$(. /etc/os-release; printf '%s' "$ID")" == ubuntu ]] || exit 1
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
runtime=/home/ukyo_vfx/kitsusync
protected=(/etc/kitsusync-deploy /var/lib/kitsusync-deploy /var/backups/kitsusync-deploy "$runtime")
tools=(/usr/local/sbin/kitsusync-deploy /usr/local/sbin/kitsusync-inspect
       /usr/local/libexec/kitsusync-sqlite-backup /usr/local/libexec/kitsusync-image-identity
       /usr/local/libexec/kitsusync-runtime-state /usr/local/libexec/kitsusync-restore-state)
for path in "${protected[@]}" "${tools[@]}"; do
  sudo test ! -e "$path" && sudo test ! -L "$path" || { printf 'refusing existing fixture path: %s\n' "$path" >&2; exit 1; }
done
sudo test ! -e /home/ukyo_vfx && sudo test ! -L /home/ukyo_vfx || exit 1
[[ -z "$(docker ps -aq --filter label=com.docker.compose.project=kitsusync)" ]] || exit 1
[[ -z "$(docker network ls -q --filter name='^kitsusync_default$')" ]] || exit 1
if docker image inspect kitsusync:v0.4.6 >/dev/null 2>&1; then
  printf 'refusing to overwrite existing v0.4.6 image reference\n' >&2; exit 1
fi
work="$(mktemp -d)"
legacy_ref="kitsusync:fixture-legacy-$$"
extra_network="kitsusync-fixture-extra-$$"
source_commit=b7b30157cb90c4500e8b00d3c26ac7038f5c8c10
cleanup() {
  local ids result=$?
  if [[ "$result" -ne 0 ]]; then
    for report in "$work/failed-deploy.log" "$work/successful-deploy.log"; do
      [[ ! -f "$report" ]] || grep -E '^(ERROR:|runtime validation failed:|rollback=)' "$report" >&2 || true
    done
  fi
  ids="$(docker ps -aq --filter label=com.docker.compose.project=kitsusync)"
  if [[ -n "$ids" ]]; then docker rm -f $ids >/dev/null 2>&1 || true; fi
  docker network rm kitsusync_default "$extra_network" >/dev/null 2>&1 || true
  docker image rm kitsusync:v0.4.6 "$legacy_ref" >/dev/null 2>&1 || true
  # All fixed paths were proven absent before this disposable fixture created them.
  sudo rm -rf -- "${protected[@]}"
  sudo rm -f -- "${tools[@]}"
  rm -rf -- "$work"
}
trap cleanup EXIT

cat >"$work/fixture.py" <<'PY'
import http.server, json, os, pathlib, sqlite3, sys, time
legacy = '--legacy' in sys.argv
data = pathlib.Path('/app/data')
assert pathlib.Path('/app/conf.toml').read_text() == 'fixture-conf\n'
assert pathlib.Path('/app/tpl/marker').read_text() == 'fixture-template\n'
assert os.getuid() == 10001 and os.getgid() == 10001
assert os.environ['APP_ENV'] == 'production'
failed_target = not legacy and (data / 'fail-target').exists()
if failed_target:
    with sqlite3.connect(data / 'sqlite.db') as db:
        db.execute("UPDATE evidence SET value='target-mutated'")
    (data / 'runtime-secret.key').write_text('target-mutated\n')
started = time.monotonic()
class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == '/health':
            code, body = 200, {'status':'ok'}
        elif self.path == '/ready':
            if legacy: code, body = 404, {}
            else:
                ready = time.monotonic() - started >= 7
                code = 200 if ready else 503
                body = {'status':'ready' if ready else 'setup_required',
                        'build':{'base_commit':'wrong' if failed_target else 'b7b30157cb90c4500e8b00d3c26ac7038f5c8c10',
                                 'build_source_id':'b7b30157cb90c4500e8b00d3c26ac7038f5c8c10'}}
        elif self.path.startswith('/bot/admin'): code, body = 401, {}
        else: code, body = 200, {}
        self.send_response(code); self.send_header('Content-Type','application/json'); self.end_headers()
        self.wfile.write(json.dumps(body,separators=(',',':')).encode())
    def log_message(self, *_): pass
http.server.ThreadingHTTPServer(('0.0.0.0',8090), Handler).serve_forever()
PY
cat >"$work/Dockerfile" <<'DOCKER'
FROM python:3.12-slim AS base
RUN apt-get update && apt-get install -y --no-install-recommends curl && rm -rf /var/lib/apt/lists/*
COPY fixture.py /fixture.py
USER 10001:10001
WORKDIR /app
CMD ["python", "/fixture.py"]
FROM base AS legacy
LABEL org.opencontainers.image.source-id="v0.4.5"
FROM base AS target
LABEL org.opencontainers.image.revision="b7b30157cb90c4500e8b00d3c26ac7038f5c8c10"
LABEL org.opencontainers.image.source-id="b7b30157cb90c4500e8b00d3c26ac7038f5c8c10"
LABEL org.opencontainers.image.version="0.4.6"
LABEL kitsusync.test-fixture="disposable-ci-only"
DOCKER
docker build --target legacy -t "$legacy_ref" "$work" >/dev/null
docker build --target target -t kitsusync:v0.4.6 "$work" >/dev/null

sudo install -d -o root -g root -m 0700 /etc/kitsusync-deploy /var/lib/kitsusync-deploy /var/backups/kitsusync-deploy
sudo install -d -o root -g root -m 0755 "$runtime" "$runtime/tpl"
sudo install -d -o 10001 -g 10001 -m 0700 "$runtime/data"
printf 'fixture-conf\n' >"$work/conf.toml"
printf 'fixture-template\n' >"$work/template"
printf 'fixture-secret-material\n' >"$work/key"
printf 'APP_ENV=production\nFIXTURE_ONLY=operator\n' >"$work/operator.env"
sudo install -o root -g root -m 0644 "$work/conf.toml" "$runtime/conf.toml"
sudo install -o root -g root -m 0644 "$work/template" "$runtime/tpl/marker"
sudo install -o 10001 -g 10001 -m 0600 "$work/key" "$runtime/data/runtime-secret.key"
sudo install -o root -g root -m 0600 "$work/operator.env" /etc/kitsusync-deploy/.env.local
sudo python3 - "$runtime/data/sqlite.db" <<'PY'
import os, sqlite3, sys
with sqlite3.connect(sys.argv[1]) as db:
    db.execute('CREATE TABLE evidence(value TEXT)')
    db.execute("INSERT INTO evidence VALUES('original')")
os.chown(sys.argv[1],10001,10001); os.chmod(sys.argv[1],0o600)
PY
mkdir "$work/extra"
chmod 0755 "$work" "$work/extra"
docker network create "$extra_network" >/dev/null
cat >"$work/legacy.yml" <<YAML
services:
  app:
    image: $legacy_ref
    command: ["python", "/fixture.py", "--legacy"]
    user: "10001:10001"
    restart: on-failure:3
    environment:
      APP_ENV: production
      OLD_ONLY: preserved
    labels:
      kitsusync.fixture-contract: legacy
    volumes:
      - $runtime/conf.toml:/app/conf.toml
      - $runtime/tpl:/app/tpl:ro
      - $runtime/data:/app/data
      - $work/extra:/legacy-extra:ro
    ports:
      - "127.0.0.1:8090:8090"
    networks:
      default:
        aliases: [legacy-alias]
      extra:
        aliases: [second-alias]
    healthcheck:
      test: ["CMD", "curl", "-fsS", "http://localhost:8090/health"]
      interval: 1s
      timeout: 1s
      start_period: 1s
      retries: 5
networks:
  extra:
    external: true
    name: $extra_network
YAML
docker compose -p kitsusync -f "$work/legacy.yml" up -d >/dev/null
prior_id="$(docker compose -p kitsusync -f "$work/legacy.yml" ps -q app)"
for attempt in $(seq 1 30); do
  [[ "$(docker inspect -f '{{.State.Health.Status}}' "$prior_id")" == healthy ]] && break
  sleep 1
done
[[ "$(docker inspect -f '{{.State.Health.Status}}' "$prior_id")" == healthy ]]
[[ "$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8090/ready)" == 404 ]]
prior_image="$(docker inspect -f '{{.Image}}' "$prior_id")"
printf '%s\n' "$prior_image" >"$work/legacy-image.id"
sudo install -o root -g root -m 0600 "$work/legacy-image.id" /etc/kitsusync-deploy/legacy-image.id
docker inspect "$prior_id" >"$work/prior-inspect.json"
python3 "$root/deploy/kitsusync-runtime-state" plan "$work/prior-inspect.json" "$work/prior-plan.json"

# Use the actual root Compose contract. The fixture supplies only its image;
# the wrapper must replace source-relative mounts and enforce production mode.
ARTIFACT_KIND=release SOURCE_COMMIT="$source_commit" SOURCE_ID="$source_commit" \
  RELEASE_COMMIT="$source_commit" RELEASE_VERSION=0.4.6 RELEASE_TAG=v0.4.6 \
  IMAGE_REF=kitsusync:v0.4.6 IMAGE_ID="$(docker image inspect -f '{{.Id}}' kitsusync:v0.4.6)" \
  DEPLOYMENT_MODE=legacy-migration COMPOSE_SOURCE="$root/docker-compose.yml" BUNDLE_OUTPUT="$work/bundle" \
  bash "$root/scripts/build-deployment-bundle.sh" >/dev/null
sudo install -o root -g root -m 0600 "$work/bundle/docker-compose.yml" /etc/kitsusync-deploy/docker-compose.yml
sudo install -o root -g root -m 0600 "$work/bundle/provenance.txt" /etc/kitsusync-deploy/provenance
sudo install -o root -g root -m 0600 "$work/bundle/deployment-mode" /etc/kitsusync-deploy/deployment-mode
sudo install -o root -g root -m 0600 "$work/bundle/kitsusync-image.tar" /var/lib/kitsusync-deploy/kitsusync-image.tar
sudo install -d -o root -g root -m 0755 /usr/local/libexec
for path in "${tools[@]}"; do sudo install -o root -g root -m 0700 "$work/bundle/$(basename "$path")" "$path"; done

sudo touch "$runtime/data/fail-target"
if sudo /usr/bin/env -i PATH=/usr/bin:/bin /usr/local/sbin/kitsusync-deploy >"$work/failed-deploy.log" 2>&1; then
  printf 'wrong target readiness identity incorrectly passed\n' >&2; exit 1
fi
grep -Fq 'runtime validation failed: check=readiness_identity' "$work/failed-deploy.log"
grep -Fxq 'rollback=verified' "$work/failed-deploy.log"
restored_id="$(docker ps -q --filter name='^/kitsusync-app-1$')"
[[ -n "$restored_id" && "$restored_id" != "$prior_id" ]]
[[ "$(docker inspect -f '{{.Image}}' "$restored_id")" == "$prior_image" ]]
docker inspect "$restored_id" >"$work/restored-inspect.json"
python3 "$root/deploy/kitsusync-runtime-state" compare "$work/prior-plan.json" "$work/restored-inspect.json"
sudo cmp -s "$work/key" "$runtime/data/runtime-secret.key"
sudo python3 - "$runtime/data/sqlite.db" <<'PY'
import sqlite3, sys
with sqlite3.connect('file:'+sys.argv[1]+'?mode=ro',uri=True) as db:
    assert db.execute('SELECT value FROM evidence').fetchall() == [('original',)]
PY
[[ "$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8090/ready)" == 404 ]]
sudo test -f /etc/kitsusync-deploy/legacy-image.id
[[ "$(sudo cat /etc/kitsusync-deploy/deployment-mode)" == legacy-migration ]]

# The restored old state can subsequently migrate successfully. The fixture's
# initial setup_required interval must be awaited, never accepted as success.
sudo rm -f -- "$runtime/data/fail-target"
sudo /usr/bin/env -i PATH=/usr/bin:/bin /usr/local/sbin/kitsusync-deploy >"$work/successful-deploy.log" 2>&1
grep -Fq 'KitsuSync deployment completed: version=0.4.6' "$work/successful-deploy.log"
[[ "$(sudo cat /etc/kitsusync-deploy/deployment-mode)" == normal ]]
sudo test ! -e /etc/kitsusync-deploy/legacy-image.id
[[ "$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8090/ready)" == 200 ]]
sudo cmp -s "$work/key" "$runtime/data/runtime-secret.key"
printf 'deployment-transaction-tests=PASS (real wrapper failure/rollback and subsequent deployment)\n'
