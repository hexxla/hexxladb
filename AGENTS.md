# Agent instructions

**Canonical architecture:** Read **`docs/context/HEXAGONAL_ARCHITECTURE.md`** before adding or moving code under **`internal/`** or **`cmd/`**. Do not duplicate its full content here; the doc is the source of truth.

**Non-negotiables (summary):**

- **`internal/domain`** and **`internal/app`** define **ports** (interfaces); **`internal/adapters/...`** implements them. Domain and app **must not** import adapter implementation packages.
- **Port types must not** reference types from **`internal/adapters/...`**.
- **Boundary:** **`internal/domain`** and **`internal/app`** must **not** import **`internal/engine`** or **`internal/index`**. Persistence and key encoding belong in **`package hexxladb`** (module root) and **`internal/record`** / **`internal/lattice`**; outbound adapters implement ports in **`internal/adapters/out/...`** by calling only the public **`hexxladb`** API.
- **Business rules** live in domain/app; **`cmd/.../main.go`** only constructs, injects, and runs (composition root).

**Module path:** `github.com/hexxla/hexxladb`

**Public API at the module root:** The stable import path is **`github.com/hexxla/hexxladb`**, implemented as top-level `.go` files next to **`go.mod`** (e.g. **`db.go`**, **`tx.go`**, **`errors.go`**, **`options.go`**, **`doc.go`**, **`coord_export.go`**, tests). That matches the v1 checklist (“stable API at module root”) and is normal for Go libraries: **`internal/...`** stays private to the module, while the root package is what external callers import. A separate **`pkg/`** tree is optional and not required here.

**Modern Go (toolchain + style):** Honor the **`go`** directive in **`go.mod`** as the minimum language version. Use **`docs/context/MODERN_GO.md`** as a **reference** (Go 1.21–1.26 inventory)—not a mandate to adopt every API listed there; pick features deliberately and read **go.dev** release notes when you adopt them. In this codebase:

- Prefer **`errors.Is` / `errors.As`** and **`fmt.Errorf` with `%w`** for stable error semantics on the public API.
- Use **integer range loops** (`for i := range n`) and other patterns supported by the **`go`** version in **`go.mod`** where they simplify code.
- New benchmarks should use **`testing.B.Loop`** (Go 1.24+) unless the benchmark requires a custom loop shape.
- Structured logging in **`cmd/`** and adapters should use **`log/slog`** (not ad-hoc printf logging).
- **`make ci`** runs **golangci-lint** (including **modernize** and other analyzers aligned with current Go); fix new diagnostics rather than silencing them without cause.
- **Linting:** address root causes (correctness, bounds, safer APIs, small refactors) instead of **`//nolint`** or blanket suppressions. Use **`nolint` only sparingly**, with a **specific** linter name and **one-line** justification, when the finding is a documented false positive or the fix would be clearly worse (e.g. generated code). If unsure, prefer a code change and re-run **`make ci`**.

**HexxlaDB roadmap (product + implementation sequencing):** **`docs/hexxladb/DEVELOPMENT_ROADMAP.md`**. It complements **`docs/hexxladb/HEXXLA_DB.md`** (storage spec), **`docs/hexxladb/HEXXLA.md`** (memory model / geometry), and **`docs/checklist/HEXXLA_DB_V1.md`** (v1 checklist). When you complete a roadmap milestone, change scope, or fix a cross-doc inconsistency, **update the roadmap** in the same change (or a follow-up) so it stays accurate. **Roadmap hygiene:** keep milestone tables and open points truthful; link to normative specs instead of copying them; fix broken relative links; remove stale “planned” language once work ships; avoid duplicating **`HEXAGONAL_ARCHITECTURE.md`**—only reference it.

For editor-specific rules and skills, see **`.cursor/rules/`** and **`.cursor/skills/`** (they reference the same doc). Optional **Git** hooks: **`.pre-commit-config.yaml`** + **`make pre-commit-install`** — still run **`make ci`** before pushing; CI does not use pre-commit.
