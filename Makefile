.PHONY: help ci integration stress bench bench-api bench-stress fuzz test vet fmt lint mod-tidy govulncheck complexity clean bench-tmp \
	pre-commit-install pre-commit-run pre-commit-update \
	build build-cli build-tui build-demo build-demo-llm build-examples build-all \
	build-linux build-darwin build-windows \
	demo demo-llm demo-spatial demo-all seed tui \
	llm-setup mutation-test mutation-test-dry ci-full \
	clean-llm clean-llm-all clean-llm-windsurf clean-llm-cursor clean-llm-claude clean-llm-continue clean-llm-codex

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
	@echo "make integration     Optional slower tests (go test -tags=integration -race -parallel=1 ./...)"
	@echo "make stress          Optional very large cell-count tests (TMPDIR defaults to ./.tmp; not CI)"
	@echo "make bench           Run all benchmarks across all packages (not in CI)"
	@echo "make bench-api       Run API-level benchmarks only — the ones shown in README (faster; not in CI)"
	@echo "make bench-stress    Longer API benches (default preload=all: 512..10k; HEXXLA_BENCH_PRELOAD=extreme for 50k; not CI)"
	@echo "make fuzz            Short fuzz smoke (internal/record + internal/engine; not in CI)"
	@echo "make test|vet|fmt    Tests (-race), vet, gofmt -w"
	@echo "make lint            golangci-lint (binary on PATH)"
	@echo "make govulncheck     Vulnerability scan only (also runs inside make ci)"
	@echo "make mod-tidy        go mod tidy"
	@echo "make complexity      Full complexity analysis: cyclomatic + cognitive + CRAP (see .complexity.yml)"
	@echo "make build           Build CLI + TUI + demos for host OS → bin/<os>-<arch>/"
	@echo "make build-cli       Build cmd/hexxladb operator CLI only"
	@echo "make build-all       Cross-compile for linux/darwin/windows (amd64)"
	@echo "make build-linux     Cross-compile all targets for linux/amd64"
	@echo "make build-darwin    Cross-compile all targets for darwin/amd64"
	@echo "make build-windows   Cross-compile all targets for windows/amd64"
	@echo "                     Override arch: make build-linux GOARCH=arm64"
	@echo "make clean           Remove bin/"
	@echo "make demo            Run conversational_memory demo (DB .tmp/conversational-memory.db, cleaned each run)"
	@echo "                     Override: make demo DEMO_DB=/path/to/my.db"
	@echo "make demo-llm        Run llm_context_engine demo (DB .tmp/llm-context-engine.db, needs Ollama)"
	@echo "                     Override: make demo-llm LLM_DB=/path/to/my.db"
	@echo "make demo-spatial    Run spatial_algorithms demo (FOV, LOD, Voronoi, pathfinding)"
	@echo "make demo-all        Run all demos in sequence"
	@echo "make seed            Seed conversational-memory DB if absent — idempotent"
	@echo "make tui             Launch TUI explorer (seeds DB first if absent)"
	@echo "                     Override: make tui TUI_DB=/path/to/my.db"
	@echo "make pre-commit-*    Optional Git hooks (see CONTRIBUTING.md)"
	@echo "make llm-setup       Regenerate .windsurf/, .cursor/, .claude/ etc. from scripts/llm/platforms/"
	@echo "make clean-llm-all   Remove all LLM tool folders (.windsurf, .cursor, .claude, .continue, .codex)"
	@echo "make clean-llm-windsurf|cursor|claude|continue|codex  Remove individual LLM folders"
	@echo "make ci-full         Full pipeline: core CI + complexity + mutation testing + coupling analysis"
	@echo "make mutation-test   Full Gremlins mutation testing (slow, thorough)"
	@echo "                     Override target: make mutation-test TARGET=internal/domain"
	@echo "make mutation-test-dry  Fast mutation dry-run (CI mode)"

# Benchmark temp directory (defaults to repo-local ./.tmp; override with TMPDIR=/path).
bench-tmp:
	@mkdir -p $(or $(TMPDIR),$(CURDIR)/.tmp)

# Run the full pipeline (same as CI). Install golangci-lint locally for the lint step.
ci:
	@./scripts/ci.sh

# Optional durability/stress tests (not run in default CI). See CONTRIBUTING.md.
integration:
	go test -count=1 -race -parallel=1 -tags=integration ./...

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

# ── Build targets ─────────────────────────────────────────────────────────────
# All binaries land in bin/<os>-<arch>/ and are gitignored.
# Set GOOS/GOARCH to cross-compile: make build GOOS=linux GOARCH=arm64

build-cli:
	@mkdir -p $(BINDIR)
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o $(BINDIR)/hexxladb$(EXE) ./cmd/hexxladb
	@echo "  → $(BINDIR)/hexxladb$(EXE)"

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
build: build-cli build-tui build-examples
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

# Run the spatial_algorithms demo.
# No external dependencies needed — runs a self-contained DB.
# DB is always cleaned before each run.
demo-spatial:
	@mkdir -p .tmp
	@rm -f .tmp/spatial-algorithms.db .tmp/spatial-algorithms.db-wal
	go run ./examples/spatial_algorithms $(if $(SPATIAL_DB),-db $(SPATIAL_DB),)

# Run all demos in sequence.
demo-all: demo demo-llm demo-spatial

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

complexity:
	@./scripts/ci/pre-push/05-complexity.sh

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

# LLM Tool Setup — generates .windsurf/, .cursor/, .claude/, etc. from scripts/llm/platforms/*/config.yaml
llm-setup:
	@./scripts/llm/llm-setup.sh

# Cleanup LLM tool folders
clean-llm-cursor:
	rm -rf .cursor

clean-llm-claude:
	rm -rf .claude

clean-llm-windsurf:
	rm -rf .windsurf

clean-llm-continue:
	rm -rf .continue

clean-llm-codex:
	rm -rf .codex

clean-llm-all:
	rm -rf .cursor .claude .windsurf .continue .codex

clean-llm: clean-llm-all

# Full CI pipeline: core CI (incl. complexity) + mutation testing (dry-run)
ci-full:
	@./scripts/ci/ci.sh
	@echo "==> Mutation testing (dry-run; use make mutation-test for full)"
	@./scripts/ci/pre-push/06-gremlins.sh

# Mutation testing with Gremlins — full run (slow, thorough)
# Override target: make mutation-test TARGET=./internal/lattice
# Without TARGET, runs all configured packages via the CI script.
mutation-test:
	@if ! command -v gremlins >/dev/null 2>&1; then \
		echo "error: gremlins not found. Install with:"; \
		echo "  go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0"; \
		exit 1; \
	fi
ifdef TARGET
	@echo "==> Running Gremlins mutation testing: $(TARGET)"
	gremlins unleash $(TARGET)
else
	@echo "==> Running Gremlins mutation testing (all targets, full)..."
	GREMLINS_FULL=1 ./scripts/ci/pre-push/06-gremlins.sh
endif

# Mutation testing dry-run (fast, same as CI pre-push)
mutation-test-dry:
	@if ! command -v gremlins >/dev/null 2>&1; then \
		echo "error: gremlins not found. Install with:"; \
		echo "  go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0"; \
		exit 1; \
	fi
	@echo "==> Running Gremlins mutation testing (dry-run)..."
	./scripts/ci/pre-push/06-gremlins.sh
