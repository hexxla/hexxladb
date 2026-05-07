---
name: go-test-coverage
description: Analyzes Go test coverage and suggests improvements
---

# Go Test Coverage Skill

Coverage numbers are a floor, not a ceiling. A test that executes code but asserts nothing is noise. Semantic coverage is what matters: do your tests catch real bugs? Mutation testing (Gremlins) validates this by introducing small bugs — if your tests pass, they are too weak.

<checklist>
1. [ ] Tests assert specific return values — not just `err == nil`
2. [ ] Table-driven tests for multiple scenarios using `t.Run`
3. [ ] Error branches tested explicitly
4. [ ] Edge cases: empty/nil input, zero values, boundary conditions
5. [ ] Tests deterministic — no `time.Now()`, no global state, no random without seed
6. [ ] Mocks implement port interfaces, not concrete types
7. [ ] Integration tests use `//go:build integration` tag
8. [ ] Coverage ≥80% on `internal/domain` and `internal/app`
9. [ ] Test file naming: `foo_test.go` alongside `foo.go`
10. [ ] `make ci` passes with all tests
</checklist>

<mutation_testing>
Run `make mutation-test-dry` for a fast check. For critical domain code, run `make mutation-test`:

<checklist>
1. [ ] `make mutation-test-dry` passes (mutants are testable)
2. [ ] Mutation score ≥80% (80%+ of mutants killed)
3. [ ] No "LIVED" mutants on critical code paths (tests should catch introduced bugs)
4. [ ] If mutation score is low, add specific assertions that would fail on the mutant
</checklist>

<examples>
<example>BAD: `result, err := Add(2, 3); assert.NoError(t, err)` — mutation changes `+` to `-`, test still passes</example>
<example>GOOD: `result, _ := Add(2, 3); assert.Equal(t, 5, result)` — mutation caught, assertion fails</example>
</examples>
</mutation_testing>
