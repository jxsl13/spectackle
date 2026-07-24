# wasm C PoC — measured results (T-0040)

Status: PoC executed, scored against docs/design-wasm-parsers.md §5's exit
criteria. All numbers below are measured on this machine
(`poc/wasmparse/cmd/poc`, `poc/wasmparse/cmd/sizewith`,
`poc/wasmparse/cmd/sizewithout`), 2026-07-24. This document is the
scoring artifact `docs/design-wasm-parsers.md`'s §5 recommendation asked
for; it does not re-litigate §1-§4, only executes the first slice and
reports what happened.

Code: `poc/wasmparse/` — its own Go module (`replace
github.com/jxsl13/spectacle => ../..`), never added to the root module.
Reproduce with `cd poc/wasmparse && go run ./cmd/poc`.

## 1. Setup

`poc/wasmparse/go.mod` requires `github.com/malivvan/tree-sitter v0.0.1`
(pulls `tetratelabs/wazero v1.8.2`, embeds a 4.2MB WASI `ts.wasm` exporting
`tree_sitter_c`/`tree_sitter_cpp`) and `github.com/jxsl13/spectacle`
(replaced to `../..`) to reach `internal/langspec`'s production `cSpec` as
the oracle. Both module fetches succeeded through the preconfigured proxy
on the first `go mod tidy` — no retries needed, nothing hand-vendored.

## 2. Corpus (Part 1)

`poc/wasmparse/corpus/gen.go` deterministically generates (seeded
`math/rand`, `Generate(42)`) 1000 functions with bodies (if/for/while/switch
control flow inside each body as negative bait — all indented, so neither
the column-0-anchored cSpec regex nor a correct AST walk should ever treat
them as top-level definitions), 100 structs/unions/enums (alternating
typedef'd/plain), and 50 function-like macros, spread across 50 `.c`/`.h`
files. `poc/wasmparse/corpus/gen_test.go`'s `TestGenerateDeterministic`
proves same-seed-same-bytes; `TestGenerateCounts` pins the 1000/100/50
sizing.

`poc/wasmparse/cmd/poc` adds this repo's one real C-adjacent fixture,
`examples/saxpy/saxpy/kernels/saxpy.h` (no other `.c`/`.h` file exists under
`examples/` — checked directly, not assumed), by path reference (read from
its real location, not copied).

**Measured corpus: 51 files, 9,621 LOC.**

Limitation worth stating up front: every generated function signature is
deliberately single-line (a design constraint of cSpec's regex — see
`internal/langspec/c.go`), and every struct/union/enum is named (anonymous
ones can't match cSpec's `(\w+)` either). The corpus is therefore shaped to
be *comparable*, not adversarial: it cannot by construction surface cases
where tree-sitter's AST would out-fidelity a line regex (multi-line
signatures, K&R declarations, anonymous types nested in typedefs, function
pointers). Part 3(a)'s clean parity result should be read with that caveat.

## 3. Both parsers over the corpus (Part 2)

- **Oracle**: `poc/wasmparse/cparser` wraps `langspec.All()` filtered to
  `graph.LangC` — the exact `cSpec` shipping in production
  (`internal/langspec/c.go`), reached through the real
  `index.LanguageParser` interface, not by referencing the unexported
  `cSpec` value directly.
- **wasm**: `poc/wasmparse/tswasm` wraps `malivvan/tree-sitter`. The
  binding is low-level (positional `Child(i)`/`ChildCount()`/`Kind()` only —
  no field-based child access such as "give me the declarator field"), so
  extraction is a full pre-order tree walk pattern-matching on node kind
  strings: `function_definition`/`declaration` (only emitted if a nested
  `function_declarator` is found — mirrors cSpec's requirement for
  `(...)`), `struct_specifier`/`union_specifier`/`enum_specifier` (only if a
  direct `type_identifier` child names it), `preproc_function_def`. Lines
  are recovered from byte offsets via a precomputed newline-offset table
  (binary search), since the binding exposes no line/column API.
  `poc/wasmparse/tswasm/tswasm_test.go` and
  `poc/wasmparse/cparser/cparser_test.go` both pin exact expected triples
  on a small fixed sample.

## 4. Measurements (Part 3)

### (a) PARITY — PASS

