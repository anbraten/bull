#!/usr/bin/env bash
# Run the integration test suite. TestMain (test/integration/framework_test.go)
# brings up the SSH containers and provisions the test key itself, so this is
# just a short alias for the go test invocation.
#
# Usage: ./test-integration.sh [go test args...]
# Example: ./test-integration.sh -run TestFileLifecycle -v

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${SCRIPT_DIR}"
exec go test -tags=integration ./test/integration/... "$@"
