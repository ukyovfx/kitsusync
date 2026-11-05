#!/usr/bin/env bash
set -Eeuo pipefail
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH
umask 077

die() { printf 'STAGING_HELPER_UPGRADE=FAIL reason=%s\n' "$1" >&2; exit 1; }
[[ "$EUID" -eq 0 && "$#" -eq 3 && "$1" =~ ^[0-9a-f]{40}$ && "$2" =~ ^[0-9a-f]{64}$ && "$3" =~ ^[0-9a-f]{64}$ ]] || die INVALID_ARGUMENT
readonly SOURCE_SHA="$1"
readonly NEW_HELPER_SHA="$2"
readonly UPGRADE_SCRIPT_SHA="$3"
readonly INCOMING="/var/tmp/kitsusync-staging-helper-upgrade-${SOURCE_SHA}"
readonly TARGET=/usr/local/sbin/kitsusync-staging-deploy
readonly CONFIG_ROOT=/etc/kitsusync-staging
readonly OPERATORS_FILE_OWNER=0:0
# Fail-closed reader for the root-owned operator account list; any deviation returns non-zero.
load_operator_accounts() {
  local file="$1" line meta
  OPERATOR_USER="" EMERGENCY_USER="" OPERATOR_UID="" EMERGENCY_UID=""
  [[ -f "$file" && ! -L "$file" ]] || return 1
  meta="$(stat -c '%u:%g:%a' "$file")" || return 1
  case "$meta" in
    "$OPERATORS_FILE_OWNER":400|"$OPERATORS_FILE_OWNER":600|"$OPERATORS_FILE_OWNER":640|"$OPERATORS_FILE_OWNER":644) ;;
    *) return 1 ;;
  esac
  while IFS= read -r line || [[ -n "$line" ]]; do
    if [[ -z "$line" || "$line" == \#* ]]; then continue; fi
    if [[ "$line" =~ ^operator_user=([a-z_][a-z0-9_-]{0,31})$ && -z "$OPERATOR_USER" ]]; then
      OPERATOR_USER="${BASH_REMATCH[1]}"
    elif [[ "$line" =~ ^emergency_user=([a-z_][a-z0-9_-]{0,31})$ && -z "$EMERGENCY_USER" ]]; then
      EMERGENCY_USER="${BASH_REMATCH[1]}"
    else
      return 1
    fi
  done < "$file"
  [[ -n "$OPERATOR_USER" && -n "$EMERGENCY_USER" && "$OPERATOR_USER" != "$EMERGENCY_USER" ]] || return 1
  OPERATOR_UID="$(id -u -- "$OPERATOR_USER")" || return 1
  EMERGENCY_UID="$(id -u -- "$EMERGENCY_USER")" || return 1
  [[ "$OPERATOR_UID" =~ ^[0-9]+$ && "$EMERGENCY_UID" =~ ^[0-9]+$ && "$OPERATOR_UID" -ne 0 && "$EMERGENCY_UID" -ne 0 ]]
}
readonly EXPECTED_OLD_HELPER_SHAS=(
  bf3909d5ea2444eb08178425305e190008e534fbee2880b0eea991d51452c845
  63371b16e7f13af1e1d7046217c8ce0ae200b0e9b42ab1bf342b158af121e78d
  eb0ec326c89c0414cceb15390e58c4a2195b4f9377cdbbc8b5e8fe2d38b0f77b
  63d53cedfced32f26f4edc7338ee38d3a4c38e816a8427a6c7194bc366d25fc8
  41dec98c9e6e6f7165733212bb3518b1cb08bcb19a17fad047fd0c76ae5f7704
  b43df984dd87321c4f9eb76478557b23db35cac8986b01ddd4b3af5a2353e5ae
  4550cecbb12e83ca8647127624ec1062f5a11c63e37171015677efb8c2e88443
  7e839420b54e39d1022b131a977c7b9f4efba0be6768002e3b58d932a83f83cc
  c2e8b1b8dda50357909e2b5f8081560c262cff30e0107958ab85b9366a726490
  b7ed3756b0df66bc70b74f399b84e93a4085742820a648bc5910d2d931cb564e
  22de0a3ac88c7ba1aef80e138b00d64ee85de05289d5dfeabec2a200498431dd
  bb27defcdd4750d842e9a46419aeff848c3528e1f318a7ee572aae3a8554010f
  8c75897d3046a9a88b165ae2c689efa457af728f3c1c5f557641d78a138fecd2
  a933fc7e55db496d11bfb2fcfe01101a96e58dec525d5d76e8b2fd4e125a9f4f
  4afd1d26869b89ab2844d4718ee9565d269683f3bec58e947c1e2e6631a7e7d7
)

incoming_uid="$(stat -c '%u' "$INCOMING" 2>/dev/null || printf missing)"
incoming_mode="$(stat -c '%a' "$INCOMING" 2>/dev/null || printf missing)"
[[ -d "$CONFIG_ROOT" && ! -L "$CONFIG_ROOT" && "$(stat -c '%u:%g:%a' "$CONFIG_ROOT")" == 0:0:700 ]] || die STAGING_CONFIG_DIRECTORY_INVALID
load_operator_accounts "$CONFIG_ROOT/operators.conf" || die OPERATORS_CONFIG_INVALID
[[ -d "$INCOMING" && ! -L "$INCOMING" && "$incoming_mode" == 700 && ( "$incoming_uid" == "$OPERATOR_UID" || "$incoming_uid" == "$EMERGENCY_UID" ) ]] || die "UPGRADE_DIRECTORY_UNSAFE observed_owner_uid=$incoming_uid observed_mode=$incoming_mode expected_owner_uids=$OPERATOR_UID,$EMERGENCY_UID expected_mode=700"

helper_file="$INCOMING/kitsusync-staging-deploy"
upgrade_file="$INCOMING/upgrade-root.sh"
for file in "$helper_file" "$upgrade_file"; do
  [[ -f "$file" && ! -L "$file" && "$(stat -c '%u:%a:%h' "$file")" == "$incoming_uid:600:1" ]] || die UPGRADE_FILE_UNSAFE
done
mapfile -t entries < <(find "$INCOMING" -mindepth 1 -maxdepth 1 -printf '%f\n' | sort)
[[ "${entries[*]}" == "kitsusync-staging-deploy upgrade-root.sh" ]] || die UPGRADE_FILE_SET_INVALID

script_sha="$(sha256sum "$upgrade_file" | cut -d' ' -f1)"
helper_sha="$(sha256sum "$helper_file" | cut -d' ' -f1)"
[[ "$script_sha" == "$UPGRADE_SCRIPT_SHA" ]] || die "UPGRADE_SCRIPT_DIGEST_INVALID expected=$UPGRADE_SCRIPT_SHA observed=$script_sha"
[[ "$helper_sha" == "$NEW_HELPER_SHA" ]] || die "NEW_HELPER_DIGEST_INVALID expected=$NEW_HELPER_SHA observed=$helper_sha"
[[ -f "$TARGET" && ! -L "$TARGET" && "$(stat -c '%u:%g:%a' "$TARGET")" == 0:0:750 ]] || die INSTALLED_HELPER_METADATA_INVALID
old_helper_sha="$(sha256sum "$TARGET" | cut -d' ' -f1)"
old_helper_accepted=0
for expected_old_sha in "${EXPECTED_OLD_HELPER_SHAS[@]}"; do
  if [[ "$old_helper_sha" == "$expected_old_sha" ]]; then old_helper_accepted=1; break; fi
done
[[ "$old_helper_accepted" -eq 1 ]] || die "OLD_HELPER_IDENTITY_MISMATCH expected=${EXPECTED_OLD_HELPER_SHAS[*]} observed=$old_helper_sha"
[[ -f "$CONFIG_ROOT/conf.toml" && ! -L "$CONFIG_ROOT/conf.toml" ]] || die STAGING_CONFIG_FILE_INVALID
config_metadata="$(stat -c '%u:%g:%a' "$CONFIG_ROOT/conf.toml")"
case "$config_metadata" in
  0:0:400|0:10001:440) ;;
  *) die STAGING_CONFIG_METADATA_UNSUPPORTED ;;
