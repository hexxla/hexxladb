# Hexagonal architecture (this repository)

**Audience:** Engineers and coding agents extending this **Go** boilerplate. Follow this document **before** adding packages under `internal/` or `cmd/`.

**What this is:** A **ports-and-adapters** (hexagonal) layout: **domain** and **application** code own **interfaces**; **adapters** implement them. Dependency arrows point **inward**. The goal is **modular packages**, **fast default tests**, and **clear boundaries**—not ceremony.

---

## Module path (imports)

All imports use the Go module path:

```text
github.com/hexxla/hexxladb
```

Example:

```go
import "github.com/hexxla/hexxladb/internal/app"
```

Rename the module in `go.mod` when you fork; update imports everywhere.

---

## Glossary (read this first)

| Term                  | Meaning in this repo                                                                                                                                                           |
| --------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **Core**              | **`internal/domain`** (entities, rules) + **`internal/app`** (use cases / orchestration).                                                                                      |
| **Port**              | A **Go `interface`** the core defines. **Primary** ports = how **inbound** adapters call the app. **Secondary** ports = what the app needs from the outside (DB, queue, etc.). |
| **Primary adapter**   | Inbound: HTTP, gRPC, CLI, Lambda handlers. Package path: **`internal/adapters/in/...`**.                                                                                       |
| **Secondary adapter** | Outbound: Postgres, Redis, email clients. Package path: **`internal/adapters/out/...`**.                                                                                       |
| **Composition root**  | **`cmd/<name>/main.go`**: only place that **constructs** concrete types and **injects** them. No business rules here.                                                          |

---

## Goals

- **Correct dependency direction:** **`internal/domain`** and **`internal/app`** define ports; **`internal/adapters/...`** implements them. Domain and app **MUST NOT** import concrete adapters.
- **Modularity:** Prefer **small packages** (by aggregate, feature, or capability). Avoid single “god” packages.
- **Fast builds and tests:** Default **`go test ./...`** stays **fast** (no real DB/network unless you opt in). Use **fakes** or small test doubles for ports.
- **Performance:** Avoid pointless layers and allocations on hot paths; **measure** (`benchmark`, `pprof`) before micro-optimizing. **I/O concerns** (retries, pooling, batching) live in **adapters**, not domain.

---

## Dependency rules (normative)

| Package                                                            | MAY import                                                                                                                                    | MUST NOT import                                                                                       |
| ------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| **`internal/domain`**                                              | Standard library; other **`internal/domain/...`** subpackages                                                                                 | **`internal/adapters/...`**; HTTP/gRPC/DB **framework or driver** packages                            |
| **`internal/app`**                                                 | **`internal/domain`**; **`internal/port`** (if used); port interfaces defined in **app** or **domain**                                        | Any **`internal/adapters/...`** implementation package                                                |
| **`internal/config`**                                              | Stdlib; env/config libraries                                                                                                                  | **`internal/adapters/...`** (keep config **parsing** here; **secrets** loaded in `cmd` if you prefer) |
| **`internal/port`** (optional)                                     | **`internal/domain`**; stdlib                                                                                                                 | **`internal/adapters/...`**                                                                           |
| **`internal/adapters/in/...`** and **`internal/adapters/out/...`** | **`internal/domain`**, **`internal/app`**, **`internal/port`**, **`internal/config`** (types only), stdlib, **infra SDKs** (sql, redis, etc.) | — (adapters are leaves that depend inward)                                                            |
| **`cmd/...`**                                                      | Any internal package (to wire the graph)                                                                                                      | **Business logic** (keep it in domain/app)                                                            |

**Invariant:** Port interfaces **MUST NOT** reference types defined in **`internal/adapters/...`**. If a handler needs a response DTO, define it in **domain** or **app** (or a neutral **`internal/port`** type), not in an adapter package.

**Note:** **`internal/config`** may stay struct-only; reading env/flags in **`cmd`** and passing **`config.Config`** into constructors is a common pattern—either is fine as long as **domain** stays free of env access.

### What needs a standalone adapter (and what does not)

**Adapters** are for **edges of the system**: an **inbound** adapter speaks a **protocol** (HTTP, gRPC, CLI, queue consumer) and turns requests into calls into **`internal/app`** / **`internal/domain`**. An **outbound** adapter **implements a port** (`interface`) whose real job is **I/O or another process**—databases, queues, HTTP clients, cloud APIs, message buses, filesystems, etc.

