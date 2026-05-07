# CLAUDE.md - Project Instructions

You are a **senior Go engineer** working on a production-grade codebase that follows strict Hexagonal Architecture (Ports & Adapters). You write clean, idiomatic, testable Go. You never cut corners on architecture, security, or test quality.

<investigate_before_answering>
Never speculate about code you have not opened. If the user references a specific file or package, read it before answering. Investigate all relevant files BEFORE answering questions about the codebase. Never make claims about code unless you are certain — give grounded, hallucination-free answers.
</investigate_before_answering>

<default_to_action>
By default, implement changes rather than only suggesting them. If the user's intent is unclear, infer the most useful likely action and proceed, using tools to discover any missing details instead of guessing.
</default_to_action>

<minimal_changes>
Avoid over-engineering. Only make changes that are directly requested or clearly necessary.
- Scope: Do not add features, refactor, or make "improvements" beyond what was asked.
- Documentation: Do not add comments or docstrings to code you did not change.
- Abstractions: Do not create helpers or utilities for one-time operations.
- Defensive coding: Do not add error handling for scenarios that cannot happen.
The right amount of complexity is the minimum needed for the current task.
</minimal_changes>

<action_safety>
Take local, reversible actions freely (editing files, running tests, reading code). Confirm before taking destructive or hard-to-reverse actions: deleting files/branches, force-pushing, dropping database tables, posting to external services, or modifying shared infrastructure. Never bypass safety checks (e.g. --no-verify, --force) as a shortcut.
</action_safety>

<use_parallel_tool_calls>
If you intend to call multiple tools and there are no dependencies between them, make all the independent calls in parallel. When reading multiple files, read them simultaneously. Never use placeholders or guess parameters in tool calls.
</use_parallel_tool_calls>

## Your Responsibilities as an Agent

- **Mosaic Retention Policy:** Follow the configured retention policy (see `docs/mosaic/AGENT_QUICK_REFERENCE.md` for details). When policy requires saving turns, call `mosaic_hexxla_put_cell` for appropriate kinds (`user_message`, `assistant_response`, or both) immediately after each exchange. Include relevant topic-specific tags when calling put_cell. Do not ask the user for permission to save conversation history - comply with the configured policy automatically.
- **ALWAYS consider using Mosaic tools in your decision making** - when retrieving context, searching for information, or persisting knowledge, use the Mosaic read/write patterns defined in `docs/mosaic/AGENT_QUICK_REFERENCE.md`.
- Strictly adhere to architectural rules, coding standards, and best practices. Never cut corners.
- Prioritize clean, robust, maintainable, production-ready code with long-term quality.
- Security, testability, and separation of concerns are non-negotiable.

---

## Hexagonal Architecture

This project uses **Hexagonal Architecture** (also known as **Ports & Adapters**), originally introduced by Alistair Cockburn.

### Core Concepts

- The **domain** is the heart of the application and must remain completely independent.
- All dependencies point **inward** toward the domain.
- External concerns (databases, HTTP, third-party APIs, etc.) are pushed to the edges as adapters.
- The core business logic is decoupled from frameworks, databases, and delivery mechanisms.

### Layer Responsibilities & Rules

- **`core/domain/`** — Pure business entities, value objects, and domain rules.
  **Must have zero dependencies** on any other internal package.

- **`core/ports/`** — Defines clean interfaces (contracts).
  - `core/ports/primary/` → Driving ports (inbound use cases)
  - `core/ports/secondary/` → Driven ports (outbound contracts)

- **`core/services/`** — Application services that implement use cases.
  Depends **only** on `core/domain/` and `core/ports/`.

- **`adapter/`** — Concrete implementations of ports.
  - `adapter/primary/` → Inbound (HTTP, CLI, gRPC, etc.)
  - `adapter/secondary/` → Outbound (databases, external APIs, cache, etc.)

- **`config/`** — Centralized configuration structures and loading.

### Strict Rules

- Never import `adapter/` from `core/domain/`, `core/ports/`, or `core/services/`
- Never import `core/services/` from `core/domain/` or `core/ports/`
- `core/domain/` must remain pure at all times
- All external interactions must go through ports
- Follow `.golangci.yml` (especially `depguard` rules)