Comparison is per-file, greedy, `±1` line tolerance on `(name, kind)`
(`poc/wasmparse/cmd/poc/main.go`'s `comparePar`). Result, stable across 5
independent process runs:

| matched | cSpec-only (oracle regressions) | tree-sitter-only (fidelity gains) |
|---|---|---|
| **1151** | **0** | **0** |

1151 = 1000 functions + 100 types + 50 macros (synthetic) + 1
(`launch_saxpy`, from `saxpy.h`). Zero cSpec-only means tree-sitter never
missed anything cSpec found; zero tree-sitter-only means, on this
corpus, tree-sitter found nothing extra either — expected given §2's
single-line/named-only construction, not evidence the two approaches are
equivalent on harder inputs. cSpec's known double-match quirk (a line like
`struct Point *make_point(void) {` mints both a `KFunc` and a `KType`
because the struct-tag regex has no trailing anchor — see
`internal/langspec/c.go`'s doc comment) was confirmed to reproduce
identically on the tree-sitter side (the return-type `struct_specifier`
is a real, distinct AST node), which is *why* parity holds rather than
tree-sitter emitting extras on those lines.

### (b) SIZE — PASS (within budget, not much headroom)

`CGO_ENABLED=0 go build`, two minimal binaries with matched non-wasm
dependencies (both stdlib-only aside from the one thing under test, so the
delta isolates just the wasm engine, not `internal/index`'s own transitive
weight such as `modernc.org/sqlite`):

| binary | imports | size |
|---|---|---|
| `poc/wasmparse/cmd/sizewith` | `tswasm` (wazero + embedded 4.2MB `ts.wasm`, parses one string) | 11,511,584 bytes |
| `poc/wasmparse/cmd/sizewithout` | none (prints a string) | 2,266,696 bytes |
| **delta** | | **9,244,888 bytes ≈ 8.82 MiB (9.24 MB)** |

Against the design doc's proposed ≤10MB single-language PoC budget
(docs/design-wasm-parsers.md §5): **passes, at ~88% of budget**, and that
8.82MB pays for *both* C and C++ (malivvan's blob statically combines
both — there is no way to strip C++ out and embed only C without
rebuilding the vendor's own wasm from source). A from-scratch single-C-only
build would likely be smaller, but that number doesn't exist without doing
the wasi-sdk build ourselves — out of scope for this PoC.

For context, `bin/spectacle` (root `make build`, `CGO_ENABLED=0`, current
full production binary with cgo tree-sitter C/C++/CUDA/ObjC/MSL backends
already linked in) is **22,174,377 bytes (≈21.2 MB)** as of this run — so
the wasm engine's marginal 8.82MB single-language cost is already ~40% of
today's *entire* multi-language binary, before the wasi-sdk work needed to
add CUDA/ObjC/MSL on top of it.

### (c) LATENCY — FAIL (inconsistent, sometimes violates the M4 budget outright)

Methodology: one shared engine/parser instance, 5 full-corpus passes;
"warm" is the min-max range across passes 2-5 (not pass 2 alone — see the
note below on why a single number would mislead), scaled linearly from
9,621 measured LOC to the M4 gate's 100k LOC, `poc/wasmparse/cmd/poc`'s
`printScaledRange`.

| side | warm range (9,621 LOC) | scaled to 100k LOC | M4 budget | headroom |
|---|---|---|---|---|
| cSpec oracle | 5.0–7.0ms | 52–73ms | 5s | **69x–96x** |
| tree-sitter wasm | 198–613ms | 2.06s–6.4s | 5s | **0.8x–2.4x** |

wazero engine compile+instantiate (one-time cold cost, amortized over the
process lifetime): ~295–332ms. First-file-post-init parse: ~7ms (not
itself expensive — the 4.2MB module compile dominates cold start).

**The wasm side's worst observed pass exceeds the M4 5s/100k-LOC budget
outright** (headroom <1x, i.e. scaled time >5s) in 2 of 5 independent
process runs; best-case passes land around 2.4x headroom — nowhere near
"comparable to today's cgo path" (cSpec's regex approach, admittedly a
different backend, clears the same budget by 69-96x). Root cause, isolated
by re-running the same corpus with Go's GC disabled: every
`malivvan/tree-sitter` host call (`Kind`/`Child`/`ChildCount`/`StartByte`/…)
returns a freshly allocated `[]uint64` from wazero's `api.Function.Call`,
and this PoC's full-tree walk — forced by the binding exposing no
field-based child access — issues many thousands of such calls per file.
With GC on, that allocation churn is reclaimed but collections spike wall
time 2-3x pass to pass (the 198ms-613ms spread above); with GC off, Go heap
grows monotonically (confirmed: 46MB → 476MB over 6 passes in a side
experiment) and passes get monotonically slower instead. This is a cost of
malivvan/tree-sitter's specific calling convention, not an indictment of
wazero or wasm parsing in general — a hand-rolled adapter batching node
reads (e.g., one host call returning a whole subtree, or a WASI
`ts_node_string`-style dump parsed once) would not pay this tax — but it is
real, measured, and this criterion is scored against what was actually
built and run, not against a hypothetical better binding.

### (d) AVAILABILITY — FAIL (as currently published; unverified either way without a real build)

Checked via GitHub/pkg metadata only, no builds, per the brief.

**tree-sitter-cuda**
([tree-sitter-grammars/tree-sitter-cuda](https://github.com/tree-sitter-grammars/tree-sitter-cuda)):
latest release `v0.21.1`, published 2025-09-18, **has zero release
assets** — no `.wasm` is published at all. Its own `package.json` pins
`tree-sitter-cli@^0.25.9` as the build-time devDependency, which predates
the wasi-sdk build-path switch
([v0.26.1](https://github.com/tree-sitter/tree-sitter/releases/tag/v0.26.1),
published 2025-12-08 — *after* cuda's latest release) — so even running
this repo's own default build script today would invoke a pre-wasi-sdk
(Emscripten) CLI. Building a wasi-sdk wasm for CUDA would require someone
to manually run a newer CLI against the grammar and verify it still
produces a working parser — unverified, not attempted here (out of scope:
"no builds").

**tree-sitter-objc**
([tree-sitter-grammars/tree-sitter-objc](https://github.com/tree-sitter-grammars/tree-sitter-objc)):
latest release `v3.0.2`, published **2024-12-16** (over a year before
v0.26.1 existed), **does publish** a `tree-sitter-objc.wasm` release asset
— but given the publish date, that asset was necessarily built with a
pre-wasi-sdk CLI, i.e. it is an Emscripten wasm build. Its `package.json`
confirms this indirectly: `tree-sitter-cli@^0.24.5` devDependency, also
pre-0.26.1. No release since the wasi-sdk switch exists to check instead.

**Neither grammar has a wasi-sdk-era (post-2025-12-08) release.** The exit
criterion asks whether "no Emscripten-only grammar `.wasm` is required" —
as of what's actually published today, CUDA has no wasm at all and ObjC's
only published wasm is Emscripten-built, so the criterion as stated does
not currently hold. Whether a *fresh* build against the current CLI would
succeed is a real, answerable, but separate question this PoC did not
attempt (it requires a build, explicitly out of scope here) — so this
should be read as "not currently available," not "provably impossible."

## 5. Verdict (Part 4)

| Criterion | Result |
|---|---|
| (a) parse output matches cgo/langspec oracle | **PASS** (1151/1151, 0 regressions, 0 gains — but see §2's corpus-construction caveat) |
| (b) binary size growth ≤10MB budget | **PASS** (8.82MB / 9.24MB, ~88% of budget, C+C++ combined) |
| (c) warm latency inside M4 5s/100k-LOC envelope, headroom comparable to cgo | **FAIL** (0.8x-2.4x headroom, violates the hard budget outright in 2/5 runs; cSpec clears the same budget by 69-96x) |
| (d) no Emscripten-only grammar `.wasm` required for CUDA/ObjC | **FAIL as published today** (CUDA: none published; ObjC: only a pre-wasi-sdk Emscripten build published) |

Two of four exit criteria fail. Per docs/design-wasm-parsers.md §5 ("Missing
any of (a)-(d) means staying on cgo tree-sitter past M6, or narrowing the
WASM migration to only the languages that clear it"), **this PoC does not
green-light the wazero/wasm migration for the remaining four languages.**

**Recommendation: stay on cgo tree-sitter (C/C++/CUDA/ObjC's existing
backends) past M6; do not adopt malivvan/tree-sitter or invest in a
wasi-sdk build pipeline on the strength of this PoC.** The size number
(b) is genuinely encouraging — a single-language wasm engine fits inside a
sane budget — but it is the only criterion that cleared with real margin.
Latency (c) is not a rounding-error miss: at worst-case GC pressure the C
PoC alone, one language, already brushes against the *whole-envelope*
budget meant to cover all future languages combined, and the measured
cause (thousands of tiny cross-boundary host calls per file, an artifact of
this specific binding's calling convention rather than of wazero or
tree-sitter-the-engine) would need a materially different binding — batched
node reads, not per-field host calls — before this criterion could be
re-run fairly. Availability (d) independently blocks CUDA and ObjC today
regardless of the latency question: there is no current wasi-sdk build to
point at for either language, only a stale Emscripten wasm for ObjC and
nothing at all for CUDA, and producing one is new upstream-adjacent build
work, not a drop-in. None of this rules out revisiting wazero/wasm later —
if a better-designed binding (batched reads) appears, or CUDA/ObjC publish
wasi-sdk builds, criteria (c) and (d) could be re-scored cheaply by rerunning
this same PoC — but on today's evidence the fidelity axis (P-0019's
langspec, already shipping) and the existing cgo backends remain the right
place to invest, not a new wasm runtime.

## Rollback

`rm -rf poc/` and this file. The root module is untouched by construction
(separate `go.mod`, no `go.work`, `poc/` was never referenced from
`cmd/`/`internal/`).
