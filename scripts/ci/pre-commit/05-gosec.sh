#!/usr/bin/env bash
# scripts/ci/pre-commit/05-gosec.sh
# Run gosec security scan

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
NC='\033[0m'

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
cd "$ROOT"

echo -e "${CYAN}> Running gosec security scan${NC}"

scripts/tool.sh gosec -quiet -severity medium -confidence medium -exclude-generated -exclude=G115 ./...
echo -e "${GREEN}gosec: OK${NC}"
echo
