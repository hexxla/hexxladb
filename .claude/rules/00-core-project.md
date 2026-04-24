# Core Project Rules

**Applies to:** All files
**Always apply:** Yes

---

## Module

- **Module path:** `github.com/hexxla/hexxladb` (see root `go.mod`). Use this prefix for all internal imports.

## Architecture (hexagonal)

The full layout, dependency table, and workflows live in **`docs/context/HEXAGONAL_ARCHITECTURE.md`**. Follow that document when it conflicts with generic "standard Go project layout" advice.

**Invariants (short):**

1. **`internal/domain`** and **`internal/app`** own **ports**; **`internal/adapters/in`** and **`internal/adapters/out`** implement them.
2. **Do not** import **`internal/adapters/...`** from **`internal/domain`** or **`internal/app`**.
3. **Port interfaces** must not mention types defined under **`internal/adapters/...`**.
4. **`internal/domain`** and **`internal/app`** must **not** import **`internal/engine`** or **`internal/index`**; use **`package hexxladb`** and ports implemented under **`internal/adapters/out/...`**.
5. **`cmd/...`** is the composition root (construct, inject, run)—no business rules.

## Public API layout (module root)

- The stable import **`github.com/hexxla/hexxladb`** is **`package hexxladb`** in the **repo root** (`.go` files next to `go.mod`). **`internal/...`** is module-private; external callers use only the root package and **`cmd/...`** for the binary.
- Full rationale and file roles: see below in this CLAUDE.md (same content expanded).

## Modern Go

- Follow the _Modern Go_ section below for toolchain version (`go.mod`), **`errors.Is` / `errors.As`**, **`log/slog`** in cmd/adapters, **`testing.B.Loop`** for new benchmarks, and CI expectations.
- Release inventory: **`docs/context/MODERN_GO.md`** — use as a **lookup**, not a checklist to apply every API.
- Verify changes with **`make ci`**.
- Prefer **fixing** linter findings over **`//nolint`**; see _Linting_ section below.

## Optional later

Error-handling and `context.Context` conventions across packages can be documented here once the team standardizes them.
