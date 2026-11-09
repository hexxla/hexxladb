# Project Instructions

## Architecture and API

Read [`docs/architecture/HEXAGONAL_ARCHITECTURE.md`](docs/architecture/HEXAGONAL_ARCHITECTURE.md) before adding or moving code under `internal/` or `cmd/`. It is the canonical boundary contract; do not duplicate it here.

- The stable import is `github.com/hexxla/hexxladb` at the module root. `internal/...` is module-private.
- The reference application under `examples/ports_adapters` owns its own ports and uses the public HexxlaDB API.
- Operator commands compose public API calls and typed diagnostics. Keep physical storage decoding out of commands.

## Go and verification

Honor the target module's `go.mod` and any file-specific build constraints. In Codex, use the [JetBrains Modern Go Guidelines plugin](https://github.com/JetBrains/go-modern-guidelines#codex) when available. Preserve public error, JSON, and storage contracts when applying modern idioms. Prefer `errors.Is` / `errors.As`, wrap public error causes with `%w`, use `log/slog` in commands and adapters, and use `testing.B.Loop` for ordinary benchmarks.

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

Keep planned work in the roadmap and completed history in `CHANGELOG.md`.

## Session and release tracking

- Add user-visible features, fixes, breaking changes, and material documentation changes under `CHANGELOG.md` → `[Unreleased]`.
- Follow [`VERSIONING.md`](VERSIONING.md) and Semantic Versioning 2.0.0. During v0.y.z, features increment the minor version and fixes increment the patch version; document pre-v1 breaking changes.

## Repository workflow

Preserve unrelated work and keep commits focused. Use short, one-line commit messages. The [optional Git hook](CONTRIBUTING.md#optional-secret-scanning-hook) scans staged changes for secrets.

Codex-compatible project skills live under `.agents/skills/<skill-name>/SKILL.md`. Add one only for a recurring HexxlaDB-specific workflow; keep general engineering policy in this file.
