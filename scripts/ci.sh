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
need_cmd mktemp

# Database stress tests can need several GiB of temporary space. Keep their
# files on the repository filesystem by default, but isolate each run and
# remove it on exit. Refuse to stack a new run on top of leftovers from an
# interrupted run; this keeps repeated failures from silently consuming disk.
CI_TMP_BASE="${TMPDIR:-$ROOT/.tmp}"
mkdir -p "$CI_TMP_BASE"
CI_TMP_BASE="$(cd "$CI_TMP_BASE" && pwd -P)"
shopt -s nullglob
stale_ci_runs=("$CI_TMP_BASE"/hexxladb-ci-run.*)
if ((${#stale_ci_runs[@]} > 0)); then
	die "stale CI temp run found at ${stale_ci_runs[0]}; remove it after confirming no CI process is active"
fi
CI_TMPDIR="$(mktemp -d "$CI_TMP_BASE/hexxladb-ci-run.XXXXXXXX")"
export TMPDIR="$CI_TMPDIR"

cleanup_ci_tmp() {
	if [[ -n "$CI_TMPDIR" && "$CI_TMPDIR" == "$CI_TMP_BASE"/hexxladb-ci-run.* ]]; then
		rm -rf --one-file-system -- "$CI_TMPDIR"
	fi
}
trap cleanup_ci_tmp EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

echo "==> Format (gofmt -l must be empty)"
# Check tracked and untracked source files while respecting .gitignore, so local
# build caches and generated evidence under ignored directories cannot pollute CI.
mapfile -t gofiles < <(git ls-files --cached --others --exclude-standard -- '*.go' | grep -v '^vendor/' || true)
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

echo "==> GitHub Actions workflow lint (pinned actionlint)"
scripts/tool.sh actionlint .github/workflows/*.yml

echo "==> go test (compiles packages + runs tests; -race catches data races)"
# -parallel 1: internal/engine tests share sync.Pool-backed read buffers; parallel subtests can
# false-positive the race detector on unrelated tests in the same package.
go test -count=1 -race -parallel 1 -timeout=30m ./...

echo "==> govulncheck (known vulnerabilities in reachable code; https://go.dev/blog/govulncheck)"
scripts/tool.sh govulncheck ./...

echo "==> golangci-lint (pinned; see .golangci.yml)"
scripts/tool.sh golangci-lint run ./...

echo "==> gosec (pinned standalone policy; medium severity and confidence)"
# G115 is enforced by golangci-lint, whose per-line //nolint:gosec explanations are
# more precise than standalone Gosec's separate #nosec syntax.
scripts/tool.sh gosec -quiet -severity medium -confidence medium -exclude-generated -exclude=G115 ./...

echo "==> Complexity analysis (cyclomatic + cognitive + CRAP; see .complexity.yml)"
bash scripts/ci/pre-push/05-complexity.sh

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
