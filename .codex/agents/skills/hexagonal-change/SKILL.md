---
name: hexagonal-change
description: Structural changes across hexagonal boundaries (domain, ports, adapters, cmd). Use when adding features, refactoring internal packages, or deciding where code lives. Read docs/context/HEXAGONAL_ARCHITECTURE.md first.
---

# Hexagonal change workflow

**Canonical reference:** `docs/context/HEXAGONAL_ARCHITECTURE.md` (layout, dependency rules, vertical-slice steps, anti-patterns). This skill is procedural; do not treat it as a second copy of that doc.

## When to use

- New behavior that touches domain + persistence or transport
- Moving code between `internal/domain`, `internal/app`, and `internal/adapters/...`
- Adding interfaces, new binaries under `cmd/`, or new adapter packages

## Steps (align with the doc's "Workflow: add a vertical slice")

1. **Domain** — types and rules in `internal/domain/...` (no I/O).
2. **Secondary port** — interface(s) in `internal/domain`, `internal/app`, or `internal/port` (if shared and cycle-free).
3. **Secondary adapter** — implement in `internal/adapters/out/<tech>`; map infra ↔ domain here.
4. **Application** — constructors in `internal/app/...` take **interface** types only.
5. **Primary adapter** — `internal/adapters/in/...` (HTTP, gRPC, CLI, etc.): decode, delegate to app, encode errors.
6. **Wire** — `cmd/.../main.go`: build `out` → inject into app → attach `in` → run.

## Verification

- Run `make ci` or `./scripts/ci.sh` from repo root after substantive changes.
- If boundaries are unclear, re-read the **Dependency rules** table in `docs/context/HEXAGONAL_ARCHITECTURE.md`.
