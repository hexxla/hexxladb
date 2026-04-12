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

**How an LLM should use this:** Treat each anchor (`go121` … `go126`) as a **chapter id**. The **subsection chain** in the table is the **exact heading sequence** in the file—use it to jump or to answer “where is X discussed?” without linear search. The **Machine-readable outline** block below is a compact duplicate of the same tree for parsing.

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

<span id="go121"></span>

## Go 1.21 — https://go.dev/doc/go1.21

### Introduction

- Maintains the [Go 1 compatibility promise](https://go.dev/doc/go1compat); compatibility behavior is strengthened via `go` line + GODEBUG (below).
- **Version numbering:** first release in a family is **Go 1.N.0** (not only “1.N”); see [Go versions](https://go.dev/doc/toolchain#version).

### Language

- **New builtins:** [`min`](https://go.dev/ref/spec#Min_and_max), [`max`](https://go.dev/ref/spec#Min_and_max), [`clear`](https://go.dev/ref/spec#Clear) (maps / slices).
- **Package initialization:** deterministic algorithm (initialize first package whose imports are all initialized; repeat). Programs relying on unspecified init order may change.
- **Generics — type inference:** generic functions as arguments to generic functions; inference from interface methods; untyped constant mixing rules aligned with operators; stricter component-type matching; spec [Type inference](https://go.dev/ref/spec#Type_inference) clarified.
- **Loop variable experiment (preview):** per-iteration loop vars — [LoopvarExperiment](https://go.dev/wiki/LoopvarExperiment).
- **`recover` / `panic(nil)`:** if `recover` is called from a deferred function while panicking, `recover` is non-nil; `panic(nil)` becomes `*runtime.PanicNilError`. `GODEBUG=panicnil=1` for old behavior; auto-enabled for `go 1.20` or earlier main modules.

### Tools (compatibility)

- **GODEBUG + `go` line:** behavior for non-breaking-but-impactful changes follows `go.work` / `go.mod` `go` version; see [Go, Backwards Compatibility, and GODEBUG](https://go.dev/doc/godebug).
- **Forward compatibility:** `go` line is a **strict minimum** (e.g. `go 1.21.0` rejects older toolchains).
- **Toolchain management:** `go` may run other toolchain versions (PATH or download); **`toolchain`** directive for suggested minimum; see [Go toolchains](https://go.dev/doc/toolchain).

### Go command

- **PGO:** `-pgo` defaults to `-pgo=auto`; `default.pgo` in main package dir enables PGO for that program; multi-main allowed with PGO.
- **`-C dir`:** must be first flag when used.
- **`go test`:** `-fullpath` for full paths in logs.
- **`go test -c`:** multiple packages → `pkg.test` per package name (error on duplicate names).
- **`go test -o`:** directory argument writes test binaries there.
- **cgo + external C linker:** `runtime/cgo` supplied to Go linker for compatibility.

### Cgo

- Error on Go methods declared on C types in `import "C"` files.

### Runtime

- Deep stacks: print innermost 50 + outermost 50 frames (not only first 100).
- Linux transparent huge pages: explicit heap/THP interaction; see [GC guide — Linux THP](https://go.dev/doc/gc-guide#Linux_transparent_huge_pages).
- GC tuning: up to ~40% tail latency reduction / memory use tradeoffs (throughput may dip slightly).
- **C→Go on C-created threads (Unix):** setup preserved across calls (~1–3µs → ~100–200ns per call).

### Compiler

- **PGO:** generally usable; 2–7% typical improvement; interface devirtualization; see [PGO user guide](https://go.dev/doc/pgo).
- **Build speed:** up to ~6% (compiler built with PGO).

### Assembler (amd64)

- Frameless nosplit functions no longer auto-`NOFRAME`; set `NOFRAME` explicitly.
- Improved `R15` verifier for dynamic linking.

### Linker

- **windows/amd64:** SEH unwinding data by default.
- Dead global **map** variables may be removed when large, side-effect-free initializers.

### Standard library — new packages

- [`log/slog`](https://pkg.go.dev/log/slog) — structured logging.
- [`testing/slogtest`](https://pkg.go.dev/testing/slogtest) — test `slog.Handler` implementations.
- [`slices`](https://pkg.go.dev/slices), [`maps`](https://pkg.go.dev/maps), [`cmp`](https://pkg.go.dev/cmp).

### Standard library — minor changes (by package)

- **archive/tar, archive/zip:** `FileInfo` from headers implements `String` via `io/fs.FormatFileInfo`; zip `DirEntry` via `FormatDirEntry`.
- **bytes:** `Buffer.Available`, `Buffer.AvailableBuffer`.
- **context:** `WithoutCancel`; `WithDeadlineCause` / `WithTimeoutCause` + `Cause`; `AfterFunc`; `Background`/`TODO` optimization note (equality when converted to shared type — comparing contexts still not defined).
- **crypto/ecdsa, crypto/rsa:** `Equal` in constant time.
- **crypto/elliptic:** `Curve` methods and `GenerateKey`/`Marshal`/`Unmarshal` deprecated; use [`crypto/ecdh`](https://pkg.go.dev/crypto/ecdh) or e.g. `filippo.io/nistec`.
- **crypto/rand:** `getrandom` on NetBSD 10+.
- **crypto/rsa:** faster private ops on amd64/arm64 vs 1.20 regression; `PrecomputedValues` private fields → call `Precompute` after deserialize; `GenerateMultiPrimeKey` / `CRTValues` deprecated (CRTValues unused in decrypt).
- **crypto/sha256:** HW acceleration amd64 (~3–4×).
- **crypto/tls:** resumed sessions skip client cert verification (except expiry); session ticket control (`SessionState`, `WrapSession`/`UnwrapSession`, `EncryptTicket`/`DecryptTicket`, client resumption APIs); ticket privacy changes; Extended Master Secret (RFC 7627); `TLSUnique` deprecation reverted where EMS; `QUICConn`; `VersionName`; better client-auth alert codes.
- **crypto/x509:** `RevokedCertificates` → `RevokedCertificateEntries` / `RevocationListEntry`; name constraints on non-leaf certs.
- **debug/elf:** `File.DynValue`; `DynFlag1`; `COMPRESS_ZSTD`; `R_PPC64_REL24_P9NOTOC`.
- **debug/pe:** read uninitialized `.bss` via `Section.Data`/`Open` → error.
- **embed:** `ReadAt` on opened files; `Stat` implements `String` via `FormatFileInfo`.
- **encoding/binary:** `NativeEndian`.
- **errors:** `ErrUnsupported`.
- **flag:** `BoolFunc` / `FlagSet.BoolFunc`; duplicate `Set` on same name panics.
- **go/ast:** `IsGenerated`; `File.GoVersion`.
- **go/build:** header `//go:` directives → `Directives` / `TestDirectives` / `XTestDirectives`.
- **go/build/constraint:** `GoVersion(expr)`.
- **go/token:** `File.Lines`.
- **go/types:** `Package.GoVersion`.
- **hash/maphash:** pure Go via `purego` build tag.
- **html/template:** `ErrJSTemplate` for JS template literals with actions.
- **io/fs:** `FormatFileInfo`, `FormatDirEntry`; `ReadDir`/`WalkDir` `DirEntry.String`.
- **math/big:** `Int.Float64`.
- **net (Linux):** Multipath TCP opt-in via `Dialer.SetMultipathTCP` / `ListenConfig.SetMultipathTCP` / `TCPConn.MultipathTCP`.
- **net/http:** `ResponseController.EnableFullDuplex`; `ErrSchemeMismatch`; `errors.Is(ErrNotSupported, errors.ErrUnsupported)` compatibility.
- **os:** `Chtimes` empty `time.Time` leaves atime/mtime unchanged; Windows `File.Chdir`; Unix non-blocking `Fd` preserves non-blocking; Windows `Truncate` nonexistent → error; `TempDir` uses GetTempPath2W; Windows UTF-16 names; Windows `Lstat` trailing slash; `ReadDir`/`DirFS` `String`/`ReadFileFS`/`ReadDirFS`.
- **path/filepath:** `WalkDir` `DirEntry.String`.
- **reflect:** `ValueOf` may avoid heap; `Value.Clear` (maps/slices).
- **runtime:** stack traces include parent goroutine IDs; `GOTRACEBACK=wer` (Windows WER); `cgocheck=2` → `GOEXPERIMENT=cgocheck2`; `Pinner` for pinned memory / cgo.
- **runtime/metrics:** more GC metrics; `GOGC` / `GOMEMLIMIT` as metrics.
- **runtime/trace:** lower CPU cost on amd64/arm64; STW events for all reasons.
- **sync:** `OnceFunc`, `OnceValue`, `OnceValues`.
- **syscall:** Windows `Fchdir`; FreeBSD `SysProcAttr.Jail`; Windows UTF-16/WTF-8; several errors match `errors.ErrUnsupported`.
- **testing:** `-test.fullpath`; `testing.Testing()`.
- **testing/fstest:** `Open`+`Stat` `String` via `FormatFileInfo`.
- **unicode:** Unicode **15.0.0**.

### Ports

- **Darwin:** macOS 10.15+ (see 1.20 announcement).
- **Windows:** Windows 10 / Server 2016+ (see 1.20 announcement).
- **ARM:** cross-compile `GOARCH=arm` default `GOARM=7` when not on ARM.
- **WebAssembly:** `go:wasmimport`; scheduler/event loop interaction; **WASI** preview `GOOS=wasip1`, `GOARCH=wasm`; `*_wasip1.go` naming.
- **ppc64/ppc64le:** Linux `GOPPC64=power10` PC-relative etc.; AIX power10 without PC-relative.
- **loong64:** `linux/loong64`: `c-archive`, `c-shared`, `pie`.

---

<span id="go122"></span>

## Go 1.22 — https://go.dev/doc/go1.22

### Language

- **`for` loop variables:** new variable per iteration (fixes closure bugs); [transition tooling](https://go.dev/wiki/LoopvarExperiment#my-test-fails-with-the-change-how-can-i-debug-it).
- **`for range n` (integer):** `n` must be integer; iterates `0..n-1`; see [spec](https://go.dev/ref/spec#For_range).
- **Preview:** range-over-func iterators — `GOEXPERIMENT=rangefunc` ([wiki](https://go.dev/wiki/RangefuncExperiment)).

### Go command

- Workspaces may use workspace **`vendor`** (from `gowork vendor`); `-mod=vendor` default when present.
- **`go get`** unsupported in `GO111MODULE=off` outside a module; `build`/`test` still work for legacy GOPATH.
- **`go mod init`:** no longer imports from other vendoring tools’ lockfiles.
- **`go test -cover`:** packages without tests show 0% coverage (unless no executable code).
- Build with external C linker but **no cgo** → error.

### Trace tool

- Web UI refresh; thread-oriented view; full syscall durations (traces from Go 1.22+).

### Vet

- Loop variable checks aligned with Go 1.22 semantics for files requiring 1.22+.
- **`append(slice)`** with no values to append.
- **`defer` + `time.Since`:** non-deferred `Since` inside defer.
- **`log/slog`:** invalid key/value pairing.

### Runtime

- GC metadata near objects: ~1–3% CPU, ~1% memory; some objects 8-byte vs 16-byte alignment — assembly assuming 16B may break (`GOEXPERIMENT=noallocheaders` temporary).
- **windows/amd64:** `SetUnhandledExceptionFilter` with `c-archive` / `c-shared`.

### Compiler

- PGO: more devirtualization; **2–14%** typical with PGO; devirtualization + inlining interleaved.
- **`GOEXPERIMENT=newinliner`:** call-site inlining heuristics ([issue #61502](https://go.dev/issue/61502)).

### Linker

- **`-s` / `-w`:** consistent behavior; `-s` implies `-w` unless `-w=0`.
- **ELF `-B gobuildid`:** GNU build ID from Go build ID.
- **Windows internal link:** preserve C `.pdata`/`.xdata` (behavior change possible).

### Bootstrap

- Requires **Go 1.20.x** final for bootstrap; Go **1.24** expected to require **1.22.x** final.

### Standard library — notable new APIs

- **[`math/rand/v2`](https://pkg.go.dev/math/rand/v2):** v2 stdlib package; ChaCha8/PCG; `Uint64` `Source`; `N` generic; top-level auto-seeded; renamed `IntN` etc.; see [proposal #61716](https://go.dev/issue/61716).
- **[`go/version`](https://pkg.go.dev/go/version):** parse/compare Go version strings.
- **`net/http.ServeMux`:** method patterns, wildcards `{id}`, `{path...}`, `{$}` exact trailing slash; `Request.PathValue` / `SetPathValue`; `GODEBUG=httpmuxgo121=1` restores old mux.

### Standard library — minor changes (by package)

- **archive/tar, archive/zip:** `Writer.AddFS`.
- **bufio:** `SplitFunc` + `ErrFinalToken` + nil token stops without empty token.
- **cmp:** `Or` (first non-zero).
- **crypto/tls:** `ExportKeyingMaterial` requires TLS 1.3 or EMS unless `tlsunsafeekm=1`; default min TLS 1.2 server (`tls10server=1`); no RSA kex suites by default (`tlsrsakex=1`); `CertPool.AddCertWithConstraint`; Android extra cert path; **`crypto/x509` `OID`, `Policies`**, `x509usepolicies` GODEBUG.
- **database/sql:** `Null[T]`.
- **debug/elf:** LoongArch reloc constants.
- **encoding/base32, base64, hex:** `AppendEncode`/`AppendDecode`; `WithPadding` panics on invalid negative padding.
- **encoding/json:** escape `\b`/`\f` properly.
- **go/ast:** deprecated old `Object`/`Scope` resolution APIs; `ast.Unparen`.
- **go/types:** `Alias` type; `Unalias`; `gotypesalias` GODEBUG; `Info.FileVersions`; `PkgNameOf`; `SizesFor` aligned with compiler `gc`.
- **html/template:** JS template literals with actions supported; `jstmpllitinterp` obsolete.
- **io:** `SectionReader.Outer`.
- **log/slog:** `SetLogLoggerLevel`.
- **math/big:** `Rat.FloatPrec`.
- **net/http:** `io.Copy` TCP→Unix uses `splice`; `ServeFileFS`, `FileServerFS`, `NewFileTransportFS`; invalid empty `Content-Length` rejected (`httplaxcontentlength=1`); Windows DNS hosts file with `netgo`.
- **net/netip:** `AddrPort.Compare`.
- **os:** `io.Copy` File→UnixConn `sendfile`; Windows `Stat` reparse points; `LookPath` / `exec` behavior; many Windows fixes.
- **reflect:** `IsZero` negative zero / blank `_` fields; `PtrTo` deprecated → `PointerTo`; **`TypeFor[T]`**.
- **runtime/metrics:** new STW histograms; mutex profile scaling; Darwin pprof memory map; **execution tracer** overhaul; `x/exp/trace` for new traces; `GOEXPERIMENT=noexectracer2` fallback.
- **slices:** `Concat`; shrink functions zero abandoned elements; `Insert` panics on out-of-range `i`.
- **syscall:** no longer marked deprecated (still frozen); Linux `SysProcAttr.PidFD`; Windows `O_SYNC` Open.
- **testing/slogtest:** `Run` with subtests.

### Ports

- **darwin/amd64:** PIE default (`-buildmode=exe` for non-PIE); **1.22 last on macOS 10.15**; 1.23 requires **macOS 11+**.
- **ARM:** `GOARM=5,6,7` optional `,softfloat` / `,hardfloat`.
- **loong64:** register args; ASan/MSan, relocations, `plugin`.
- **openbsd/ppc64:** experimental port.

---

<span id="go123"></span>

## Go 1.23 — https://go.dev/doc/go1.23

### Language

- **`range` over iterator functions** — `func(func() bool)`, `func(func(K) bool)`, `func(func(K, V) bool)`; see [`iter`](https://pkg.go.dev/iter), [spec](https://go.dev/ref/spec#For_range), [blog](https://go.dev/blog/range-functions).
- **Generic type aliases (preview):** `GOEXPERIMENT=aliastypeparams` (within package only).

### Tools

- **Go telemetry:** opt-in via `go telemetry`; see [Go Telemetry](https://go.dev/doc/telemetry).
- **Go command:** `GOROOT_FINAL` no effect; `go env -changed`; `go mod tidy -diff`; `go list -m -json` adds `Sum`, `GoModSum`; **`godebug` directive** in `go.mod`/`go.work`.
- **Vet:** **`stdversion`** analyzer.
- **Cgo:** `-ldflags` for C linker (avoids “argument list too long”).
- **Trace:** tolerates partial/crash traces.

### Runtime

- Panic traceback: continuation lines of panic message indented with tab.

### Compiler

- PGO build overhead much lower (single-digit % vs 100%+).
- Stack frame overlap for disjoint variable regions.
- **386/amd64:** PGO hot-block alignment (`-gcflags=-d=alignhot=0` to disable).

### Linker

- Stricter **`//go:linkname`** to std internal symbols (existing corpus grandfathered); `-checklinkname=0`.
- ELF PIE: `-bindnow`.

### Standard library — timers (`time`)

- **`Timer`/`Ticker`:** GC of unstopped timers/tickers; **unbuffered** channels; semantics tied to `go 1.23.0+` in `go.mod`; `GODEBUG=asynctimerchan=1` reverts async channel behavior.

### Standard library — new packages

- [`unique`](https://pkg.go.dev/unique), [`iter`](https://pkg.go.dev/iter), [`structs`](https://pkg.go.dev/structs) (`HostLayout`).

### Standard library — slices / maps (iterators)

- **slices:** `All`, `Values`, `Backward`, `Collect`, `AppendSeq`, `Sorted`, `SortedFunc`, `SortedStableFunc`, `Chunk`.
- **maps:** `All`, `Keys`, `Values`, `Insert`, `Collect`.

### Standard library — minor changes (by package)

- **archive/tar:** `FileInfoNames` for Uname/Gname.
- **crypto/tls:** ECH client draft; `QUICConn` session events; remove default 3DES (`tls3des=1`); X25519Kyber768 default (`tlskyber=0`); `X509KeyPair` populates `Leaf` (`x509keypairleaf`).
- **crypto/x509:** CSR RSA-PSS; CSR/CRL verify signatures; `x509sha1` removal next release warning; `ParseOID`; `OID` marshaling interfaces.
- **database/sql:** wrap `driver.Valuer` errors.
- **debug/elf:** `PT_OPENBSD_NOBTCFI`; symbol type constants.
- **encoding/binary:** `Encode`, `Decode`, `Append`.
- **go/ast:** `Preorder`.
- **go/types:** `Func.Signature`; `Alias.Rhs` and generic alias methods; default **`gotypesalias=1`**.
- **math/rand/v2:** `Uint`, `Uint64`; `ChaCha8.Read`.
- **net:** `KeepAliveConfig`; `DNSError` wraps timeout/cancel; `netedns0=0`.
- **net/http:** cookie quoting/`Quoted`; `CookiesNamed`; `Partitioned`; mux spaces after method; `ParseCookie`/`ParseSetCookie`; `ServeContent`/`ServeFile*` strip headers on errors; `Request.Pattern`; `httpservecontentkeepheaders=1`.
- **net/http/httptest:** `NewRequestWithContext`.
- **net/netip:** `DeepEqual` IPv4 vs IPv4-mapped IPv6 fix.
- **os:** Windows socket `ModeSocket`; `winsymlink`/`winreadlinkvolume`; `CopyFS`; Linux pidfd for `Process`.
- **path/filepath:** `Localize`; Windows `EvalSymlinks` behavior with GODEBUG.
- **reflect:** `Type` methods mirroring `Value`; `SliceAt`; `Pointer`/`UnsafePointer` for strings; `Seq`/`Seq2`, `CanSeq`/`CanSeq2`.
- **runtime/debug:** `SetCrashOutput`.
- **runtime/pprof:** profile depth 32→128 for several profile types.
- **runtime/trace:** flush on panic during trace.
- **slices:** `Repeat`.
- **sync:** `Map.Clear`.
- **sync/atomic:** `And`, `Or`.
- **syscall:** `WSAENOPROTOOPT`; `GetsockoptInt` on Windows.
- **testing/fstest:** `TestFS` structured unwrap.
- **text/template:** `else with`.
- **time:** parse zone offset range error; Windows 0.5ms resolution timers.
- **unicode/utf16:** `RuneLen`.

### Ports

- **Darwin:** **macOS 11+** required.
- **Linux:** last release with kernel **2.6.32+**; 1.24 requires **3.2+**.
- **OpenBSD riscv64:** experimental.
- **ARM64:** `GOARM64` (v8.x, v9.x + options).
- **RISC-V:** `GORISCV64` `rva20u64` / `rva22u64`.
- **Wasm:** `wasmtime` ≥ 14 for `go_wasip1_wasm_exec`.

---

<span id="go124"></span>

## Go 1.24 — https://go.dev/doc/go1.24

### Language

- **Generic type aliases** fully supported (`GOEXPERIMENT=noaliastypeparams` to disable until 1.25 removes it).

### Go command

- **`tool` directive** + **`go tool`**; `go get -tool`; **`tool` meta-pattern**; `go run`/`go tool` cache in build cache; **`go build`/`install -json`**; **`go test -json`** build events (`gotestjsonbuildtext=1`); **`GOAUTH`**; **`-buildvcs`** main module version in binary; **`toolchaintrace=1`**.

### Cgo

- `#cgo noescape` / `#cgo nocallback`; duplicate incompatible C declarations error (improved cross-file).

### Objdump

- loong64, riscv64, s390x disassembly support.

### Vet

- **`tests`** analyzer; **`printf`** non-constant format-only string (go **1.24+** only); **`buildtag`** invalid `go1.23.1`-style; **`copylock`** 3-clause `for` + mutex ([issue #66387](https://go.dev/issue/66387)).

### GOCACHEPROG

- External cache via `GOCACHEPROG` JSON protocol.

### Runtime

- Swiss Tables map; small alloc improvements; spin-bit mutex; `GOEXPERIMENT=noswissmap` / `nospinbitmutex`.

### Compiler

- cgo receiver through alias → error.

### Linker

- Default GNU build ID (ELF) / UUID (Mach-O); `-B none` / `-B 0xNNNN`.

### Bootstrap

- Requires **Go 1.22.6+**; **1.26** expected to require **1.24+** point release.

### Standard library — major additions

- **`os.Root` / `os.OpenRoot`** — chroot-like safe FS API.
- **`testing.B.Loop`** — benchmark loops.
- **`runtime.AddCleanup`** — preferred over `SetFinalizer`.
- **`weak`** + **`maphash.Comparable`**.
- **`crypto/mlkem`**, **`crypto/hkdf`**, **`crypto/pbkdf2`**, **`crypto/sha3`** (stdlib).
- **FIPS 140-3:** [`GOFIPS140`](https://go.dev/doc/security/fips140), `fips140` GODEBUG.
- **`testing/synctest`** (experimental): `GOEXPERIMENT=synctest`.

### Standard library — minor changes (by package)

- **archive/tar, archive/zip:** `Writer.AddFS` writes a directory header for an empty directory.
- **bytes, strings:** `Lines`, `SplitSeq`, `SplitAfterSeq`, `FieldsSeq`, `FieldsFuncSeq` (iterators).
- **crypto/aes:** value from `NewCipher` no longer has undocumented `NewCTR`/`NewGCM`/CBC methods — use `crypto/cipher` with the `Block`.
- **crypto/cipher:** `NewGCMWithRandomNonce`; `NewCTR` faster on amd64/arm64; deprecate `NewOFB`, `NewCFBEncrypter`, `NewCFBDecrypter` (prefer AEAD or `NewCTR`).
- **crypto/ecdsa:** `PrivateKey.Sign` deterministic per RFC 6979 when `rand` is nil.
- **crypto/md5, sha1, sha256:** digest values implement `encoding.BinaryAppender` where noted.
- **crypto/rand:** `Read` always returns `nil` error (fatal crash if `Reader` fails — override caveat); Linux 6.11+ vDSO `getrandom`; OpenBSD `arc4random_buf`; new `Text`.
- **crypto/rsa:** min 1024-bit keys enforced (`rsa1024min=0` for tests); `Precompute` before `Validate` safe/faster; stricter invalid key rejection; PKCS1v15 Sign/Verify SHA-512/224, SHA-512/256, SHA-3; `GenerateKey` Carmichael totient; wasm faster ops.
- **crypto/sha512:** (see notes for `BinaryAppender` where applicable).
- **crypto/subtle:** `WithDataIndependentTiming` (arm64 DIT; `dataindependenttiming=1` for whole program); `XORBytes` overlap rules strict (panic).
- **crypto/tls:** server ECH via `Config.EncryptedClientHelloKeys`; `X25519MLKEM768` default (`tlsmlkem=0`); remove `X25519Kyber768Draft00`; `CurvePreferences` order ignored (contents only enable/disable); `ClientHelloInfo.Extensions`.
- **crypto/x509:** remove `x509sha1` GODEBUG — no SHA-1 signature verify; `OID` implements `BinaryAppender`/`TextAppender`; default policies field `Certificate.Policies` (`x509usepolicies=0` revert); `CreateCertificate` RFC 5280 serial when nil; `Verify` policy graphs via `VerifyOptions.CertificatePolicies`; `MarshalPKCS8PrivateKey` errors on invalid RSA; `ParsePKCS1PrivateKey`/`ParsePKCS8PrivateKey` use CRT values (`x509rsacrt=0`).
- **debug/elf:** `DynamicVersions`, `DynamicVersionNeeds`, `Symbol.HasVersion`, `Symbol.VersionIndex`.
- **encoding:** new `TextAppender`, `BinaryAppender` (append to slice, avoid alloc).
- **encoding/json:** struct tag **`omitzero`**; `UnmarshalTypeError.Field` includes embedded structs.
- **go/types:** iterator methods on sequences (`Params().Variables()`, etc.).
- **hash/adler32, crc32, crc64:** `BinaryAppender` on returned hash values.
- **hash/maphash:** `Comparable`, `WriteComparable`.
- **log/slog:** `DiscardHandler`; `Level`/`LevelVar` implement `encoding.TextAppender`.
- **math/big:** `Float`, `Int`, `Rat` implement `encoding.TextAppender`.
- **math/rand:** top-level `Seed` no-op (`randseednop=0` restores old behavior).
- **math/rand/v2:** `ChaCha8`, `PCG` implement `encoding.BinaryAppender`.
- **net:** `ListenConfig` uses MPTCP by default where supported; `IP` implements `encoding.TextAppender`.
- **net/http:** 1xx response limits vs `MaxResponseHeaderBytes` / `Got1xxResponse`; `Server`/`Transport` **`HTTP2`** field; **`Protocols`** including **Unencrypted HTTP/2** (“prior knowledge”, not h2c Upgrade).
- **net/url:** `URL` implements `encoding.BinaryAppender`.
- **os/user:** Windows Nano Server; built-in service accounts; faster domain `Current`; impersonation returns process owner.
- **regexp:** `Regexp` implements `encoding.TextAppender`.
- **runtime:** `GOROOT` deprecated — prefer `go env GOROOT`.
- **sync:** `Map` new implementation (`GOEXPERIMENT=nosynchashtriemap` revert).
- **testing:** `T.Context`, `B.Context`; `T.Chdir`, `B.Chdir`.
- **text/template:** `range` over function iterators and integers.
- **time:** `Time` implements `encoding.BinaryAppender` and `encoding.TextAppender`.

### Ports

- **Linux:** kernel **3.2+**.
- **Darwin:** **1.24 last on macOS 11**; 1.25 requires **macOS 12+**.
- **WebAssembly:** `go:wasmexport`; WASI `c-shared` reactor; `go:wasmimport`/`export` more types; support files → `lib/wasm`; smaller initial memory.

---

<span id="go125"></span>

## Go 1.25 — https://go.dev/doc/go1.25

### Language

- No program-visible syntax changes; spec: “core types” removed — see [blog](https://go.dev/blog/coretypes).

### Go command

- **`go build -asan`:** default C leak detection at exit (`ASAN_OPTIONS=detect_leaks=0` to disable).
- Fewer prebuilt tools — on-demand `go tool`.
- **`go.mod` `ignore`** directive.
- **`go doc -http`**.
- **`go version -m -json`**.
- VCS subdirectory syntax for module roots.
- **`work` package pattern** (workspace/main modules).
- **`go`/`go.work` updates:** no longer auto-add `toolchain` line matching current command.

### Vet

- **`waitgroup`**, **`hostport`**.

### Runtime

- **Container-aware `GOMAXPROCS`:** cgroup CPU on Linux; periodic updates; `containermaxprocs=0` / `updatemaxprocs=0`.
- **`GOEXPERIMENT=greenteagc`** — experimental GC ([issue #73581](https://go.dev/issue/73581)).
- **`runtime/trace.FlightRecorder`** ring buffer + `WriteTo`.
- **Repanned panic output** (`[recovered, repanicked]`).
- **Linux VMA names** (`decoratemappings=0`).

### Compiler

- **Nil check fix** ([issue #72860](https://go.dev/issue/72860)) — use results only after error check.
- **DWARF 5** (`GOEXPERIMENT=nodwarf5`).
- **Stack slice backing** (`bisect`, `-gcflags=-d=variablemakehash=n`).

### Linker

- **`-funcalign=N`**.

### Standard library — major additions

- **`testing/synctest`** stable (`Test`/`Wait`); old experimental API under `GOEXPERIMENT=synctest` removed in **1.26**.
- **`GOEXPERIMENT=jsonv2`:** `encoding/json/v2`, `encoding/json/jsontext`; `encoding/json` wires to new implementation when enabled.

### Standard library — minor changes (by package)

- **archive/tar:** `AddFS` symlinks via `ReadLinkFS`.
- **encoding/asn1:** T61String/BMPString stricter.
- **crypto:** `MessageSigner`, `SignMessage`; `fips140` runtime change no-op; SHA slower without AVX2.
- **crypto/ecdsa:** raw parse/bytes APIs.
- **crypto/elliptic:** remove hidden `Inverse`/`CombinedMult`.
- **crypto/rsa:** faster keygen; modulus not “secret” for verify docs.
- **crypto/sha1, sha3:** faster on some CPUs; `SHA3.Clone` / `hash.Cloner`.
- **crypto/tls:** `ConnectionState.CurveID`; `GetEncryptedClientHelloKeys`; `tlssha1`; FIPS EMS / curves.
- **crypto/x509:** `MessageSigner` for create APIs; truncated SHA-256 SKID (`x509sha256skid=0`); stricter BasicConstraints/pathLen; T61/BMP parsing.
- **debug/elf:** RISC-V ELF constants.
- **go/ast, go/parser, go/token, go/types:** deprecations and new APIs (`PreorderStack`, `ParseDir` deprecated, `AddExistingFiles`, `LookupSelection`, etc.).
- **hash:** `XOF`, `Cloner` — all std `Hash` implement `Cloner`.
- **hash/maphash:** `Hash.Clone`.
- **io/fs:** `ReadLinkFS`.
- **log/slog:** `GroupAttrs`, `Record.Source`.
- **mime/multipart:** `FileContentDisposition`.
- **net:** `LookupMX` IP-like names; Windows multicast IPv6; `FileConn`/`FileListener`/etc.
- **net/http:** `CrossOriginProtection`.
- **os:** Windows async I/O `NewFile`; `DirFS`/`Root.FS` `ReadLinkFS`; `CopyFS` symlinks; more `Root` methods.
- **reflect:** `TypeAssert`.
- **regexp/syntax:** `\p` names expanded; case-insensitive names.
- **runtime:** `AddCleanup` concurrent; `GODEBUG=checkfinalizers=1`; `SetDefaultGOMAXPROCS`.
- **runtime/pprof:** mutex profile stacks for runtime locks fixed.
- **sync:** `WaitGroup.Go`.
- **testing:** `Attr`, `Output`, `AllocsPerRun` + parallel panic.
- **testing/fstest:** `ReadLinkFS`; `TestFS` no unbounded symlink follow.
- **unicode:** `CategoryAliases`, `Cn`, `LC`.
- **unique:** more eager reclamation; `Handle` chains collect faster.

### Ports

- **Darwin:** **macOS 12+** required.
- **Windows:** **32-bit `windows/arm` last release** (removed in 1.26).
- **AMD64:** `GOAMD64=v3+` fused multiply-add.
- **Loong64:** race, cgo traceback, internal link.
- **riscv64:** `plugin` mode; `GORISCV64=rva23u64`.

---

<span id="go126"></span>

## Go 1.26 — https://go.dev/doc/go1.26

### Language

- **`new(expr)`** — initial value expression (e.g. optional pointer fields for JSON/protobuf).
- **Generic self-reference in constraints** — e.g. `type Adder[A Adder[A]] interface { Add(A) A }`.

### Go command

- **`go fix`** modernizers + `//go:fix inline`; obsolete old fixers removed.
- **`go mod init`** defaults `go` to **1.(N−1).0** for toolchain 1.N.x (broader compatibility story).
- **`cmd/doc` / `go tool doc` removed** — use **`go doc`**.

### Pprof

- **`-http`:** default flame graph.

### Runtime

- **Green Tea GC** default (`GOEXPERIMENT=nogreenteagc` opt-out until ~1.27).
- **~30% faster cgo** call baseline.
- **64-bit heap base randomization** (`GOEXPERIMENT=norandomizedheapbase64`).
- **`GOEXPERIMENT=goroutineleakprofile`** — `goroutineleak` profile + `/debug/pprof/goroutineleak` ([issue #74609](https://go.dev/issue/74609)).

### Compiler

- More stack allocation for slice backing (same debugging flags as 1.25).

### Linker

- **windows/arm64** internal link for cgo (`-ldflags=-linkmode=internal`).
- ELF/Mach-O layout details (`moduledata`, `.gopclntab`, `.gosymtab` removed, etc.) — affects binary analysis tools.

### Bootstrap

- Requires **Go 1.24.6+**; **1.28** expected to require **1.26+** point release.

### Standard library — new / experimental

- **`crypto/hpke`** (RFC 9180, post-quantum hybrid KEMs).
- **`GOEXPERIMENT=simd`:** [`simd/archsimd`](https://pkg.go.dev/simd/archsimd) (amd64, experimental API).
- **`GOEXPERIMENT=runtimesecret`:** [`runtime/secret`](https://pkg.go.dev/runtime/secret) (amd64/arm64 Linux).

### Standard library — minor changes (by package)

- **bytes:** `Buffer.Peek`.
- **crypto:** new `Encapsulator` and `Decapsulator` interfaces for abstract KEM keys.
- **crypto/dsa:** `GenerateKey` ignores `rand` — uses secure random; tests use `testing/cryptotest.SetGlobalRandom`; `GODEBUG=cryptocustomrand=1` restores old behavior.
- **crypto/ecdh:** same random-source behavior; new `KeyExchanger` implemented by `PrivateKey`.
- **crypto/ecdsa:** `big.Int` fields on keys deprecated; `GenerateKey`/`Sign*` ignore `rand` (use `cryptotest` + `cryptocustomrand=1`).
- **crypto/ed25519:** `GenerateKey(nil)` uses secure random (not overridable `rand.Reader`); `cryptocustomrand=1` restores.
- **crypto/fips140:** module v1.26.0 selectable with `GOFIPS140`; `WithoutEnforcement`, `Enforced`; `Version` with frozen module.
- **crypto/mlkem:** `DecapsulationKey768/1024.Encapsulator`; ~18% faster encaps/decaps; new **`crypto/mlkem/mlkemtest`** with derandomized encaps for KATs.
- **crypto/rand:** `Prime` ignores `rand` parameter (same testing/GODEBUG story).
- **crypto/rsa:** `EncryptOAEPWithOptions`; `GenerateKey`/`GenerateMultiPrimeKey`/`EncryptPKCS1v15` ignore `rand`; `Validate` fails if fields changed after `Precompute`; stricter `D` vs precomputed values; deprecate unsafe PKCS#1 v1.5 encryption padding APIs.
- **crypto/sha3:** zero value of `SHA3` = SHA3-256; zero value of `SHAKE` = SHAKE256.
- **crypto/subtle:** `WithDataIndependentTiming` no longer locks OS thread; child goroutines inherit; cgo interaction as in release notes.
- **crypto/tls:** hybrid `SecP256r1MLKEM768` / `SecP384r1MLKEM1024` on by default (`tlssecpmlkem=0`); `ClientHelloInfo.HelloRetryRequest`, `ConnectionState.HelloRetryRequest`; `QUICConn` TLS handshake error event; `Certificate.PrivateKey` may implement `crypto.MessageSigner` for TLS 1.2+; GODEBUG `tlsunsafeekm`, `tlsrsakex`, `tls10server`, `tls3des`, `x509keypairleaf` **removed in Go 1.27** (new defaults always on).
- **crypto/x509:** `ExtKeyUsage`/`KeyUsage` `String`; `ExtKeyUsage.OID`; `OIDFromASN1OID`.
- **debug/elf:** additional `R_LARCH_*` constants (LoongArch psABI v2.40).
- **errors:** `AsType` (generic `As`).
- **fmt:** unformatted `fmt.Errorf("x")` allocation aligned with `errors.New`.
- **go/ast:** `ParseDirective`; `BasicLit.ValueEnd` (fixes `End` for multiline literals — update tools that mutate `ValuePos`).
- **go/token:** `File.End`.
- **go/types:** `gotypesalias` GODEBUG removed in **Go 1.27** — always `Alias` nodes.
- **image/jpeg:** new faster encoder/decoder (bit-exact output may differ).
- **io:** `ReadAll` faster and smaller allocations.
- **log/slog:** `NewMultiHandler`.
- **net:** `Dialer.DialIP`, `DialTCP`, `DialUDP`, `DialUnix` (context-aware dialing by network type).
- **net/http:** `HTTP2Config.StrictMaxConcurrentRequests`; `Transport.NewClientConn`; `Client` cookie scoping with `Request.Host`; `ServeMux` trailing-slash redirects use **307**; see notes for cookie/`Pattern` behavior.
- **net/http/httptest:** `Server.Client` redirects `example.com` and subdomains to the test server.
- **net/http/httputil:** `ReverseProxy.Director` deprecated in favor of `Rewrite` (hop-by-hop header issue).
- **net/netip:** `Prefix.Compare`.
- **net/url:** `Parse` rejects malformed colon hosts (`urlstrictcolons=0` restores).
- **os:** `Process.WithHandle` (pidfd / Windows handle); Windows `OpenFile` may combine general flags with Windows-specific flags.
- **os/signal:** `NotifyContext` cancels with `context.CancelCauseFunc` and signal error.
- **reflect:** `Type.Fields`, `Methods`, `Ins`, `Outs`; `Value.Fields`, `Methods` iterators.
- **runtime/metrics:** `/sched/goroutines/*`, `/sched/threads:threads`, `/sched/goroutines-created:goroutines`, etc.
- **testing:** `T`/`B`/`F` `.ArtifactDir`; `-artifacts` flag; `B.Loop` no longer blocks inlining incorrectly.
- **testing/cryptotest:** `SetGlobalRandom` for deterministic crypto tests.
- **time:** `asynctimerchan` removed in **Go 1.27** — unbuffered timer channels always.

### Ports

- **Darwin:** **1.26 last on macOS 12**; **1.27 requires macOS 13+**.
- **FreeBSD riscv64:** broken.
- **Windows:** **32-bit `windows/arm` removed**.
- **linux/ppc64:** last **ELFv1**; **ELFv2** in 1.27.
- **linux/riscv64:** race detector.
- **s390x:** register ABI.
- **WebAssembly:** sign-extension/sat-conversion always on; `GOWASM` `signext`/`satconv` ignored; smaller heap chunks for small heaps.

---

<span id="doc-using"></span>

## Using this document

- For **exact wording**, migration steps, and **code samples**, open the **release notes URL** at the top of each section.
- For **GODEBUG keys** and defaults, see [godebug](https://go.dev/doc/godebug) and the per-release notes.
- Compatibility: [Go 1 compatibility](https://go.dev/doc/go1compat).