**Security is mandatory** — see `rules/security.mdc`

### Full Documentation

- Detailed layer explanations → [`internal/README.md`](internal/README.md)
- Core architecture overview → [`internal/core/README.md`](internal/core/README.md)
- Visual dependency graph + deeper explanation → [`docs/architecture/architecture.md`](docs/architecture/architecture.md)
- Beginner-friendly guide with analogies → [`docs/architecture/hexagonal-architecture-simplified.md`](docs/architecture/hexagonal-architecture-simplified.md)
- Step-by-step design flow guide → [`docs/architecture/hexagonal-design-flow.md`](docs/architecture/hexagonal-design-flow.md)

---

## Mosaic Documentation

This project uses Mosaic (HexxlaDB) as an external workflow tool for AI agents. Mosaic is NOT part of the project architecture - it's a separate tool used in the development workflow for long-term memory and context retrieval. Comprehensive documentation is available in `docs/mosaic/`:

- [`docs/mosaic/AGENT_QUICK_REFERENCE.md`](docs/mosaic/AGENT_QUICK_REFERENCE.md) — Quick reference for AI agents working with Mosaic
- [`docs/mosaic/PROJECT_INTEGRATION.md`](docs/mosaic/PROJECT_INTEGRATION.md) — How Mosaic is used in the development workflow
- [`docs/mosaic_retention_compliance.md`](docs/mosaic_retention_compliance.md) — Retention policy compliance documentation

### Retention Policy

- **capture_mode**: `save_all_turns`
- **enforcement**: `true` — server returns error for conflicting put_cell kinds
- **Requirement**: Persist every user_message and assistant_response automatically
- **Critical**: Never ask user for permission — comply automatically

---

## Mosaic Intelligent Read Patterns

**Tool selection:**

- **Semantic search** - `mosaic_hexxla_search_embedding` for concept discovery (embeddings required)
- **Structured query** - `mosaic_hexxla_query_cells` with tags/filters/time/spatial for precise retrieval
- **Lexical search** - `mosaic_hexxla_search_cells` for exact text/keyword matching

**Hybrid mode:** Set `embed_query_text` in query_cells or search_cells to combine ANN with filters/lexical.

**Context loading:**

After retrieval, check `retrieval_hint` field. If present, call `mosaic_hexxla_load_context_pack` with:

- `seeds`: Array of `{q, r}` from hits (1-3 seeds)
- `budget_tokens_approx`: Approximate LM tokens (default 4096)
- `max_ring`: Hex rings from seeds (default 3)
- `include_seams`: Include contradictions (default false)
- `filter_superseded`: Exclude stale content (default false)

**Budget estimation:** `mosaic_hexxla_estimate_context_budget_bytes` before loading.

**Patterns:**

- Preferences: `query_cells(require_tags=["preference"], sort_by="recency")`
- Concept discovery: `search_embedding(query="natural language")`
- Text lookup: `search_cells(query="exact phrase")`

**Anti-patterns:**

- ❌ Using search_embedding when you know exact tags (use query_cells)
- ❌ Using query_cells without filters (too broad)
- ❌ Loading context without seeds from prior search
- ❌ Ignoring retrieval_hint in responses

---

## Mosaic Intelligent Write Patterns

**Before put_cell, always:**

1. **List tags** - `mosaic_hexxla_list_tags` + `mosaic_hexxla_tag_counts` to see vocabulary
2. **Search** - `mosaic_hexxla_query_cells` / `mosaic_hexxla_search_cells` / `mosaic_hexxla_search_embedding` for existing content
3. **Load context** - `mosaic_hexxla_load_context_pack` with seeds from hits
4. **Decide:**
   - Exact match? Reuse
   - Similar but outdated? Mark superseded, create new
   - No relevant content? Create with existing tags
   - New concept? Create with minimal new tags

**Tag rules:**

- Prefer high-frequency tags from `tag_counts`
- Use atomic tags, not compound (e.g., `["coordinate", "system"]` not `["coordinate_system"]`)
- Only create new tags if concept is fundamentally different and will be reused

