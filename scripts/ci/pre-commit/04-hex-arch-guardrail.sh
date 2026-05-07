#!/usr/bin/env bash
# scripts/ci/pre-commit/04-hex-arch-guardrail.sh
# Check hexagonal architecture rules

set -euo pipefail

echo "Checking hexagonal architecture rules..."

if [[ -f ./scripts/check-hex-boundaries.sh ]]; then
    ./scripts/check-hex-boundaries.sh
else
    echo "Warning: check-hex-boundaries.sh not found (skipped)"
fi
