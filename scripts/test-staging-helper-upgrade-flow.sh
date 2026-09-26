#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
upgrade_source="${root}/deploy/kitsusync-staging-helper-upgrade-root.sh"
sandbox="$(mktemp -d)"
trap 'rm -rf -- "$sandbox"' EXIT
mkdir -p "${sandbox}/var-tmp" "${sandbox}/usr-local-sbin"

uid="$(id -u)"
gid="$(id -g)"
source_sha=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
helper_file="${sandbox}/kitsusync-staging-deploy"
old_file="${sandbox}/old-helper"
upgrade_file="${sandbox}/upgrade-root.sh"
mode_log="${sandbox}/helper-invocation-modes"
config_dir="${sandbox}/etc-kitsusync-staging"
config_file="${config_dir}/conf.toml"

install -d -m 0700 "${config_dir}"
printf 'private-config-fixture-do-not-display\n' >"${config_file}"
chmod 0400 "${config_file}"
config_sha_before="$(sha256sum "${config_file}" | cut -d' ' -f1)"

cat >"${helper_file}" <<'HELPER'
#!/usr/bin/env bash
set -euo pipefail
[[ "$#" -eq 1 && "$1" == --contract-info ]]
printf '%s\n' "$(stat -c '%a' "$0")" >> "$MODE_LOG"
printf 'STAGING_HELPER_CONTRACT=staging-v7 incoming=/var/tmp/kitsusync-staging-candidate-<sha> owners=ukyo_vfx,vfx-breakglass\n'
HELPER
cat >"${old_file}" <<'OLD_HELPER'
#!/usr/bin/env bash
exit 0
OLD_HELPER
chmod 0600 "${helper_file}"
chmod 0750 "${old_file}"
old_sha="$(sha256sum "${old_file}" | cut -d' ' -f1)"

"${PYTHON:-python3}" - "${upgrade_source}" "${upgrade_file}" "${sandbox}" "${uid}" "${gid}" "${old_sha}" <<'PY'
from pathlib import Path
import sys

source, output, sandbox, uid, gid, old_sha = sys.argv[1:]
script = Path(source).read_text()
replacements = {
    '[[ "$EUID" -eq 0 &&': '[[ "$EUID" -ge 0 &&',
    'readonly INCOMING="/var/tmp/kitsusync-staging-helper-upgrade-${SOURCE_SHA}"': f'readonly INCOMING="{sandbox}/var-tmp/kitsusync-staging-helper-upgrade-${{SOURCE_SHA}}"',
    'readonly TARGET=/usr/local/sbin/kitsusync-staging-deploy': f'readonly TARGET={sandbox}/usr-local-sbin/kitsusync-staging-deploy',
    'readonly CONFIG_ROOT=/etc/kitsusync-staging': f'readonly CONFIG_ROOT={sandbox}/etc-kitsusync-staging',
    '  63d53cedfced32f26f4edc7338ee38d3a4c38e816a8427a6c7194bc366d25fc8': f'  {old_sha}',
    '  41dec98c9e6e6f7165733212bb3518b1cb08bcb19a17fad047fd0c76ae5f7704': f'  {old_sha}',
    '  b43df984dd87321c4f9eb76478557b23db35cac8986b01ddd4b3af5a2353e5ae': f'  {old_sha}',
    '  4550cecbb12e83ca8647127624ec1062f5a11c63e37171015677efb8c2e88443': f'  {old_sha}',
    '  eb0ec326c89c0414cceb15390e58c4a2195b4f9377cdbbc8b5e8fe2d38b0f77b': f'  {old_sha}',
    'id -u ukyo_vfx': f'printf {uid}',
    'id -u vfx-breakglass': f'printf {uid}',
    '0:0:700': f'{uid}:{gid}:700',
    '0:0:400': f'{uid}:{gid}:400',
    '0:10001:440': f'{uid}:{gid}:440',
    'chown root:10001': f'chown {uid}:{gid}',
    'chown root:root': f'chown {uid}:{gid}',
    'mktemp -d /var/tmp/kitsusync-staging-helper-upgrade-root.XXXXXX': f'mktemp -d "{sandbox}/var-tmp/kitsusync-staging-helper-upgrade-root.XXXXXX"',
    'mktemp /usr/local/sbin/.kitsusync-staging-deploy.XXXXXX': f'mktemp "{sandbox}/usr-local-sbin/.kitsusync-staging-deploy.XXXXXX"',
    'install -o root -g root -m ': f'install -o {uid} -g {gid} -m ',
    '== 0:0:750': f'== {uid}:{gid}:750',
}
for old, new in replacements.items():
    if old not in script:
        raise SystemExit(f'fixture could not adapt expected contract: {old}')
    script = script.replace(old, new)
Path(output).write_text(script)
PY

incoming="${sandbox}/var-tmp/kitsusync-staging-helper-upgrade-${source_sha}"
install -d -m 0700 "${incoming}"
install -m 0600 "${helper_file}" "${incoming}/kitsusync-staging-deploy"
install -m 0600 "${upgrade_file}" "${incoming}/upgrade-root.sh"
install -m 0750 "${old_file}" "${sandbox}/usr-local-sbin/kitsusync-staging-deploy"

helper_sha="$(sha256sum "${incoming}/kitsusync-staging-deploy" | cut -d' ' -f1)"
upgrade_sha="$(sha256sum "${incoming}/upgrade-root.sh" | cut -d' ' -f1)"
export MODE_LOG="${mode_log}"
output="$(bash "${incoming}/upgrade-root.sh" "${source_sha}" "${helper_sha}" "${upgrade_sha}")"
grep -Fq 'STAGING_HELPER_UPGRADE=PASS' <<<"${output}"
grep -Fq 'config_migration=ROOT_10001_0440' <<<"${output}"
[[ "$(cat "${mode_log}")" == $'600\n750' ]]
[[ "$(stat -c '%a' "${sandbox}/usr-local-sbin/kitsusync-staging-deploy")" == 750 ]]
[[ "$(sha256sum "${sandbox}/usr-local-sbin/kitsusync-staging-deploy" | cut -d' ' -f1)" == "${helper_sha}" ]]
[[ "$(stat -c '%u:%g:%a' "${config_file}")" == "${uid}:${gid}:440" ]]
[[ "$(sha256sum "${config_file}" | cut -d' ' -f1)" == "${config_sha_before}" ]]
[[ ! -e "${incoming}" ]]
printf 'staging-helper-upgrade-flow=PASS config-migration=ROOT_RUNTIME_GID_0440\n'
