#!/usr/bin/env bash
# Mosaic-specific context injection for file reads
# Injects Mosaic-specific context when reading mosaic-related files

set -euo pipefail

CYAN='\033[0;36m'
YELLOW='\033[1;33m'
NC='\033[0m'

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
cd "$ROOT"

file_path=$(echo "$1" | jq -r '.file_path // empty' 2>/dev/null || echo "")

if [[ -z "$file_path" ]]; then
  exit 0
fi

# Check if file is mosaic-related
mosaic_related=false
if [[ "$file_path" == cmd/mosaic-* ]] || \
   [[ "$file_path" == internal/adapters/in/mcpsrv/* ]] || \
   [[ "$file_path" == internal/adapters/out/* ]] || \
   [[ "$file_path" == internal/domain/* ]] || \
   [[ "$file_path" == internal/app/* ]] || \
   [[ "$file_path" == internal/port/* ]] || \
   [[ "$file_path" == docs/mosaic/* ]] || \
   [[ "$file_path" == docs/context/* ]] || \
   [[ "$file_path" == docs/hexxladb/* ]]; then
    mosaic_related=true
fi

if [[ "$mosaic_related" == false ]]; then
  exit 0
fi

echo -e "${CYAN}=== MOSAIC FILE CONTEXT ===${NC}"
echo "File: $file_path"
echo ""

# Determine which part of mosaic this is
if [[ "$file_path" == cmd/mosaic-* ]]; then
    echo "Mosaic Entry Point (cmd/)"
    echo "- Composition root only: construct, inject, run"
    echo "- mosaic-mcp: MCP server for HexxlaDB"
    echo ""
elif [[ "$file_path" == internal/adapters/in/mcpsrv/* ]]; then
    echo "MCP Server Adapter (Primary/Inbound)"
    echo "- Implements MCP tool handlers"
    echo "- Calls internal/app ports only"
    echo ""
elif [[ "$file_path" == internal/adapters/out/* ]]; then
    echo "Outbound Adapter"
    echo "- Implements ports from internal/app or internal/domain"
    echo "- Calls public hexxladb API only"
    echo ""
elif [[ "$file_path" == internal/domain/* ]]; then
    echo "Domain Layer"
    echo "- Pure business entities and value objects"
    echo "- Zero imports from internal/adapters/, internal/engine, internal/index"
    echo ""
elif [[ "$file_path" == internal/app/* ]]; then
    echo "Application Layer"
    echo "- Use case orchestration, defines ports (interfaces)"
    echo "- May import internal/domain only"
    echo ""
elif [[ "$file_path" == internal/port/* ]]; then
    echo "Port Layer (shared interfaces)"
    echo "- Shared port definitions used across app and domain"
    echo ""
elif [[ "$file_path" == docs/mosaic/* ]]; then
    echo "Mosaic Documentation"
    echo "- MCP tool documentation and configuration guides"
    echo ""
elif [[ "$file_path" == docs/context/* ]]; then
    echo "Architecture Context Documentation"
    echo "- HEXAGONAL_ARCHITECTURE.md is the source of truth for layer rules"
    echo ""
elif [[ "$file_path" == docs/hexxladb/* ]]; then
    echo "HexxlaDB Documentation"
    echo "- Storage layout, API reference, operations guides"
    echo ""
fi

echo "Key Principles:"
echo "- Hexagonal Architecture: all dependencies point inward"
echo "- Public API: package hexxladb (repo root)"
echo "- internal/ is module-private"
echo ""
echo -e "${CYAN}=== END MOSAIC CONTEXT ===${NC}"

exit 0
