---
name: go-concurrency
description: Validates Go concurrency patterns
---

# Go Concurrency Skill

Concurrency bugs are among the hardest to debug — they are often non-deterministic. This skill identifies goroutine leaks, data races, and unsafe shared state. Run `go test -race` to confirm findings.

<checklist>
1. [ ] All goroutines are awaited — `WaitGroup`, channel drain, or `errgroup`
2. [ ] No goroutine leaks — goroutines can always be cancelled via context
3. [ ] Channels closed by sender only, never receiver
4. [ ] Mutex `Unlock` always deferred: `defer mu.Unlock()`
5. [ ] No data races — shared state protected by mutex or channel
6. [ ] `go test -race` passes
7. [ ] Channel buffer sizes justified — unbuffered preferred for synchronisation
8. [ ] Errors from goroutines collected and surfaced — not silently dropped
9. [ ] `sync.Pool` considered for high-frequency allocations
</checklist>

<examples>
<example>BAD: `go func() { process(item) }()` — goroutine not awaited, cannot be cancelled</example>
<example>GOOD: `g.Go(func() error { return process(ctx, item) })` — errgroup with cancellation</example>
<example>BAD: `mu.Lock(); doWork(); mu.Unlock()` — Unlock not deferred, panics lose lock</example>
<example>GOOD: `mu.Lock(); defer mu.Unlock(); doWork()`</example>
</examples>
