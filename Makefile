.PHONY: help ci test vet fmt lint mod-tidy govulncheck clean \
	pre-commit-install pre-commit-run pre-commit-update run build

# Bare `make` runs the full CI pipeline (same as `make ci`). Use `make help` to list targets.
.DEFAULT_GOAL := ci

help:
	@echo "make ci              Full pipeline (same as GitHub Actions: ./scripts/ci.sh)"
	@echo "make test|vet|fmt    Tests (-race), vet, gofmt -w"
	@echo "make lint            golangci-lint (binary on PATH)"
	@echo "make govulncheck     Vulnerability scan only (also runs inside make ci)"
	@echo "make mod-tidy        go mod tidy"
	@echo "make run|build|clean Run server, build bin/app, remove bin/"
	@echo "make pre-commit-*    Optional Git hooks (see CONTRIBUTING.md)"

# Run the full pipeline (same as CI). Install golangci-lint locally for the lint step.
ci:
	@./scripts/ci.sh

# Run the HTTP demo (see README for env vars).
run:
	go run ./cmd/app

# Production-style binary under bin/ (gitignored).
build:
	@mkdir -p bin
	go build -o bin/app ./cmd/app

clean:
	rm -rf bin

# Same invocation as scripts/ci.sh (handy to debug one step).
govulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# Optional Git hooks — requires: pip install pre-commit (or pipx install pre-commit)
pre-commit-install:
	pre-commit install

pre-commit-run:
	pre-commit run --all-files

pre-commit-update:
	pre-commit autoupdate

test:
	go test -count=1 -race ./...

vet:
	go vet ./...

fmt:
	@gofmt -w $$(find . -type f -name '*.go' ! -path './vendor/*' ! -path './.git/*')

lint:
	golangci-lint run ./...

mod-tidy:
	go mod tidy
