#!/usr/bin/env bash
set -Eeuo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
helper="${root}/deploy/kitsusync-staging-deploy"
compose_file="${root}/deploy/kitsusync-staging-compose.yml"
source_sha="${SOURCE_SHA:?SOURCE_SHA is required}"
image_tag="${KITSUSYNC_IMAGE_TAG:?KITSUSYNC_IMAGE_TAG is required}"
image_ref="kitsusync:${image_tag}"
project=kitsusync-staging-runtime-e2e
fixture="$(mktemp -d)"
override_file="$fixture/compose.override.yml"
mock_log="$fixture/mock-kitsu.log"
mock_pid=
compose=(docker compose --project-name "$project" --project-directory "$fixture" --file "$compose_file" --file "$override_file")
compose_started=0

die() { printf 'staging-runtime-e2e=FAIL reason=%s\n' "$1" >&2; exit 1; }
cleanup() {
  if [[ "$compose_started" -eq 1 ]]; then
    "${compose[@]}" down --volumes --remove-orphans >/dev/null 2>&1 || true
  fi
  if [[ -n "$mock_pid" ]]; then kill "$mock_pid" >/dev/null 2>&1 || true; wait "$mock_pid" >/dev/null 2>&1 || true; fi
  rm -rf -- "$fixture"
}
trap cleanup EXIT

[[ "$source_sha" =~ ^[0-9a-f]{40}$ ]] || die SOURCE_SHA_INVALID
[[ "$(docker image inspect --format '{{.Config.User}}' "$image_ref")" == 10001:10001 ]] || die IMAGE_RUNTIME_USER_INVALID
[[ "$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image_ref")" == "$source_sha" ]] || die IMAGE_REVISION_INVALID
[[ "$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.source-id"}}' "$image_ref")" == "$source_sha" ]] || die IMAGE_SOURCE_INVALID

if docker inspect kitsusync-staging >/dev/null 2>&1; then die FIXED_CONTAINER_NAME_OCCUPIED; fi
if docker volume inspect kitsusync-staging-data >/dev/null 2>&1; then die FIXED_DATA_VOLUME_OCCUPIED; fi
if docker network inspect kitsusync-staging-network >/dev/null 2>&1; then die FIXED_NETWORK_OCCUPIED; fi

production_before="$(docker ps -q --filter publish=8090 | sort)"
install -d -m 0700 "$fixture"
cat >"$fixture/conf.toml" <<'CONFIG'
[kitsu]
hostname = "http://host.docker.internal:18082/"
email = ""
password = ""

[discord]
embedsPerRequests = 10
RequestsPerMinute = 50
CONFIG
sudo chown root:root "$fixture/conf.toml"
sudo chmod 0400 "$fixture/conf.toml"

# Reproduce the installed v5 configuration contract using the actual app image
# and its configured non-root runtime identity. Keep startup output private.
set +e
docker run --rm --network none \
  --mount "type=bind,src=$fixture/conf.toml,dst=/app/conf.toml,readonly" \
  "$image_ref" >"$fixture/old-permission-startup.log" 2>&1
old_config_status=$?
set -e
[[ "$old_config_status" -ne 0 ]] || die OLD_ROOT_ONLY_CONFIG_UNEXPECTEDLY_STARTED
grep -qi 'permission denied' "$fixture/old-permission-startup.log" || die OLD_CONFIG_FAILURE_NOT_PERMISSION_DENIED
printf 'staging-runtime-e2e=reproduced-root-0400-read-failure\n'

# Migration target: root-owned config, readable only by the app's runtime GID.
sudo chown root:10001 "$fixture/conf.toml"
sudo chmod 0440 "$fixture/conf.toml"
[[ "$(stat -c '%u:%g:%a' "$fixture/conf.toml")" == 0:10001:440 ]] || die CONFIG_METADATA_INVALID
cat >"$override_file" <<'OVERRIDE'
services:
  app:
    extra_hosts:
      - "host.docker.internal:host-gateway"
    environment:
      KITSU_HOSTNAME: "http://host.docker.internal:18082/"
OVERRIDE
cat >"$fixture/mock-kitsu.py" <<'PY'
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json

class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass
    def do_GET(self):
        if self.path == "/api/status":
            body = json.dumps({"name":"Zou","database-up":True,"event-stream-up":True,"key-value-store-up":True,"version":"fixture"}).encode()
            self.send_response(200)
        elif self.path == "/api/":
            body = b"{}"
            self.send_response(200)
        else:
            body = b"{}"
            self.send_response(401)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)
    def do_POST(self):
        if self.path != "/api/auth/login":
            self.send_response(404)
        else:
            self.rfile.read(int(self.headers.get("Content-Length", "0")))
            print("AUTH_LOGIN_REACHED", flush=True)
            self.send_response(401)
        self.send_header("Content-Length", "0")
        self.end_headers()

