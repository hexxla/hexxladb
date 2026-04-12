# Agent instructions

**Canonical architecture:** Read **`docs/context/HEXAGONAL_ARCHITECTURE.md`** before adding or moving code under **`internal/`** or **`cmd/`**. Do not duplicate its full content here; the doc is the source of truth.

**Non-negotiables (summary):**

- **`internal/domain`** and **`internal/app`** define **ports** (interfaces); **`internal/adapters/...`** implements them. Domain and app **must not** import adapter implementation packages.
- **Port types must not** reference types from **`internal/adapters/...`**.
- **Business rules** live in domain/app; **`cmd/.../main.go`** only constructs, injects, and runs (composition root).

**Module path:** `github.com/sploitzberg/go-hexagonal-architecture-template`

For editor-specific rules and skills, see **`.cursor/rules/`** and **`.cursor/skills/`** (they reference the same doc). Optional **Git** hooks: **`.pre-commit-config.yaml`** + **`make pre-commit-install`** — still run **`make ci`** before pushing; CI does not use pre-commit.
