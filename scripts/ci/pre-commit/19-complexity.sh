#!/usr/bin/env bash
# scripts/ci/pre-commit/19-complexity.sh
# Fast complexity check on changed Go files only
# Runs gocyclo and gocognit with layer-specific thresholds

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
cd "$ROOT"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m'

errors=0

# Check if complexity checking is enabled
if [[ -f ".complexity.yml" ]]; then
    enabled=$(grep "enabled:" .complexity.yml | head -1 | awk '{print $2}' || echo "true")
    if [[ "$enabled" == "false" ]]; then
        echo -e "${YELLOW}> Complexity checks disabled in .complexity.yml${NC}"
        exit 0
    fi
fi

# Get changed Go files
changed_files=$(git diff --cached --name-only --diff-filter=ACM | grep '\.go$' || true)

if [[ -z "$changed_files" ]]; then
    echo -e "${GREEN}> No Go files changed - skipping complexity check${NC}"
    exit 0
fi

echo -e "${CYAN}> Running complexity check on changed files${NC}"

# Layer detection — maps file paths to .complexity.yml threshold keys.
# More-specific prefixes must come before less-specific ones.
detect_layer() {
    local file="$1"
    if [[ "$file" == internal/engine/* ]]; then
        echo "engine"
    elif [[ "$file" == internal/changelog/* ]]; then
        echo "changelog"
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
        echo "pkg_root"
    fi
}

# Get threshold from .complexity.yml or use default
get_threshold() {
    local layer="$1"
    local metric="$2"  # cyclomatic or cognitive

    if [[ -f ".complexity.yml" ]]; then
        # Extract threshold using simple grep/awk
        # Format: cyclomatic: 5 (indented under layer)
        local val
        val=$(grep -A 5 "${layer}:" .complexity.yml 2>/dev/null | grep "${metric}:" | head -1 | awk '{print $2}' || true)
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
        changelog)    [[ "$metric" == "cyclomatic" ]] && echo 20 || echo 30 ;;
        hnsw)         [[ "$metric" == "cyclomatic" ]] && echo 18 || echo 30 ;;
        record)       [[ "$metric" == "cyclomatic" ]] && echo 18 || echo 25 ;;
        views)        [[ "$metric" == "cyclomatic" ]] && echo 20 || echo 30 ;;
        lattice)      [[ "$metric" == "cyclomatic" ]] && echo 15 || echo 25 ;;
        pkg_root)     [[ "$metric" == "cyclomatic" ]] && echo 30 || echo 40 ;;
        cmd)          [[ "$metric" == "cyclomatic" ]] && echo 20 || echo 25 ;;
        examples)     [[ "$metric" == "cyclomatic" ]] && echo 150 || echo 250 ;;
        *)            [[ "$metric" == "cyclomatic" ]] && echo 15 || echo 20 ;;
    esac
}

# Check cyclomatic complexity
check_cyclomatic() {
    local file="$1"
    local layer
    local max
    local violations
    layer=$(detect_layer "$file")
    max=$(get_threshold "$layer" "cyclomatic")

    # gocyclo outputs: <complexity> <package> <function> <file:line>
    violations=$("$ROOT/scripts/tool.sh" gocyclo "$file" 2>/dev/null | awk -v max="$max" '$1 > max {print}' || true)

    if [[ -n "$violations" ]]; then
        echo -e "${RED}Cyclomatic complexity violation in $file (layer: $layer, max: $max):${NC}"
        echo "$violations" | while read -r line; do
            local comp
            local func
            local loc
            comp=$(echo "$line" | awk '{print $1}')
            func=$(echo "$line" | awk '{print $3}')
            loc=$(echo "$line" | awk '{print $4}')
            echo "  $func: $comp (max: $max) at $loc"
        done
        ((errors++))
    fi
}

# Check cognitive complexity
check_cognitive() {
    local file="$1"
    local layer
    local max
    local violations
    layer=$(detect_layer "$file")
    max=$(get_threshold "$layer" "cognitive")

    # gocognit outputs similar format
    violations=$("$ROOT/scripts/tool.sh" gocognit "$file" 2>/dev/null | awk -v max="$max" '$1 > max {print}' || true)

    if [[ -n "$violations" ]]; then
        echo -e "${RED}Cognitive complexity violation in $file (layer: $layer, max: $max):${NC}"
        echo "$violations" | while read -r line; do
            local comp
            local func
            local loc
            comp=$(echo "$line" | awk '{print $1}')
            func=$(echo "$line" | awk '{print $3}')
            loc=$(echo "$line" | awk '{print $4}')
            echo "  $func: $comp (max: $max) at $loc"
        done
        ((errors++))
    fi
}

# Process each changed file
for file in $changed_files; do
    if [[ -f "$file" ]]; then
        check_cyclomatic "$file"
        check_cognitive "$file"
    fi
done

if ((errors > 0)); then
    echo
    echo -e "${RED}error:${NC} Complexity check FAILED with $errors violation(s)"
    echo "  Fix the violations or adjust thresholds in .complexity.yml"
    exit 1
else
    echo -e "${GREEN}Complexity check: OK${NC}"
    echo
    exit 0
fi
