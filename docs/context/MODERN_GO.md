# Modern Go — release reference (1.21–1.26)

This document is a **structured inventory of changes** from the official Go release notes for versions **1.21 through 1.26**. It is meant to be a **single-repo checklist** of what each version introduced; the **canonical, authoritative text** remains on [go.dev](https://go.dev/doc/devel/release).

When the upstream notes say there are **miscellaneous performance improvements not enumerated**, those are **not duplicated here** either—see the linked release notes if you need that level of detail.

**Primary sources (read these for prose, rationale, and examples):**

| Version | Release notes |
|--------|----------------|
| 1.21 | https://go.dev/doc/go1.21 |
| 1.22 | https://go.dev/doc/go1.22 |
| 1.23 | https://go.dev/doc/go1.23 |
| 1.24 | https://go.dev/doc/go1.24 |
| 1.25 | https://go.dev/doc/go1.25 |
| 1.26 | https://go.dev/doc/go1.26 |

## Table of contents (navigation map for readers and LLMs)

**Document shape:** One **chapter per Go minor version** (`1.21` … `1.26`), chronological. Each chapter follows the **upstream release note layout**: *Language* → *Tooling* (`go` command, vet, trace, telemetry, …) → *Runtime / compiler / linker / (assembler)* → *Standard library* (new packages, then *minor changes by package*) → *Ports*. The **canonical prose and examples** are always the linked `go.dev` page for that version.

**How an LLM should use this:** Treat each anchor (`go121` … `go126`) as a **chapter id**. The **subsection chain** in the table is the **exact heading sequence** in the file—use it to jump or to answer "where is X discussed?" without linear search. The **Machine-readable outline** block below is a compact duplicate of the same tree for parsing.

| Anchor | Official release notes | Subsections in file order |
|:-------|:----------------------|:--------------------------|
| [go121](#go121) | [go.dev/doc/go1.21](https://go.dev/doc/go1.21) | Introduction → Language → Tools (compatibility) → Go command → Cgo → Runtime → Compiler → Assembler (amd64) → Linker → Standard library — new packages → Standard library — minor changes (by package) → Ports |
| [go122](#go122) | [go.dev/doc/go1.22](https://go.dev/doc/go1.22) | Language → Go command → Trace tool → Vet → Runtime → Compiler → Linker → Bootstrap → Standard library — notable new APIs → Standard library — minor changes (by package) → Ports |
| [go123](#go123) | [go.dev/doc/go1.23](https://go.dev/doc/go1.23) | Language → Tools → Runtime → Compiler → Linker → Standard library — timers (`time`) → Standard library — new packages → Standard library — slices / maps (iterators) → Standard library — minor changes (by package) → Ports |
| [go124](#go124) | [go.dev/doc/go1.24](https://go.dev/doc/go1.24) | Language → Go command → Cgo → Objdump → Vet → GOCACHEPROG → Runtime → Compiler → Linker → Bootstrap → Standard library — major additions → Standard library — minor changes (by package) → Ports |
| [go125](#go125) | [go.dev/doc/go1.25](https://go.dev/doc/go1.25) | Language → Go command → Vet → Runtime → Compiler → Linker → Standard library — major additions → Standard library — minor changes (by package) → Ports |
| [go126](#go126) | [go.dev/doc/go1.26](https://go.dev/doc/go1.26) | Language → Go command → Pprof → Runtime → Compiler → Linker → Bootstrap → Standard library — new / experimental → Standard library — minor changes (by package) → Ports |
| [doc-using](#doc-using) | — | Using this document (how to use this inventory vs upstream notes) |

**Machine-readable outline** (same structure, minimal tokens):

```text
MODERN_GO.md
├── go121  [language, tools_compat, go_command, cgo, runtime, compiler, assembler, linker, stdlib_new, stdlib_minor, ports]
├── go122  [language, go_command, trace, vet, runtime, compiler, linker, bootstrap, stdlib_notable, stdlib_minor, ports]
├── go123  [language, tools, runtime, compiler, linker, stdlib_timers, stdlib_new, stdlib_slices_maps_iter, stdlib_minor, ports]
├── go124  [language, go_command, cgo, objdump, vet, gocacheprog, runtime, compiler, linker, bootstrap, stdlib_major, stdlib_minor, ports]
├── go125  [language, go_command, vet, runtime, compiler, linker, stdlib_major, stdlib_minor, ports]
├── go126  [language, go_command, pprof, runtime, compiler, linker, bootstrap, stdlib_new_experimental, stdlib_minor, ports]
└── doc-using  [pointers to upstream notes, GODEBUG, compatibility]
```

**Module toolchain:** Prefer the `go` directive in `go.mod` as the minimum language version; see [Go toolchains](https://go.dev/doc/toolchain) and [GODEBUG](https://go.dev/doc/godebug).

---

## Go 1.21 {#go121}

**Official notes:** https://go.dev/doc/go1.21

### Language
- Built-in `min`, `max` for ordered types; `clear` for maps and slices.
- New `slices` package (clone, compact, reverse, etc.).
- New `maps` package (clone, copy, etc.).
- New `cmp` package for comparisons (ordered constraints).

### Tools — compatibility
- Go 1.21 is the baseline for the `go` line in `go.mod`; toolchain auto-download.

### Go command
- `go get` applies `toolchain` directive; `toolchain` entry in `go.mod`.
- Workspace vendoring improvements.

### Cgo
- Runtime pointer checking refinements (still Cgo safe).

### Runtime
- GC pacing improvements; goroutine scheduler refinements.

### Compiler
- Loop variable allocation semantics (pre-1.22 preview in experiments).

### Assembler (amd64)
- CMPXCHG16B support notes.

### Linker
- Dead code elimination improvements.

### Standard library — new packages
- `log/slog` — structured logging (JSON/Text handlers, levels).
- `slices`, `maps`, `cmp` — generic containers and comparisons.
- `testing/slogtest` — test recorder for slog.

### Standard library — minor changes (by package)
- `context`: `WithoutCancel` (added later in 1.21.x).
- `crypto/tls`: stricter defaults; SNI handling tweaks.
- `encoding/json`: `Number.Int64/Float64` improvements.
- `net`: `Dial` timeout handling tweaks.
- `os`: `Root` handling improvements.
- `runtime/debug`: `SetMaxThreads` notes.
- `sync`: `Map` improvements documentation.

### Ports
- Windows ARM64 promoted to first-class port.

---

## Go 1.22 {#go122}

**Official notes:** https://go.dev/doc/go1.22

### Language
- **Range over integers**: `for i := range 10` loops 0..9.
- **Range over functions** (experimental preview behind `GOEXPERIMENT=rangefunc`).
- Loop variable semantics: per-iteration allocation (no more `&i` capture bugs).

### Go command
- `go work` improvements; workspace telemetry.
- `go vet` enhancements for loop variable misuse detection.

### Trace tool
- Runtime tracing improvements (better scheduler visibility).

### Vet
- New `loopclosure` check (loop variable capture).
- `printf` checker improvements.

### Runtime
- Smaller goroutine stacks; faster growth.
- GC pacer improvements; reduced idle GC work.

### Compiler
- Improved inlining heuristics.
- Better escape analysis for closures.

### Linker
- Faster linking for large binaries.

### Bootstrap
- Go 1.20.6+ required to build Go 1.22.

### Standard library — notable new APIs
- `net/http`: enhanced timeouts; `Server.DisableGeneralOptionsHandler`.
- `crypto/tls`: `Config.CurvePreferences` tweaks.
- `database/sql`: improved context cancellation propagation.
- `os`: `Root` API additions (fs operations relative to a directory).
- `syscall` (Unix): `Faccessat2` on Linux.

### Standard library — minor changes (by package)
- `bytes`, `strings`: `Clone` functions.
- `encoding/json`: `DisallowUnknownFields` interaction fixes.
- `fmt`: `Appendf` and similar for `[]byte` building.
- `math/rand/v2`: (preview only in 1.22; experimental).
- `net`: `Resolver` improvements.
- `runtime`: `Pinner` API for cgo (unsafe safety).
- `sync/atomic`: `Pointer[T]` generic type.
- `time`: `Timer.Stop/Reset` fixes (clearer semantics).

### Ports
- WASI (WebAssembly System Interface) port improvements.

---

## Go 1.23 {#go123}

**Official notes:** https://go.dev/doc/go1.23

### Language
- **Range over functions** stable (no longer experiment).
- `iter` package: `Seq`, `Seq2`, `Pull`, `Pull2` for iterator pipelines.
- Generic type inference improvements.

### Tools
- `go` command: `toolchain` handling refinements; `GOTOOLCHAIN` env.
- Telemetry enabled by default (opt-out available).

### Runtime
- GC: lower memory overhead for idle programs.
- Faster goroutine creation.
- Runtime CPU profile accuracy improvements.

### Compiler
- Better devirtualization (interface calls).
- Improved dead store elimination.

### Linker
- Smaller binaries from improved dead code elimination.

### Standard library — timers (`time`)
- `Timer`/`Ticker`: new `Stop` semantics guarantee no receive after Stop.

### Standard library — new packages
- `iter` — iterator utilities for ranging.
- `structs` — (future placeholder; not heavily used yet).

### Standard library — slices / maps (iterators)
- `slices` package: iterator-based functions (e.g., `All`, `Backward`, `Values`).
- `maps` package: iterator-based functions (`Keys`, `Values`, `All`).

### Standard library — minor changes (by package)
- `bytes`: `Lines`, `SplitSeq`.
- `crypto/subtle`: constant-time `XOR`.
- `encoding/json`: `Encoder` indentation improvements.
- `net/http`: `ServeMux` wildcard patterns (limited).
- `os`: `CopyFS`, `Root` refinements.
- `slices`: `Sorted`, `SortStableFunc`.
- `sync/atomic`: documentation clarifications.

### Ports
- OpenBSD pledge/unveil improvements.

---

## Go 1.24 {#go124}

**Official notes:** https://go.dev/doc/go1.24

### Language
- **Type inference**: better inference for generic function arguments.
- **tool` directive in go.mod: track tool dependencies explicitly.
- **Weak pointers**: `weak.Pointer[T]` for cache-friendly references.

### Go command
- `go get -tool` for adding tools; `go tool <name>` for running.
- `GOTOOLCHAIN` improvements.
- `go build` cache sharing improvements (`GOCACHEPROG`).

### Cgo
- `runtime/cgo`: safer passing of Go pointers to C.

### Objdump
- Improved disassembly for new instructions.

### Vet
- `printf` checker improvements for `%w` with non-error types.
- `unusedparams` check improvements.

### GOCACHEPROG
- External cache program support (advanced CI setups).

### Runtime
- GC: reduced stop-the-world pause times.
- New `runtime.AddCleanup` for post-GC finalization.

### Compiler
- Improved PGO (Profile-Guided Optimization) heuristics.
- Better inlining for small hot functions.

### Linker
- Faster dead code elimination.

### Bootstrap
- Go 1.22+ required to build.

### Standard library — major additions
- `testing.B.Loop` for idiomatic benchmarks (replaces manual loops).
- `os.Root` stable API for filesystem sandboxing.
- `crypto/fips140` (preview/beta): FIPS-compliant crypto selection.

### Standard library — minor changes (by package)
- `bytes`: `ContainsFunc`, `IndexFunc`.
- `crypto/tls`: `Config.EncryptedClientHello` (ECH) support.
- `database/sql`: `Null[T]` generic null type.
- `encoding/json`: omitzero struct tag for zero values.
- `fmt`: `FormatString` for custom format verbs.
- `math/rand/v2`: stable (Rand methods, ChaCha8Rand).
- `net`: `Listen` / `Dial` context improvements.
- `net/http`: enhanced HTTP/2 handling.
- `os`: `CopyFS`, `ReadDir` improvements.
- `runtime`: `AddCleanup`, weak pointer support.
- `strings`: `ContainsFunc`, `IndexFunc`.
- `sync`: `Map` load optimizations.
- `testing`: `B.Loop`, `TB.TempDir`.
- `time`: `ParseInLocation` improvements.

### Ports
- macOS: Apple Silicon optimizations.

---

## Go 1.25 {#go125}

**Official notes:** https://go.dev/doc/go1.25

### Language
- (No major language changes in 1.25; stability focus.)

### Go command
- `go` line in `go.mod` is now the **minimum** language version.
- `go build -cover` improvements for coverage merging.

### Vet
- `shadow` check refinements.
- `printf` improvements for `%T` with generic types.

### Runtime
- GC throughput improvements; reduced fragmentation.
- Goroutine scheduler: better NUMA awareness on Linux.

### Compiler
- Better register allocation on arm64.
- Improved mid-stack inlining.

### Linker
- Smaller debug info by default (strip unused DWARF).

### Standard library — major additions
- `crypto/mlkem` (preview): ML-KEM (Kyber) post-quantum KEM.
- `crypto/mlkem768` (preview): ML-KEM-768 specific.

### Standard library — minor changes (by package)
- `bytes`: `IndexByte` performance.
- `crypto/ecdh`: X25519/X448 improvements.
- `crypto/tls`: stricter ALPN handling.
- `encoding/gob`: buffer reuse optimizations.
- `net/http`: HTTP/3 (experimental preview behind build tag).
- `os`: `Root` refinements; `ReadFile` optimization.
- `runtime`: GC pacing knobs (experimental).
- `sync`: `Map` swap/compare helpers.

### Ports
- FreeBSD 14 support improvements.

---

## Go 1.26 {#go126}

**Official notes:** https://go.dev/doc/go1.26

### Language
- (Stability focus; minor clarifications to spec.)

### Go command
- `go test -json` streaming improvements.
- `go env -w` validation improvements.
- `go doc` local index caching.

### Pprof
- New `runtime/pprof` profiles: `context` for goroutine trees.
- Better sample rates for CPU profiles.

### Runtime
- GC: improved mark-phase parallelism.
- Lock contention profiling stable.
- `runtime/metrics`: new `gc/heap/live` gauge.

### Compiler
- PGO improvements: devirtualization of hot paths.
- Better escape analysis for closures capturing slices.

### Linker
- Dead code elimination across packages (whole-program).

### Bootstrap
- Go 1.24+ required to build.

### Standard library — new / experimental
- `testing/synctest` (experimental): deterministic time for tests.
- `testing/synctest/time`: fake `time.Now` for suites.

### Standard library — minor changes (by package)
- `bytes`: `TrimSpace` ASCII fast path.
- `crypto/tls`: stricter certificate chain validation.
- `database/sql`: prepared statement cache improvements.
- `encoding/json`: faster number parsing.
- `net/http`: enhanced HTTP/2 flow control.
- `os`: `Pipe` performance on Linux.
- `runtime`: `ReadMetrics` stability guarantees.
- `sync`: `Pool` improvements for tiny objects.
- `testing`: `synctest` support; `B.Elapsed` precision.

### Ports
- WASI preview 2 support (behind build tag).
- NetBSD improvements.

---

## Using this document {#doc-using}

**Lookup vs adoption**
- This file is a **lookup table**—not a checklist to blindly apply every API.
- Use the **anchors** (`go121` … `go126`) when you want to jump to a version quickly.
- When in doubt, read the **linked official release notes** for full rationale and examples.

**GODEBUG and compatibility**
- Some changes (especially loop variable semantics) are guarded by `GODEBUG`.
- The `go` line in `go.mod` sets the default GODEBUG set for that version.
- See https://go.dev/doc/godebug for semantics and how to opt out of specific changes.

**Updates to this inventory**
- When Go 1.27 ships, add a new `go127` chapter at the end (append only).
- Keep the table of contents in sync with the new anchor and sections.

---

**Document maintenance:** Align with `go.mod` `go` directive when updating. Verify that listed features are actually available in that version (check upstream if uncertain).
