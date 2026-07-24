# Design research — wazero × tree-sitter feasibility (R-0004)

Status: research, no implementation. Scope: is the M6 "wazero/WASM parser
backend replaces cgo tree-sitter" target picture (docs/architecture.md §2,
docs/roadmap.md M6) actually buildable today, and with what first slice?
Method: verified candidates by `go get`-ing them into a throwaway scratch
module (never added to spectacle's own `go.mod`) and reading source/go.mod
from the module cache, plus GitHub/pkg.go.dev pages, 2026-07-24. Nothing
below is asserted from memory alone.

## 1. Candidates (verified, not assumed)

| Candidate | Version / stand | Imports `tetratelabs/wazero`? | Notes |
|---|---|---|---|
| [malivvan/tree-sitter](https://github.com/malivvan/tree-sitter) ([pkg.go.dev](https://pkg.go.dev/github.com/malivvan/tree-sitter)) | v0.0.1, MIT, published 2025-01-25 | **Yes** — `go.mod` requires `github.com/tetratelabs/wazero v1.8.2`, confirmed by `go get` and reading `treesitter.go` | Embeds one 4.2 MB `lib/ts.wasm` (`//go:embed`), instantiates it with `wazero.NewRuntime` + `wasi_snapshot_preview1` (real WASI, not emscripten JS glue). `src/` vendors C sources for ~25 grammars, but `treesitter.go` only wires and exports two: `languageC`/`languageCpp`. Repo hygiene is thin (`.idea/` committed, 4 GitHub stars, no evidence of recent activity beyond the single tag) — usable as a proof that the approach works, not as a dependency to build on as-is. |
| [odvcencio/gotreesitter](https://github.com/odvcencio/gotreesitter) ([pkg.go.dev](https://pkg.go.dev/github.com/odvcencio/gotreesitter)) | v0.47.0, MIT, `go.mod` pulls only `x/sync`+`yaml.v3` | **No** — grep of the full downloaded module (1712 `.go` files, 39 MB) finds zero references to `wazero`; its `wasm/` directory (`wasm_exec.js`, `loader.js`) is tooling to cross-compile *gotreesitter itself* to `wasip1`, unrelated to loading grammars | Not a wazero runtime at all — a from-scratch pure-Go reimplementation of tree-sitter's parsing engine, claiming 206 embedded grammars. Real and it builds, but out of scope for this question (no WASM/wazero involved), and its provenance is a yellow flag worth naming: extremely high commit churn for its age, mechanically-patterned file/test names (`parser_result_forth.go`, `w1_block_splice_test.go`), and a v0.47.0 tag days old at research time. Treat as unverified-for-correctness until someone runs its own conformance suite against upstream grammars — not a dependency to bet fidelity on sight-unseen. |

Both packages exist, are publicly fetchable, and compile — that part of the
question is answered. Neither is a "just import it" answer for spectacle:
one is a thin, unmaintained-looking 2-language PoC; the other doesn't use
wazero and carries its own credibility questions.

## 2. Grammar `.wasm` availability and ABI

Per-grammar `.wasm` builds are real and published (`tree-sitter build --wasm`
producing `tree-sitter-<lang>.wasm`, distributed via npm — e.g.
`node_modules/tree-sitter-javascript/tree-sitter-javascript.wasm` — or
GitHub release assets). Historically these were Emscripten builds: the
tree-sitter CLI's wasm compiler embeds Emscripten-specific host imports
(`emscripten_notify_memory_growth`, `__assert_fail`, dynamic-linking
metadata such as `__memory_base`/`__stack_pointer` in a `dylink_0`
section) — wazero cannot satisfy that surface without a hand-rolled
Emscripten shim. As of tree-sitter CLI
[v0.26.1](https://github.com/tree-sitter/tree-sitter/releases/tag/v0.26.1)
the build path switched to `wasi-sdk` (auto-downloaded, no toolchain
install needed), which targets WASI — the surface wazero's built-in
`wasi_snapshot_preview1` module already implements.

That toolchain switch is necessary but not sufficient. Grammar `.wasm`
modules are designed to be loaded as *side modules* into a shared
tree-sitter *core* engine at runtime (web-tree-sitter's `Language.load`) —
a dynamic-linking scheme independent of WASI-vs-Emscripten. Nobody in §1
has published a general "load an arbitrary grammar `.wasm` into a wazero
host at runtime" loader; malivvan/tree-sitter sidesteps the problem
entirely by statically compiling engine + specific grammars into one fixed
`.wasm` blob at *their own* build time, wired through hand-written exported
functions per language. That is workable but means each new language is a
new upstream C build + wasm recompile + Go export, not a runtime-pluggable
module drop-in — closer in spirit to the cgo approach we already have than
to "download a `.wasm` and go."

## 3. Integration shape (if pursued)

The target shape fits `index.LanguageParser` cleanly regardless of which
loading strategy wins: one adapter type owns a single `wazero.Runtime` plus
N compiled grammar modules (either N standalone WASI binaries, or — more
realistically given §2 — one combined binary exporting N `language*()`
functions); `Parse` calls into the WASM tree-sitter API (`ts_parser_new`,
`ts_parser_parse_string`, node walk) exactly like the current cgo bindings
do, so `graph.Node`/`graph.Edge` construction and the resolver layer are
untouched. **Symbol extraction is unaffected either way**: tags queries
(`queries/tags.scm`) already ship per-grammar upstream and are independent
of the C-vs-WASM execution backend — confirmed present for
[tree-sitter-cpp](https://github.com/tree-sitter/tree-sitter-cpp/tree/master/queries)
(`highlights.scm`, `injections.scm`, `tags.scm`). What must be *written*
new, not reused: the WASM host-function bindings (§1's `treesitter.go` is
the only public example, and only exposes 2 of the ~5 languages M6 targets
— CUDA and ObjC grammars would need their own build+export step with no
existing reference), and — if the runtime-pluggable route is chosen over
malivvan's static-binary route — a general grammar-module loader, which is
unbuilt anywhere in the ecosystem today.

## 4. Cost estimate

**Binary size**: malivvan's combined engine + 2 grammars embeds at 4.2 MB
(measured directly from the downloaded module). That is a *lower* bound for
M6's five-language target (C, C++/MSL, CUDA, ObjC, plus whatever engine
overhead is fixed vs. per-grammar) — no verified per-grammar delta exists
in public data reviewed here, so budget conservatively: single-digit MB
added to the release binary, not the tens-of-KB a line scanner costs.
**Latency**: no candidate ships parse-time benchmarks against native/cgo.
The only verified data point is generic: wazero's compiler mode (the fast
path; interpreter mode is ~10x slower still) runs roughly
[4.7x slower than native code](https://00f.net/2026/06/23/webassembly-runtimes-2026/)
in a 2026 cross-runtime comparison — not tree-sitter-specific, but the right
order of magnitude to expect relative to the current cgo tree-sitter path,
and far above a line scanner's near-zero per-file cost (plan9asm parses
this repo in single-digit ms; see docs/design-incremental-index.md §3 for
the comparable Go/cgo baseline). Neither cost is disqualifying on its own —
M4's gate is 100k LOC warm in <5s and today's warm numbers have an order of
magnitude of headroom — but both are real, unmeasured-for-tree-sitter
regressions worth pricing before committing five languages to this path.

## 5. Recommendation: defer, with a narrow first slice

**Defer** the full M6 migration; **do not** adopt either §1 candidate as a
dependency today. Neither is a safe foundation: malivvan is a thin,
low-activity 2-language PoC; gotreesitter doesn't use wazero at all and its
own provenance needs independent vetting before it's trusted for parse
fidelity. The ABI story (§2) is now plausible — the wasi-sdk switch removes
the Emscripten blocker — but the runtime-loader gap means "wazero ×
tree-sitter" is a **build-it-yourself** project, not an integration one.

**First-slice PoC, if/when picked up**: one language, C (already spectacle's
simplest cgo backend, docs/architecture.md §2), built the way
malivvan/tree-sitter does it — tree-sitter core (wasi-sdk build) statically
combined with `tree-sitter-c`'s grammar, one exported `language()` function,
consumed via a `wazero.Runtime` behind a new `CParserWASM` implementing
`index.LanguageParser`, `Parse` output diffed byte-for-byte against the
existing cgo `CParser` (SPX-GRA-001 determinism gives a free oracle) across
this repo's own C/CUDA/ObjC-adjacent fixtures plus a synthetic corpus sized
like docs/design-incremental-index.md §2's `synth1000`.

**Exit criterion**: green light for the remaining four languages only if
the C PoC clears *all* of: (a) parse output matches the cgo backend exactly
on the fixture corpus, (b) `CGO_ENABLED=0` release binary size growth stays
within a pre-agreed budget (propose ≤10 MB for the single-language PoC,
re-baselined once real numbers exist), (c) warm parse latency for this
repo's C-adjacent files stays inside the M4 5s/100k-LOC envelope with
headroom comparable to today's cgo path, (d) no Emscripten-only grammar
`.wasm` is required for any of the remaining four languages (CUDA and ObjC
grammars have not been checked here — that check is part of the PoC, not
assumed). Missing any of (a)-(d) means staying on cgo tree-sitter past M6,
or narrowing the WASM migration to only the languages that clear it.

## 6. Fallback: this is a fidelity axis, not a breadth axis

P-0019 (`internal/langspec`, declarative regex-based `SpecParser`) already
covers *breadth* today — adding a language is one spec file, pure Go, no
WASM/cgo either way, shipping now. wazero/WASM is orthogonal: it only
matters for the languages that need real tree-sitter grammars for
call-graph fidelity (C/C++/CUDA/ObjC's existing cgo backends,
docs/architecture.md §2) rather than langspec's line/regex-level symbol
extraction. Neither track blocks the other; M6 can ship the langspec
cookbook on schedule while this WASM question stays open past it.
