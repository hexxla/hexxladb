#!/usr/bin/env bash
# scripts/ci/pre-commit/07-secrets.sh
# Scan for secrets

set -euo pipefail

YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m'

echo -e "${CYAN}> Scanning for secrets${NC}"

if [[ -f ./scripts/ci/check-secrets.sh ]]; then
    ./scripts/ci/check-secrets.sh
else
    echo -e "${YELLOW}warning:${NC} check-secrets.sh not found (skipped)"
fi
echo