ThreadingHTTPServer(("0.0.0.0", 18082), Handler).serve_forever()
PY
python3 "$fixture/mock-kitsu.py" >"$mock_log" 2>&1 &
mock_pid=$!
for _ in $(seq 1 30); do
  if curl --silent --output /dev/null --max-time 1 http://127.0.0.1:18082/api/status; then break; fi
  sleep 1
done
curl --fail --silent --output /dev/null --max-time 1 http://127.0.0.1:18082/api/status || die MOCK_KITSU_NOT_READY
export KITSUSYNC_IMAGE_TAG="$image_tag"
"${compose[@]}" config -q || die COMPOSE_CONFIG_INVALID
compose_started=1
"${compose[@]}" up -d --force-recreate >/dev/null || die FIRST_START_FAILED

sed -n '/^wait_staging_http() {/,/^verify_staging_network_membership()/p' "$helper" | sed '$d' >"$fixture/wait-function.sh"
source "$fixture/wait-function.sh"
wait_routes() {
  local suffix="$1"
  wait_staging_http health "$source_sha" "$fixture/health-${suffix}.json" http://127.0.0.1:8091 30 1 || return 1
  wait_staging_http ready "$source_sha" "$fixture/ready-${suffix}.json" http://127.0.0.1:8091 30 1 || return 1
  wait_staging_http admin-users "$source_sha" "$fixture/users-${suffix}.json" http://127.0.0.1:8091 30 1 || return 1
  wait_staging_http admin-health "$source_sha" "$fixture/admin-health-${suffix}.json" http://127.0.0.1:8091 30 1 || return 1
}
wait_routes first || die FIRST_START_ROUTES_FAILED

