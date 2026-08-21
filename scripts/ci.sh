#!/usr/bin/env bash
# Single entry point for the same checks CI runs — use before push: ./scripts/ci.sh or `task ci`.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

RED='\033[0;31m'
NC='\033[0m'
die() {
	echo -e "${RED}error:${NC} $*" >&2
	exit 1
}

need_cmd() {
	command -v "$1" >/dev/null 2>&1 || die "missing '$1' — install it and retry (PATH: $PATH)"
}

need_cmd go
need_cmd git

echo "==> Format (gofmt -l must be empty)"
mapfile -t gofiles < <(find . -type f -name '*.go' ! -path './vendor/*' ! -path './.git/*' 2>/dev/null || true)
if ((${#gofiles[@]} > 0)); then
	out=$(gofmt -l "${gofiles[@]}")
	if [[ -n "${out}" ]]; then
		echo "These files need gofmt (run: gofmt -w <file> or task fmt):"
		echo "${out}"
		exit 1
	fi
fi

echo "==> go vet (includes compiler-backed checks on packages)"
go vet ./...

echo "==> Hex boundaries (full hexagonal architecture validation per HEXAGONAL_ARCHITECTURE.md)"
bash scripts/check-hex-boundaries.sh

echo "==> go test (compiles packages + runs tests; -race catches data races)"
# -parallel 1: internal/engine tests share sync.Pool-backed read buffers; parallel subtests can
# false-positive the race detector on unrelated tests in the same package.
go test -count=1 -race -parallel 1 ./...

echo "==> govulncheck (known vulnerabilities in reachable code; https://go.dev/blog/govulncheck)"
go run golang.org/x/vuln/cmd/govulncheck@latest ./...

if command -v golangci-lint >/dev/null 2>&1; then
	echo "==> golangci-lint (see .golangci.yml: standard preset + errorlint, gosec, revive, …)"
	golangci-lint run ./...
else
	echo "==> golangci-lint (skipped — not on PATH)"
	echo "    Install: https://golangci-lint.run/welcome/install/"
	echo "    Example: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest"
	if [[ "${CI:-}" == "true" ]]; then
		die "golangci-lint is required in CI"
	fi
fi

echo "==> Complexity analysis (cyclomatic + cognitive + CRAP; see .complexity.yml)"
if command -v gocyclo >/dev/null 2>&1 && command -v gocognit >/dev/null 2>&1; then
	bash scripts/ci/pre-push/05-complexity.sh
else
	echo "    gocyclo/gocognit not on PATH — skipping"
	echo "    Install: go install github.com/fzipp/gocyclo/cmd/gocyclo@latest"
	echo "             go install github.com/uudashr/gocognit/cmd/gocognit@latest"
	if [[ "${CI:-}" == "true" ]]; then
		die "gocyclo and gocognit are required in CI"
	fi
fi

echo "==> go mod tidy (ensure go.mod / go.sum match module graph)"
go mod tidy
# Only enforce when CI runs or go.mod is in the index (skip repos with no go.mod yet).
if [[ "${CI:-}" == "true" ]] || git ls-files --error-unmatch go.mod >/dev/null 2>&1; then
	# Do not use plain `git status --porcelain` for go.mod: a staged new file shows as "A" even when tidy made no edits.
	if [[ -n "$(git diff --name-only -- go.mod go.sum 2>/dev/null)" ]]; then
		git diff -- go.mod go.sum >&2 || true
		die "go.mod or go.sum differ from index after go mod tidy — run: go mod tidy && git add go.mod go.sum && git commit"
	fi
	if [[ -f go.sum ]] && ! git ls-files --error-unmatch go.sum >/dev/null 2>&1; then
		die "go.sum exists but is not tracked — run: git add go.sum"
	fi
fi

echo "==> OK"
