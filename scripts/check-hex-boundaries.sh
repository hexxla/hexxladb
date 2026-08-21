#!/usr/bin/env bash
# Enhanced hexagonal boundary checker based on docs/context/HEXAGONAL_ARCHITECTURE.md
# Validates all dependency rules for hexagonal architecture compliance
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

errors=0
warnings=0

die() {
    echo -e "${RED}error:${NC} $*" >&2
    ((errors++))
}

warn() {
    echo -e "${YELLOW}warning:${NC} $*" >&2
    ((warnings++))
}

success() {
    echo -e "${GREEN}success:${NC} $*"
}

# Check if directory exists
check_dir() {
    local dir="$1"
    [[ -d "$dir" ]]
}

# Check for forbidden imports in a directory
check_forbidden_imports() {
    local dir="$1"
    local -a forbidden=("${@:2}")

    if ! check_dir "$dir"; then
        return 0
    fi

    for imp in "${forbidden[@]}"; do
        if grep -R --include='*.go' -l -F "\"$imp\"" "$dir" 2>/dev/null | grep -q .; then
            echo "Forbidden import of $imp under $dir:" >&2
            grep -R --include='*.go' -n -F "\"$imp\"" "$dir" >&2 || true
            die "hex boundary check failed: $dir importing $imp"
        fi
    done
}

# Check for allowed imports (should exist)
check_allowed_imports() {
    local dir="$1"
    local -a allowed=("${@:2}")

    if ! check_dir "$dir"; then
        return 0
    fi

    for imp in "${allowed[@]}"; do
        if ! grep -R --include='*.go' -l -F "\"$imp\"" "$dir" 2>/dev/null | grep -q .; then
            warn "Expected import $imp not found in $dir (may be intentional)"
        fi
    done
}

