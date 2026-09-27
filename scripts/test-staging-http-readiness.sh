#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
helper="${root}/deploy/kitsusync-staging-deploy"
function_source="$(awk '
  /^(wait_staging_http|verify_production_unchanged)\(\) \{/ { copying = 1; found++ }
  copying { print }
  copying && /^\}/ { copying = 0 }
  END { if (found != 2) exit 1 }
' "${helper}")" || { printf 'staging-http-readiness=FAIL missing-function\n' >&2; exit 1; }
eval "${function_source}"

# Deterministic unit cases exercise curl exit/HTTP/body handling and bounded attempts.
curl_mode=health-delayed
sleep() { :; }
curl() {
  local output_file= url= path= code= body= count counter_file
  while (($#)); do
    case "$1" in
      --output) output_file="$2"; shift 2 ;;
      --write-out|--max-time) shift 2 ;;
      --silent) shift ;;
      *) url="$1"; shift ;;
    esac
  done
  path="${url#*8091}"
  counter_file="${tmp}/calls-$(printf '%s' "$path" | tr '/-' '__')"
  count=0; [[ ! -f "$counter_file" ]] || read -r count <"$counter_file"
  count=$((count + 1)); printf '%s\n' "$count" >"$counter_file"
  case "${curl_mode}:${path}:${count}" in
    health-delayed:/health:1|health-delayed:/health:2) : >"${output_file}"; return 7 ;;
    health-never:/health:*) : >"${output_file}"; return 7 ;;
    ready-transient:/ready:1) code=500; body='{}' ;;
    admin-transient:/bot/admin/users:1|admin-transient:/bot/admin/health:1) code=503; body='{}' ;;
    *)
      case "$path" in
        /health) code=200; body='{"status":"ok"}' ;;
        /ready) code=503; body='{"status":"setup_required","build":{"base_commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","build_source_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}' ;;
        /bot/admin/*) code=401; body='{}' ;;
        *) return 22 ;;
      esac
      ;;
  esac
  printf '%s' "$body" >"${output_file}"
  printf '%s' "$code"
}

sha_a=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
tmp="$(mktemp -d)"
trap 'rm -rf -- "$tmp"' EXIT
wait_staging_http health "$sha_a" "$tmp/health.json" http://127.0.0.1:8091 4 0
[[ "$(cat "$tmp/calls-_health")" -eq 3 ]] || { printf 'staging-http-readiness=FAIL health-not-retried\n' >&2; exit 1; }

curl_mode=health-never; rm -f "$tmp"/calls-*
if wait_staging_http health "$sha_a" "$tmp/health.json" http://127.0.0.1:8091 3 0 2>"$tmp/diagnostic"; then
  printf 'staging-http-readiness=FAIL unavailable-health-accepted\n' >&2; exit 1
fi
[[ "$(cat "$tmp/calls-_health")" -eq 3 ]] && grep -Fq 'STAGING_HTTP_WAIT_FAILED endpoint=health attempts=3 last_http_code=000 last_curl_status=7' "$tmp/diagnostic" || {
  printf 'staging-http-readiness=FAIL unavailable-health-diagnostic\n' >&2; exit 1;
}

curl_mode=ready-transient; rm -f "$tmp"/calls-*
wait_staging_http ready "$sha_a" "$tmp/ready.json" http://127.0.0.1:8091 4 0
[[ "$(cat "$tmp/calls-_ready")" -eq 2 ]] || { printf 'staging-http-readiness=FAIL ready-not-retried\n' >&2; exit 1; }

curl_mode=admin-transient; rm -f "$tmp"/calls-*
wait_staging_http admin-users "$sha_a" "$tmp/users.json" http://127.0.0.1:8091 4 0
wait_staging_http admin-health "$sha_a" "$tmp/admin-health.json" http://127.0.0.1:8091 4 0
[[ "$(cat "$tmp/calls-_bot_admin_users")" -eq 2 && "$(cat "$tmp/calls-_bot_admin_health")" -eq 2 ]] || {
  printf 'staging-http-readiness=FAIL admin-routes-not-retried\n' >&2; exit 1;
}
unset -f curl sleep
printf 'staging-http-readiness-unit=PASS\n'

[[ "${GITHUB_ACTIONS:-}" == true && "${RUNNER_ENVIRONMENT:-}" == github-hosted && "${RUNNER_OS:-}" == Linux && "${EUID}" -ne 0 ]] || {
  if [[ "${STAGING_REQUIRE_DOCKER_FIXTURE:-0}" == 1 ]]; then
    printf 'staging-http-docker-fixture=FAIL hosted-linux-runner-required\n' >&2; exit 1
  fi
  printf 'staging-http-docker-fixture=SKIP hosted-linux-runner-required\n'
  printf 'staging-http-readiness=PASS\n'
  exit 0
}
command -v docker >/dev/null && command -v curl >/dev/null || { printf 'staging-http-docker-fixture=FAIL docker-or-curl-unavailable\n' >&2; exit 1; }
docker info >/dev/null 2>&1 || { printf 'staging-http-docker-fixture=FAIL daemon-unavailable\n' >&2; exit 1; }

fixture="$(mktemp -d)"
project_suffix="${BASHPID}"
prod_project="kitsusync-prod-readiness-${project_suffix}"
stage_project="kitsusync-stage-readiness-${project_suffix}"
prod_port=8090
stage_port=8091
python3 - "$prod_port" "$stage_port" <<'PY'
import socket, sys
for value in sys.argv[1:]:
    sock=socket.socket()
    try: sock.bind(("127.0.0.1", int(value)))
    except OSError: raise SystemExit(f"required disposable fixture port is occupied: {value}")
    finally: sock.close()
PY
cleanup_fixture() {
  docker compose --project-name "$stage_project" --file "$fixture/compose.yml" down --volumes --remove-orphans >/dev/null 2>&1 || true
  docker compose --project-name "$prod_project" --file "$fixture/compose.yml" down --volumes --remove-orphans >/dev/null 2>&1 || true
  docker image rm "kitsusync-staging-readiness:${project_suffix}" >/dev/null 2>&1 || true
  rm -rf -- "$fixture"
}
trap cleanup_fixture EXIT
cat >"$fixture/Dockerfile" <<'DOCKER'
FROM python:3.12-alpine
COPY server.py /server.py
CMD ["python", "/server.py"]
DOCKER
cat >"$fixture/server.py" <<'PY'
import http.server, json, os, time
time.sleep(float(os.environ.get("START_DELAY", "0")))
source = os.environ.get("SOURCE_SHA", "")
failed = os.environ.get("FAIL_MODE", "0") == "1"
class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if failed:
            code, body = 500, {"status":"error"}
        elif self.path == "/health":
            code, body = 200, {"status":"ok"}
        elif self.path == "/ready":
            code, body = 503, {"status":"setup_required", "build":{"base_commit":source,"build_source_id":source}}
        elif self.path in ("/bot/admin/users", "/bot/admin/health"):
            code, body = 401, {}
        else:
            code, body = 404, {}
        raw = json.dumps(body, separators=(",", ":")).encode()
        self.send_response(code); self.send_header("Content-Type", "application/json"); self.end_headers(); self.wfile.write(raw)
    def log_message(self, *_): pass
http.server.ThreadingHTTPServer(("0.0.0.0", 8090), Handler).serve_forever()
PY
cat >"$fixture/compose.yml" <<'YAML'
services:
  app:
    image: "kitsusync-staging-readiness:${FIXTURE_TAG}"
    command: ["python", "/server.py"]
    environment:
      START_DELAY: "${START_DELAY:-0}"
      SOURCE_SHA: "${SOURCE_SHA:-}"
      FAIL_MODE: "${FAIL_MODE:-0}"
    ports:
      - "127.0.0.1:${FIXTURE_PORT}:8090"
    volumes:
      - readiness_data:/data
volumes:
  readiness_data:
    name: "${FIXTURE_PROJECT}-data"
YAML
docker build --tag "kitsusync-staging-readiness:${project_suffix}" "$fixture" >/dev/null
prod_env="$fixture/production.env"
printf 'FIXTURE_TAG=%s\nFIXTURE_PORT=%s\nFIXTURE_PROJECT=%s\nSOURCE_SHA=%s\nSTART_DELAY=0\nFAIL_MODE=0\n' \
  "$project_suffix" "$prod_port" "$prod_project" "$sha_a" >"$prod_env"
docker compose --project-name "$prod_project" --env-file "$prod_env" --file "$fixture/compose.yml" up -d >/dev/null
prod_id="$(docker compose --project-name "$prod_project" --env-file "$prod_env" --file "$fixture/compose.yml" ps -q app)"
[[ "$prod_id" =~ ^[0-9a-f]{12,64}$ ]]
prod_container_id="$(docker inspect --format '{{.Id}}' "$prod_id")"
prod_published="$(docker port "$prod_id" 8090/tcp)"
[[ "$prod_published" == "127.0.0.1:${prod_port}" ]]
prod_before="$(docker ps -q --filter publish=8090 | sort)"
verify_production_unchanged
assert_production_unchanged() {
  verify_production_unchanged
  [[ "$(docker inspect --format '{{.Id}}' "$prod_id")" == "$prod_container_id" ]]
  [[ "$(docker port "$prod_id" 8090/tcp)" == "127.0.0.1:${prod_port}" ]]
  [[ "$(docker ps -aq --filter "label=com.docker.compose.project=${stage_project}" | wc -l)" -eq 1 ]]
}

candidate_a=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
candidate_b=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
candidate_bad=cccccccccccccccccccccccccccccccccccccccc
stage_env="$fixture/staging.env"
write_stage_env() { printf 'FIXTURE_TAG=%s\nFIXTURE_PORT=%s\nFIXTURE_PROJECT=%s\nSOURCE_SHA=%s\nSTART_DELAY=%s\nFAIL_MODE=%s\n' \
  "$project_suffix" "$stage_port" "$stage_project" "$1" "$2" "$3" >"$stage_env"; }
write_stage_env "$candidate_a" 3 0
docker compose --project-name "$stage_project" --env-file "$stage_env" --file "$fixture/compose.yml" up -d --force-recreate >/dev/null
stage_id_a="$(docker compose --project-name "$stage_project" --env-file "$stage_env" --file "$fixture/compose.yml" ps -q app)"
curl --silent --output /dev/null --max-time 1 "http://127.0.0.1:${stage_port}/health" >/dev/null 2>&1 && {
  printf 'staging-http-docker-fixture=FAIL expected-initial-connection-refused\n' >&2; exit 1;
}
wait_staging_http health "$candidate_a" "$fixture/health-a.json" "http://127.0.0.1:${stage_port}" 30 1
wait_staging_http ready "$candidate_a" "$fixture/ready-a.json" "http://127.0.0.1:${stage_port}" 30 1
wait_staging_http admin-users "$candidate_a" "$fixture/users-a.json" "http://127.0.0.1:${stage_port}" 30 1
wait_staging_http admin-health "$candidate_a" "$fixture/admin-health-a.json" "http://127.0.0.1:${stage_port}" 30 1
assert_production_unchanged
printf 'staging-http-docker-fixture=PASS scenario=first-deploy-initial-refusal-recovered\n'

write_stage_env "$candidate_b" 0 0
docker compose --project-name "$stage_project" --env-file "$stage_env" --file "$fixture/compose.yml" up -d --force-recreate >/dev/null
stage_id_b="$(docker compose --project-name "$stage_project" --env-file "$stage_env" --file "$fixture/compose.yml" ps -q app)"
[[ -n "$stage_id_b" && "$stage_id_b" != "$stage_id_a" ]]
wait_staging_http health "$candidate_b" "$fixture/health-b.json" "http://127.0.0.1:${stage_port}" 30 1
wait_staging_http ready "$candidate_b" "$fixture/ready-b.json" "http://127.0.0.1:${stage_port}" 30 1
wait_staging_http admin-users "$candidate_b" "$fixture/users-b.json" "http://127.0.0.1:${stage_port}" 30 1
wait_staging_http admin-health "$candidate_b" "$fixture/admin-health-b.json" "http://127.0.0.1:${stage_port}" 30 1
assert_production_unchanged
printf 'staging-http-docker-fixture=PASS scenario=second-deploy\n'

write_stage_env "$candidate_bad" 0 1
docker compose --project-name "$stage_project" --env-file "$stage_env" --file "$fixture/compose.yml" up -d --force-recreate >/dev/null
if wait_staging_http health "$candidate_bad" "$fixture/health-bad.json" "http://127.0.0.1:${stage_port}" 3 0; then
  printf 'staging-http-docker-fixture=FAIL accepted-failed-candidate\n' >&2; exit 1;
fi
write_stage_env "$candidate_b" 0 0
docker compose --project-name "$stage_project" --env-file "$stage_env" --file "$fixture/compose.yml" up -d --force-recreate >/dev/null
wait_staging_http health "$candidate_b" "$fixture/health-rollback.json" "http://127.0.0.1:${stage_port}" 30 1
wait_staging_http ready "$candidate_b" "$fixture/ready-rollback.json" "http://127.0.0.1:${stage_port}" 30 1
wait_staging_http admin-users "$candidate_b" "$fixture/users-rollback.json" "http://127.0.0.1:${stage_port}" 30 1
wait_staging_http admin-health "$candidate_b" "$fixture/admin-health-rollback.json" "http://127.0.0.1:${stage_port}" 30 1
assert_production_unchanged
printf 'staging-http-docker-fixture=PASS scenario=rollback-and-production-unchanged\n'
printf 'staging-http-readiness=PASS\n'
