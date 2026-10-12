#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
deploy="${root}/deploy/kitsusync-deploy"
inspect="${root}/deploy/kitsusync-inspect"
bootstrap="${root}/deploy/kitsusync-bootstrap"
backup="${root}/deploy/kitsusync-sqlite-backup"
identity="${root}/deploy/kitsusync-image-identity"
preflight="${root}/deploy/kitsusync-preflight-collect"
bundle="${root}/scripts/build-deployment-bundle.sh"
tmp="$(mktemp -d)"
python_bin="${PYTHON_BIN:-python3}"
trap 'rm -rf "${tmp}"' EXIT

for script in "${deploy}" "${inspect}" "${bootstrap}" "${bundle}"; do bash -n "${script}"; done
"${python_bin}" -c 'import pathlib,sys; compile(pathlib.Path(sys.argv[1]).read_text(), sys.argv[1], "exec")' "${backup}"
"${python_bin}" -c 'import pathlib,sys; compile(pathlib.Path(sys.argv[1]).read_text(), sys.argv[1], "exec")' "${identity}"
"${python_bin}" -c 'import pathlib,sys; compile(pathlib.Path(sys.argv[1]).read_text(), sys.argv[1], "exec")' "${preflight}"

require() { grep -Fq -- "$1" "$2" || { printf 'missing bootstrap security contract: %s (%s)\n' "$1" "$2" >&2; exit 1; }; }
for script in "${deploy}" "${inspect}" "${bootstrap}"; do
  [[ "$(head -n 1 "${script}")" == '#!/bin/bash' ]]
  require 'PATH=/usr/sbin:/usr/bin:/sbin:/bin' "${script}"
  require 'arguments are not accepted' "${script}"
done
require 'DOCKER_*|COMPOSE_*' "${deploy}"
require 'protected environment contains Docker/Compose control variables' "${deploy}"
require '--project-name "${PROJECT_NAME}"' "${deploy}"
require 'CONTROL_DIR=/etc/kitsusync-deploy' "${deploy}"
require 'STATE_DIR=/var/lib/kitsusync-deploy' "${deploy}"
require 'BACKUP_ROOT=/var/backups/kitsusync-deploy' "${deploy}"
require 'secure_file' "${deploy}"
require '! -L' "${deploy}"
require 'backup-complete' "${deploy}"
require 'prior immutable image identity' "${deploy}"
require 'legacy-migration' "${deploy}"
require 'legacy-image.id' "${deploy}"
require 'legacy_status_allows' "${deploy}"
require 'fresh-install' "${deploy}"
require 'fresh-install' "${bootstrap}"
require 'fresh-install' "${inspect}"
require 'FRESH_CONF_SHA256' "${bundle}"
require 'backup_is_complete' "${deploy}"
require 'sqlite.db-wal' "${deploy}"
require 'sqlite.db-shm' "${deploy}"
require 'NOSETENV:' "${bootstrap}"
require '/usr/local/sbin/kitsusync-deploy ""' "${bootstrap}"
require '/usr/local/sbin/kitsusync-inspect ""' "${bootstrap}"
require "sed 's/=.*//'" "${inspect}"
require 'command_arguments=[redacted]' "${inspect}"
require 'docker save' "${bundle}"
require 'docker load' "${bundle}"
require 'image_archive_sha256' "${deploy}"
require 'image_content_digest' "${deploy}"
require '/usr/local/libexec/kitsusync-image-identity' "${deploy}"
require '/usr/local/libexec/kitsusync-image-identity' "${bootstrap}"
require 'kitsusync-image-identity' "${bundle}"
require '/usr/local/libexec/kitsusync-runtime-state' "${deploy}"
require '/usr/local/libexec/kitsusync-restore-state' "${deploy}"
require 'kitsusync-runtime-state' "${bootstrap}"
require 'kitsusync-restore-state' "${bootstrap}"
require 'RUNTIME_STATE_TOOL_SHA256' "${bundle}"
require 'RESTORE_STATE_TOOL_SHA256' "${bundle}"
require '/usr/local/libexec/kitsusync-preflight-collect' "${inspect}"
require 'kitsusync-preflight-collect' "${bootstrap}"
require 'PREFLIGHT_COLLECTOR_TOOL_SHA256' "${bundle}"
require 'preflight_evidence_json=' "${inspect}"

for script in "${deploy}" "${inspect}" "${bootstrap}"; do
  if "${script}" unexpected >/dev/null 2>"${tmp}/error"; then
    printf 'script accepted an argument: %s\n' "${script}" >&2; exit 1
  fi
  grep -Fq 'arguments are not accepted' "${tmp}/error"
  if DOCKER_HOST=tcp://attacker.invalid "${script}" >/dev/null 2>"${tmp}/error"; then
    printf 'script accepted Docker environment injection: %s\n' "${script}" >&2; exit 1
  fi
  grep -Fq 'inherited Docker/Compose control variables are not accepted' "${tmp}/error"
done

stop_line="$(grep -n '^${DOCKER_BIN} stop "${container_id}"' "${deploy}" | cut -d: -f1)"
load_line="$(grep -n '${DOCKER_BIN} load' "${deploy}" | cut -d: -f1 | head -n 1)"
sqlite_line="$(grep -n 'sqlite_backup "${RUNTIME_ROOT}/data/sqlite.db"' "${deploy}" | cut -d: -f1)"
backup_line="$(grep -n ': >"${backup_dir}/backup-complete"' "${deploy}" | cut -d: -f1)"
replacement_line="$(grep -n 'if ! compose_override up -d --no-deps --force-recreate' "${deploy}" | cut -d: -f1)"
[[ -n "${load_line}" && -n "${stop_line}" && -n "${sqlite_line}" && -n "${backup_line}" && -n "${replacement_line}" \
  && "${load_line}" -lt "${stop_line}" && "${stop_line}" -lt "${sqlite_line}" \
  && "${sqlite_line}" -lt "${backup_line}" && "${backup_line}" -lt "${replacement_line}" ]] || {
  printf 'deployment recovery ordering is unsafe\n' >&2; exit 1;
}

"${python_bin}" - "${tmp}/live.db" <<'PY'
import sqlite3, sys
db = sqlite3.connect(sys.argv[1])
db.execute("PRAGMA journal_mode=WAL")
db.execute("CREATE TABLE proof(value TEXT)")
db.execute("INSERT INTO proof VALUES ('present')")
db.commit()
db.close()
PY
"${python_bin}" "${backup}" "${tmp}/live.db" "${tmp}/backup.db" | grep -Fxq 'sqlite-backup=verified'
"${python_bin}" - "${tmp}/backup.db" <<'PY'
import sqlite3, sys
db = sqlite3.connect(f"file:{sys.argv[1]}?mode=ro", uri=True)
assert db.execute("PRAGMA integrity_check").fetchone() == ("ok",)
assert db.execute("SELECT count(*) FROM proof").fetchone() == (1,)
PY

if "${python_bin}" "${backup}" "${tmp}/live.db" "${tmp}/backup.db" >/dev/null 2>&1; then
  printf 'SQLite helper overwrote an existing backup\n' >&2
  exit 1
fi
ln -s "${tmp}/live.db" "${tmp}/link.db"
if [[ -L "${tmp}/link.db" ]] && "${python_bin}" "${backup}" "${tmp}/link.db" "${tmp}/other.db" >/dev/null 2>&1; then
  printf 'SQLite helper accepted a source symlink\n' >&2
  exit 1
fi

printf 'bootstrap-security-tests=PASS\n'
