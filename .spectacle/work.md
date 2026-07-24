---
schema: v0
---

## D-0002 R-0004 empfiehlt: wasm/tree-sitter-Parser DEFERREN; Erst-Slice waere ein C-PoC (tree-sitter-c via wazero) mit Parity-Oracle gegen die Regex-Kette, Binärgrößen- und Latenzbudget innerhalb der M4-Envelope (docs/design-wasm-parsers.md). Wie verfahren wir mit wazero?
kind: decision
state: done
created: 2026-07-24

kind: radio
options: defer — langspec-Regex-Kette bleibt der Weg, wasm-PoC erst bei echtem Parity-Bedarf, c-poc — jetzt einen C-PoC-Task minten (tree-sitter-c + wazero, Parity-Oracle), no-go — wasm-Pfad endgültig verwerfen
choice: c-poc — jetzt einen C-PoC-Task minten (tree-sitter-c + wazero

## P-0022 wazero C-PoC: measure the wasm/tree-sitter path against the langspec chain (D-0002: c-poc)
kind: proposal
state: approved
created: 2026-07-24
grilled: 2026-07-24
targets: docs/design-wasm-parsers.md, internal/langspec/c.go

Executes decision D-0002 (c-poc) grounded in R-0004 (docs/design-wasm-parsers.md §5): a NARROW, throwaway proof-of-concept that prices the wazero x tree-sitter path for C with real numbers instead of estimates — parity vs. the shipping langspec cSpec chain, binary-size delta, warm parse latency — WITHOUT touching spectacle's own go.mod, build, or release artifacts (root stays CGO_ENABLED=0, zero new deps). Vehicle: malivvan/tree-sitter v0.0.1 (MIT, wazero-backed, embedded wasi ts.wasm with C+C++ exports) used strictly as a measurement instrument per R-0004's verdict ('usable as a proof that the approach works, not as a dependency to build on'). Output: docs/design-wasm-poc-c.md with a results table scored against R-0004's exit criteria (a) parity, (b) size budget <=10MB, (c) M4 latency envelope, (d) grammar availability notes for cuda/objc — feeding the final go/no-go decide for the remaining languages.

## T-0040 wasm C-PoC: isolated module, parity oracle vs cSpec, size+latency numbers, verdict doc
kind: task
state: approved
created: 2026-07-24
parent: P-0022
targets: docs/design-wasm-parsers.md

SCOPE (disjoint from everything else; NOTHING outside these paths): poc/wasmparse/ (new directory, own Go module) and docs/design-wasm-poc-c.md (new file). You MUST NOT touch the root go.mod/go.sum, Makefile, .goreleaser.yaml, internal/, cmd/, or CI files. Root `make all` must pass unchanged before AND after (run it once at the end to prove it).

GOAL: execute R-0004's first-slice PoC (docs/design-wasm-parsers.md §5 — read it first) and score its exit criteria (a)-(d) with measured numbers, so the follow-up decide can green-light or kill the wasm path for the remaining languages.

SETUP: poc/wasmparse/go.mod — module github.com/jxsl13/spectacle/poc/wasmparse, go 1.24. Require github.com/jxsl13/spectacle (replace => ../..) to import internal/langspec + internal/graph + internal/index for the oracle side. Require github.com/malivvan/tree-sitter v0.0.1 (pulls tetratelabs/wazero v1.8.2, embeds a 4.2MB wasi ts.wasm exporting C and C++). Network goes through the preconfigured module proxy — plain `go get` works; if the module fetch fails, STOP and report (do not vendor by hand).

PART 1 — corpus: poc/wasmparse/corpus/gen.go generates a synthetic C corpus deterministically (seeded): 1000 functions with bodies, 100 structs/unions/enums (typedef'd and not), 50 function-like macros, spread over ~50 .c/.h files with control flow (if/for/while/switch) inside bodies as negative bait. Also include the repo's real C-adjacent fixtures by path reference (examples/saxpy/saxpy/kernels/saxpy.h and any .c/.h under examples/).

PART 2 — both parsers over the corpus: (i) oracle side: langspec cSpec via langspec.All() filtered to Lang c (it implements index.LanguageParser; call Parse per file, collect nodes: name, kind, line). (ii) wasm side: malivvan/tree-sitter — parse each file with its C language, walk the tree, extract function_definition/declaration names+lines, struct/union/enum specifiers, preproc_function_def — mapping to the same (name, kind, line) triple space.

PART 3 — measurements, all recorded in docs/design-wasm-poc-c.md:
(a) PARITY: every triple cSpec finds must be found by tree-sitter within ±1 line (tree-sitter names the definition line precisely; regex may anchor on the prototype line). Report: matched, cSpec-only (= oracle regressions, MUST be zero or itemized with cause), tree-sitter-only (= fidelity gain, count + 5 examples).
(b) SIZE: `go build` a minimal binary that embeds+instantiates the wasm engine and one that does not; report the delta and compare against R-0004's <=10MB PoC budget. Also report bin/spectacle's current size for context.
(c) LATENCY: cold (first Parse incl. wazero compile) and warm (subsequent files) wall time for the full corpus, per side; scale to the M4 envelope (100k LOC < 5s warm) and state the headroom multiple.
(d) AVAILABILITY: check (via the module cache / GitHub, no builds) whether tree-sitter-cuda and tree-sitter-objc publish grammars buildable with tree-sitter CLI >=0.26.1 wasi builds; one paragraph each, links.

PART 4 — verdict: final section of docs/design-wasm-poc-c.md scoring (a)-(d) as pass/fail against R-0004 §5's exit criteria, with a one-paragraph recommendation (proceed to own wasi-sdk build pipeline / stay on langspec+regex / hybrid). Be honest: a failed criterion is a result, not a problem.

LIFECYCLE: lease claim paths=["poc/wasmparse/","docs/design-wasm-poc-c.md"] item=T-0040; move active before coding, done after; release lease. NEVER edit .spectacle/ directly; never commit/push.

VERIFY: cd poc/wasmparse && go build ./... && go vet ./... && go test ./... (write at least one test: the corpus generator is deterministic — same seed, same bytes). Then cd ../.. && make all (must stay green, PoC module is outside the root workspace).

ROLLBACK: rm -rf poc/ + the doc — the root module is untouched by construction.

EXIT CRITERION: docs/design-wasm-poc-c.md exists with real measured numbers in all four sections + verdict; both builds green; root make all green.