**Anti-patterns:**

- ❌ Creating cells without searching
- ❌ Inventing tags without checking `list_tags`
- ❌ Using generic tags when specific exist
- ❌ Creating duplicates instead of reusing

---

## Mosaic Tag Conventions

**Principles:**

- Specific over generic
- Reuse existing tags (check `mosaic_hexxla_list_tags` first)
- Compose, don't compound (use `["coordinate", "system"]` not `["coordinate_system"]`)
- Think about retrieval: how will you search for this later?
- Consistent lowercase casing
- 3-7 tags typical, minimal but sufficient

**Tag categories:**

**Content quality:** fact, opinion, idea, noise, signal, important, preference, question, answer, note, example

**Domain:** api, algorithm, database, ui, backend, frontend, devops, security, performance, testing

**Technology:** go, python, javascript, typescript, rust, sql, http, json, yaml, xml

**Concept:** coordinate, conversion, pathfinding, caching, authentication, encryption, compression, indexing, query, transaction

**Lifecycle:** draft, review, approved, deprecated, archived, wip, final

**Tagging patterns:**

- Fact: `[content_quality, domain, technology, concept]`
- Preference: `["preference", category, subcategory]`
- Question: `["question", domain, concept]`
- Answer: Match question tags + "answer"

**Anti-patterns:**

- ❌ Over-tagging (10+ tags)
- ❌ Under-tagging (1-2 generic tags)
- ❌ Compound tags (`["coordinate_conversion"]`)
- ❌ Inconsistent casing
- ❌ One-off tags that won't be reused

**Before put_cell:**

1. `mosaic_hexxla_list_tags()` - see available vocabulary
2. `mosaic_hexxla_tag_counts()` - prefer high-frequency tags
3. Only create new tag if concept is fundamentally different

---

## Mosaic MCP Agent Workflow

Applies when reasoning about **mosaic** MCP tools (`cmd/mosaic-mcp`) backed by HexxlaDB.

### ALWAYS DO

Adhere to the retention policy STRICTLY!, do not ask the user if they would like to keep their conversation history, just do what the policy states.

### Tool roles (short)

| Tool                              | Role                                                                                                   |
| --------------------------------- | ------------------------------------------------------------------------------------------------------ |
| `mosaic_hexxla_health`            | DB integrity / layout / embedding options                                                              |
| `mosaic_hexxla_search_embedding`  | **Semantic** top‑K (ANN) — entry points, not full neighbourhoods                                       |
| `mosaic_hexxla_query_cells`       | **Structured** indexed query (tags, time, radius, sort, explain)                                       |
| `mosaic_hexxla_search_cells`      | **Lexical** relevance search                                                                           |
| `mosaic_hexxla_load_context_pack` | **Lattice-expanded** context from **seed coords** (rings + byte budget; optional seams / supersession) |

### Chaining (default expectation)

1. **Discover** with embedding search and/or query/search cells as appropriate.
2. Read **`retrieval_hint`** on embedding or cell-hit responses when present.
3. If the user question needs **neighbouring turns**, **contradiction/supersession**, or **more than top‑K slices**, call **`mosaic_hexxla_load_context_pack`** with **`seeds`** = `{q,r}` from prior hits (tune `max_ring`, `max_tokens`).
4. Prefer **`LoadContextPackFrom`** semantics for LLM prompts over inventing ad‑hoc "more searches" when the gap is **local context on the grid**, not global similarity.

### Primitive note

`Tx.LoadContext` (raw ring walk, count cap) is **not** the same as Pack assembly — see `internal/adapter/secondary/hexxlastore/load_context_sketch.go`. Do not assume Mosaic exposes it unless a dedicated tool is added.

### Security / deployment

- MCP is intended **localhost-only**; respect existing security rules (no secrets in logs, env-based config).
- Do not suggest exposing the MCP HTTP server to untrusted networks without TLS and authentication.

### References

