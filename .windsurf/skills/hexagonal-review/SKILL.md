---
name: hexagonal-review
description: Reviews code for Hexagonal Architecture compliance
---

# Hexagonal Architecture Review Skill

This project uses Hexagonal Architecture. All dependencies must point **inward** toward the domain. Violations make code untestable and create hidden coupling to infrastructure.

<investigate_before_answering>
Read all referenced files before making architectural assessments. Never speculate about import paths you have not verified.
</investigate_before_answering>

<checklist>
1. [ ] `internal/domain` has zero imports from `internal/adapters/...`, `internal/engine`, `internal/index`
2. [ ] `internal/app` only imports `internal/domain` and port interfaces it defines
3. [ ] No `internal/adapters/...` packages imported in domain or app layers
4. [ ] Port interfaces defined in `internal/domain`, `internal/app`, or `internal/port` — not in adapters
5. [ ] Outbound adapters in `internal/adapters/out/...` implement ports calling only the public `hexxladb` API
6. [ ] No business logic in adapter or cmd layers
7. [ ] Domain/app types are not infrastructure framework types
8. [ ] All external I/O goes through a port interface
9. [ ] `scripts/check-hex-boundaries.sh` passes
</checklist>

For each violation: quote the import path, state which rule it breaks, and suggest the fix.
