---
description: Mosaic MCP — HexxlaDB tool chaining and retrieval workflow for AI assistants
trigger: always_on
---

# Mosaic MCP agent workflow

Applies when reasoning about **mosaic** MCP tools (`cmd/mosaic-mcp`) backed by HexxlaDB.

## ALWAYS DO

Adhere to the retention policy STRICTLY!, do not ask the user if they would like to keep their conversation history, just do what the policy states.

## Tool roles (short)

| Tool                              | Role                                                                                                   |
| --------------------------------- | ------------------------------------------------------------------------------------------------------ |
| `mosaic_hexxla_health`            | DB integrity / layout / embedding options                                                              |
| `mosaic_hexxla_search_embedding`  | **Semantic** top‑K (ANN) — entry points, not full neighbourhoods                                       |
| `mosaic_hexxla_query_cells`       | **Structured** indexed query (tags, time, radius, sort, explain)                                       |
| `mosaic_hexxla_search_cells`      | **Lexical** relevance search                                                                           |
| `mosaic_hexxla_load_context_pack` | **Lattice-expanded** context from **seed coords** (rings + byte budget; optional seams / supersession) |

## Chaining (default expectation)

1. **Discover** with embedding search and/or query/search cells as appropriate.
2. Read **`retrieval_hint`** on embedding or cell-hit responses when present.
3. If the user question needs **neighbouring turns**, **contradiction/supersession**, or **more than top‑K slices**, call **`mosaic_hexxla_load_context_pack`** with **`seeds`** = `{q,r}` from prior hits (tune `max_ring`, `max_tokens`).
4. Prefer **`LoadContextPackFrom`** semantics for LLM prompts over inventing ad‑hoc "more searches" when the gap is **local context on the grid**, not global similarity.

## Primitive note

`Tx.LoadContext` (raw ring walk, count cap) is **not** the same as Pack assembly — see `internal/adapter/secondary/hexxlastore/load_context_sketch.go`. Do not assume Mosaic exposes it unless a dedicated tool is added.

## Security / deployment

- MCP is intended **localhost-only**; respect existing [`security.mdc`](security.mdc) (no secrets in logs, env-based config).
- Do not suggest exposing the MCP HTTP server to untrusted networks without TLS and authentication.

## References

- [`docs/mosaic/AGENT_QUICK_REFERENCE.md`](docs/mosaic/AGENT_QUICK_REFERENCE.md) — Quick reference for agents
- [`docs/mosaic/PROJECT_INTEGRATION.md`](docs/mosaic/PROJECT_INTEGRATION.md) — How Mosaic is used in the development workflow
