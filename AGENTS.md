# Project Instructions

## Architecture and API

Read [`docs/architecture/HEXAGONAL_ARCHITECTURE.md`](docs/architecture/HEXAGONAL_ARCHITECTURE.md) before adding or moving code under `internal/` or `cmd/`. It is the canonical boundary contract; do not duplicate it here.

- The stable import is `github.com/hexxla/hexxladb` at the module root. `internal/...` is module-private.
- `internal/domain` and `internal/app` own ports and must not import adapters, `internal/engine`, or `internal/index`.
- Port types must not reference adapter implementation types.
- Outbound adapters call only the public root package. Business rules stay in domain/app; `cmd/...` only constructs, injects, and runs.

## Go and verification

Honor the minimum Go version in `go.mod` and use the matching project skill when its description applies. Prefer `errors.Is` / `errors.As`, wrap public error causes with `%w`, use `log/slog` in commands and adapters, and use `testing.B.Loop` for ordinary benchmarks.

Run the narrowest relevant checks first, then `task ci` before pushing. Fix lint root causes; use a specific `//nolint` only with a one-line justification.

## Documentation ownership

Update only the documents owned by changed behavior:

| Document                                                 | Responsibility                                         |
| -------------------------------------------------------- | ------------------------------------------------------ |
| `docs/architecture/HEXAGONAL_ARCHITECTURE.md`            | Package boundaries and dependency direction            |
| `docs/hexxladb/API_REFERENCE.md`, `doc.go`               | Public API guidance and package overview               |
| `docs/hexxladb/HEXXLA_DB.md`                             | Storage families, keys, and physical model             |
| `docs/hexxladb/HEXXLA.md`                                | Product memory concepts, independent of implementation |
| `docs/hexxladb/TX.md`, `DURABILITY.md`, `CHANGEFEED.md`  | Transaction, durability, and changefeed contracts      |
| `docs/hexxladb/CONFIGURATION.md`, `ENCRYPTION.md`        | Configuration and encryption behavior                  |
| `docs/hexxladb/OPERATIONS.md`, `PERFORMANCE_EVIDENCE.md` | Operations and reproducible performance evidence       |
| `docs/ROADMAP.md`                                        | Pending or deliberately deferred work only             |

Do not put completed history in the roadmap or `TODO.md`; it belongs in `CHANGELOG.md`.

## Session and release tracking

- [`TODO.md`](TODO.md) contains only active and pending session work.
- Add user-visible features, fixes, breaking changes, and material documentation changes under `CHANGELOG.md` → `[Unreleased]`.
- Follow [`VERSIONING.md`](VERSIONING.md) and Semantic Versioning 2.0.0. During v0.y.z, features increment the minor version and fixes increment the patch version; document pre-v1 breaking changes.

## Repository workflow

Preserve unrelated work and keep commits focused. Use short, one-line commit messages. Optional hooks are installed with `task pre-commit-install`, but they do not replace `task ci`.

Codex-compatible project skills live under `.agents/skills/<skill-name>/SKILL.md`. Add one only for a recurring HexxlaDB-specific workflow; keep general engineering policy in this file.
