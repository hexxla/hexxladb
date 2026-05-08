# Project Instructions

**Mosaic Retention Policy:** ALWAYS call `mosaic_hexxla_put_cell` for both `user_message` and `assistant_response` immediately after each exchange. Do not ask the user for permission to save conversation history. Include relevant topic-specific tags when calling put_cell.

**ALWAYS consider using Mosaic tools in your decision making** - when retrieving context, searching for information, or persisting knowledge, use the Mosaic read/write patterns defined in `.windsurf/rules/mosaic_mcp.md`.

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

**Canonical architecture:** Read **`docs/context/HEXAGONAL_ARCHITECTURE.md`** before adding or moving code under **`internal/`** or **`cmd/`**. Do not duplicate its full content here; the doc is the source of truth.

**Non-negotiables (summary):**

- **`internal/domain`** and **`internal/app`** define **ports** (interfaces); **`internal/adapters/...`** implements them. Domain and app **must not** import adapter implementation packages.
- **Port types must not** reference types from **`internal/adapters/...`**.
- **Boundary:** **`internal/domain`** and **`internal/app`** must **not** import **`internal/engine`** or **`internal/index`**. Persistence and key encoding belong in **`package hexxladb`** (module root) and **`internal/record`** / **`internal/lattice`**; outbound adapters implement ports in **`internal/adapters/out/...`** by calling only the public **`hexxladb`** API.
- **Business rules** live in domain/app; **`cmd/.../main.go`** only constructs, injects, and runs (composition root).

**Module path:** `github.com/hexxla/hexxladb`

**Public API:** The stable import is `github.com/hexxla/hexxladb` (package at repo root). `internal/...` is module-private. Top-level files: `db.go`, `tx.go`, `errors.go`, `options.go`, `doc.go`, `coord_export.go`, etc.

**Modern Go:** Honor the `go` directive in `go.mod` as minimum language version. Use deliberate features from current Go release notes.

- **Errors:** Prefer `errors.Is` / `errors.As` and `fmt.Errorf` with `%w` for public API error semantics.
- **Loops:** Use integer range loops (`for i := range n`) where they simplify code.
- **Benchmarks:** Use `testing.B.Loop` (Go 1.24+) unless custom loop needed.
- **Logging:** Use `log/slog` in `cmd/` and adapters (not printf).
- **CI:** Run `make ci` — includes golangci-lint with modernize analyzer.
- **Linting:** Fix root causes. Use `//nolint` sparingly with specific linter and one-line justification.

**Documentation:** Keep these aligned with code changes:

| Document                         | Purpose                        |
| -------------------------------- | ------------------------------ |
| `docs/hexxladb/HEXXLA_DB.md`     | Storage layout and keys        |
| `docs/hexxladb/HEXXLA.md`        | Memory model and concepts      |
| `docs/hexxladb/API_REFERENCE.md` | Exported API surface           |
| `docs/hexxladb/OPERATIONS.md`    | Production operations          |
| `docs/ROADMAP.md`                | Roadmap and out-of-scope items |
| `doc.go`                         | Package-level documentation    |

**Session tracking:** Update `TODOS.md` and `CHANGELOG.md` when working on related items:

- **TODOS.md** — lightweight session state; move items to Completed as work finishes; add new discoveries to Pending
- **CHANGELOG.md** — user-facing changes; add entries under `## [Unreleased]` for any user-visible feature, fix, or breaking change

**Versioning:** Follow `VERSIONING.md` (Semantic Versioning 2.0.0). Current: `v0.3.0`.

- Propose version bumps based on work: **minor** for features, **patch** for fixes, document breaking changes
- During v0.y.z phase: increment minor for releases with new features, patch for fixes only
- Before v1.0.0: breaking changes allowed but must be documented in CHANGELOG

**Git commits:** Keep messages short and simple. Avoid complex quoting, newlines, or special characters that confuse shells. One line preferred.

**Git hooks (optional):** `.pre-commit-config.yaml` + `make pre-commit-install` — still run `make ci` before pushing.

**Mosaic workflow:**

- **Retention policy:** See `.windsurf/workflows/mosaic-save-turns.md` for step-by-step workflow for saving conversation turns
- **Intelligent retrieval:** See `.windsurf/workflows/mosaic-intelligent-retrieval.md` for context retrieval patterns
- **Tag reuse:** See `.windsurf/workflows/mosaic-tag-reuse.md` for tag discovery and reuse before writing cells
- **Read patterns:** See `.windsurf/rules/mosaic_intelligent_reads.md` for choosing the right search tool
- **Write patterns:** See `.windsurf/rules/mosaic_intelligent_writes.md` for tag discovery before put_cell
- **Tag conventions:** See `.windsurf/rules/mosaic_tag_conventions.md` for tagging best practices
- **MCP workflow:** See `.windsurf/rules/mosaic_mcp.md` for tool chaining and retrieval workflow