# Check for import cycles
check_import_cycles() {
    local dir="$1"

    if ! check_dir "$dir"; then
        return 0
    fi

    # Simple cycle detection - look for packages that import each other
    local -a packages
    mapfile -t packages < <(find "$dir" -name "*.go" -exec dirname {} \; | sort -u)

    for pkg in "${packages[@]}"; do
        local -a imports
        mapfile -t imports < <(grep -h --include='*.go' '^import' "$pkg"/*.go 2>/dev/null | grep -o '"[^"]*internal/[^"]*"' | sort -u || true)
        for imp in "${imports[@]}"; do
            local imp_path="${imp//\"/}"
            local imp_dir="$ROOT/$imp_path"
            if [[ -d "$imp_dir" ]] && [[ "$imp_dir" != "$pkg" ]]; then
                # Check if the imported package imports back
                if grep -R --include='*.go' -l -F "\"$(basename "$dir")/$(basename "$pkg")\"" "$imp_dir" 2>/dev/null | grep -q .; then
                    warn "Potential import cycle between $pkg and $imp_dir"
                fi
            fi
        done
    done
}

# Validate port interfaces don't reference adapter types
check_port_interface_types() {
    local port_dir="$1"

    if ! check_dir "$port_dir"; then
        return 0
    fi

    # Look for interface definitions that reference adapter types
    local -a interfaces
    mapfile -t interfaces < <(find "$port_dir" -name "*.go" -exec grep -l "type.*interface" {} \;)

    for intf_file in "${interfaces[@]}"; do
        if grep -q "github.com/hexxla/hexxladb/internal/adapters" "$intf_file"; then
            die "Port interface $intf_file references adapter types (forbidden by hexagonal architecture)"
        fi
    done
}

echo "==> Enhanced Hexagonal Boundary Check"
echo "==> Validating against docs/context/HEXAGONAL_ARCHITECTURE.md"
echo

# 1. Core packages MUST NOT import adapters
echo "Checking core packages for forbidden adapter imports..."
forbidden_adapters=(
    "github.com/hexxla/hexxladb/internal/adapters"
    "github.com/hexxla/hexxladb/internal/engine"
    "github.com/hexxla/hexxladb/internal/index"
)

for dir in internal/domain internal/app; do
    echo "  Checking $dir..."
    check_forbidden_imports "$dir" "${forbidden_adapters[@]}"
done

# 2. Domain should only import stdlib and domain packages
echo
echo "Checking domain package import restrictions..."
if check_dir "internal/domain"; then
    # Check for adapter imports in domain (forbidden)
    check_forbidden_imports "internal/domain" "github.com/hexxla/hexxladb/internal/adapters"
    check_forbidden_imports "internal/domain" "github.com/hexxla/hexxladb/internal/engine"
    check_forbidden_imports "internal/domain" "github.com/hexxla/hexxladb/internal/index"
    check_forbidden_imports "internal/domain" "github.com/hexxla/hexxladb/internal/app"
    check_forbidden_imports "internal/domain" "github.com/hexxla/hexxladb/internal/config"
    # Note: record package is allowed in domain for port interface types (Storage interface uses record.CellRecord)
    check_forbidden_imports "internal/domain" "github.com/hexxla/hexxladb/internal/changelog"
    check_forbidden_imports "internal/domain" "github.com/hexxla/hexxladb/internal/mvcc"
    check_forbidden_imports "internal/domain" "github.com/hexxla/hexxladb/internal/mvccspike"

    # Check for framework imports in domain (forbidden)
    if grep -R --include='*.go' -l -E "(github\.com/gin|github\.com/grpc|database/sql|github\.com/lib/pq|github\.com/go-redis)" internal/domain 2>/dev/null | grep -q .; then
        die "Framework imports found in domain package (forbidden)"
    fi
fi

# 3. App may import domain and config, but not adapters
echo
echo "Checking app package import restrictions..."
if check_dir "internal/app"; then
    # Check for forbidden imports in app
    check_forbidden_imports "internal/app" "github.com/hexxla/hexxladb/internal/adapters"

    # Should import domain (allowed)
    check_allowed_imports "internal/app" "github.com/hexxla/hexxladb/internal/domain"
fi

# 4. Adapters may import domain, app, config, but not other adapters (except in/out)
echo
echo "Checking adapter package import restrictions..."
for adapter_dir in internal/adapters/in/* internal/adapters/out/*; do
    if [[ -d "$adapter_dir" ]]; then
        echo "  Checking $adapter_dir..."
        # Adapters shouldn't import other adapters (except stdlib cross-adapter interfaces)
        other_adapters=$(find internal/adapters -mindepth 1 -maxdepth 1 -type d ! -name "$(basename "$adapter_dir")" | sed 's|.*/||')
        for other in $other_adapters; do
            check_forbidden_imports "$adapter_dir" "github.com/hexxla/hexxladb/internal/adapters/$other"
        done
    fi
done

# 5. Check cmd directory for business logic
echo
echo "Checking cmd directory for business logic violations..."
if check_dir "cmd"; then
    # Look for business logic patterns in cmd (should be minimal)
    business_logic=$(find cmd -name "*.go" -exec grep -l -E "(func.*Business|func.*Validate|func.*Calculate)" {} \; || true)
    if [[ -n "$business_logic" ]]; then
        warn "Business logic found in cmd directory (should be in domain/app):"
        echo "$business_logic"
    fi
fi

# 6. Validate port interfaces
echo
echo "Checking port interface type references..."
check_port_interface_types "internal/domain"
check_port_interface_types "internal/app"

# 7. Check for import cycles
echo
echo "Checking for import cycles..."
check_import_cycles "internal/domain"
check_import_cycles "internal/app"
check_import_cycles "internal/adapters"

# 8. Validate specific architectural rules from the doc
echo
echo "Checking specific architectural rules..."

# Rule: Domain and app MUST NOT import HTTP/gRPC/DB framework packages
framework_imports=$(grep -R --include='*.go' -l -E "(github\.com/gin|github\.com/grpc|database/sql|github\.com/lib/pq)" internal/domain internal/app 2>/dev/null || true)
if [[ -n "$framework_imports" ]]; then
    die "Framework imports found in core packages:"
    echo "$framework_imports"
fi

# Rule: Port interfaces MUST NOT reference adapter types
port_violations=$(
    while IFS= read -r file; do
        if grep -q "github.com/hexxla/hexxladb/internal/adapters" "$file"; then
            printf '%s\n' "$file"
        fi
    done < <(find internal/domain internal/app -name "*.go" -exec grep -l "interface" {} \;)
)
if [[ -n "$port_violations" ]]; then
    die "Port interfaces referencing adapter types:"
    echo "$port_violations"
fi

# Summary
echo
if ((errors > 0)); then
    echo -e "${RED}==> Hexagonal boundary check FAILED with $errors error(s)${NC}"
    exit 1
elif ((warnings > 0)); then
    echo -e "${YELLOW}==> Hexagonal boundary check PASSED with $warnings warning(s)${NC}"
    exit 0
else
    success "Hexagonal boundaries OK - all architectural rules validated"
    echo "  - Core packages (domain, app) do not import adapters"
    echo "  - Adapters follow dependency direction rules"
    echo "  - Port interfaces are clean"
    echo "  - No import cycles detected"
    echo "  - Framework packages isolated to adapters"
    exit 0
fi
