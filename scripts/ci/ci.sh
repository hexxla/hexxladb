#!/usr/bin/env bash
# scripts/ci/ci.sh — Extended analysis pipeline for hexxladb
# Runs the core pipeline (scripts/ci.sh) then adds:
#   - complexity analysis (gocyclo + gocognit + CRAP)
#   - mutation testing (gremlins)
#   - coupling analysis (goda)
#
# The base pipeline (format, vet, tests, lint, govulncheck, mod-tidy) lives in
# scripts/ci.sh and is what `task ci` calls. This script extends it.
# Run with: ./scripts/ci/ci.sh or `task ci-full`

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

RED='\033[0;31m'
NC='\033[0m'

die() {
    echo -e "${RED}error:${NC} $*" >&2
    exit 1
}

# Run the core CI pipeline first
echo "==> Running core CI pipeline"
bash scripts/ci.sh

# Run complexity analysis (uses coverage from test run above)
echo "==> Running complexity analysis"
bash scripts/ci/pre-push/05-complexity.sh

# Run mutation testing (gremlins, dry-run in CI)
echo "==> Running mutation testing (gremlins)"
bash scripts/ci/pre-push/06-gremlins.sh

# Run coupling analysis
echo "==> Running coupling analysis"
bash scripts/ci/pre-push/07-coupling.sh

echo "==> OK (full pipeline)"
