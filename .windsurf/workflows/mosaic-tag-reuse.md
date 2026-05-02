---
description: Workflow for intelligent tag discovery and reuse before writing cells
---

# Mosaic Tag Reuse Workflow

**Before put_cell:**

1. **List tags** - `mosaic_hexxla_list_tags()` + `mosaic_hexxla_tag_counts()` to see vocabulary
2. **Search** - `mosaic_hexxla_query_cells` / `mosaic_hexxla_search_cells` / `mosaic_hexxla_search_embedding` for existing content
3. **Load context** - `mosaic_hexxla_load_context_pack(seeds=hits, budget_tokens_approx=2048)`
4. **Decide:**
   - Exact match? Reuse
   - Similar but outdated? Mark superseded, create new
   - No relevant content? Create with existing tags
   - New concept? Create with minimal new tags
5. **Write** - `mosaic_hexxla_put_cell(kind, raw_content, source_id, confidence, tags, q, r)`

**Critical rules:**

- ALWAYS check existing tags before creating new ones
- ALWAYS search for existing content before writing
- PREFER high-frequency tags over new ones
- AVOID duplicate content - reuse or supersede
- USE specific tags, not generic
- COMBINE existing tags instead of creating compound tags
