---
description: Workflow for intelligent context retrieval using Mosaic
---

# Mosaic Intelligent Retrieval Workflow

**Step 1: Choose search tool**

- Semantic: `mosaic_hexxla_search_embedding(query)` for concept discovery
- Structured: `mosaic_hexxla_query_cells(require_tags=[...])` for precise retrieval
- Lexical: `mosaic_hexxla_search_cells(query="text")` for exact matches
- Hybrid: Set `embed_query_text` in query_cells or search_cells

**Step 2: Execute search**

- Use appropriate parameters (max_results, tags, filters)
- Set `explain: true` to understand why cells matched

**Step 3: Check retrieval_hint**

- Present? → Proceed to step 4
- Not present? → Top-K hits sufficient, stop

**Step 4: Load context pack**

```go
mosaic_hexxla_load_context_pack(
    seeds=[{q,r} from hits],
    budget_tokens_approx=2048,
    max_ring=3,
    include_seams=false
)
```

**Step 5: Process and respond**

- Review cells for relevance
- Check for contradictions if seams included
- Formulate response using retrieved context

**Scenarios:**

- API design: search_embedding → load_context
- Preferences: query_cells(require_tags=["preference"])
- Exact function: search_cells(query="function name")
