# Resilience and error handling

**Resilience patterns** (fault-tolerance / stability patterns) are design techniques that help applications handle failures gracefully, limit **cascading** failures, and keep availability reasonable in unreliable environments (microservices, cloud, anything over the network). They matter most when latency, timeouts, and **partial failure** are normal.

This document complements **`docs/context/HEXAGONAL_ARCHITECTURE.md`**: resilience and transport-specific **timeouts/retries** belong at **adapter** boundaries and in **`cmd`**; **domain** and **app** stay focused on rules and **what** can fail, not on HTTP client policies.

---

## Core resilience patterns

### Bulkhead

- **Purpose:** Isolate resources (threads, connections, memory, concurrency slots) so one failing or slow component cannot exhaust shared capacity for the rest of the system.
- **Benefit:** Stops one downstream dependency from starving the whole process.
- **Analogy:** Ship bulkheads that confine flooding to one compartment.

### Circuit breaker

- **Purpose:** Track failures to a dependency; when failures exceed a threshold, **open** the circuit and **fail fast** instead of hammering a sick service.
- **Benefit:** Reduces cascading failure and gives the dependency time to recover.
- **States:** Closed (normal) → Open (fail fast) → Half-open (probe for recovery).

### Retry

- **Purpose:** Retry failed operations, usually with **exponential backoff** and **jitter**.
- **Benefit:** Absorbs **transient** failures (brief network errors, short overload).
- **Best practice:** Only retry when safe (**idempotent** operations or explicit deduplication). Do not retry obvious non-transient errors blindly.

### Timeout

- **Purpose:** Bound how long an operation may run before it is cancelled and treated as failed.
- **Benefit:** Stops hung work from holding threads, connections, or user-facing slots indefinitely.

### Fallback

- **Purpose:** When the primary path fails, return an alternative (cached value, default, degraded feature).
- **Benefit:** Better UX than a hard error when degradation is acceptable.

### Rate limiting

- **Purpose:** Cap requests per time window (per client, per route, or globally).
- **Benefit:** Protects services from overload and abuse.

### Throttling

- **Purpose:** Slow down work when the system is under stress (admission control, queue delays).
- **Benefit:** Graceful degradation instead of sudden collapse under load.

---

## Other important practices

| Practice                     | Notes                                                                                                             |
| ---------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| **Chaos engineering**        | Deliberately inject failures in safe environments to learn where resilience is weak—not a single library pattern. |
| **Fail fast**                | Detect invalid state early; avoid long chains of partial success.                                                 |
| **Graceful degradation**     | Keep core paths available when non-critical parts fail.                                                           |
| **Self-healing**             | Automated recovery where safe (restarts, reconnects)—often infrastructure-level.                                  |
| **Leader election / quorum** | Coordination for distributed state (e.g. Raft)—only when your architecture needs it.                              |
| **Idempotency**              | Safe retries require idempotent handlers or deduplication keys.                                                   |

---

## Composing policies (outer → inner)

Patterns are often **layered**. A typical ordering (outer first, **timeout** innermost around the actual call) is:

1. **Fallback** (if you use one)
2. **Retry** (with backoff/jitter; only for safe/idempotent operations)
3. **Circuit breaker**
4. **Bulkhead** or **rate limiter**
5. **Timeout** (tight bound on the actual I/O)

Exact order depends on semantics; the point is to **avoid redundant work** (e.g. retrying after a breaker is open without reason) and to **bound** time and concurrency.

---

## Error handling in Go (idioms)

- Use **`fmt.Errorf("...: %w", err)`** for wrapping; **`errors.Is`** / **`errors.As`** at boundaries.
- **Sentinel errors** and **typed errors** in **domain** when they represent business outcomes; wrap **infrastructure** errors in **adapters** when crossing into app/domain.
- Prefer **`context.Context`** for cancellation and deadlines on **outbound** calls (adapters).
- **Do not** use **`panic`** for normal control flow.

---

## Should this template ship resilience “in the box”?

**Default stance: document patterns; do not add a generic resilience package to `internal/` in the skeleton.**

| Approach                                                | Recommendation                                                                                                                                                                                                   |
| ------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Only docs (this file + architecture doc)**            | Enough for a **template**: keeps dependencies minimal and avoids prescribing libraries before you know workloads (sync vs async, gRPC vs HTTP, queues).                                                          |
| **Per-adapter libraries**                               | When you need a breaker, retries, or limits, add **focused** dependencies in **`internal/adapters/out/...`** (or thin wrappers in **`cmd`**)—e.g. circuit breaker around one client, retry policy for one queue. |
| **Shared `internal/platform` or `internal/resilience`** | Optional **later**, if several adapters share identical policies and you want one thin wrapper—still **no** business rules inside; only composition of timeouts/breakers/retries around I/O.                     |

**Why not a big built-in package?** Resilience is **policy** and **operational**: thresholds differ per dependency and environment. Shipping unused abstractions fights the goal of a **small, fast** module graph. **Hexagonal** placement: **secondary adapters** and **composition root** own **how** calls are wrapped; **domain/app** own **what** errors mean for the business.

**When you add code:** prefer **`context`** deadlines, small **wrapper functions** or **typed clients** in the adapter package, and well-maintained **third-party** libraries at the edge, chosen per integration—not a one-size-fits-all `internal/resilience` framework on day one.

---

## When you add `http.Server`: timeouts and graceful shutdown

If you add an HTTP inbound adapter, expose **`ReadTimeout`**, **`WriteTimeout`**, **`IdleTimeout`**, and a **shutdown deadline** (e.g. via **`HTTP_*`** env vars) in **`internal/config`**, apply the first three to **`http.Server`**, and on **`SIGINT`** / **`SIGTERM`** call **`srv.Shutdown`** with **`context.WithTimeout`**. Handlers should pass **`r.Context()`** into **`internal/app`** and secondary adapters so work cancels when the client disconnects—aligned with the **timeout** and **context** guidance above.

---

## Related

- **`docs/context/HEXAGONAL_ARCHITECTURE.md`** — dependency direction, ports, adapters, where configuration lives.
- Go blog: [Error handling](https://go.dev/blog/error-handling-and-go) and [Go 1.13 errors](https://go.dev/blog/go1.13-errors).
