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
       /usr/local/libexec/kitsusync-runtime-state /usr/local/libexec/kitsusync-restore-state
       /usr/local/libexec/kitsusync-preflight-collect)
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
source_commit=b7b30157cb90c4500e8b00d3c26ac7038f5c8c10
stage=fixture-setup
cleanup() {
  local result="$1" failed_command="$2" ids
  if [[ "$result" -ne 0 ]]; then
    printf 'deployment-transaction-failed-stage=%s\n' "$stage" >&2
    printf 'deployment-transaction-failed-command=%s\n' "$failed_command" >&2
    for report in "$work/failed-deploy.log" "$work/successful-deploy.log" \
                  "$work/setup-required-deploy.log" "$work/enospc-deploy.log" \
                  "$work/enospc-recovery-failed.log"; do
      [[ ! -f "$report" ]] || grep -E '^(ERROR:|runtime validation failed:|rollback=)' "$report" \
        | sed "s#^#$(basename "$report"): #" >&2 || true
    done
  fi
  ids="$(docker ps -aq --filter label=com.docker.compose.project=kitsusync)"
  if [[ -n "$ids" ]]; then docker rm -f $ids >/dev/null 2>&1 || true; fi
  docker network rm kitsusync_default >/dev/null 2>&1 || true
  docker image rm kitsusync:v0.4.6 "$legacy_ref" >/dev/null 2>&1 || true
  # All fixed paths were proven absent before this disposable fixture created them.
  sudo rm -rf -- "${protected[@]}"
  sudo rm -f -- "${tools[@]}"
  rm -rf -- "$work"
}
trap 'cleanup "$?" "$BASH_COMMAND"' EXIT

cat >"$work/fixture.py" <<'PY'
import http.server, json, os, pathlib, sqlite3, sys, time
legacy = '--legacy' in sys.argv
data = pathlib.Path('/app/data')
fail_health_at_start = (data / 'fail-health-on-start').exists()
if fail_health_at_start:
    (data / 'fail-health-on-start').unlink()
assert pathlib.Path('/app/conf.toml').read_text() == 'fixture-conf\n'
assert pathlib.Path('/app/tpl/marker').read_text() == 'fixture-template\n'
assert os.getuid() == 10001 and os.getgid() == 10001
assert os.environ['APP_ENV'] == 'production'
# Keep an uncheckpointed committed WAL frame alive until Docker stops this
# process. The deploy backup must use SQLite's online backup API, not copy files.
wal_db = sqlite3.connect(data / 'sqlite.db')
wal_db.execute('PRAGMA journal_mode=WAL')
wal_db.execute('PRAGMA wal_autocheckpoint=0')
wal_db.execute("UPDATE evidence SET value='original'")
wal_db.commit()
failed_target = not legacy and (data / 'fail-target').exists()
setup_required_after_restart = not legacy and not failed_target and (data / 'setup-required-after-restart').exists()
if failed_target:
    with sqlite3.connect(data / 'sqlite.db') as db:
        db.execute("UPDATE evidence SET value='target-mutated'")
    (data / 'runtime-secret.key').write_text('target-mutated\n')
    (data / 'fail-target').unlink()
