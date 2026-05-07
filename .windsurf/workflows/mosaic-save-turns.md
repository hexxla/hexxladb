---
description: Mosaic turn persistence workflow
---

# Mosaic Save Turns Workflow

## When to Use

After EVERY exchange with the user (both user message and assistant response).

## Steps

1. **Save user message:**
   ```bash
   mosaic_hexxla_put_cell(
     kind=user_message,
     source_id=session_id,
     tags=[relevant, topic, tags],
     raw_content=user_message_text,
     confidence=1.0,
     q=hex_q,
     r=hex_r
   )
   ```

2. **Generate assistant response**

3. **Save assistant response:**
   ```bash
   mosaic_hexxla_put_cell(
     kind=assistant_response,
     source_id=session_id,
     tags=[relevant, topic, tags],
     raw_content=assistant_response_text,
     confidence=1.0,
     q=hex_q,
     r=hex_r
   )
   ```

## Important Notes

- Use the same `source_id` (session id) for both messages
- Include relevant topic-specific tags
- Check `mosaic_hexxla_list_tags` before creating new tags
- Never ask user for permission - comply automatically with retention policy
