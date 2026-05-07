---
name: go-complexity
description: Validates Go code complexity against layer-specific thresholds
---

# Go Complexity Skill

When reviewing or writing Go code, check complexity against hexagonal architecture layer thresholds:

## Layer Thresholds

| Layer | Cyclomatic Max | Cognitive Max |
|-------|---------------|---------------|
| `core/domain/` | 5 | 10 |
| `core/ports/` | 3 | 5 |
| `core/services/` | 10 | 15 |
| `adapter/primary/` | 15 | 20 |
| `adapter/secondary/` | 12 | 18 |

## When Writing Code

- Keep functions small and focused (single responsibility)
- Avoid deeply nested conditions (use early returns instead)
- Use guard clauses to reduce nesting
- Extract complex logic into smaller helper functions
- Replace nested if/else with switch statements where appropriate
- Aim for max 3-4 levels of nesting

## When Reviewing Code

- Identify the hexagonal layer of the code
- Check if cyclomatic complexity exceeds layer threshold
- Check if cognitive complexity exceeds layer threshold
- Suggest refactoring if thresholds are exceeded
- Recommend extracting helper functions for complex logic
- Suggest using early returns to reduce nesting

## CRAP Score Considerations

- High CRAP = complex AND poorly tested = dangerous to change
- If CRAP is high, suggest either:
  1. Refactoring to reduce complexity
  2. Adding specific test assertions
  3. Adding property-based tests for domain logic
  4. Adding contract tests for port implementations
