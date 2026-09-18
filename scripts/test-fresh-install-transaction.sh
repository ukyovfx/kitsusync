#!/usr/bin/env bash
set -euo pipefail

[[ "${GITHUB_ACTIONS:-}" == true && "${RUNNER_ENVIRONMENT:-}" == github-hosted && "${RUNNER_OS:-}" == Linux && "${EUID}" -ne 0 ]] || {
  printf 'fresh-install transaction test requires an unprivileged GitHub-hosted Linux runner\n' >&2; exit 1;
}
[[ "$(. /etc/os-release; printf '%s' "$ID")" == ubuntu ]] || exit 1

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
runtime=/home/ukyo_vfx/kitsusync
source_commit=b7b30157cb90c4500e8b00d3c26ac7038f5c8c10
protected=(/etc/kitsusync-deploy /var/lib/kitsusync-deploy /var/backups/kitsusync-deploy "$runtime" /root/kitsusync-release-stage)
tools=(/usr/local/sbin/kitsusync-deploy /usr/local/sbin/kitsusync-inspect
       /usr/local/libexec/kitsusync-sqlite-backup /usr/local/libexec/kitsusync-image-identity
       /usr/local/libexec/kitsusync-runtime-state /usr/local/libexec/kitsusync-restore-state)
for path in "${protected[@]}" "${tools[@]}"; do
  sudo test ! -e "$path" && sudo test ! -L "$path" || { printf 'refusing existing fixture path: %s\n' "$path" >&2; exit 1; }
done
[[ -z "$(docker ps -aq --filter label=com.docker.compose.project=kitsusync)" ]] || exit 1
[[ -z "$(docker network ls -q --filter label=com.docker.compose.project=kitsusync)" ]] || exit 1
if docker image inspect kitsusync:v0.4.6 >/dev/null 2>&1; then
  printf 'refusing to overwrite existing v0.4.6 image reference\n' >&2; exit 1
fi

work="$(mktemp -d)"
stage=fixture-setup
cleanup() {
  local result=$? ids
  if [[ "${result}" -ne 0 ]]; then
    printf 'fresh-install-transaction-failed-stage=%s\n' "${stage}" >&2
    for report in "${work}/failed.log" "${work}/success.log" "${work}/upgrade.log"; do
      [[ ! -f "${report}" ]] || grep -E '^(ERROR:|runtime validation failed:|KitsuSync fresh)' "${report}" >&2 || true
    done
  fi
  ids="$(docker ps -aq --filter label=com.docker.compose.project=kitsusync)"
  if [[ -n "${ids}" ]]; then docker rm -f ${ids} >/dev/null 2>&1 || true; fi
  docker network rm kitsusync_default >/dev/null 2>&1 || true
  docker image rm kitsusync:v0.4.6 >/dev/null 2>&1 || true
  sudo rm -rf -- "${protected[@]}"
  sudo rm -f -- "${tools[@]}" /etc/sudoers.d/kitsusync-deploy
  rm -rf -- "${work}"
  exit "${result}"
}
trap cleanup EXIT

cat >"${work}/fixture.py" <<'PY'
import http.server, json, os, pathlib, sqlite3
data = pathlib.Path('/app/data')
with sqlite3.connect(data / 'sqlite.db') as db:
    db.execute('CREATE TABLE IF NOT EXISTS evidence(value TEXT)')
fail = os.environ.get('FIXTURE_FAIL') == '1'
class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == '/health': code, body = 200, {'status':'ok'}
        elif self.path == '/ready':
            ready = (data / 'ready').exists()
            code = 200 if ready else 503
            body = {'status':'degraded' if fail else ('ready' if ready else 'setup_required'),
                    'build':{'base_commit':'b7b30157cb90c4500e8b00d3c26ac7038f5c8c10',
                             'build_source_id':'b7b30157cb90c4500e8b00d3c26ac7038f5c8c10'}}
        elif self.path == '/bot/login': code, body = 200, {'login':'reachable'}
        elif self.path.startswith('/bot/') or self.path.startswith('/api/'): code, body = 401, {}
        else: code, body = 404, {}
        self.send_response(code); self.send_header('Content-Type','application/json'); self.end_headers()
        self.wfile.write(json.dumps(body,separators=(',',':')).encode())
    def log_message(self, *_): pass
