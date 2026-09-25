#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
helper="${root}/deploy/kitsusync-staging-deploy"
function_source="$(awk '
  /^verify_staging_network_membership\(\) \{/ { copying = 1 }
  copying { print }
  copying && /^\}/ { found = 1; exit }
  END { if (!found) exit 1 }
' "${helper}")" || {
  printf 'staging-network-membership=FAIL missing-function\n' >&2
  exit 1
}

eval "${function_source}"

python3() {
  "${TEST_PYTHON:-python3}" "$@"
}

full_id=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
short_id="${full_id:0:12}"
network_output="{\"${full_id}\":{\"Name\":\"kitsusync-staging\",\"EndpointID\":\"fedcba9876543210\"}}"
network_mode=expected

docker() {
  if [[ "$1" == inspect && "$2" == --format && "$3" == '{{.Id}}' && "$4" == "${short_id}" ]]; then
    printf '%s\n' "${full_id}"
  elif [[ "$1" == network && "$2" == inspect && "$3" == --format && "$4" == '{{json .Containers}}' && "$5" == kitsusync-staging-network ]]; then
    case "${network_mode}" in
      expected) printf '%s\n' "${network_output}" ;;
      unrelated) printf '%s\n' '{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa":{"Name":"other"}}' ;;
      multiple) printf '%s\n' "{\"${full_id}\":{\"Name\":\"kitsusync-staging\"},\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\":{\"Name\":\"other\"}}" ;;
      *) return 2 ;;
    esac
  else
    printf 'unexpected docker invocation: %s\n' "$*" >&2
    return 2
  fi
}

verify_staging_network_membership "${short_id}"
network_mode=unrelated
if verify_staging_network_membership "${short_id}"; then
  printf 'staging-network-membership=FAIL accepted-unrelated-container\n' >&2
  exit 1
fi
network_mode=multiple
if verify_staging_network_membership "${short_id}"; then
  printf 'staging-network-membership=FAIL accepted-multiple-containers\n' >&2
  exit 1
fi

printf 'staging-network-membership=PASS\n'
