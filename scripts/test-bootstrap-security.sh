#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
deploy="${root}/deploy/kitsusync-deploy"
inspect="${root}/deploy/kitsusync-inspect"
bootstrap="${root}/deploy/kitsusync-bootstrap"
backup="${root}/deploy/kitsusync-sqlite-backup"
identity="${root}/deploy/kitsusync-image-identity"
bundle="${root}/scripts/build-deployment-bundle.sh"
tmp="$(mktemp -d)"
python_bin="${PYTHON_BIN:-python3}"
trap 'rm -rf "${tmp}"' EXIT

for script in "${deploy}" "${inspect}" "${bootstrap}" "${bundle}"; do bash -n "${script}"; done
"${python_bin}" -c 'import pathlib,sys; compile(pathlib.Path(sys.argv[1]).read_text(), sys.argv[1], "exec")' "${backup}"
"${python_bin}" -c 'import pathlib,sys; compile(pathlib.Path(sys.argv[1]).read_text(), sys.argv[1], "exec")' "${identity}"

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

backup_line="$(grep -n ': >"${backup_dir}/backup-complete"' "${deploy}" | cut -d: -f1)"
stop_line="$(grep -n '${DOCKER_BIN} stop' "${deploy}" | cut -d: -f1)"
load_line="$(grep -n '${DOCKER_BIN} load' "${deploy}" | cut -d: -f1)"
[[ -n "${backup_line}" && "${backup_line}" -lt "${stop_line}" && "${stop_line}" -lt "${load_line}" ]] || { printf 'backup/mutation ordering is unsafe\n' >&2; exit 1; }

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
