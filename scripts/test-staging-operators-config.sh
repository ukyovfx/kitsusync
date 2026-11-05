#!/usr/bin/env bash
# Verifies the fail-closed operators.conf reader shared by the staging helper, its upgrade installer and the bootstrap.
# shellcheck disable=SC2016  # fixed strings are matched literally, not expanded
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
sandbox="$(mktemp -d)"
trap 'rm -rf -- "$sandbox"' EXIT
uid="$(id -u)"
gid="$(id -g)"
[[ "$uid" -ne 0 ]] || { echo 'run this test as a non-root user' >&2; exit 1; }

sources=(
  "${root}/deploy/kitsusync-staging-deploy"
  "${root}/deploy/kitsusync-staging-helper-upgrade-root.sh"
  "${root}/deploy/kitsusync-staging-bootstrap"
)

extract() {
  tr -d '\r' <"$1" | awk '/^readonly OPERATORS_FILE_OWNER=0:0$/ {on=1} on {print} on && /^}$/ {on=0}'
}

# All three installed scripts must carry the same reader, and it must pin root:root ownership.
reference="$(extract "${sources[0]}")"
[[ -n "$reference" ]] || { echo 'reader not found in the staging helper' >&2; exit 1; }
for source in "${sources[@]}"; do
  [[ "$(extract "$source")" == "$reference" ]] || { echo "reader differs in ${source}" >&2; exit 1; }
done
grep -Fq 'load_operator_accounts' "${root}/deploy/kitsusync-staging-deploy"
grep -Fq 'die OPERATORS_CONFIG_INVALID' "${root}/deploy/kitsusync-staging-deploy"
grep -Fq 'die OPERATORS_CONFIG_INVALID' "${root}/deploy/kitsusync-staging-helper-upgrade-root.sh"
grep -Fq 'die OPERATORS_CONFIG_INVALID' "${root}/deploy/kitsusync-staging-bootstrap"
grep -Fq 'die OPERATORS_CONFIG_DIRECTORY_INVALID' "${root}/deploy/kitsusync-staging-bootstrap"
grep -Fq '"$OPERATOR_USER" > "$sudoers_tmp"' "${root}/deploy/kitsusync-staging-bootstrap"
grep -Fq 'owners=operators.conf' "${root}/deploy/kitsusync-staging-deploy"
grep -Fq 'load_operator_accounts "$CONFIG_ROOT/operators.conf" || die OPERATORS_CONFIG_INVALID' "${root}/deploy/kitsusync-staging-deploy"
grep -Fq '4afd1d26869b89ab2844d4718ee9565d269683f3bec58e947c1e2e6631a7e7d7' "${root}/deploy/kitsusync-staging-helper-upgrade-root.sh"

# No account name may be compiled into the helper, and the workflow must take the operator account and network tags from variables.
if grep -Fq 'getpwnam(' "${root}/deploy/kitsusync-staging-deploy"; then echo 'helper resolves a fixed account name' >&2; exit 1; fi
workflow="${root}/.github/workflows/staging-deploy.yml"
grep -Fq 'vars.KITSUSYNC_STAGING_SSH_USER' "${workflow}"
grep -Fq 'tags: ${{ vars.KITSUSYNC_TAILSCALE_TAGS }}' "${workflow}"
if grep -Eq 'tags:[[:space:]]*tag:' "${workflow}"; then echo 'workflow hard-codes network tags' >&2; exit 1; fi
if grep -Eq 'KITSUSYNC_STAGING_SSH_USER=[^"$]' "${workflow}"; then echo 'workflow hard-codes the SSH account' >&2; exit 1; fi

# Load the reader with the root ownership pin replaced by the current user, and with stand-in account lookups.
reader="${sandbox}/reader.sh"
{
  printf 'id() {\n'
  printf '  case "$*" in\n'
  printf '    "-u -- fixture-operator") printf "%%s\\n" "%s" ;;\n' "$uid"
  printf '    "-u -- fixture-emergency") printf "%%s\\n" "%s" ;;\n' "$((uid + 1))"
  printf '    "-u -- fixture-root") printf "0\\n" ;;\n'
  printf '    *) return 1 ;;\n'
  printf '  esac\n'
  printf '}\n'
  printf '%s\n' "$reference" | sed "s/^readonly OPERATORS_FILE_OWNER=0:0\$/readonly OPERATORS_FILE_OWNER=${uid}:${gid}/"
} >"$reader"
# shellcheck source=/dev/null
source "$reader"

