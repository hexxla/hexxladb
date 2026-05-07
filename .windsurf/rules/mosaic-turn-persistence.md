---
description: Mosaic turn persistence - save all conversation turns
trigger: always_on
---

# Mosaic Turn Persistence

This project uses Mosaic for long-term memory and context retrieval. Per the retention policy configured in the Mosaic server (see config/config.yaml), ALL conversation turns MUST be persisted automatically.

## Retention Policy

- **capture_mode**: save_all_turns
- **enforcement**: true — server returns error for conflicting put_cell kinds

## Required Actions

After EVERY exchange with the user:

1. **User turn**: Call `mosaic_hexxla_put_cell(kind=user_message, source_id=session_id, tags=[...], raw_content=..., confidence=1.0, q, r)`
2. **Assistant turn**: Call `mosaic_hexxla_put_cell(kind=assistant_response, source_id=session_id, tags=[...], raw_content=..., confidence=1.0, q, r)`

Use the same `source_id` (session id) for both messages in the same exchange.

## Tag Hygiene

- Include relevant topic-specific tags when calling put_cell
- Reuse existing tags from `mosaic_hexxla_list_tags` when possible
- Check `mosaic_hexxla_tag_counts` to prefer high-frequency tags
- See `.windsurf/rules/mosaic_tag_conventions.md` for detailed tagging guidelines

## Never Ask Permission

Do NOT ask the user if they want to save conversation history. Comply with the retention policy automatically.

## References

- `docs/mosaic/AGENT_QUICK_REFERENCE.md` — Quick reference for agents
- `docs/mosaic/PROJECT_INTEGRATION.md` — How Mosaic is used in the development workflow
- `docs/mosaic_retention_compliance.md` — Retention policy compliance documentation
