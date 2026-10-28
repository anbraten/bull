#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KEY_DIR="${SCRIPT_DIR}/config/.ssh"
KEY_PATH="${KEY_DIR}/id_ed25519"
PUB_PATH="${KEY_PATH}.pub"
SERVICES=(sshd sshd-alpine)

usage() {
	cat <<EOF
Usage: ./setup.sh <command>

Commands:
  up      Build/start test container and provision SSH key (default)
  down    Stop and remove test container
  reset   down + remove generated test key + up
  logs    Tail container logs
EOF
}

ensure_keypair() {
	mkdir -p "${KEY_DIR}"
	chmod 700 "${KEY_DIR}"

	if [[ ! -f "${KEY_PATH}" || ! -f "${PUB_PATH}" ]]; then
		ssh-keygen -t ed25519 -N "" -f "${KEY_PATH}" -C "bull-test-key" >/dev/null
	fi
}

provision_public_key() {
	local service="$1"
	docker compose -f "${SCRIPT_DIR}/docker-compose.yml" cp "${PUB_PATH}" "${service}:/tmp/bull_test_key.pub"
	docker compose -f "${SCRIPT_DIR}/docker-compose.yml" exec -T "${service}" sh -lc '
		set -e
		mkdir -p /root/.ssh
		touch /root/.ssh/authorized_keys
		chmod 700 /root/.ssh
		if ! grep -qxF "$(cat /tmp/bull_test_key.pub)" /root/.ssh/authorized_keys; then
			cat /tmp/bull_test_key.pub >> /root/.ssh/authorized_keys
		fi
		chmod 600 /root/.ssh/authorized_keys
		rm -f /tmp/bull_test_key.pub
	'
}

cmd_up() {
	ensure_keypair
	docker compose -f "${SCRIPT_DIR}/docker-compose.yml" up -d --build
	for service in "${SERVICES[@]}"; do
		provision_public_key "${service}"
	done
	echo "Test containers are ready."
	echo "Use key: ${KEY_PATH}"
	echo "  sshd        (debian/apt) -> localhost:2223"
	echo "  sshd-alpine (alpine/apk) -> localhost:2224"
	echo "Example: bull plan ${SCRIPT_DIR}/config/infra.lua"
}

cmd_down() {
	docker compose -f "${SCRIPT_DIR}/docker-compose.yml" down
}

cmd_reset() {
	cmd_down || true
	rm -f "${KEY_PATH}" "${PUB_PATH}"
	cmd_up
}

cmd_logs() {
	local service="${1:-}"
	docker compose -f "${SCRIPT_DIR}/docker-compose.yml" logs -f ${service}
}

COMMAND="${1:-up}"

case "${COMMAND}" in
	up)
		cmd_up
		;;
	down)
		cmd_down
		;;
	reset)
		cmd_reset
		;;
	logs)
		cmd_logs "${2:-}"
		;;
	-h|--help|help)
		usage
		;;
	*)
		echo "Unknown command: ${COMMAND}" >&2
		usage
		exit 1
		;;
esac

