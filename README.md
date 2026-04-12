# go-hexagonal-architecture-template

Minimal [hexagonal](docs/context/HEXAGONAL_ARCHITECTURE.md) Go service: **`cmd/`**, **`internal/`**, **`docs/`** (template handbook under **`docs/context/`**), **`scripts/`**, **`.github/`**, **`.cursor/`** (tracked rules and skills for editors/agents). Runtime wiring (timeouts, **`r.Context()`**, **`slog`**, graceful shutdown) is summarized under **[HTTP server, graceful shutdown, request context, and logging](docs/context/HEXAGONAL_ARCHITECTURE.md#http-server-graceful-shutdown-request-context-and-logging)** in the architecture doc. Contributor setup: **[`CONTRIBUTING.md`](CONTRIBUTING.md)** · local env template: **[`.env.example`](.env.example)** · versioning / release notes: **[`CHANGELOG.md`](CHANGELOG.md)** (how-to and examples, not a maintained project log).

## Demo API

Run the server:

```bash
make run
# or: go run ./cmd/app
```

Default listen address is **`:8080`**. Override with `LISTEN_ADDR` (e.g. `LISTEN_ADDR=:3000`).

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/health` | Liveness JSON `{"status":"ok"}` |
| `POST` | `/v1/hash` | Body `{"message":"..."}` → `{"sha256":"<hex>"}` |
| `POST` | `/v1/store` | Body `{"text":"..."}` → `201` with `{"ok":true}` (in-memory, process-local) |
| `GET` | `/v1/messages` | `{"messages":["..."]}` — texts stored with `/v1/store` |

### Examples

```bash
curl -s http://127.0.0.1:8080/health
curl -s -X POST http://127.0.0.1:8080/v1/hash -H 'Content-Type: application/json' \
  -d '{"message":"hello"}'
curl -s -X POST http://127.0.0.1:8080/v1/store -H 'Content-Type: application/json' \
  -d '{"text":"alpha"}'
curl -s http://127.0.0.1:8080/v1/messages
```

Build a binary:

```bash
make build
./bin/app
```

### Configuration (environment)

| Variable | Default | Meaning |
| --- | --- | --- |
| `LISTEN_ADDR` | `:8080` | TCP address for `http.Server` |
| `HTTP_READ_TIMEOUT` | `15s` | |
| `HTTP_WRITE_TIMEOUT` | `15s` | |
| `HTTP_IDLE_TIMEOUT` | `60s` | |
| `HTTP_SHUTDOWN_TIMEOUT` | `10s` | Graceful shutdown deadline |
| `LOG_LEVEL` | `INFO` | `slog` level (`DEBUG`, `INFO`, `WARN`, `ERROR`, …) |
| `HTTP_MAX_BODY_BYTES` | `1048576` | Max JSON body size (aligned with domain max content size) |

**Quality gates:** **`make`** or **`make ci`** (runs **`scripts/ci.sh`** — same as GitHub Actions: format, **`go vet`**, tests with **`-race`**, **`govulncheck`**, **`golangci-lint`**, **`go mod tidy`**). Dependency update PRs: **`.github/dependabot.yml`**. Optional **[pre-commit](https://pre-commit.com)** hooks in **`.pre-commit-config.yaml`** — install with **`make pre-commit-install`**.
