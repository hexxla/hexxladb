.PHONY: help ci integration stress bench bench-api bench-stress fuzz test vet fmt lint mod-tidy govulncheck clean bench-tmp \
	pre-commit-install pre-commit-run pre-commit-update run \
	build build-tui build-demo build-demo-llm build-examples build-all \
	build-linux build-darwin build-windows \
	demo demo-llm demo-all seed tui

# Detect host OS and architecture for output directory naming.
GOOS   ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
BINDIR  = bin/$(GOOS)-$(GOARCH)
# Windows binaries get .exe suffix; everything else gets none.
EXE     = $(if $(filter windows,$(GOOS)),.exe,)

# Bare `make` runs the full CI pipeline (same as `make ci`). Use `make help` to list targets.
.DEFAULT_GOAL := ci

help:
	@echo "make ci              Full pipeline (same as GitHub Actions: ./scripts/ci.sh)"
	@echo "make integration     Optional slower tests (go test -tags=integration -race ./...)"
	@echo "make stress          Optional very large cell-count tests (TMPDIR defaults to ./.tmp; not CI)"
	@echo "make bench           Run all benchmarks across all packages (not in CI)"
	@echo "make bench-api       Run API-level benchmarks only — the ones shown in README (faster; not in CI)"
	@echo "make bench-stress    Longer API benches (default preload=all: 512..10k; HEXXLA_BENCH_PRELOAD=extreme for 50k; not CI)"
	@echo "make fuzz            Short fuzz smoke (internal/record + internal/engine; not in CI)"
	@echo "make test|vet|fmt    Tests (-race), vet, gofmt -w"
	@echo "make lint            golangci-lint (binary on PATH)"
	@echo "make govulncheck     Vulnerability scan only (also runs inside make ci)"
	@echo "make mod-tidy        go mod tidy"
	@echo "make build           Build cmd/tui + both demos for host OS → bin/<os>-<arch>/"
	@echo "make build-all       Cross-compile for linux/darwin/windows (amd64)"
	@echo "make build-linux     Cross-compile all targets for linux/amd64"
	@echo "make build-darwin    Cross-compile all targets for darwin/amd64"
	@echo "make build-windows   Cross-compile all targets for windows/amd64"
	@echo "                     Override arch: make build-linux GOARCH=arm64"
	@echo "make clean           Remove bin/"
	@echo "make run             Run cmd/tui (go run, no compile)"
	@echo "make demo            Run conversational_memory demo (DB .tmp/conversational-memory.db, cleaned each run)"
	@echo "                     Override: make demo DEMO_DB=/path/to/my.db"
	@echo "make demo-llm        Run llm_context_engine demo (DB .tmp/llm-context-engine.db, needs Ollama)"
	@echo "                     Override: make demo-llm LLM_DB=/path/to/my.db"
	@echo "make demo-all        Run both demos in sequence"
	@echo "make seed            Seed conversational-memory DB if absent — idempotent"
	@echo "make tui             Launch TUI explorer (seeds DB first if absent)"
	@echo "                     Override: make tui TUI_DB=/path/to/my.db"
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

# Focused API benchmarks — only the operations shown in the README benchmark table.
# DB files land in .tmp/. Output is printed live and saved to .tmp/bench-api.txt.
bench-api:
	@$(MAKE) bench-tmp
	@echo "==> HexxlaDB API benchmarks (Intel Core i9-14900HX, Go $(shell go env GOVERSION), $(shell uname -s))"
	@echo "==> Output saved to .tmp/bench-api.txt"
	TMPDIR=$(or $(TMPDIR),$(CURDIR)/.tmp) go test -count=1 -bench='BenchmarkAPI_' -benchmem -benchtime=3s -v -run=^$ . 2>&1 | tee .tmp/bench-api.txt

# Longer runs: preload 512, 2k, 10k (default). Override: make bench-stress HEXXLA_BENCH_PRELOAD=extreme (adds 50k; needs huge TMPDIR).
bench-stress:
	@$(MAKE) bench-tmp
	TMPDIR=$(or $(TMPDIR),$(CURDIR)/.tmp) HEXXLA_BENCH_PRELOAD=$(or $(HEXXLA_BENCH_PRELOAD),all) go test -count=1 -bench='BenchmarkAPI_(GetCell|AscendCellsBySource|LoadContext|LoadContextAt|WalkRing|WalkRingAt)/' -benchmem -benchtime=500ms ./.

