#!/usr/bin/env bash
# Fail if internal/domain or internal/app import forbidden engine/index packages.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

RED='\033[0;31m'
NC='\033[0m'
die() {
	echo -e "${RED}error:${NC} $*" >&2
	exit 1
}

forbidden=(
	"github.com/hexxla/hexxladb/internal/engine"
	"github.com/hexxla/hexxladb/internal/index"
)
for dir in internal/domain internal/app; do
	if [[ ! -d "$dir" ]]; then
		continue
	fi
	for imp in "${forbidden[@]}"; do
		if grep -R --include='*.go' -l -F "\"$imp\"" "$dir" 2>/dev/null | grep -q .; then
			echo "Forbidden import of $imp under $dir:" >&2
			grep -R --include='*.go' -n -F "\"$imp\"" "$dir" >&2 || true
			die "hex boundary check failed"
		fi
	done
done

echo "==> Hex boundaries OK (internal/domain and internal/app do not import internal/engine or internal/index)"