esac
config_digest_before="$(sha256sum "$CONFIG_ROOT/conf.toml" | cut -d' ' -f1)"

root_tmp="$(mktemp -d /var/tmp/kitsusync-staging-helper-upgrade-root.XXXXXX)"
target_tmp=""
config_migration_attempted=0
helper_replaced=0
cleanup_upgrade() {
  if [[ "$config_migration_attempted" -eq 1 && "$helper_replaced" -eq 0 ]]; then
    chown root:root "$CONFIG_ROOT/conf.toml" 2>/dev/null || true
    chmod 0400 "$CONFIG_ROOT/conf.toml" 2>/dev/null || true
  fi
  rm -rf -- "$root_tmp"
  [[ -z "$target_tmp" ]] || rm -f -- "$target_tmp"
}
trap cleanup_upgrade EXIT
install -o root -g root -m 0600 "$helper_file" "$root_tmp/kitsusync-staging-deploy"
[[ "$(sha256sum "$root_tmp/kitsusync-staging-deploy" | cut -d' ' -f1)" == "$NEW_HELPER_SHA" ]] || die ROOT_COPY_DIGEST_INVALID
bash -n "$root_tmp/kitsusync-staging-deploy" || die NEW_HELPER_SYNTAX_INVALID
/bin/bash "$root_tmp/kitsusync-staging-deploy" --contract-info | grep -Fq 'STAGING_HELPER_CONTRACT=staging-v10' || die NEW_HELPER_CONTRACT_INVALID

target_tmp="$(mktemp /usr/local/sbin/.kitsusync-staging-deploy.XXXXXX)"
install -o root -g root -m 0750 "$root_tmp/kitsusync-staging-deploy" "$target_tmp"
if [[ "$config_metadata" == 0:0:400 ]]; then
  config_migration_attempted=1
  chown root:10001 "$CONFIG_ROOT/conf.toml" || die STAGING_CONFIG_MIGRATION_FAILED
  chmod 0440 "$CONFIG_ROOT/conf.toml" || die STAGING_CONFIG_MIGRATION_FAILED
fi
[[ "$(stat -c '%u:%g:%a' "$CONFIG_ROOT/conf.toml")" == 0:10001:440 && "$(sha256sum "$CONFIG_ROOT/conf.toml" | cut -d' ' -f1)" == "$config_digest_before" ]] || die STAGING_CONFIG_MIGRATION_FAILED
mv -f -- "$target_tmp" "$TARGET"
target_tmp=""
helper_replaced=1
[[ "$(sha256sum "$TARGET" | cut -d' ' -f1)" == "$NEW_HELPER_SHA" ]] || die INSTALLED_HELPER_DIGEST_INVALID
"$TARGET" --contract-info | grep -Fq 'STAGING_HELPER_CONTRACT=staging-v10' || die INSTALLED_HELPER_CONTRACT_INVALID
rm -rf -- "$INCOMING"
printf 'STAGING_HELPER_UPGRADE=PASS source_sha=%s helper_sha256=%s config_migration=ROOT_10001_0440\n' "$SOURCE_SHA" "$NEW_HELPER_SHA"
