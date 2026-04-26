.PHONY: help ci integration stress bench bench-stress fuzz test vet fmt lint mod-tidy govulncheck clean bench-tmp \
	pre-commit-install pre-commit-run pre-commit-update run build demo

# Bare `make` runs the full CI pipeline (same as `make ci`). Use `make help` to list targets.
.DEFAULT_GOAL := ci

help:
	@echo "make ci              Full pipeline (same as GitHub Actions: ./scripts/ci.sh)"
	@echo "make integration     Optional slower tests (go test -tags=integration -race ./...)"
	@echo "make stress          Optional very large cell-count tests (TMPDIR defaults to ./.tmp; not CI)"
	@echo "make bench           Run benchmarks (go test -bench=. -benchmem ./...; not in CI)"
	@echo "make bench-stress    Longer API benches (default preload=all: 512..10k; HEXXLA_BENCH_PRELOAD=extreme for 50k; not CI)"
	@echo "make fuzz            Short fuzz smoke (internal/record + internal/engine; not in CI)"
	@echo "make test|vet|fmt    Tests (-race), vet, gofmt -w"
	@echo "make lint            golangci-lint (binary on PATH)"
	@echo "make govulncheck     Vulnerability scan only (also runs inside make ci)"
	@echo "make mod-tidy        go mod tidy"
	@echo "make run|build|clean Run cmd/hexxladb, build bin/hexxladb, remove bin/"
	@echo "make demo            Run conversational_memory example (DB defaults to .tmp/demo/memory.db)"
	@echo "                     Override DB path: make demo DEMO_DB=/path/to/my.db"
	@echo "make pre-commit-*    Optional Git hooks (see CONTRIBUTING.md)"

# Benchmark temp directory (defaults to repo-local ./.tmp; override with TMPDIR=/path).
bench-tmp:
	@mkdir -p $(or $(TMPDIR),$(CURDIR)/.tmp)

# Run the full pipeline (same as CI). Install golangci-lint locally for the lint step.
ci:
	@./scripts/ci.sh

# Optional durability/stress tests (not run in default CI). See CONTRIBUTING.md.
integration:
	go test -count=1 -race -tags=integration ./...

# Extreme scale (100k+ cells by default; minutes, large disk). TMPDIR defaults to repo ./.tmp (override if needed). See CONTRIBUTING.md.
stress:
	@$(MAKE) bench-tmp
	TMPDIR=$(or $(TMPDIR),$(CURDIR)/.tmp) go test -count=1 -race -tags=stress ./...

# Benchmarks — not part of default CI (keeps PRs fast). See CONTRIBUTING.md.
bench:
	@$(MAKE) bench-tmp
	TMPDIR=$(or $(TMPDIR),$(CURDIR)/.tmp) go test -count=1 -bench=. -benchmem ./...

# Longer runs: preload 512, 2k, 10k (default). Override: make bench-stress HEXXLA_BENCH_PRELOAD=extreme (adds 50k; needs huge TMPDIR).
bench-stress:
	@$(MAKE) bench-tmp
	TMPDIR=$(or $(TMPDIR),$(CURDIR)/.tmp) HEXXLA_BENCH_PRELOAD=$(or $(HEXXLA_BENCH_PRELOAD),all) go test -count=1 -bench='BenchmarkAPI_(GetCell|AscendCellsBySource|LoadContext|LoadContextAt|WalkRing|WalkRingAt)/' -benchmem -benchtime=500ms ./.

# Short fuzz smoke — not part of default CI. For longer runs: go test -fuzz=... -fuzztime=30s ./path
fuzz:
	go test ./internal/record -fuzz=FuzzDecodeCell -fuzztime=2s
	go test ./internal/engine -fuzz=FuzzDecodeHeaderPage -fuzztime=2s
	go test ./internal/engine -fuzz=FuzzParseAndReplayWAL -fuzztime=2s

# Run the composition-root binary (see README for env vars).
run:
	go run ./cmd/hexxladb

# Production-style binary under bin/ (gitignored).
build:
	@mkdir -p bin
	go build -o bin/hexxladb ./cmd/hexxladb

clean:
	rm -rf bin

# Run the conversational_memory example demo.
# Database defaults to .tmp/demo/memory.db (created on first run, reused on subsequent runs).
# Override: make demo DEMO_DB=/absolute/or/relative/path/to/my.db
demo:
	@mkdir -p .tmp/demo
	go run ./examples/conversational_memory $(if $(DEMO_DB),-db $(DEMO_DB),)

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
