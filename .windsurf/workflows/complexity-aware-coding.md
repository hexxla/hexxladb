---
description: Write complexity-aware code respecting hexagonal architecture thresholds
---

# Complexity-Aware Coding Workflow

You are writing Go code for a production-grade hexagonal architecture project. This workflow ensures your code respects complexity guardrails and maintains semantic stability.

## Complexity Thresholds by Layer

Before writing code, check the layer you're working in and respect these limits:

| Layer | Cyclomatic Max | Cognitive Max | Rationale |
|-------|---------------|---------------|-----------|
| `internal/domain` | 5 | 10 | Pure business logic, must be simple and focused |
| `internal/app` | 10 | 15 | Orchestration allowed, but keep manageable |
| `internal/adapters/in/...` | 15 | 20 | HTTP/CLI handlers — external concerns |
| `internal/adapters/out/...` | 12 | 18 | DB/API adapters — external integrations |
| package root (`*.go`) | 12 | 18 | Public API surface — moderate complexity OK |

## Before Writing Code

1. **Identify the layer** - Determine which hexagonal layer you're working in
2. **Review thresholds** - Check the max cyclomatic and cognitive complexity for that layer
3. **Plan the function** - Design small, focused functions that stay within limits
4. **Consider testability** - Complex code is hard to test with meaningful assertions

## While Writing Code

**Keep functions small:**
- Prefer single-responsibility functions
- Extract complex logic into smaller helper functions
- Avoid deeply nested conditions (use early returns instead)
- Use guard clauses to reduce nesting

**Prefer simple control flow:**
- Replace nested if/else with early returns
- Use switch statements for multiple conditions
- Avoid deep nesting (max 3-4 levels)
- Extract complex boolean logic into named functions

**Domain layer specific:**
- Keep business rules simple and declarative
- Use value objects to encapsulate complex logic
- Pure functions are easier to test and reason about
- Aim for cyclomatic ≤ 5, cognitive ≤ 10

**Services layer specific:**
- Orchestrate, don't implement complex logic
- Delegate complex operations to domain layer
- Use composition over inheritance
- Aim for cyclomatic ≤ 10, cognitive ≤ 15

**Adapters specific:**
- Translation logic can be moderately complex
- Isolate external concerns from core logic
- Use middleware for cross-cutting concerns
- Aim for cyclomatic ≤ 12-15, cognitive ≤ 18-20

## After Writing Code

1. **Run CI** - `make ci` or `./scripts/ci.sh` to catch all issues
2. **If violations found:**
   - Refactor function into smaller pieces
   - Extract helper functions
   - Simplify control flow
   - Consider if logic belongs in a different layer

3. **Check mutation testing** - Run `make mutation-test-dry` after writing domain/app logic
4. **If CRAP is high:**
   - Improve test coverage with specific assertions
   - Add property-based tests for domain logic
   - Add contract tests for port implementations
   - Or refactor to reduce complexity

## Semantic Stability Considerations

**High complexity = hard to test semantically:**
- Complex code is difficult to write meaningful tests for
- Mutation testing (gremlins) validates test effectiveness
- Aim for high mutation score (80%+) on critical paths

**When writing complex code:**
- Add property-based tests for pure functions (domain layer)
- Add contract tests for port interfaces
- Write tests that assert specific behavior, not just execution
- Consider invariants that should hold across inputs

## Examples

**Bad (high complexity):**
```go
func (db *DB) Put(key []byte, val []byte, opts ...Option) error {
    if key != nil {
        if len(key) > 0 {
            if val != nil {
                if len(opts) > 0 {
                    if opts[0].ttl > 0 {
                        // ... deeply nested logic
                    }
                }
            }
        }
    }
}
```

**Good (low complexity):**
```go
func (db *DB) Put(key []byte, val []byte, opts ...Option) error {
    if err := validateKey(key); err != nil {
        return fmt.Errorf("put: %w", err)
    }
    if val == nil {
        return fmt.Errorf("put: %w", ErrNilValue)
    }
    return db.applyOptions(key, val, opts)
}
```
