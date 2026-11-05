#!/usr/bin/env bash
# Verifies the root-run staging helper isolates its Python from the caller's
# working directory and environment (cd / plus python3 -I on every call).
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
helper="${root}/deploy/kitsusync-staging-deploy"

fail() { printf 'staging-python-isolation=FAIL %s\n' "$1" >&2; exit 1; }

# Resolve to a path now so the python3() test wrapper below cannot recurse.
python_bin="$(command -v "${TEST_PYTHON:-python3}")" || fail python-unavailable

# Static: the helper leaves the caller's directory before any Python runs.
cd_line="$(tr -d '\r' <"${helper}" | grep -nx 'cd /' | head -n1 | cut -d: -f1)"
first_python_line="$(tr -d '\r' <"${helper}" | grep -n 'python3' | head -n1 | cut -d: -f1)"
[[ -n "${cd_line}" && -n "${first_python_line}" && "${cd_line}" -lt "${first_python_line}" ]] || fail cd-root-missing

# Static: every python3 invocation (bare or /usr/bin) runs in isolated mode.
unisolated="$(tr -d '\r' <"${helper}" | grep -E '(^|[^[:alnum:]_.-])(/usr/bin/)?python3([[:space:]]|$)' | grep -vE '(/usr/bin/)?python3 -I( |$)' || true)"
[[ -z "${unisolated}" ]] || fail "python3-without-isolated-mode: ${unisolated}"

# Dynamic: a json.py planted in the working directory must not be imported.
function_source="$(tr -d '\r' <"${helper}" | awk '
  /^verify_staging_network_name\(\) \{/ { copying = 1; found++ }
  copying { print }
  copying && /^\}/ { copying = 0 }
  END { if (found != 1) exit 1 }
')" || fail missing-function
eval "${function_source}"

sandbox="$(mktemp -d)"
trap 'rm -rf -- "${sandbox}"' EXIT
planted="${sandbox}/cwd"
mkdir "${planted}"
marker="${planted}/planted-json-imported"
printf 'open("planted-json-imported", "w").close()\nraise ImportError("planted json.py")\n' >"${planted}/json.py"

docker() { printf '%s\n' '{"kitsusync-staging-network":{"NetworkID":"network-id"}}'; }
python3() { "${python_bin}" "$@"; }

(cd "${planted}" && verify_staging_network_name fixture) || fail isolated-call-failed
[[ ! -e "${marker}" ]] || fail planted-json-imported

# Negative control: without -I the same cwd would hijack the import.
(cd "${planted}" && printf '{}\n' | "${python_bin}" -c 'import json' 2>/dev/null) || true
[[ -e "${marker}" ]] || fail control-not-reproduced
rm -f -- "${marker}"

printf 'staging-python-isolation=PASS\n'
