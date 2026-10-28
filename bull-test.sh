#!/usr/bin/env bash
# Run bull (via `go run .`) against the local test SSH container,
# starting the container first if it isn't already up.
#
# Usage: ./bull-test.sh <bull args...>
# Example: ./bull-test.sh plan test/config/infra.lua

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEST_DIR="${SCRIPT_DIR}/test"
COMPOSE_FILE="${TEST_DIR}/docker-compose.yml"
export EDITOR=nano

is_running() {
	local running
	running="$(docker compose -f "${COMPOSE_FILE}" ps --status running --services 2>/dev/null)"
	grep -qx sshd <<<"${running}" && grep -qx sshd-alpine <<<"${running}"
}

if ! is_running; then
	echo "Test container not running, starting it..."
	"${TEST_DIR}/setup.sh" up
fi

cd "${SCRIPT_DIR}"
exec go run . "$@"