**You do not need a new package under `internal/adapters/in` or `.../out` for every technical step.** Pure behavior—rules, validation, deterministic transforms, and calculations that only need **domain types** and the **standard library** (hashing, formatting, parsing domain strings)—belongs in **`internal/domain`** (or orchestration in **`internal/app`**). That is not “missing an adapter”; it is **core** logic with no external integration.

**Add a port + outbound adapter** when behavior is **not** pure in that sense: it crosses a **network**, **disk**, **vendor SDK**, **HSM/KMS**, or you **must** substitute real vs test doubles behind a stable **`interface`** shared by multiple implementations. **Add or extend an inbound adapter** when you introduce a **new way into** the application (new routes, transport, or CLI)—not for each internal step of a single use case.

| Kind of work                                                            | Typical home                    |
| ----------------------------------------------------------------------- | ------------------------------- |
| Deterministic rules; pure functions; domain errors                      | **`internal/domain`**           |
| Orchestration: call domain + ports in order                             | **`internal/app`**              |
| Decode/encode a **protocol**; HTTP status mapping; auth middleware glue | **`internal/adapters/in/...`**  |
| Implement **`interface`** with **I/O** or **external systems**          | **`internal/adapters/out/...`** |

**Anti-pattern:** Creating **`internal/adapters/out/foo`** (or extra inbound layers) only to wrap stdlib or domain code with no real boundary—adds indirection without a new **integration** or **substitution** point. Prefer **domain** until a port is justified.

### Organizing `internal/domain` (flat files vs subpackages)

Separation of concerns **inside** the core is still **domain** vs **domain**—not adapters.

- **Default:** One package **`internal/domain`** with several **`*.go` files** (e.g. `limits.go`, `errors.go`, `user.go`) is appropriate while the model is small. Shared **`package domain`** keeps imports simple.
- **Add `internal/domain/<topic>/`** (a **subpackage**) or a **peer** package like **`internal/lattice`** (this repo) when a **cohesive** cluster of types, rules, and tests grows large enough that a dedicated import path and boundary reduce noise—still **pure**, still **no** `adapters`.
- **Do not** create a subfolder per helper file “just because.” Split when **concepts** and **exports** clearly belong to a named subdomain; otherwise keep related logic in the same `domain` package with extra files.

This keeps **hex boundaries** (adapters vs core) separate from **Go package boundaries** (how you subdivide the domain model).

---

## Canonical directory layout

