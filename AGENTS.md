# Project Instructions

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
| `docs/context/MODERN_GO.md`      | Go 1.21–1.26 feature inventory |

**Session tracking:** Update [`TODOS.md`](../TODOS.md) and [`CHANGELOG.md`](../CHANGELOG.md) when working on related items:

- **TODOS.md** — lightweight session state; move items to Completed as work finishes; add new discoveries to Pending
- **CHANGELOG.md** — user-facing changes; add entries under `## [Unreleased]` for any user-visible feature, fix, or breaking change

**Versioning:** Follow [`VERSIONING.md`](../VERSIONING.md) (Semantic Versioning 2.0.0). Current: `v0.1.0`.

- Propose version bumps based on work: **minor** for features, **patch** for fixes, document breaking changes
- During v0.y.z phase: increment minor for releases with new features, patch for fixes only
- Before v1.0.0: breaking changes allowed but must be documented in CHANGELOG

**Git commits:** Keep messages short and simple. Avoid complex quoting, newlines, or special characters that confuse shells. One line preferred. Example: `git commit -m "Add feature X"` not `git commit -m "Add feature X\n\nDetails..."`

Do not duplicate `HEXAGONAL_ARCHITECTURE.md` — reference it.

**Git hooks (optional):** `.pre-commit-config.yaml` + `make pre-commit-install` — still run `make ci` before pushing.
