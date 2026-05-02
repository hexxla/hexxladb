---
description: Mosaic intelligent write patterns - tag discovery and reuse before put_cell
trigger: always_on
---

# Mosaic Intelligent Write Patterns

**Before put_cell, always:**

1. **List tags** - `mosaic_hexxla_list_tags` + `mosaic_hexxla_tag_counts` to see vocabulary
2. **Search** - `mosaic_hexxla_query_cells` / `mosaic_hexxla_search_cells` / `mosaic_hexxla_search_embedding` for existing content
3. **Load context** - `mosaic_hexxla_load_context_pack` with seeds from hits
4. **Decide:**
   - Exact match? Reuse
   - Similar but outdated? Mark superseded, create new
   - No relevant content? Create with existing tags
   - New concept? Create with minimal new tags

**Tag rules:**

- Prefer high-frequency tags from `tag_counts`
- Use atomic tags, not compound (e.g., `["coordinate", "system"]` not `["coordinate_system"]`)
- Only create new tags if concept is fundamentally different and will be reused
- See `.windsurf/rules/mosaic_tag_conventions.md` for detailed tagging guidelines

**Anti-patterns:**

- ❌ Creating cells without searching
- ❌ Inventing tags without checking `list_tags`
- ❌ Using generic tags when specific exist
- ❌ Creating duplicates instead of reusing