http.server.ThreadingHTTPServer(('0.0.0.0',8090), Handler).serve_forever()
PY
cat >"${work}/Dockerfile" <<'DOCKER'
FROM python:3.12-slim AS base
RUN apt-get update && apt-get install -y --no-install-recommends curl && rm -rf /var/lib/apt/lists/*
COPY fixture.py /fixture.py
USER 10001:10001
WORKDIR /app
HEALTHCHECK --interval=1s --timeout=1s --start-period=1s --retries=10 CMD curl -fsS http://localhost:8090/health || exit 1
CMD ["python", "/fixture.py"]
FROM base AS bad
ENV FIXTURE_FAIL=1
LABEL org.opencontainers.image.revision="b7b30157cb90c4500e8b00d3c26ac7038f5c8c10"
LABEL org.opencontainers.image.source-id="b7b30157cb90c4500e8b00d3c26ac7038f5c8c10"
LABEL org.opencontainers.image.version="0.4.6"
FROM base AS good
LABEL org.opencontainers.image.revision="b7b30157cb90c4500e8b00d3c26ac7038f5c8c10"
LABEL org.opencontainers.image.source-id="b7b30157cb90c4500e8b00d3c26ac7038f5c8c10"
LABEL org.opencontainers.image.version="0.4.6"
DOCKER

build_bundle() {
  local target="$1" output="$2"
  docker build --target "${target}" -t kitsusync:v0.4.6 "${work}" >/dev/null
  ARTIFACT_KIND=release SOURCE_COMMIT="${source_commit}" SOURCE_ID="${source_commit}" \
    RELEASE_COMMIT="${source_commit}" RELEASE_VERSION=0.4.6 RELEASE_TAG=v0.4.6 \
    IMAGE_REF=kitsusync:v0.4.6 IMAGE_ID="$(docker image inspect -f '{{.Id}}' kitsusync:v0.4.6)" \
    DEPLOYMENT_MODE=fresh-install COMPOSE_SOURCE="${root}/docker-compose.yml" APP_SOURCE_ROOT="${root}" BUNDLE_OUTPUT="${output}" \
    bash "${root}/scripts/build-deployment-bundle.sh" >/dev/null
}
stage_bundle() {
  local source="$1"
  sudo install -d -o root -g root -m 0700 /root/kitsusync-release-stage
  sudo find /root/kitsusync-release-stage -mindepth 1 -maxdepth 1 -type f -delete
  for file in "${source}"/*; do sudo install -o root -g root -m 0600 "${file}" "/root/kitsusync-release-stage/$(basename "${file}")"; done
  printf 'http://host.docker.internal:8080/\n' >"${work}/fresh-kitsu-hostname"
  sudo install -o root -g root -m 0600 "${work}/fresh-kitsu-hostname" /root/kitsusync-release-stage/fresh-kitsu-hostname
}

sudo install -d -o root -g root -m 0755 /home/ukyo_vfx
stage=build-bad-bundle
build_bundle bad "${work}/bad-bundle"
stage_bundle "${work}/bad-bundle"
stage=bootstrap-empty-host
sudo /usr/bin/env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin /bin/bash /root/kitsusync-release-stage/kitsusync-bootstrap
sudo test -f "${runtime}/.fresh-install-seed"
sudo test -f /var/lib/kitsusync-deploy/fresh-install.pending
sudo test "$(sudo cat /etc/kitsusync-deploy/deployment-mode)" = fresh-install
sudo test "$(sudo cat /etc/kitsusync-deploy/.env.local)" = 'KITSU_HOSTNAME=http://host.docker.internal:8080/'

stage=failed-fresh-deploy
if sudo /usr/bin/env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin /usr/local/sbin/kitsusync-deploy >"${work}/failed.log" 2>&1; then
  printf 'degraded fresh runtime incorrectly passed validation\n' >&2; exit 1
fi
grep -Fq 'fresh deployment validation failed; cleanup follows' "${work}/failed.log"
[[ -z "$(docker ps -aq --filter label=com.docker.compose.project=kitsusync)" ]]
[[ -z "$(docker network ls -q --filter label=com.docker.compose.project=kitsusync)" ]]
sudo test -z "$(sudo find "${runtime}/data" -mindepth 1 -print -quit)"
sudo test "$(sudo cat /etc/kitsusync-deploy/deployment-mode)" = fresh-install

stage=retry-bootstrap
build_bundle good "${work}/good-bundle"
stage_bundle "${work}/good-bundle"
sudo /usr/bin/env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin /bin/bash /root/kitsusync-release-stage/kitsusync-bootstrap
stage=successful-fresh-deploy
sudo /usr/bin/env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin /usr/local/sbin/kitsusync-deploy >"${work}/success.log" 2>&1
grep -Fq 'KitsuSync fresh installation completed: version=0.4.6' "${work}/success.log"
sudo test "$(sudo cat /etc/kitsusync-deploy/deployment-mode)" = normal
sudo test ! -e /var/lib/kitsusync-deploy/fresh-install.pending
sudo test ! -e "${runtime}/.fresh-install-seed"
container_id="$(docker ps -q --filter label=com.docker.compose.project=kitsusync --filter label=com.docker.compose.service=app)"
[[ -n "${container_id}" ]]
[[ "$(docker inspect -f '{{.State.Status}}' "${container_id}")" == running ]]
[[ "$(docker inspect -f '{{.State.Health.Status}}' "${container_id}")" == healthy ]]
[[ "$(docker inspect -f '{{.Config.User}}' "${container_id}")" == 10001:10001 ]]
[[ "$(docker inspect -f '{{.HostConfig.RestartPolicy.Name}}' "${container_id}")" == unless-stopped ]]
[[ "$(docker port "${container_id}" 8090/tcp)" == 127.0.0.1:8090 ]]
[[ "$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8090/health)" == 200 ]]
[[ "$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8090/ready)" == 503 ]]
[[ "$(curl -s http://127.0.0.1:8090/ready | python3 -c 'import json,sys; print(json.load(sys.stdin)["status"])')" == setup_required ]]
[[ "$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8090/bot/login)" == 200 ]]

stage=completed-bootstrap-retry
if sudo /usr/bin/env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin /bin/bash /root/kitsusync-release-stage/kitsusync-bootstrap >"${work}/completed-bootstrap.log" 2>&1; then
  printf 'completed fresh bootstrap unexpectedly reset normal mode\n' >&2; exit 1
fi
grep -Fq 'fresh install has already completed' "${work}/completed-bootstrap.log"

stage=subsequent-normal-upgrade
sudo touch "${runtime}/data/ready"
sudo chown 10001:10001 "${runtime}/data/ready"
sudo chmod 0600 "${runtime}/data/ready"
sudo /usr/bin/env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin /usr/local/sbin/kitsusync-deploy >"${work}/upgrade.log" 2>&1
grep -Fq 'KitsuSync deployment completed: version=0.4.6' "${work}/upgrade.log"
[[ "$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8090/ready)" == 200 ]]

printf 'fresh-install-transaction-tests=PASS (empty host, cleanup, retry, transition, normal upgrade)\n'
