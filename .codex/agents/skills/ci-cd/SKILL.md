---
name: ci-cd
description: Continuous integration for this repo — GitHub Actions, local parity with CI, and release notes. Use when editing workflows, pipelines, or release automation.
---

# CI/CD

## GitHub Actions

- Workflow: `.github/workflows/ci.yml` — runs `./scripts/ci.sh` on push/PR to `main` / `master` (same checks as local `make ci`).
- **Install golangci-lint** in CI via the official install script (pinned `v2.1.6`); `GO_VERSION` comes from `go.mod` via `actions/setup-go`.

## Optional Git pre-commit

- Config: **`.pre-commit-config.yaml`** — mirrors **golangci-lint** version with CI, runs **`golangci-lint-fmt`**, **`golangci-lint-full`**, **`go-unit-tests`**, plus generic hooks (YAML/JSON, whitespace). Install: `pip install pre-commit`, then `make pre-commit-install`. Not required for CI to pass; CI is still **`./scripts/ci.sh`**.

## Local parity (before push)

Run from the repo root:

```bash
make ci
# or
./scripts/ci.sh
```

Install **golangci-lint** once: [Install | golangci-lint](https://golangci-lint.run/welcome/install/) — e.g. `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest` and ensure `$(go env GOPATH)/bin` is on `PATH`.

If `golangci-lint` is missing locally, the script still runs fmt/vet/test/tidy and **skips** lint (CI always requires it via `CI=true`).

## Optional (later)

- [nektos/act](https://github.com/nektos/act) to run workflows locally
- A `build/` or `deployments/` tree (per [standard layout](https://github.com/golang-standards/project-layout)) if you add Docker, goreleaser, or k8s manifests—this minimal repo does not include those dirs by default
- Runbooks and product docs under **`docs/`**; template handbook (hexagonal layout, modern Go, resilience) under **`docs/context/`**
