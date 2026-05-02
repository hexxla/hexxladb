---
description: How to save conversation turns to Mosaic according to retention policy
---

# Mosaic Save Turns Workflow

This workflow ensures compliance with the Mosaic retention policy (save_all_turns).

## When to use

Always use this workflow for every conversation turn when Mosaic MCP tools are available.

## Steps

1. **Receive user message**
   - Read the user's incoming message

2. **Save user message to Mosaic**
   - Call `mosaic_hexxla_put_cell` with:
     - `kind`: `user_message`
     - `raw_content`: the exact user message text
     - `source_id`: session identifier (e.g., `session-current`)
     - `confidence`: `1.0`
     - `tags`: relevant tags including topic-specific keywords (e.g., "animal", "rabbit", "dog", "preference", "question") plus standard tags like "conversation", "user-message"
     - `q`, `r`: available axial coordinates (increment as needed)

3. **Generate response**
   - Process the user's request and generate your response

4. **Save response to Mosaic**
   - Call `mosaic_hexxla_put_cell` with:
     - `kind`: `assistant_response`
     - `raw_content`: your exact response text
     - `source_id`: same session identifier as step 2
     - `confidence`: `1.0`
     - `tags`: relevant topic-specific tags matching the user message context plus standard tags like "assistant-response", "conversation"
     - `q`, `r`: next available axial coordinates

5. **Display response to user**
   - Show your response to the user in the chat

## Critical Rules

- **DO NOT** ask the user if they would like to keep their conversation history
- This must be automatic and systematic, not reliant on memory or discretion
- Both user_message and assistant_response must be saved for every turn
- Use the same source_id for both messages in the same exchange
- **ALWAYS include relevant topic-specific tags** (not just generic "conversation" tags) to enable effective tag-based retrieval

## Notes

- The retention policy is configured in `config/config.yaml`
- capture_mode: `save_all_turns`
- enforcement: `true` — server rejects put_cell operations that conflict with capture_mode
- **Tagging strategy**: Use specific, searchable tags based on content topics (e.g., "animal", "preference", "question", "fact") to complement semantic search. Tag searches can find content when semantic search misses relevant entries.