conf="${sandbox}/operators.conf"
expect_accept() {
  local name="$1" mode="${2:-0600}"
  chmod "$mode" "$conf"
  load_operator_accounts "$conf" || { echo "reader rejected a valid file: ${name}" >&2; exit 1; }
}
expect_reject() {
  local name="$1" mode="${2:-0600}"
  chmod "$mode" "$conf"
  if load_operator_accounts "$conf"; then echo "reader accepted an invalid file: ${name}" >&2; exit 1; fi
}

printf 'operator_user=fixture-operator\nemergency_user=fixture-emergency\n' >"$conf"
expect_accept valid
[[ "$OPERATOR_USER" == fixture-operator && "$EMERGENCY_USER" == fixture-emergency && "$OPERATOR_UID" == "$uid" ]]
printf '# comment\n\noperator_user=fixture-operator\nemergency_user=fixture-emergency' >"$conf"
expect_accept 'comments, blank lines and no trailing newline'
for mode in 0400 0640 0644; do expect_accept "mode ${mode}" "$mode"; done

printf 'operator_user=fixture-operator\nemergency_user=fixture-emergency\n' >"$conf"
for mode in 0666 0662 0620 0660 0604 0606; do expect_reject "mode ${mode}" "$mode"; done
expect_reject 'executable mode' 0700

rm -f "$conf"
if load_operator_accounts "$conf"; then echo 'reader accepted a missing file' >&2; exit 1; fi
[[ -z "$OPERATOR_USER" && -z "$EMERGENCY_USER" ]]

write_case() { printf '%b' "$1" >"$conf"; }
write_case ''; expect_reject 'empty file'
write_case 'operator_user=fixture-operator\n'; expect_reject 'missing emergency_user'
write_case 'emergency_user=fixture-emergency\n'; expect_reject 'missing operator_user'
write_case 'operator_user=fixture-operator\noperator_user=fixture-operator\nemergency_user=fixture-emergency\n'; expect_reject 'duplicate key'
write_case 'operator_user=fixture-operator\nemergency_user=fixture-operator\n'; expect_reject 'same account twice'
write_case 'operator_user=fixture-operator\nemergency_user=fixture-emergency\nextra=1\n'; expect_reject 'unknown key'
write_case 'operator_user=fixture-operator\nemergency_user=fixture-emergency # note\n'; expect_reject 'trailing text'
write_case 'operator_user = fixture-operator\nemergency_user=fixture-emergency\n'; expect_reject 'spaces around ='
write_case 'operator_user=fixture-operator\r\nemergency_user=fixture-emergency\r\n'; expect_reject 'CRLF line endings'
write_case 'operator_user=Fixture-Operator\nemergency_user=fixture-emergency\n'; expect_reject 'uppercase name'
write_case 'operator_user=fixture-operator;id\nemergency_user=fixture-emergency\n'; expect_reject 'shell metacharacter'
write_case 'operator_user=$(id)\nemergency_user=fixture-emergency\n'; expect_reject 'command substitution text'
write_case 'operator_user=\nemergency_user=fixture-emergency\n'; expect_reject 'empty value'
write_case 'operator_user=fixture-missing\nemergency_user=fixture-emergency\n'; expect_reject 'unknown account'
write_case 'operator_user=fixture-root\nemergency_user=fixture-emergency\n'; expect_reject 'uid 0 operator'
write_case 'operator_user=fixture-operator\nemergency_user=fixture-root\n'; expect_reject 'uid 0 emergency account'

# Symlinks are refused even when the target is valid.
printf 'operator_user=fixture-operator\nemergency_user=fixture-emergency\n' >"${sandbox}/target.conf"
chmod 0600 "${sandbox}/target.conf"
rm -f "$conf"
ln -s "${sandbox}/target.conf" "$conf"
if load_operator_accounts "$conf"; then echo 'reader accepted a symlink' >&2; exit 1; fi
rm -f "$conf"

# Ownership: a file whose owner differs from the pinned owner is refused.
printf 'operator_user=fixture-operator\nemergency_user=fixture-emergency\n' >"$conf"
chmod 0600 "$conf"
reader_wrong_owner="${sandbox}/reader-wrong-owner.sh"
sed "s/^readonly OPERATORS_FILE_OWNER=.*\$/readonly OPERATORS_FILE_OWNER=$((uid + 1)):${gid}/" "$reader" >"$reader_wrong_owner"
if bash -c 'source "$1" && load_operator_accounts "$2"' _ "$reader_wrong_owner" "$conf"; then
  echo 'reader accepted a file with the wrong owner' >&2
  exit 1
fi

printf 'staging-operators-config=PASS\n'
