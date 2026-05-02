---
description: Mosaic tag conventions for optimal retrieval - best practices for tagging cells
trigger: always_on
---

# Mosaic Tag Conventions

**Principles:**

- Specific over generic
- Reuse existing tags (check `mosaic_hexxla_list_tags` first)
- Compose, don't compound (use `["coordinate", "system"]` not `["coordinate_system"]`)
- Think about retrieval: how will you search for this later?
- Consistent lowercase casing
- 3-7 tags typical, minimal but sufficient

**Tag categories:**

**Content quality:** fact, opinion, idea, noise, signal, important, preference, question, answer, note, example

**Domain:** api, algorithm, database, ui, backend, frontend, devops, security, performance, testing

**Technology:** go, python, javascript, typescript, rust, sql, http, json, yaml, xml

**Concept:** coordinate, conversion, pathfinding, caching, authentication, encryption, compression, indexing, query, transaction

**Lifecycle:** draft, review, approved, deprecated, archived, wip, final

**Tagging patterns:**

- Fact: `[content_quality, domain, technology, concept]`
- Preference: `["preference", category, subcategory]`
- Question: `["question", domain, concept]`
- Answer: Match question tags + "answer"

**Anti-patterns:**

- ❌ Over-tagging (10+ tags)
- ❌ Under-tagging (1-2 generic tags)
- ❌ Compound tags (`["coordinate_conversion"]`)
- ❌ Inconsistent casing
- ❌ One-off tags that won't be reused

**Before put_cell:**

1. `mosaic_hexxla_list_tags()` - see available vocabulary
2. `mosaic_hexxla_tag_counts()` - prefer high-frequency tags
3. Only create new tag if concept is fundamentally different
