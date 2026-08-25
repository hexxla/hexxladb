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

die() {
    echo -e "${RED}error:${NC} $*" >&2
    exit 1
}

need_cmd() {
    command -v "$1" >/dev/null 2>&1 || die "missing '$1' — install it and retry (PATH: $PATH)"
}

need_cmd du
need_cmd git
need_cmd go
need_cmd gremlins
need_cmd grep
need_cmd mktemp
need_cmd awk
need_cmd tar
need_cmd tail
need_cmd timeout

# Gremlins copies the module once per worker. Never let those copies live below
# the module being copied: that recursively copied prior worker directories and
# exhausted the host filesystem in August 2026. Instead, copy only Git-visible
# source into a bounded snapshot and place Gremlins' work directory beside it.
TMP_ROOT="$ROOT/.tmp"
MAX_SOURCE_FILES=20000
MAX_SOURCE_KIB=262144

mkdir -p "$TMP_ROOT"
shopt -s nullglob
stale_runs=("$TMP_ROOT"/gremlins-run.*)
if ((${#stale_runs[@]} > 0)); then
    die "stale Gremlins run found at ${stale_runs[0]}; remove .tmp/gremlins-run.* after confirming no Gremlins process is active"
fi

RUN_ROOT="$(mktemp -d "$TMP_ROOT/gremlins-run.XXXXXXXX")"
SNAPSHOT="$RUN_ROOT/source"
WORK="$RUN_ROOT/work"
GO_CACHE="$RUN_ROOT/go-cache"
mkdir -p "$SNAPSHOT" "$WORK" "$GO_CACHE"

cleanup() {
    if [[ -n "${RUN_ROOT:-}" && "$RUN_ROOT" == "$TMP_ROOT"/gremlins-run.* ]]; then
        rm -rf --one-file-system -- "$RUN_ROOT"
    fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

mapfile -d '' -t source_files < <(git ls-files -z --cached --others --exclude-standard)
((${#source_files[@]} > 0)) || die "Git-visible source set is empty"
if ((${#source_files[@]} > MAX_SOURCE_FILES)); then
    die "source snapshot has ${#source_files[@]} files; safety limit is $MAX_SOURCE_FILES"
fi

source_kib="$(printf '%s\0' "${source_files[@]}" |
    du --apparent-size -ck --files0-from=- |
    tail -n 1 |
    awk '{print $1}')"
if ((source_kib > MAX_SOURCE_KIB)); then
    die "Git-visible source is ${source_kib} KiB; safety limit is ${MAX_SOURCE_KIB} KiB"
fi

printf '%s\0' "${source_files[@]}" |
    tar --null -T - -cf - |
    tar -xf - -C "$SNAPSHOT"

snapshot_kib="$(du --apparent-size -sk "$SNAPSHOT" | awk '{print $1}')"
if ((snapshot_kib > MAX_SOURCE_KIB)); then
    die "source snapshot is ${snapshot_kib} KiB; safety limit is ${MAX_SOURCE_KIB} KiB"
fi

case "$WORK/" in
    "$SNAPSHOT/"*) die "Gremlins work directory must not be inside its source snapshot" ;;
esac

echo -e "${CYAN}> Gremlins isolated snapshot: ${#source_files[@]} files, ${snapshot_kib} KiB${NC}"

# Packages suitable for mutation testing (fast, self-contained tests).
# Add packages here as their test suites become self-contained and fast.
MUTATION_TARGETS=(
    "./internal/lattice"
    "./internal/record"
    "./internal/changelog"
    "./internal/hnsw"
    "./internal/index"
)

if [[ -n "${TARGET:-}" ]]; then
    MUTATION_TARGETS=("$TARGET")
fi

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

# CI mode: use --dry-run for speed in pre-push; full runs via `task mutation-test`.
DRY_RUN=true
if [[ "${GREMLINS_FULL:-}" == "1" ]]; then
    DRY_RUN=false
fi

RUN_TIMEOUT=10m
if [[ "$DRY_RUN" == "false" ]]; then
    RUN_TIMEOUT=2h
fi

overall_ok=true

for pkg in "${MUTATION_TARGETS[@]}"; do
    if ! list_error=$(cd "$SNAPSHOT" && GOCACHE="$GO_CACHE" go list "$pkg" 2>&1); then
        die "cannot load mutation target $pkg from isolated snapshot: $list_error"
    fi

    echo -e "${CYAN}> Gremlins: $pkg${NC}"

    if [[ "$DRY_RUN" == "true" ]]; then
        (cd "$SNAPSHOT" && GOCACHE="$GO_CACHE" TMPDIR="$WORK" timeout --kill-after=30s "$RUN_TIMEOUT" gremlins unleash --dry-run "$pkg")
        echo -e "${GREEN}  ✓ $pkg (dry-run)${NC}"
        continue
    fi

    # Full run — capture output to parse gates
    output=$(cd "$SNAPSHOT" && GOCACHE="$GO_CACHE" TMPDIR="$WORK" timeout --kill-after=30s "$RUN_TIMEOUT" gremlins unleash "$pkg" 2>&1)
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
    echo -e "${YELLOW}> For full mutation testing: task mutation-test${NC}"
    exit 1
fi

if [[ "$DRY_RUN" == "true" ]]; then
    echo -e "${GREEN}> Mutation dry-run complete (no gates enforced; run 'task mutation-test' for full check)${NC}"
else
    echo -e "${GREEN}> Mutation testing passed${NC}"
fi
