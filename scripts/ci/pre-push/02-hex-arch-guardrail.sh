#!/usr/bin/env bash
# scripts/ci/pre-push/02-hex-arch-guardrail.sh
# Check hexagonal architecture rules

set -euo pipefail

YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m'

echo -e "${CYAN}> Checking hexagonal architecture rules${NC}"

if [[ -f ./scripts/check-hex-boundaries.sh ]]; then
    ./scripts/check-hex-boundaries.sh
else
    echo -e "${YELLOW}warning:${NC} check-hex-boundaries.sh not found (skipped)"
fi
echo
