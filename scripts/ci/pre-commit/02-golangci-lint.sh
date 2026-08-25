#!/usr/bin/env bash
# scripts/ci/pre-commit/02-golangci-lint.sh
# Run golangci-lint with configurable timeout

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
NC='\033[0m'

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
cd "$ROOT"
echo -e "${CYAN}> Running golangci-lint${NC}"

# Use GOLANGCI_LINT_TIMEOUT env var or default to 2m for pre-commit
TIMEOUT=${GOLANGCI_LINT_TIMEOUT:-2m}

scripts/tool.sh golangci-lint run --timeout="$TIMEOUT"
echo -e "${GREEN}golangci-lint: OK${NC}"
echo
