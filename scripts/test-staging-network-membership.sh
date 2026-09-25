#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
helper="${root}/deploy/kitsusync-staging-deploy"
function_source="$(awk '
  /^verify_staging_network_(membership|attachment_count|name)\(\) \{/ { copying = 1; found++ }
  copying { print }
  copying && /^\}/ { copying = 0 }
  END { if (found != 3) exit 1 }
' "${helper}")" || {
  printf 'staging-network-membership=FAIL missing-function\n' >&2
  exit 1
}

eval "${function_source}"

if [[ -n "${TEST_PYTHON:-}" ]]; then
  python3() { "${TEST_PYTHON}" "$@"; }
fi

full_id=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
short_id="${full_id:0:12}"
network_output="{\"${full_id}\":{\"Name\":\"kitsusync-staging\",\"EndpointID\":\"fedcba9876543210\"}}"
network_mode=expected
attachment_count=1
attachment_output='{"kitsusync-staging-network":{"NetworkID":"network-id"}}'

docker() {
  if [[ "$1" == inspect && "$2" == --format && "$3" == '{{.Id}}' && "$4" == "${short_id}" ]]; then
    printf '%s\n' "${full_id}"
  elif [[ "$1" == inspect && "$2" == --format && "$3" == '{{len .NetworkSettings.Networks}}' && "$4" == "${short_id}" ]]; then
    printf '%s\n' "${attachment_count}"
  elif [[ "$1" == inspect && "$2" == --format && "$3" == '{{json .NetworkSettings.Networks}}' && "$4" == "${short_id}" ]]; then
    printf '%s\n' "${attachment_output}"
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

verify_staging_network_attachment_count "${short_id}"
verify_staging_network_name "${short_id}"
verify_staging_network_membership "${short_id}"
attachment_count=2
if verify_staging_network_attachment_count "${short_id}"; then
  printf 'staging-network-membership=FAIL accepted-multiple-attachments\n' >&2
  exit 1
fi
attachment_count=1
attachment_output='{"other-network":{"NetworkID":"network-id"}}'
if verify_staging_network_name "${short_id}"; then
  printf 'staging-network-membership=FAIL accepted-unexpected-network-name\n' >&2
  exit 1
fi
attachment_output='{"kitsusync-staging-network":{"NetworkID":"network-id"}}'
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

if [[ "${STAGING_REQUIRE_DOCKER_FIXTURE:-0}" == 1 ]]; then
  command -v docker >/dev/null || { printf 'staging-network-membership=FAIL docker-unavailable\n' >&2; exit 1; }
  docker info >/dev/null 2>&1 || { printf 'staging-network-membership=FAIL docker-daemon-unavailable\n' >&2; exit 1; }
  unset -f docker
  fixture_root="$(mktemp -d)"
  fixture_project="kitsusync-net-contract-${BASHPID}"
  fixture_started_ms="$(date +%s%3N)"
  cleanup_fixture() {
    docker compose --project-name "${fixture_project}" --file "${fixture_root}/compose.yml" down --volumes --remove-orphans >/dev/null 2>&1 || true
    rm -rf -- "${fixture_root}"
  }
  trap cleanup_fixture EXIT
  if docker network inspect kitsusync-staging-network >/dev/null 2>&1; then
    printf 'staging-network-membership=FAIL fixture-network-name-in-use\n' >&2
    exit 1
  fi
  cat >"${fixture_root}/compose.yml" <<'YAML'
services:
  app:
    image: busybox:1.36.1
    command: ["sh", "-c", "sleep 600"]
    networks:
      - staging
networks:
  staging:
    name: kitsusync-staging-network
YAML
  docker compose --project-name "${fixture_project}" --file "${fixture_root}/compose.yml" up -d --pull always >/dev/null
  fixture_elapsed_ms="$(( $(date +%s%3N) - fixture_started_ms ))"
  fixture_short_id="$(docker ps -q --filter "label=com.docker.compose.project=${fixture_project}" --filter status=running)"
  [[ "${fixture_short_id}" =~ ^[0-9a-f]{12,64}$ ]] || { printf 'staging-network-membership=FAIL fixture-ps-id-shape\n' >&2; exit 1; }
  fixture_full_id="$(docker inspect --format '{{.Id}}' "${fixture_short_id}")"
  [[ "${fixture_full_id}" =~ ^[0-9a-f]{64}$ && "${fixture_full_id}" == "${fixture_short_id}"* ]] || { printf 'staging-network-membership=FAIL fixture-inspect-id-shape\n' >&2; exit 1; }
  network_shape="$(docker inspect --format '{{json .NetworkSettings.Networks}}' "${fixture_short_id}" | python3 -c 'import json,sys; n=json.load(sys.stdin); print(len(n),",".join(sorted(n)))')"
  containers_shape="$(docker network inspect --format '{{json .Containers}}' kitsusync-staging-network | python3 -c 'import json,sys; c=json.load(sys.stdin) or {}; print(len(c),",".join(sorted(c)))')"
  printf 'staging-docker-fixture=ps_q_id_length:%s full_id_length:%s network_keys:%s network_containers:%s elapsed_ms:%s\n' "${#fixture_short_id}" "${#fixture_full_id}" "${network_shape}" "${containers_shape}" "${fixture_elapsed_ms}"
  verify_staging_network_attachment_count "${fixture_short_id}"
  verify_staging_network_name "${fixture_short_id}"
  verify_staging_network_membership "${fixture_short_id}"
  cleanup_fixture
  trap - EXIT
fi

printf 'staging-network-membership=PASS\n'