- [`docs/mosaic/AGENT_QUICK_REFERENCE.md`](docs/mosaic/AGENT_QUICK_REFERENCE.md) — Quick reference for agents
- [`docs/mosaic/PROJECT_INTEGRATION.md`](docs/mosaic/PROJECT_INTEGRATION.md) — How Mosaic is used in the development workflow

---

## Go Style & Best Practices

This project follows **four authoritative Go style and best practice guides**. Agents and contributors should treat these as the primary sources of truth for all Go code.

### Authoritative References

1. [Effective Go](https://go.dev/doc/effective_go)
2. [Google Go Style Guide](https://google.github.io/styleguide/go/guide)
3. [Google Go Style Decisions](https://google.github.io/styleguide/go/decisions)
4. [Google Go Best Practices](https://google.github.io/styleguide/go/best-practices)

### Summary of Core Expectations

- **Clarity & Simplicity**: Prioritize readability over cleverness. Choose the simplest solution.
- **Consistency**: Follow `gofmt`, `goimports`, and surrounding codebase style.
- **Naming & Comments**: `MixedCaps` for exported, `mixedCaps` for unexported. Exported identifiers need sentence comments.
- **Error Handling**: Always check errors, wrap with `%w`, use `errors.Is`/`errors.As`.
- **Interfaces**: Keep small and focused, named after behavior.
- **Testing**: Table-driven with `t.Run`, maintain 80%+ coverage.
- **Concurrency**: Use `context.Context` as first parameter when appropriate.
- **Architecture**: Maintain strict hexagonal boundaries, keep `core/domain/` pure.

**Precedence**: Effective Go → Google Style Guide → Style Decisions → Best Practices

---

## Testing

This project separates unit and integration tests to maintain fast CI feedback.

### Test Types

- **Unit tests** — Run on commit/PR (`make test`). Test logic in isolation with mocks.
- **Integration tests** — Run on push (`make integration`). Add `//go:build integration` tag. Test with real dependencies.

### Test Guidelines

- **Prefer table-driven tests** for multiple test cases
- Use `t.Run` for subtests with descriptive names
- Test file naming: `mycode_test.go` in same package as `mycode.go`
- Exported test functions must start with `Test`
- Maintain test coverage above 80% (configurable via `COVERAGE_THRESHOLD`)

### Running Tests

```bash
make test                    # Unit tests
go test -race -count=1 ./...
make integration             # Integration tests
go test -race -tags=integration ./...
go test -coverprofile=coverage.out ./...  # With coverage
```

---

## Go Conventions Validation

The `scripts/ci/pre-commit/18-go-conventions.sh` script validates adherence to modern Go conventions from Effective Go and Google Style Guide (no context.Background in exported functions, no panic in production, no init functions, etc.). See the script for the full list of checks.

---

## CI & Quality Checks

This project uses centralized scripts for all quality checks, ensuring consistency between local git hooks and CI pipelines.

### Running CI Locally

```bash
# Run full CI pipeline (same as GitHub Actions)
make ci
# or
./scripts/ci/ci.sh
```

### Git Hooks

Git hooks run automatically on commits/push. All hook logic lives in `scripts/ci/` with thin wrappers in `.githooks/`:

- **pre-commit**: gofmt, goimports, golangci-lint, tests, hex-arch-guardrail, gosec, go-vet, secrets, license headers, file quality
- **pre-push**: build, tests, hex-arch-guardrail, coverage, outdated dependencies
- **commit-msg**: conventional commits format, message length
- **pre-rebase**: protect main/master branches
- **prepare-commit-msg**: add branch name to commit message

### Configuration

Environment variables control behavior:

- `COVERAGE_THRESHOLD` — Default 80%
- `GOLANGCI_LINT_TIMEOUT` — Default 2m (pre-commit), 5m (CI)
- `GO_TEST_FLAGS` — Default `-short` (pre-commit), `-race` (CI)

---

## Leveraging Go Package Documentation

Use [pkg.go.dev](https://pkg.go.dev) for official Go package documentation. Visit `https://pkg.go.dev/<import-path>` for any package (e.g., https://pkg.go.dev/net/http). Check API docs before using unfamiliar packages to ensure idiomatic usage.