# Short fuzz smoke — not part of default CI. For longer runs: go test -fuzz=... -fuzztime=30s ./path
fuzz:
	go test ./internal/record -fuzz=FuzzDecodeCell -fuzztime=2s
	go test ./internal/engine -fuzz=FuzzDecodeHeaderPage -fuzztime=2s
	go test ./internal/engine -fuzz=FuzzParseAndReplayWAL -fuzztime=2s

# Run the TUI directly via go run (no compile step).
run:
	go run ./cmd/tui -path $(or $(TUI_DB),.tmp/conversational-memory.db)

# ── Build targets ─────────────────────────────────────────────────────────────
# All binaries land in bin/<os>-<arch>/ and are gitignored.
# Set GOOS/GOARCH to cross-compile: make build GOOS=linux GOARCH=arm64

build-tui:
	@mkdir -p $(BINDIR)
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o $(BINDIR)/hexxladb-tui$(EXE) ./cmd/tui
	@echo "  → $(BINDIR)/hexxladb-tui$(EXE)"

build-demo:
	@mkdir -p $(BINDIR)
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o $(BINDIR)/hexxladb-demo$(EXE) ./examples/conversational_memory
	@echo "  → $(BINDIR)/hexxladb-demo$(EXE)"

build-demo-llm:
	@mkdir -p $(BINDIR)
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o $(BINDIR)/hexxladb-demo-llm$(EXE) ./examples/llm_context_engine
	@echo "  → $(BINDIR)/hexxladb-demo-llm$(EXE)"

build-examples: build-demo build-demo-llm

# Build everything for the host OS/arch.
build: build-tui build-examples
	@echo "==> Built all targets for $(GOOS)/$(GOARCH)"

# Cross-compile helpers — override GOARCH if needed (e.g. make build-linux GOARCH=arm64).
build-linux:
	$(MAKE) build GOOS=linux GOARCH=$(or $(GOARCH),amd64)

build-darwin:
	$(MAKE) build GOOS=darwin GOARCH=$(or $(GOARCH),amd64)

build-windows:
	$(MAKE) build GOOS=windows GOARCH=$(or $(GOARCH),amd64)

# Build for all three major platforms (amd64).
build-all: build-linux build-darwin build-windows
	@echo "==> Built all platforms into bin/"

clean:
	rm -rf bin

# Run the conversational_memory example demo.
# DB is cleaned before every run so each invocation shows a fresh walkthrough.
# Override: make demo DEMO_DB=/path/to/my.db
demo:
	@mkdir -p .tmp
	@rm -f .tmp/conversational-memory.db .tmp/conversational-memory.db-wal .tmp/conversational-memory.db-changelog
	go run ./examples/conversational_memory $(if $(DEMO_DB),-db $(DEMO_DB),)

# Run the llm_context_engine demo.
# Requires Ollama running locally: ollama serve && ollama pull all-minilm
# DB is always cleaned before each run (demo is self-contained).
# Override: make demo-llm LLM_DB=/path/to/my.db
demo-llm:
	@mkdir -p .tmp
	@rm -f .tmp/llm-context-engine.db .tmp/llm-context-engine.db-wal
	go run ./examples/llm_context_engine $(if $(LLM_DB),-db $(LLM_DB),)

# Run both demos in sequence.
demo-all: demo demo-llm

# Seed the conversational-memory DB if it does not already exist (idempotent).
# The conversational_memory example handles reuse: if the DB is present it skips re-seeding.
seed:
	@mkdir -p .tmp
	@if [ ! -f .tmp/conversational-memory.db ]; then \
		echo "==> Seeding demo database (.tmp/conversational-memory.db)..."; \
		go run ./examples/conversational_memory; \
	else \
		echo "==> Demo database already exists (.tmp/conversational-memory.db) — skipping seed."; \
	fi

# Launch the TUI database explorer.
# Depends on seed so the demo DB is always present before the TUI opens.
# Override: make tui TUI_DB=/path/to/my.db
tui: seed
	go run ./cmd/tui -path $(or $(TUI_DB),.tmp/conversational-memory.db)

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
