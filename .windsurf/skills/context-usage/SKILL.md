---
name: go-context-usage
description: Validates Go context.Context usage patterns
---

# Go Context Usage Skill

`context.Context` enables cancellation, deadlines, and request-scoped values across API boundaries. Misuse causes goroutine leaks, uncancellable operations, and hidden coupling.

<checklist>
1. [ ] `context.Context` is the **first parameter** of every function that needs it
2. [ ] Context is never stored in a struct field — pass it explicitly
3. [ ] Context is passed through the full call chain to I/O operations
4. [ ] `context.Background()` only used at program entry points or tests
5. [ ] `context.WithValue` used only for request-scoped metadata, not config
6. [ ] Cancellation checked in long-running loops: `select { case <-ctx.Done(): }`
7. [ ] Context not nil when passed to functions
</checklist>

<examples>
<example>BAD: `type Service struct { ctx context.Context }` — never store context in struct</example>
<example>BAD: `func FindOrder(id string) error` — missing ctx for any I/O operation</example>
<example>GOOD: `func FindOrder(ctx context.Context, id string) error`</example>
</examples>