Only **`internal/...`** is hidden from **external** modules importing this repo. If you later publish reusable libraries for **other** modules, add a root **`pkg/`** tree (see [Standard Go Project Layout](https://github.com/golang-standards/project-layout)); this minimal template does **not** ship **`pkg/`** by default.

```text
cmd/
  <appname>/
    main.go                 # composition root ONLY: config, new adapters, inject, run
  <another>/                # optional: second binary (worker, CLI, one Lambda per dir)

internal/
  config/                   # typed configuration (structs, validation); env/flags often in cmd
  domain/                   # entities, value objects, domain errors; business invariants
    lattice/                # e.g. pure lattice helpers (subpackage when cohesive)
  app/                      # use cases: orchestrate domain + secondary ports
  port/                     # OPTIONAL: shared port interfaces (still adapter-free)
  adapters/
    in/                     # primary adapters (inbound) — e.g. http/, grpc/
    out/                    # secondary adapters (outbound) — e.g. hexxladb/, postgres/
  tests/                    # OPTIONAL: shared test helpers, mocks, integration suites (or use build tags)

docs/                       # product or project documentation (add your own tree under here)
  context/                  # boilerplate handbook: hexagonal layout, modern Go, resilience
scripts/                    # helper scripts (e.g. local CI)
.github/                    # CI workflows
```

**Minimal repository root:** this checkout only includes the directories above plus metadata (`LICENSE`, `Makefile`, `go.mod`, etc.). It does **not** create empty [standard-layout](https://github.com/golang-standards/project-layout) folders such as **`api/`** (contract artifacts), **`web/`**, **`website/`**, **`build/`**, **`deployments/`**, **`configs/`**, **`test/`**, **`tools/`**, **`vendor/`**, **`third_party/`**, **`assets/`**, **`examples/`**, **`init/`**, or **`githooks/`**—add those when a product needs them so the tree stays small and assumptions (e.g. “we have a website”) are not baked in.

**Protocol artifacts:** OpenAPI, protobuf, or GraphQL schemas can live under **`docs/`** or a future **`api/`** you add deliberately—not at the root by default.

**MAY** add more **`cmd/...`** entrypoints that reuse the same **`internal/app`** core.

**Modularity:** **`internal/port`** exists only when **multiple** packages need the **same** interface and defining it in **domain** or **app** would cause **import cycles**. Otherwise **colocate** the interface with the type that **uses** it (often **`internal/app`** for application-facing ports, **`internal/domain`** for persistence-shaped ports).

**Optional:** When **`main` grows large**, introduce **`internal/bootstrap`** (or Wire under **`cmd/.../wire.go`**) to hold construction only—still **no** business rules.

---

## When developing a package

This module can be **application-only** (deployable service, nothing to `import` from outside) or **library + application** (others depend on your module and use a **published** API). Hexagonal rules are the same; **visibility** and **compatibility** change.

### Go import visibility

| Location                                         | Who may import it                                                                                                                                                             |
| ------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **`internal/...`**                               | **Only this module** (enforced by the compiler). Default home for the full hexagon: **domain**, **app**, **adapters**, **config** used by your binaries.                      |
| **`pkg/...`**                                    | **Any module** that adds a `require` on yours ([Standard Go Project Layout](https://github.com/golang-standards/project-layout)). Use for **stable, intentional** public API. |
| **Packages at the module root** (not `internal`) | Also importable by others—many teams prefer **`pkg/`** so “public surface” is obvious and **`internal/`** stays the default for private code.                                 |

**Go’s `internal` rule (compiler):** Packages inside **`internal/...`** may only be imported from code **under the same parent directory** as **`internal`** (i.e. the rest of **this module**), which **includes** **`pkg/...`**. So **`pkg` is allowed to import `internal`**—the language does not forbid it.

**Practical guideline (architecture):** **Adapters live under `internal/adapters/...` and are constructed and injected in `cmd` (and consumed through ports from `internal/app`).** You do **not** need **`pkg`** to import **`internal/adapters`** for the hexagon to work: **`cmd`** builds concrete adapters, passes **interface** values into **`internal/app`** (or into a **`pkg`** facade whose **exported** API only mentions **types and interfaces defined in `pkg`** or stdlib). Prefer **`pkg` not depending on `internal/adapters`** so your **published** surface does not couple to a specific DB/HTTP stack; keep that wiring in **`cmd`** + **`internal`**. If **`pkg`** imports other **`internal`** packages (e.g. shared helpers), avoid leaking **`internal`** types through **`pkg`**’s **exported** API—external modules must be able to use **`pkg`** without referencing **`internal`** paths in **their** code.

### What belongs in `pkg/` vs `internal/`

| Put in **`pkg/<name>/...`**                                                                                       | Keep in **`internal/...`**                                                                                  |
| ----------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| Types, interfaces, and **pure** functions you **semver** and document for external callers                        | Concrete **adapters** (HTTP, SQL, queues), **internal** app wiring, and anything tied to **one** deployment |
| A **narrow facade** (e.g. `NewClient`, `Service` interface, shared DTOs) if you hide implementation behind an API | **`cmd/`**-specific glue except what you deliberately expose                                                |
| Reusable **domain concepts** you want consumers to embed (optional—only if they are part of your **contract**)    | **Secrets**, env-only config, and **composition roots**                                                     |

Keep **`pkg/`** **small**. Every exported symbol is a **compatibility** promise; adapters and churn stay **private** in **`internal/`**.

### Hexagonal mapping (library + service)

- **Domain / ports** you **publish** → **`pkg/...`** (or a dedicated root package you commit to), so importers get rules and contracts **without** being forced to use your default HTTP/DB stacks.
- **Adapters** → **`internal/adapters/...`** (and optional **reference** implementations under **`pkg/...`** only if you explicitly semver them).
- **Composition** → **`cmd/...`** constructs adapters, injects ports into **`internal/app`** (and into **`pkg`** facades **via interfaces** when needed).
- **Typical flow:** **`cmd`** → **`internal/adapters`** (concrete) → **`internal/app`** (ports) → **`internal/domain`** / **`pkg`** (published types). **`pkg`** does not need to import adapters if **`cmd`** does all wiring.

### Versioning and docs

- Treat **`pkg/`** as **stable API**: follow [semantic versioning](https://semver.org/) for module tags; avoid breaking changes without a **major** bump.
- Document exported packages in **`CONTRIBUTING.md`** or **package doc comments**. If a recurring agent workflow needs extra guidance, keep that focused procedure in **`.agents/skills/`** rather than duplicating the architecture contract.

This template **does not** create **`pkg/`** by default. Add it when you have a **clear** public surface; until then, **`internal/`** + **`cmd/`** are enough for a service-only repo.

---

## Current state of this template

The repo is a **minimal composition root**, not an empty skeleton:

- **`cmd/hexxladb/main.go`** — **`config.Load()`**, **`slog`** JSON handler, **`app.New()`**, then exits (library-first; add servers or signal handling here if you build a long-running process). Wire **`hexxladb.Open`**, adapters, and any transports here.
- **`internal/config`** — **`Config`** + **`Load()`** from the environment (**`LOG_LEVEL`** for now).
- **`internal/domain`** — shared limits (**`MaxContentLen`**) and sentinel errors (**`ErrContentTooLarge`**, **`ErrInvalidInput`**); extend with pure types and rules as features land.
- **`internal/lattice`** — pure hex geometry (**`Coord`**, distance); **`PackedCoord`** / Morton encoding (**[`PACKED_COORD.md`](../../internal/lattice/PACKED_COORD.md)**).
- **`internal/app`** — **`Service`** shell; add ports and use cases when you wire storage or APIs.
- **`internal/adapters`** — placeholder only (**[`internal/adapters/README.md`](../../internal/adapters/README.md)**); add **`in/`** and **`out/`** packages when you introduce transports or infrastructure.

### Logging and signals

- **Logging:** The composition root sets **`slog.SetDefault`** with a JSON handler. Keep **business** logging out of **`internal/domain`** unless it helps operators without transport details.
- **Signals:** The process exits cleanly on **`SIGINT`** / **`SIGTERM`**. When you add an **`http.Server`** or gRPC server, set **timeouts** on the server, use **`Shutdown`** with a **deadline context**, and pass **`r.Context()`** (or equivalent) into **`internal/app`** so work cancels with the client.

---

## Workflow: add a vertical slice (for implementers / LLMs)

When adding behavior (e.g. “create user”), follow this order. **Do not** skip dependency direction.

1. **Domain:** Add or extend types and rules in **`internal/domain/...`** (no I/O).
2. **Secondary port:** Define the **interface(s)** the app needs (e.g. `UserRepository`) in **`internal/domain`** or **`internal/app`** (or **`internal/port`** if shared and cycle-free).
3. **Secondary adapter:** Implement the interface in **`internal/adapters/out/<tech>`** (e.g. `postgres`). Map DB rows ↔ domain types **here**.
4. **Application service:** Add a constructor in **`internal/app/...`** that accepts the **interface** type(s), not concrete adapters. Implement the use case by calling domain logic and ports.
5. **Primary port (optional):** If inbound adapters should depend on a narrow API, expose a small interface from **`internal/app`** (e.g. `CreateUser(ctx, email) error`).
6. **Primary adapter:** In **`internal/adapters/in/http`** (or grpc/cli), parse request → call **`internal/app`** → map result/errors to HTTP. **No business rules**—only validation of shape and mapping.
7. **Wire in `cmd/.../main.go`:** Load **`internal/config`**, construct **`adapters/out`**, pass interfaces into **`app`**, construct **`adapters/in`**, run (e.g. start a server or worker).

**Tests:** Write **`internal/app`** and **`internal/domain`** tests first using **fakes** or test doubles for secondary ports. Keep **`go test ./...`** fast.

---

## Composition root (`cmd/.../main.go`)

**Responsibilities (only):**

1. Load configuration (env, flags) into **`internal/config`** (or pass structs built here).
2. Build **secondary** adapters (repos, clients).
3. Construct **application** services with **constructor injection** of **interface** types.
4. Attach **primary** adapters and run (listen, poll, etc.).

**MUST NOT:** Encode business rules, SQL, or HTTP path logic beyond routing glue.

**Serverless:** Add **`cmd/lambda-<action>/main.go`** (or similar) **per deployable binary**—do not place `main` under **`internal/`**; keep **`internal/`** for libraries and tests only.

---

## Runtime path (one inbound request)

```text
Network client
  → internal/adapters/in/...     # decode protocol → app/domain inputs
  → internal/app (+ internal/domain)
  → secondary port (interface)
  → internal/adapters/out/...   # I/O
  → encode response → client
```

**Wiring** (what runs in `main` once) is separate from **request handling** (what runs per call).

---

## Build, test, and CI

- **Authoritative gate (same as GitHub Actions):** **`make ci`** (runs **`scripts/ci.sh`**) — `gofmt` check, **`go vet`**, **`go test -race`**, **`govulncheck`**, **`golangci-lint run`**, **`go mod tidy`** (+ git cleanliness for module files when **`CI=true`**).
- **Optional Git pre-commit:** **`.pre-commit-config.yaml`** ([pre-commit.com](https://pre-commit.com)) runs on `git commit`: file hygiene, **`golangci-lint fmt`** / **`golangci-lint-full`** (pinned like CI), **`go test`** (without `-race` for speed). Install: `pip install pre-commit` and `make pre-commit-install`. This does **not** replace `make ci` before push.
- **Editor integration:** Tracked **`.vscode/`** and **`.zed/`** settings provide format-on-save and tasks that invoke the repository's existing `make` targets. Validation and security checks remain editor-independent.

**Unit tests:** Prefer **fakes** and small **test doubles** for ports; keep the default test run **fast**.

**Integration tests:** Use a **build tag** (e.g. `//go:build integration`) and/or a separate Makefile target; optionally centralize helpers under **`internal/tests`** (like several reference repos).

**Import boundaries:** **`depguard`** in **`.golangci.yml`** blocks imports of **`internal/adapters/...`** from **`internal/domain`** and **`internal/app`** (composition from **`cmd`** and adapters remains unchanged). Extend rules there if you add **`pkg/`** or stricter layers.

---

## Performance notes

- **Domain/app:** Keep hot paths allocation-light; avoid framework types.
- **Ports:** Each interface call is an indirection—**avoid** splitting one cohesive operation into many micro-interfaces on hot paths without profiling data.
- **Adapters:** Pool connections, bound retries, and timeouts belong here.

---

## Anti-patterns (do not do this)

- Opening DB connections or HTTP clients **inside** **`internal/domain`** or **`internal/app`** constructors (inject **interfaces** from `main`).
- Importing **Gin**, **gRPC generated code**, or **SQL drivers** into **`internal/domain`** or **`internal/app`**.
- Putting **business rules** in HTTP handlers or repository structs without going through **domain/app**.
- **Adapters calling adapters** while skipping **app/domain**.
- **Port interfaces** that mention types from **`internal/adapters/...`**.
- Creating many packages for a trivial change—**modularity serves clarity**, not box-ticking.

---

## Testing strategy

- **SHOULD:** Test **domain** and **app** with **port fakes** or test doubles.
- **SHOULD:** Add integration tests **only** where boundaries matter; tag or script them separately.
- **SHOULD:** Add contract tests for port behaviour. See **`internal/domain/storagecontract`** — 22 reusable tests validating `domain.Storage`; any adapter calls `storagecontract.RunAll(t, factory)` to prove conformance.

---

## Non-goals

This template does **not** require DDD aggregates, event sourcing, CQRS, or extra Clean Architecture rings. Add patterns **when the problem warrants them**.

---

## Where else to look

| Location                              | Role                                                                                                                          |
| ------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| **`.vscode/`**, **`.zed/`**           | Shared editor settings and tasks backed by repository-native `make` commands.                                                 |
| **`.agents/skills/`**                  | Focused Codex-compatible workflows for recurring HexxlaDB tasks.                                                              |
| **`Makefile`**, **`scripts/ci.sh`**    | Full local CI parity (`make ci`).                                                                                             |
| **`CHANGELOG.md`**                     | User-facing release history and semver-relevant changes.                                                                      |
| **`.pre-commit-config.yaml`**          | Optional Git `pre-commit` hooks (`make pre-commit-install`).                                                                  |
| **`AGENTS.md`**                        | Short agent-facing invariants and a pointer to this document.                                                                 |

When automating refactors, treat **`docs/context/HEXAGONAL_ARCHITECTURE.md`** as the **source of truth** for layout and dependency direction.
