#!/usr/bin/env bash
# Secret scanning for CI and local development
# Uses high-signal patterns and falls back gracefully

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
NC='\033[0m'

echo -e "${CYAN}> Running secret scanning${NC}"

errors=0

scan_files=()
while IFS= read -r -d '' file; do
    scan_files+=("$file")
done < <(git ls-files -z --cached --others --exclude-standard)

contains_secret_pattern() {
    local pattern="$1"
    ((${#scan_files[@]} > 0)) && grep -IqE -- "$pattern" "${scan_files[@]}"
}

die() {
    echo -e "${RED}error:${NC} $*" >&2
    ((errors++))
}

# ======================
# High-signal secret patterns
# ======================

echo "Checking for AWS credentials"
if contains_secret_pattern 'AKIA[0-9A-Z]{16}'; then
    die "Possible AWS Access Key ID found"
fi

echo "Checking for GitHub tokens"
if contains_secret_pattern 'gh[pousr]_[A-Za-z0-9_]{20,}'; then
    die "Possible GitHub Personal Access Token found"
fi

echo "Checking for private keys"
if contains_secret_pattern '-----BEGIN (RSA |OPENSSH |EC |PGP )?PRIVATE KEY-----'; then
    die "Private key material found"
fi

echo "Checking for OpenAI/Anthropic keys"
if contains_secret_pattern 'sk-[A-Za-z0-9]{48}'; then
    die "Possible OpenAI or Anthropic API key found"
fi

# Add more patterns here as needed

# ======================
# Try gitleaks if available (more comprehensive)
# ======================
if command -v gitleaks >/dev/null 2>&1; then
    echo "Running gitleaks for comprehensive secret detection"
    if ! gitleaks detect --source . --verbose 2>/dev/null; then
        die "Gitleaks detected potential secrets"
    fi
fi

# ======================
# Summary
# ======================

if ((errors > 0)); then
    echo -e "${RED}error:${NC} Secret scanning FAILED with $errors finding(s)"
    echo "Please remove any secrets before committing."
    exit 1
else
    echo -e "${GREEN}Secret scanning: OK${NC}"
fi
echo
