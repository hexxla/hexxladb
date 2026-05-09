#!/usr/bin/env bash
# scripts/ci/pre-push/06-gremlins.sh
# Mutation testing using Gremlins for semantic stability.
# Targets fast, self-contained packages with high test coverage.
# Config: .gremlins.yaml (workers, timeout-coefficient, mutant toggles).
#
# NOTE: gremlins dev build does not exit non-zero on threshold violations.
# We parse stdout directly to enforce gates.

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

# Packages suitable for mutation testing (fast, self-contained tests).
# Add packages here as their test suites become self-contained and fast.
MUTATION_TARGETS=(
    "./internal/lattice"
    "./internal/record"
    "./internal/changelog"
    "./internal/hnsw"
    "./internal/index"
)

# Per-package minimum mutant-coverage thresholds (%).
# Enforced in full-run mode by parsing stdout. Baseline: 2026-05-09.
# internal/index is low because key encode funcs are only tested indirectly.
declare -A MIN_MCOVER=(
    ["./internal/lattice"]=85
    ["./internal/record"]=90
    ["./internal/changelog"]=95
    ["./internal/hnsw"]=88
    ["./internal/index"]=50
)

# CI mode: use --dry-run for speed in pre-push; full runs via `make mutation-test`.
DRY_RUN=true
if [[ "${GREMLINS_FULL:-}" == "1" ]]; then
    DRY_RUN=false
fi

overall_ok=true

for pkg in "${MUTATION_TARGETS[@]}"; do
    # Skip packages that don't exist or have no Go files
    if ! go list "$pkg" >/dev/null 2>&1; then
        continue
    fi

    echo -e "${CYAN}> Gremlins: $pkg${NC}"

    if [[ "$DRY_RUN" == "true" ]]; then
        gremlins unleash --dry-run "$pkg"
        echo -e "${GREEN}  ✓ $pkg (dry-run)${NC}"
        continue
    fi

    # Full run — capture output to parse gates
    output=$(gremlins unleash "$pkg" 2>&1)
    echo "$output"

    # Gate 1: zero survived mutants (Lived must be 0)
    lived=$(echo "$output" | grep -oP '(?<=Lived: )\d+' || echo "0")
    if [[ "${lived:-0}" -gt 0 ]]; then
        echo -e "${RED}  ✗ $pkg — $lived mutant(s) SURVIVED (test suite has gaps)${NC}"
        overall_ok=false
        continue
    fi

    # Gate 2: mutant coverage above per-package minimum
    mcover_raw=$(echo "$output" | grep -oP '(?<=Mutator coverage: )\d+\.\d+' || echo "0")
    mcover=${mcover_raw%.*}  # integer part
    min_cover="${MIN_MCOVER[$pkg]:-50}"
    if [[ "${mcover:-0}" -lt "$min_cover" ]]; then
        echo -e "${RED}  ✗ $pkg — mutant coverage ${mcover}% below minimum ${min_cover}%${NC}"
        overall_ok=false
        continue
    fi

    echo -e "${GREEN}  ✓ $pkg (efficacy=100% mcover=${mcover}% ≥ ${min_cover}%)${NC}"
done

if [[ "$overall_ok" == "false" ]]; then
    echo -e "${RED}> Mutation testing FAILED${NC}"
    echo -e "${YELLOW}> For full mutation testing: make mutation-test${NC}"
    exit 1
fi

if [[ "$DRY_RUN" == "true" ]]; then
    echo -e "${GREEN}> Mutation dry-run complete (no gates enforced; run 'make mutation-test' for full check)${NC}"
else
    echo -e "${GREEN}> Mutation testing passed${NC}"
fi