started = time.monotonic()
class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == '/health':
            code, body = (503, {'status':'injected_failure'}) if fail_health_at_start else (200, {'status':'ok'})
        elif self.path == '/ready':
            if legacy: code, body = 404, {}
            else:
                ready = time.monotonic() - started >= 7
                state = 'setup_required' if setup_required_after_restart else ('ready' if ready else 'setup_required')
                code = 503 if state == 'setup_required' else 200
                body = {'status':state,
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
cp "$root/docker-compose.yml" "$work/compose.yml"
cp "$work/operator.env" "$work/.env.local"
# Reproduce the recovered legacy runtime precisely: it carries the KitsuSync
# project/service labels but not Compose's config bookkeeping.
# Give the disposable network the metadata Compose expects while keeping the
# recovered container itself deliberately incomplete (no config-hash).
docker network create \
  --label com.docker.compose.network=default \
  --label com.docker.compose.project=kitsusync \
  kitsusync_default >/dev/null
# Pin the fixture endpoint MAC so the transaction test exercises exact
# rollback identity rather than a daemon-generated address that varies by engine.
prior_id="$(docker run -d --name kitsusync-app-1 \
  --network kitsusync_default --network-alias kitsusync-app \
  --mac-address 02:11:22:33:44:55 \
  --restart on-failure:3 \
  --env APP_ENV=production --env OLD_ONLY=preserved \
  --label com.docker.compose.project=kitsusync \
  --label com.docker.compose.service=app \
  --label kitsusync.fixture-contract=legacy \
  --mount "type=bind,src=$runtime/conf.toml,dst=/app/conf.toml,readonly" \
  --mount "type=bind,src=$runtime/tpl,dst=/app/tpl,readonly" \
  --mount "type=bind,src=$runtime/data,dst=/app/data" \
  --mount "type=bind,src=$work/extra,dst=/legacy-extra,readonly" \
  --publish 127.0.0.1:8090:8090 \
  --health-cmd 'curl -fsS http://localhost:8090/health' \
  --health-interval 1s --health-timeout 1s --health-start-period 1s --health-retries 5 \
  "$legacy_ref" python /fixture.py --legacy)"
[[ -z "$(docker inspect -f '{{index .Config.Labels "com.docker.compose.config-hash"}}' "$prior_id")" ]]
compose_discovery="$(KITSUSYNC_APP_VERSION=0.4.6 KITSUSYNC_IMAGE_TAG=v0.4.6 KITSUSYNC_COMMIT_SHA="$source_commit" KITSUSYNC_BUILD_SOURCE_ID="$source_commit" KITSUSYNC_IMAGE_REVISION="$source_commit" KITSUSYNC_WORKTREE_DIRTY=false docker compose -p kitsusync --env-file "$work/operator.env" -f "$work/compose.yml" ps -q app)"
[[ -z "${compose_discovery}" ]]
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

# Inject ENOSPC after the prior container is stopped but before any runtime
# or database mutation. The live SQLite file must remain untouched, the same
# prior container must be restartable, and only this invocation's incomplete
# backup directory may be removed.
stage=quiesced-backup-enospc
cat >"$work/enospc-sqlite-backup" <<'PY'
import errno, os, sys
try:
    fd = os.open(sys.argv[2], os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    os.write(fd, b"partial snapshot")
    os.close(fd)
    raise OSError(errno.ENOSPC, "injected backup filesystem exhaustion")
except OSError:
    print("ERROR: SQLite online backup failed", file=sys.stderr)
    raise SystemExit(1)
PY
chmod 0600 "$work/enospc-sqlite-backup"
fake_backup_sha="$(sha256sum "$work/enospc-sqlite-backup" | cut -d' ' -f1)"
sed "s/^sqlite_backup_tool_sha256=.*/sqlite_backup_tool_sha256=${fake_backup_sha}/" \
  "$work/bundle/provenance.txt" >"$work/enospc-provenance"
sudo install -o root -g root -m 0600 "$work/enospc-provenance" /etc/kitsusync-deploy/provenance
sudo install -o root -g root -m 0700 "$work/enospc-sqlite-backup" /usr/local/libexec/kitsusync-sqlite-backup
backup_count_before="$(sudo find /var/backups/kitsusync-deploy -mindepth 1 -maxdepth 1 -type d | wc -l)"
if sudo /usr/bin/env -i PATH=/usr/bin:/bin /usr/local/sbin/kitsusync-deploy >"$work/enospc-deploy.log" 2>&1; then
  printf 'quiesced backup ENOSPC incorrectly proceeded\n' >&2; exit 1
fi
grep -Fq 'quiesced SQLite backup failed' "$work/enospc-deploy.log"
[[ "$(docker inspect -f '{{.State.Running}}' "$prior_id")" == true ]]
[[ "$(sudo python3 - "$runtime/data/sqlite.db" <<'PYDB'
import sqlite3, sys
with sqlite3.connect('file:'+sys.argv[1]+'?mode=ro',uri=True) as db:
 print(db.execute('SELECT value FROM evidence').fetchone()[0])
PYDB
)" == original ]]
[[ "$(sudo find /var/backups/kitsusync-deploy -mindepth 1 -maxdepth 1 -type d | wc -l)" == "$backup_count_before" ]]
sudo install -o root -g root -m 0600 "$work/bundle/provenance.txt" /etc/kitsusync-deploy/provenance
sudo install -o root -g root -m 0700 "$work/bundle/kitsusync-sqlite-backup" /usr/local/libexec/kitsusync-sqlite-backup

# If injected ENOSPC is followed by a failed old-runtime health validation,
# retain the partial backup and report that recovery is incomplete.
stage=quiesced-backup-enospc-recovery-validation-failure
sed "s/^sqlite_backup_tool_sha256=.*/sqlite_backup_tool_sha256=${fake_backup_sha}/" \
  "$work/bundle/provenance.txt" >"$work/enospc-provenance"
sudo install -o root -g root -m 0600 "$work/enospc-provenance" /etc/kitsusync-deploy/provenance
sudo install -o root -g root -m 0700 "$work/enospc-sqlite-backup" /usr/local/libexec/kitsusync-sqlite-backup
sudo touch "$runtime/data/fail-health-on-start"
backup_count_before="$(sudo find /var/backups/kitsusync-deploy -mindepth 1 -maxdepth 1 -type d | wc -l)"
if sudo /usr/bin/env -i PATH=/usr/bin:/bin /usr/local/sbin/kitsusync-deploy >"$work/enospc-recovery-failed.log" 2>&1; then
  printf 'failed old-runtime validation after backup ENOSPC incorrectly passed\n' >&2; exit 1
fi
grep -Fq 'automatic recovery incomplete; protected backup=' "$work/enospc-recovery-failed.log"
[[ "$(docker inspect -f '{{.State.Running}}' "$prior_id")" == true ]]
[[ "$(sudo find /var/backups/kitsusync-deploy -mindepth 1 -maxdepth 1 -type d | wc -l)" == "$((backup_count_before + 1))" ]]
retained_partial="$(sudo find /var/backups/kitsusync-deploy -mindepth 1 -maxdepth 1 -type d -name '*-*' -printf '%T@ %p\n' | sort -nr | head -n 1 | cut -d' ' -f2-)"
sudo test -s "$retained_partial/sqlite.quiesced.db"
sudo test ! -e "$retained_partial/backup-complete"
docker restart "$prior_id" >/dev/null
for attempt in $(seq 1 30); do
  [[ "$(docker inspect -f '{{.State.Health.Status}}' "$prior_id")" == healthy ]] && break
  sleep 1
done
[[ "$(docker inspect -f '{{.State.Health.Status}}' "$prior_id")" == healthy ]]
sudo install -o root -g root -m 0600 "$work/bundle/provenance.txt" /etc/kitsusync-deploy/provenance
sudo install -o root -g root -m 0700 "$work/bundle/kitsusync-sqlite-backup" /usr/local/libexec/kitsusync-sqlite-backup

sudo touch "$runtime/data/fail-target"
stage=failed-target-deploy
if sudo /usr/bin/env -i PATH=/usr/bin:/bin /usr/local/sbin/kitsusync-deploy >"$work/failed-deploy.log" 2>&1; then
  printf 'wrong target readiness identity incorrectly passed\n' >&2; exit 1
fi
stage=rollback-contract
grep -Fq 'runtime validation failed: check=readiness_identity' "$work/failed-deploy.log"
grep -Fxq 'rollback=verified' "$work/failed-deploy.log"
stage=rollback-restored-container
restored_id="$(docker ps -q --filter name='^/kitsusync-app-1$')"
[[ -n "$restored_id" && "$restored_id" != "$prior_id" ]]
stage=rollback-image
[[ "$(docker inspect -f '{{.Image}}' "$restored_id")" == "$prior_image" ]]
stage=rollback-runtime-plan
docker inspect "$restored_id" >"$work/restored-inspect.json"
python3 "$root/deploy/kitsusync-runtime-state" compare "$work/prior-plan.json" "$work/restored-inspect.json"
stage=rollback-secret
sudo cmp -s "$work/key" "$runtime/data/runtime-secret.key"
stage=rollback-sqlite
sudo python3 - "$runtime/data/sqlite.db" <<'PY'
import sqlite3, sys
with sqlite3.connect('file:'+sys.argv[1]+'?mode=ro',uri=True) as db:
    assert db.execute('SELECT value FROM evidence').fetchall() == [('original',)]
PY
stage=rollback-readiness
[[ "$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8090/ready)" == 404 ]]
stage=rollback-legacy-image
sudo test -f /etc/kitsusync-deploy/legacy-image.id
stage=rollback-mode
[[ "$(sudo cat /etc/kitsusync-deploy/deployment-mode)" == legacy-migration ]]

# The restored old state can subsequently migrate successfully. The fixture's
# initial setup_required interval must be awaited, never accepted as success.
sudo rm -f -- "$runtime/data/fail-target"
stage=successful-deploy
sudo /usr/bin/env -i PATH=/usr/bin:/bin /usr/local/sbin/kitsusync-deploy >"$work/successful-deploy.log" 2>&1
stage=successful-contract
grep -Fq 'KitsuSync deployment completed: version=0.4.6' "$work/successful-deploy.log"
[[ "$(sudo cat /etc/kitsusync-deploy/deployment-mode)" == normal ]]
sudo test ! -e /etc/kitsusync-deploy/legacy-image.id
[[ "$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8090/ready)" == 200 ]]
sudo cmp -s "$work/key" "$runtime/data/runtime-secret.key"

# Reproduce the production rollback edge with a Compose-managed normal prior
# runtime. The target mutates SQLite and the secret, then fails identity. On
# reconstruction the prior runtime resumes safely as setup_required.
stage=setup-required-fixture
prior_normal_id="$(docker ps -q --filter name='^/kitsusync-app-1$')"
[[ -n "$prior_normal_id" ]]
prior_normal_image="$(docker inspect -f '{{.Image}}' "$prior_normal_id")"
template_before="$(sudo sha256sum "$runtime/tpl/marker")"
sudo touch "$runtime/data/setup-required-after-restart" "$runtime/data/fail-target"
stage=failed-normal-deploy
if sudo /usr/bin/env -i PATH=/usr/bin:/bin /usr/local/sbin/kitsusync-deploy >"$work/setup-required-deploy.log" 2>&1; then
  printf 'wrong target readiness identity incorrectly passed on normal prior runtime\n' >&2; exit 1
fi
stage=setup-required-rollback-contract
grep -Fq 'runtime validation failed: check=readiness_identity' "$work/setup-required-deploy.log"
grep -Fxq 'rollback=verified' "$work/setup-required-deploy.log"
! grep -Fq 'automatic recovery incomplete' "$work/setup-required-deploy.log"
stage=setup-required-restored-container
restored_setup_id="$(docker ps -q --filter name='^/kitsusync-app-1$')"
[[ -n "$restored_setup_id" && "$restored_setup_id" != "$prior_normal_id" ]]
[[ "$(docker inspect -f '{{.Image}}' "$restored_setup_id")" == "$prior_normal_image" ]]
stage=setup-required-restored-state
sudo cmp -s "$work/key" "$runtime/data/runtime-secret.key"
sudo python3 - "$runtime/data/sqlite.db" <<'PY'
import sqlite3, sys
with sqlite3.connect('file:'+sys.argv[1]+'?mode=ro',uri=True) as db:
    assert db.execute('SELECT value FROM evidence').fetchall() == [('original',)]
PY
[[ "$(sudo sha256sum "$runtime/tpl/marker")" == "$template_before" ]]
stage=setup-required-readiness
[[ "$(curl -s -o "$work/ready.json" -w '%{http_code}' http://127.0.0.1:8090/ready)" == 503 ]]
grep -Fq '"status":"setup_required"' "$work/ready.json"
[[ "$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8090/health)" == 200 ]]
stage=setup-required-backup
latest_backup="$(sudo find /var/backups/kitsusync-deploy -mindepth 1 -maxdepth 1 -type d -name '*-*' -printf '%T@ %p\n' | sort -nr | head -n 1 | cut -d' ' -f2-)"
[[ -n "$latest_backup" ]]
sudo test -f "$latest_backup/backup-complete"
[[ "$(sudo find "$latest_backup/runtime" -mindepth 1 -maxdepth 1 -type f -printf '%f\n')" == plan.json ]]
sudo python3 - "$latest_backup/sqlite.db" <<'PY'
import sqlite3, sys
with sqlite3.connect('file:'+sys.argv[1]+'?immutable=1&mode=ro',uri=True) as db:
    assert db.execute('PRAGMA integrity_check').fetchall() == [('ok',)]
    assert db.execute('SELECT value FROM evidence').fetchall() == [('original',)]
PY
sudo test ! -e "$latest_backup/restored-container"
sudo test ! -e "$latest_backup/restored-inspect.json"
sudo test ! -e "$latest_backup/failed-container-inspect.json"
sudo test ! -e "$latest_backup/probe-inspect.json"
sudo test -f "$latest_backup/rollback-image-ref"
printf 'deployment-transaction-tests=PASS (real wrapper failure/rollback and subsequent deployment)\n'
