#!/usr/bin/env bash
# scripts/ci/pre-push/06-gremlins.sh
# Mutation testing using Gremlins for semantic stability.
# Targets fast, self-contained packages with high test coverage.
# Config: .gremlins.yaml (timeout-coefficient, mutant toggles, thresholds).

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
cd "$ROOT"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m'

# Check for gremlins tool
if ! command -v gremlins >/dev/null 2>&1; then
    echo -e "${YELLOW}> gremlins not installed, skipping mutation testing${NC}"
    echo -e "${YELLOW}> Install with: go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0${NC}"
    exit 0
fi

# Packages suitable for mutation testing (fast, self-contained tests):
# Add packages here as their test suites become self-contained and fast.
MUTATION_TARGETS=(
    "./internal/lattice"
    "./internal/record"
    "./internal/changelog"
    "./internal/hnsw"
    "./internal/index"
)

# CI mode: use --dry-run for speed in pre-push; full runs via `make mutation-test`.
MODE="--dry-run"
if [[ "${GREMLINS_FULL:-}" == "1" ]]; then
    MODE=""
fi

overall_ok=true

for pkg in "${MUTATION_TARGETS[@]}"; do
    # Skip packages that don't exist or have no Go files
    if ! go list "$pkg" >/dev/null 2>&1; then
        continue
    fi

    echo -e "${CYAN}> Gremlins: $pkg${NC}"
    if gremlins unleash $MODE "$pkg"; then
        echo -e "${GREEN}  ✓ $pkg${NC}"
    else
        echo -e "${RED}  ✗ $pkg${NC}"
        overall_ok=false
    fi
done

if [[ "$overall_ok" == "false" ]]; then
    echo -e "${RED}> Mutation testing found issues${NC}"
    echo -e "${YELLOW}> For full mutation testing: make mutation-test${NC}"
    exit 1
fi

echo -e "${GREEN}> Mutation testing passed${NC}"
