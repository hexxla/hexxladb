#!/usr/bin/env bash
# scripts/ci/pre-push/05-complexity.sh
# Full complexity analysis with CRAP scoring
# Runs on all Go files before push

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
cd "$ROOT"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m'

errors=0
crap_errors=0
total_funcs=0
violation_funcs=0

# Check if complexity checking is enabled
if [[ -f ".complexity.yml" ]]; then
    enabled=$(grep "enabled:" .complexity.yml | head -1 | awk '{print $2}' || echo "true")
    if [[ "$enabled" == "false" ]]; then
        echo -e "${YELLOW}> Complexity checks disabled in .complexity.yml${NC}"
        exit 0
    fi
fi

# Read fail_on_violation (default true)
fail_on_violation=true
if [[ -f ".complexity.yml" ]]; then
    fov=$(grep "fail_on_violation:" .complexity.yml | head -1 | awk '{print $2}' || true)
    [[ "$fov" == "false" ]] && fail_on_violation=false
fi

# Check for required tools
if ! command -v gocyclo >/dev/null 2>&1; then
    echo -e "${RED}error:${NC} gocyclo not found. Install with: go install github.com/fzipp/gocyclo/cmd/gocyclo@latest"
    exit 1
fi

if ! command -v gocognit >/dev/null 2>&1; then
    echo -e "${RED}error:${NC} gocognit not found. Install with: go install github.com/uudashr/gocognit/cmd/gocognit@latest"
    exit 1
fi

echo -e "${CYAN}> Running full complexity analysis${NC}"

