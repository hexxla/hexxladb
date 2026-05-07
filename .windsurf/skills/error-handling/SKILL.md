---
name: go-error-handling
description: Validates Go error handling patterns
---

# Go Error Handling Skill

Errors in Go are values and must always be handled. Ignored errors hide bugs. Unwrapped errors lose context. This skill enforces idiomatic, informative Go error handling.

<checklist>
1. [ ] All errors checked — never `_ = someFunc()`
2. [ ] Errors wrapped with `fmt.Errorf("...: %w", err)` to preserve context
3. [ ] `errors.Is` / `errors.As` used for comparison — never string matching
4. [ ] Errors returned early (fail fast, avoid deeply nested success paths)
5. [ ] Domain errors are sentinel values (`var ErrNotFound = errors.New(...)`)
6. [ ] Exported error variables for callers to compare against
7. [ ] No error swallowing in goroutines
8. [ ] Error messages are lowercase, no trailing punctuation (Go convention)
</checklist>

<examples>
<example>BAD: `result, _ := repo.Find(id)` — error silently discarded</example>
<example>BAD: `return fmt.Errorf("failed: %v", err)` — %v loses the error chain</example>
<example>GOOD: `return fmt.Errorf("order service find: %w", err)` — %w preserves chain</example>
<example>GOOD: `if errors.Is(err, ErrNotFound) { ... }` — type-safe comparison</example>
</examples>