container=kitsusync-staging
[[ "$(docker inspect --format '{{.Config.User}}' "$container")" == 10001:10001 ]] || die RUNNING_IDENTITY_INVALID
[[ "$(docker exec "$container" stat -c '%u:%g:%a' /app/conf.toml)" == 0:10001:440 ]] || die MOUNTED_CONFIG_METADATA_INVALID
docker exec --user 10001:10001 "$container" /bin/sh -ec 'test -r /app/conf.toml && test -w /app/data' || die RUNTIME_MOUNT_ACCESS_INVALID
[[ "$(docker port "$container" 8090/tcp)" == 127.0.0.1:8091 ]] || die LOOPBACK_BIND_INVALID
docker inspect --format '{{json .Config.Env}}' "$container" | python3 -c '
import json,sys
env=dict(item.split("=",1) for item in json.load(sys.stdin) if "=" in item)
assert env.get("KITSU_HOSTNAME") == "http://host.docker.internal:18082/"
assert env.get("KITSUSYNC_DISABLE_POLL") == "1"
assert not any(env.get(name,"").strip() for name in ("DISCORD_BOT_TOKEN","DISCORD_GUILD_ID","DISCORD_WEBHOOK_URL"))
' || die RUNTIME_AUTHORITY_OR_ISOLATION_INVALID
docker inspect --format '{{json .Mounts}}' "$container" | python3 -c '
import json,sys
mounts=json.load(sys.stdin)
expected={
    ("volume","kitsusync-staging-data","/app/data",True),
    ("bind",sys.argv[1],"/app/conf.toml",False),
}
actual={(m.get("Type"),m.get("Name") or m.get("Source"),m.get("Destination"),m.get("RW")) for m in mounts}
assert len(mounts)==2 and actual==expected
' "$fixture/conf.toml" || die PRODUCTION_STATE_MOUNTED
curl --silent --show-error --max-time 5 http://127.0.0.1:8091/bot/login >"$fixture/login-page.html" || die LOGIN_PAGE_FAILED
! grep -Fq 'Kitsu authentication authority is not configured' "$fixture/login-page.html" || die LOGIN_AUTHORITY_MISSING
login_code="$(curl --silent --output "$fixture/login-post.html" --write-out '%{http_code}' --max-time 10 --data-urlencode 'email=fixture@example.invalid' --data-urlencode 'password=fixture-only' http://127.0.0.1:8091/bot/login)" || die LOGIN_POST_TRANSPORT_FAILED
[[ "$login_code" == 401 ]] || die LOGIN_POST_DID_NOT_REACH_KITSU
for _ in $(seq 1 10); do grep -Fq 'AUTH_LOGIN_REACHED' "$mock_log" && break; sleep 1; done
grep -Fq 'AUTH_LOGIN_REACHED' "$mock_log" || die KITSU_AUTH_ENDPOINT_NOT_REACHED
setup_code="$(curl --silent --output /dev/null --write-out '%{http_code}' --max-time 5 http://127.0.0.1:8091/bot/setup)" || die SETUP_ROUTE_FAILED
[[ "$setup_code" == 303 ]] || die SETUP_ROUTE_NOT_SESSION_PROTECTED
printf 'staging-runtime-e2e=first-deploy-pass empty-volume=PASS trusted-hostname=PASS login-post=KITSU_AUTH_REACHED setup=session-protected credentials-state=isolated\n'

# Exercise a second deployment of the exact image, then a failed start and
# restoration of the previously working candidate through the real Compose file.
"${compose[@]}" up -d --force-recreate >/dev/null || die SECOND_START_FAILED
wait_routes second || die SECOND_START_ROUTES_FAILED
sudo chown root:root "$fixture/conf.toml"
sudo chmod 0400 "$fixture/conf.toml"
"${compose[@]}" up -d --force-recreate >/dev/null || die FAILED_START_FIXTURE_COULD_NOT_START
if wait_staging_http health "$source_sha" "$fixture/failed-compose-health.json" http://127.0.0.1:8091 5 1 2>"$fixture/failed-compose-health.log"; then
  die ROOT_ONLY_CONFIG_COMPOSE_START_UNEXPECTEDLY_HEALTHY
fi
grep -Fq 'last_http_code=000' "$fixture/failed-compose-health.log" || die ROOT_ONLY_CONFIG_HTTP_FAILURE_NOT_OBSERVED
grep -qi 'permission denied' < <(docker logs kitsusync-staging 2>&1) || die COMPOSE_CONFIG_READ_FAILURE_NOT_OBSERVED
set +e
docker run --rm --network none --mount "type=bind,src=$fixture/conf.toml,dst=/app/conf.toml,readonly" "$image_ref" >"$fixture/rollback-failure.log" 2>&1
failed_runtime_status=$?
set -e
[[ "$failed_runtime_status" -ne 0 ]] || die FAILED_START_FIXTURE_UNEXPECTEDLY_STARTED
sudo chown root:10001 "$fixture/conf.toml"
sudo chmod 0440 "$fixture/conf.toml"
"${compose[@]}" up -d --force-recreate >/dev/null || die ROLLBACK_START_FAILED
wait_routes rollback || die ROLLBACK_ROUTES_FAILED

production_after="$(docker ps -q --filter publish=8090 | sort)"
[[ "$production_after" == "$production_before" ]] || die PRODUCTION_CONTAINER_SET_CHANGED
printf 'staging-runtime-e2e=PASS second-deploy=PASS rollback=PASS production=unchanged\n'