# Layer detection — maps file paths to .complexity.yml threshold keys.
# More-specific prefixes must come before less-specific ones.
detect_layer() {
    local file="$1"
    # internal layers (most-specific first)
    if [[ "$file" == internal/engine/* ]]; then
        echo "engine"
    elif [[ "$file" == internal/hnsw/* ]]; then
        echo "hnsw"
    elif [[ "$file" == internal/record/* ]]; then
        echo "record"
    elif [[ "$file" == internal/views/* ]]; then
        echo "views"
    elif [[ "$file" == internal/lattice/* ]]; then
        echo "lattice"
    elif [[ "$file" == internal/domain/* ]]; then
        echo "domain"
    elif [[ "$file" == internal/app/* ]]; then
        echo "app"
    elif [[ "$file" == internal/adapters/in/* ]]; then
        echo "adapters_in"
    elif [[ "$file" == internal/adapters/out/* ]]; then
        echo "adapters_out"
    elif [[ "$file" == internal/* ]]; then
        echo "default"
    elif [[ "$file" == cmd/* ]]; then
        echo "cmd"
    elif [[ "$file" == examples/* ]]; then
        echo "examples"
    else
        # Root package (./*.go) — public API surface
        echo "pkg_root"
    fi
}

# Get threshold from config
get_threshold() {
    local layer="$1"
    local metric="$2"

    if [[ -f ".complexity.yml" ]]; then
        local val=$(grep -A 5 "${layer}:" .complexity.yml 2>/dev/null | grep "${metric}:" | head -1 | awk '{print $2}' || true)
        if [[ -n "$val" ]]; then
            echo "$val"
            return
        fi
    fi

    # Hardcoded fallbacks mirror .complexity.yml defaults
    case "$layer" in
        domain)       [[ "$metric" == "cyclomatic" ]] && echo 5  || echo 10 ;;
        app)          [[ "$metric" == "cyclomatic" ]] && echo 10 || echo 15 ;;
        adapters_in)  [[ "$metric" == "cyclomatic" ]] && echo 15 || echo 20 ;;
        adapters_out) [[ "$metric" == "cyclomatic" ]] && echo 12 || echo 18 ;;
        engine)       [[ "$metric" == "cyclomatic" ]] && echo 25 || echo 40 ;;
        hnsw)         [[ "$metric" == "cyclomatic" ]] && echo 18 || echo 30 ;;
        record)       [[ "$metric" == "cyclomatic" ]] && echo 18 || echo 25 ;;
        views)        [[ "$metric" == "cyclomatic" ]] && echo 20 || echo 30 ;;
        lattice)      [[ "$metric" == "cyclomatic" ]] && echo 15 || echo 25 ;;
        pkg_root)     [[ "$metric" == "cyclomatic" ]] && echo 25 || echo 35 ;;
        cmd)          [[ "$metric" == "cyclomatic" ]] && echo 20 || echo 25 ;;
        examples)     [[ "$metric" == "cyclomatic" ]] && echo 150 || echo 250 ;;
        *)            [[ "$metric" == "cyclomatic" ]] && echo 15 || echo 20 ;;
    esac
}

# Get CRAP threshold
get_crap_threshold() {
    if [[ -f ".complexity.yml" ]]; then
        local val=$(grep -A 8 "crap:" .complexity.yml 2>/dev/null | grep "threshold:" | head -1 | awk '{print $2}' || true)
        if [[ -n "$val" ]]; then
            echo "$val"
            return
        fi
    fi
    echo 100
}

# Coverage map: funcname -> coverage_pct (numeric string, e.g. "87.5")
# Keyed by the bare function name as output by `go tool cover -func`.
declare -A coverage_map

load_coverage() {
    local coverage_file="${1:-coverage.out}"

    # Always regenerate — a stale profile from a partial run causes false CRAP violations.
    echo -e "${YELLOW}  Generating coverage profile (go test -count=1 ./...)...${NC}"
    rm -f "$coverage_file"
    go test -count=1 -coverprofile="$coverage_file" -covermode=atomic ./... >/dev/null 2>&1 || true

    if [[ ! -f "$coverage_file" ]]; then
        echo -e "${YELLOW}  Coverage generation failed — CRAP scores assume 0% coverage${NC}"
        return
    fi

    # `go tool cover -func` output (one line per function):
    #   github.com/hexxla/hexxladb/query_exec.go:295:  applyPredicates   82.4%
    #   github.com/hexxla/hexxladb/mvcc.go:10:         (*Tx).getCellVisibleRaw  100.0%
    # Fields: $1=pkg/file:line  $2=FuncName  $3=pct%
    while IFS= read -r covline; do
        [[ "$covline" == total:* ]] && continue
        local fname pct
        fname=$(echo "$covline" | awk '{print $2}')
        pct=$(echo "$covline"   | awk '{print $3}' | tr -d '%')
        [[ -z "$fname" || -z "$pct" ]] && continue
        coverage_map["$fname"]="$pct"
    done < <(go tool cover -func="$coverage_file" 2>/dev/null || true)
}

# Calculate CRAP score: (cyclomatic² × (1 - coverage/100)³) + cyclomatic
# Returns integer part only. Uses awk (always available) instead of bc.
calculate_crap() {
    local cyclomatic="$1"
    local coverage_pct="${2:-0}"
    awk -v c="$cyclomatic" -v p="$coverage_pct" 'BEGIN {
        f = p / 100
        d = 1 - f
        printf "%d\n", int(c * c * d * d * d + c)
    }'
}

# Look up per-function coverage percentage.
# gocyclo $3 field is the bare function name (e.g. "applyPredicates" or "(*Tx).QueryCells").
# go tool cover -func emits the same names, so we can match directly.
get_coverage_for_function() {
    local func="$1"

    # Direct match
    if [[ -n "${coverage_map[$func]:-}" ]]; then
        echo "${coverage_map[$func]}"
        return
    fi

    # gocyclo strips pointer syntax in some versions; try without * and parens
    local stripped="${func//\*/}"
    stripped="${stripped//(/}"
    stripped="${stripped//)/}"
    if [[ -n "${coverage_map[$stripped]:-}" ]]; then
        echo "${coverage_map[$stripped]}"
        return
    fi

    echo "0"
}

# Run analysis on all packages
echo -e "${CYAN}  Analyzing cyclomatic complexity...${NC}"

# Collect all violations - initialize with empty string to avoid unbound issues
cyclo_violations=()
cog_violations=()
crap_violations=()

# Process all Go files - cyclomatic
while IFS= read -r line; do
    [[ -z "$line" ]] && continue

    total_funcs=$((total_funcs + 1))

    # Parse: <complexity> <package> <function> <file:line>
    comp=$(echo "$line" | awk '{print $1}')
    pkg=$(echo "$line" | awk '{print $2}')
    func=$(echo "$line" | awk '{print $3}')
    loc=$(echo "$line" | awk '{print $4}')
    file=$(echo "$loc" | cut -d: -f1)
    # Strip leading ./ so glob patterns in detect_layer() match correctly
    file="${file#./}"

    # Skip test files — large integration/stress tests are not production code
    [[ "$file" == *_test.go ]] && continue

    layer=$(detect_layer "$file")
    max_cyclo=$(get_threshold "$layer" "cyclomatic")

    # Check cyclomatic
    if [[ "$comp" -gt "$max_cyclo" ]]; then
        cyclo_violations+=("$file|$func|$comp|$max_cyclo|$layer|$loc")
        violation_funcs=$((violation_funcs + 1))
    fi
done < <(find . -name '*.go' -not -path './vendor/*' -not -name '*_test.go' -exec gocyclo {} + 2>/dev/null || true)

# Check cognitive complexity
echo -e "${CYAN}  Analyzing cognitive complexity...${NC}"

while IFS= read -r line; do
    [[ -z "$line" ]] && continue

    comp=$(echo "$line" | awk '{print $1}')
    pkg=$(echo "$line" | awk '{print $2}')
    func=$(echo "$line" | awk '{print $3}')
    loc=$(echo "$line" | awk '{print $4}')
    file=$(echo "$loc" | cut -d: -f1)
    file="${file#./}"

    [[ "$file" == *_test.go ]] && continue

    layer=$(detect_layer "$file")
    max_cog=$(get_threshold "$layer" "cognitive")

    if [[ "$comp" -gt "$max_cog" ]]; then
        cog_violations+=("$file|$func|$comp|$max_cog|$layer|$loc")
    fi
done < <(find . -name '*.go' -not -path './vendor/*' -not -name '*_test.go' -exec gocognit {} + 2>/dev/null || true)

# CRAP scoring
echo -e "${CYAN}  Calculating CRAP scores...${NC}"
load_coverage
crap_threshold=$(get_crap_threshold)

# Recalculate CRAP for functions with coverage data
while IFS= read -r line; do
    [[ -z "$line" ]] && continue

    cyclo=$(echo "$line" | awk '{print $1}')
    pkg=$(echo "$line" | awk '{print $2}')
    func=$(echo "$line" | awk '{print $3}')
    loc=$(echo "$line" | awk '{print $4}')
    file=$(echo "$loc" | cut -d: -f1)
    file="${file#./}"

    [[ "$file" == *_test.go ]] && continue
    [[ "$file" == examples/* ]] && continue

    coverage=$(get_coverage_for_function "$func")
    crap=$(calculate_crap "$cyclo" "$coverage")

    if [[ "$crap" -gt "$crap_threshold" ]]; then
        crap_violations+=("$file|$func|$crap|$crap_threshold|$cyclo|$coverage|$loc")
    fi
done < <(find . -name '*.go' -not -path './vendor/*' -not -name '*_test.go' -exec gocyclo {} + 2>/dev/null || true)

# Report violations
cyclo_count=${#cyclo_violations[@]}
if [[ $cyclo_count -gt 0 ]]; then
    echo
    echo -e "${RED}Cyclomatic Complexity Violations:${NC}"
    printf "%-40s %-30s %8s %8s %15s\n" "File" "Function" "Actual" "Max" "Layer"
    echo "-----------------------------------------------------------------------------------------------"
    for v in "${cyclo_violations[@]}"; do
        IFS='|' read -r file func comp max layer loc <<< "$v"
        printf "%-40s %-30s %8s %8s %15s\n" "$file" "$func" "$comp" "$max" "$layer"
    done
    errors=$((errors + cyclo_count))
fi

cog_count=${#cog_violations[@]}
if [[ $cog_count -gt 0 ]]; then
    echo
    echo -e "${RED}Cognitive Complexity Violations:${NC}"
    printf "%-40s %-30s %8s %8s %15s\n" "File" "Function" "Actual" "Max" "Layer"
    echo "-----------------------------------------------------------------------------------------------"
    for v in "${cog_violations[@]}"; do
        IFS='|' read -r file func comp max layer loc <<< "$v"
        printf "%-40s %-30s %8s %8s %15s\n" "$file" "$func" "$comp" "$max" "$layer"
    done
    errors=$((errors + cog_count))
fi

crap_count=${#crap_violations[@]}
if [[ $crap_count -gt 0 ]]; then
    echo
    echo -e "${RED}CRAP Score Violations (threshold: $crap_threshold):${NC}"
    printf "%-40s %-30s %8s %8s %12s\n" "File" "Function" "CRAP" "Cyclo" "Coverage"
    echo "-----------------------------------------------------------------------------------------------"
    for v in "${crap_violations[@]}"; do
        IFS='|' read -r file func crap cyclo coverage loc <<< "$v"
        printf "%-40s %-30s %8s %8s %11s%%\n" "$file" "$func" "$crap" "$cyclo" "$coverage"
    done
    crap_errors=$((crap_errors + crap_count))
fi

# Summary
echo
total_exit_errors=$((errors + crap_errors))

if ((errors > 0)); then
    echo -e "${RED}error:${NC} Cyclomatic/cognitive complexity FAILED ($errors violation(s))"
fi
if ((crap_errors > 0)); then
    echo -e "${YELLOW}warn:${NC}  CRAP score violations: $crap_errors function(s) complex AND undertested"
    echo "  CRAP = (cyclomatic² × (1 - coverage/100)³) + cyclomatic"
    echo "  Threshold: $(get_crap_threshold) — tighten toward 50 as coverage improves"
fi

if ((total_exit_errors == 0)); then
    echo -e "${GREEN}Complexity analysis: OK${NC}"
    echo "  Total functions analyzed: $total_funcs"
    echo "  All metrics within thresholds"
    echo
    exit 0
elif [[ "$fail_on_violation" == "false" ]]; then
    echo
    echo "  Total functions analyzed: $total_funcs"
    echo "  fail_on_violation=false in .complexity.yml — exiting 0 (warn only)"
    echo
    exit 0
else
    echo
    echo "  Total functions analyzed: $total_funcs"
    echo "  Fix violations or adjust thresholds in .complexity.yml"
    echo
    exit 1
fi
