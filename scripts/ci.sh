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

# Database tests can need several GiB of temporary space. Keep their
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

go mod tidy -diff
mkdir -p .tmp
scripts/tool.sh actionlint .github/workflows/*.yml
scripts/tool.sh golangci-lint run ./...
go run ./scripts/api_surface.go
go test -count=1 -race -parallel=1 -timeout=30m -covermode=atomic -coverprofile="${COVERAGE_PROFILE:-.tmp/coverage.out}" ./...
scripts/tool.sh govulncheck ./...
scripts/tool.sh go-licenses check ./... --disallowed_types=forbidden,restricted,unknown
