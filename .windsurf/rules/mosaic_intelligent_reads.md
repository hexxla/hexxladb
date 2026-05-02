---
description: Mosaic intelligent read patterns - choosing the right search tool and loading context
trigger: always_on
---

# Mosaic Intelligent Read Patterns

**Tool selection:**

- **Semantic search** - `mosaic_hexxla_search_embedding` for concept discovery (embeddings required)
- **Structured query** - `mosaic_hexxla_query_cells` with tags/filters/time/spatial for precise retrieval
- **Lexical search** - `mosaic_hexxla_search_cells` for exact text/keyword matching

**Hybrid mode:** Set `embed_query_text` in query_cells or search_cells to combine ANN with filters/lexical.

**Context loading:**

After retrieval, check `retrieval_hint` field. If present, call `mosaic_hexxla_load_context_pack` with:

- `seeds`: Array of `{q, r}` from hits (1-3 seeds)
- `budget_tokens_approx`: Approximate LM tokens (default 4096)
- `max_ring`: Hex rings from seeds (default 3)
- `include_seams`: Include contradictions (default false)
- `filter_superseded`: Exclude stale content (default false)

**Budget estimation:** `mosaic_hexxla_estimate_context_budget_bytes` before loading.

**Patterns:**

- Preferences: `query_cells(require_tags=["preference"], sort_by="recency")`
- Concept discovery: `search_embedding(query="natural language")`
- Text lookup: `search_cells(query="exact phrase")`

**Anti-patterns:**

- ❌ Using search_embedding when you know exact tags (use query_cells)
- ❌ Using query_cells without filters (too broad)
- ❌ Loading context without seeds from prior search
- ❌ Ignoring retrieval_hint in responses
