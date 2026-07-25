---
schema: v0
---

## ADR-0012 R-0005 found major parser gaps in 30 of 32 languages. Which remediation path should spectackle take?
kind: adr
state: done
created: 2026-07-25
context: Empirical per-language probes show three fixable classes: missing/over-strict langspec Def regexes (data-only fixes, biggest recall wins: JS/TS class methods, Java constructors, Python async def, C# Allman braces, Rust const fn, Swift override init, plus asm/cuda/go hand-parser fixes), an engine gap for end-terminated languages (no body spans, some with no call edges: ruby, lua, elixir, julia, fortran, perl, php, r, shell), and the standing wazero/tree-sitter alternative (ADR-0010/0011: latency-red, availability-gated for CUDA/ObjC, but would fix span+edge+Def classes wholesale where grammars exist). Correctness-first axis per prior user steer.
decision: engine-endspan: harden regexes AND add end-keyword span/edge support to the langspec engine
consequences: Stays pure Go (no wazero adoption; ADR-0010 availability gate unchanged). Two work streams: (1) per-language Def/Call regex hardening across all 27 brace-style langspec files plus the go/asm/cuda hand parsers; (2) a langspec engine extension for end-keyword-terminated blocks (lua, ruby, elixir, julia, fortran) providing real body spans and enabling call-edge extraction where CallRe is nil today (also perl/php/r/shell, which are brace-style and just lack CallRe). Trade-off accepted: regex hardening is asymptotically inferior to a real grammar, but every identified miss is a bounded pattern fix, and the wazero option remains open behind its availability gate.
status: accepted

kind: radio
option: harden-langspec: fix Def/Call regexes per language + hand-parser fixes, keep pure-Go chain
option: engine-endspan: harden regexes AND add end-keyword span/edge support to the langspec engine
option: wazero-partial: adopt wazero/tree-sitter for the worst offenders where grammars exist, langspec for the rest
option: record-only: keep findings as research, no implementation now
choice: engine-endspan: harden regexes AND add end-keyword span/edge support to the langspec engine

## P-0084 langspec engine: end-keyword body spans, so end-terminated languages get real spans and call edges
kind: proposal
state: active
created: 2026-07-25
refs: ADR-0012, R-0005
grilled: 2026-07-25
targets: internal/cspan/cspan.go, internal/langspec/langspec.go

Per ADR-0012 (engine-endspan) resolving R-0005. The langspec engine bounds bodies exclusively by brace counting (cspan.Span), so end-terminated languages (lua, ruby, elixir, julia, fortran) ship EndLine==Line for every symbol and can never set CallRe — impact BFS is blind there. Add a keyword-counting span: Spec gains an optional EndSpan config (open/close regexes); Parse uses it in place of cspan.Span when set, feeding the existing callEdges loop unchanged. cspan gains KeywordSpan alongside Span, same leaf-package discipline. Default nil = byte-identical current behavior (same guarantee CallRe made when introduced). Then per-language data updates switch the five end-terminated languages onto it. Rejected: indentation-based spans for python/haskell in this pass (different mechanism, separate proposal if the hardened regexes prove insufficient); tree-sitter adoption (ADR-0012 kept the pure-Go chain, wazero stays gated per ADR-0010). Scope disjoint: engine task owns internal/cspan + langspec.go; data task owns the five language files and runs only after the engine task merges. Exit: engine tests green including a no-behavior-change guard for nil EndSpan; the five languages report multi-line spans and call edges over the R-0005 scratch fixtures. Rollback: the field and KeywordSpan are additive; reverting restores brace-only behavior.

## P-0085 langspec Def/Call hardening: close the R-0005 regex misses across all brace-style languages
kind: proposal
state: active
created: 2026-07-25
refs: ADR-0012, R-0005
grilled: 2026-07-25
targets: internal/langspec

Per ADR-0012 (engine-endspan) resolving R-0005. Every brace-style langspec file has concrete, empirically confirmed Def misses (constructs minting no node: JS/TS class methods, Java constructors, Python async def, C# Allman-style block bodies — note cspan already handles Allman braces since T-0053, so those are def-line regex fixes, not engine work — Rust const fn, Swift override init, Groovy default visibility, ObjC @protocol, Zig error sets, OCaml and-chains, GLSL structs, Metal multi-line signatures, and more per the R-0005 findings), and perl/php/r/shell additionally leave CallRe nil despite having braces. Fix per language: extend or add Def regexes, set CallRe+Stop where missing, keep QualMode untouched. Ground truth: each language's R-0005 scratch fixture and findings file. Batched into five disjoint tasks by family (web, jvm/dotnet, c-family/shader, scripting, systems/functional) so leases never overlap; python and javascript also gain their missing _test.go siblings. Exit per language: previously-missed constructs mint nodes with correct spans over the R-0005 fixture, package tests green. Rollback: regex-level data changes per file, individually revertible.

## P-0086 hand-written parser fixes: go call-edge coverage, asm linker-suffix symbols, cuda kernel modifiers
kind: proposal
state: active
created: 2026-07-25
refs: ADR-0012, R-0005
grilled: 2026-07-25
targets: internal/index

Per ADR-0012 resolving R-0005. Three empirically confirmed hand-parser gaps. Go (nodes perfect, two edge-coverage holes): callEdges/typedCallEdges walk only *ast.FuncDecl bodies, skipping package-level var F = func(){} initializer bodies, and the callee switch handles only *ast.Ident/*ast.SelectorExpr, dropping explicit generic instantiation Foo[T]() (*ast.IndexExpr/*ast.IndexListExpr). Asm: TEXT/GLOBL patterns miss file-local <> suffixed symbols, <ABIInternal>-tagged symbols (pervasive since Go 1.17), and quoted method-shaped linker symbols. CUDA: static-qualified __global__ kernels (and sibling modifier-order forms) mint no node. Fix in the hand parsers with the R-0005 scratch fixtures as regression inputs. Two disjoint tasks: go edge coverage; asm+cuda symbol patterns. Exit: previously-missed forms produce nodes/edges over the fixtures, package tests green under -race. Rollback: bounded pattern/switch-case additions, revertible per file.

## T-0118 langspec engine: Spec.EndSpan keyword-counting body spans (cspan.KeywordSpan)
kind: task
state: done
created: 2026-07-25
parent: P-0084
refs: R-0005, ADR-0012
targets: internal/cspan/cspan.go, internal/cspan/cspan_test.go, internal/langspec/langspec.go, internal/langspec/langspec_test.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first.

GOAL: end-terminated languages currently get EndLine==Line and can never emit call edges (SpecParser.Parse bounds bodies only via cspan.Span brace counting; luaSpec's comment documents the limitation). Add the keyword-counting alternative.

DESIGN (fixed):
1. internal/cspan: add KeywordSpan(lines []string, start int, open, close *regexp.Regexp) (end int, ok bool) alongside Span — depth-counts open/close matches per line from the def line (the def line itself counts as one open when it matches open), returns the line index where depth returns to 0. Handle one-line bodies (open and close on the def line). Keep cspan a leaf package (stdlib + regexp only). Do NOT modify Span.
2. internal/langspec: Spec gains EndSpan *EndSpanSpec {Open, Close *regexp.Regexp}. In Parse, when def.Kind is KFunc/KMethod: if S.EndSpan != nil use cspan.KeywordSpan for the body span (and feed the existing callEdges loop, gated on CallRe != nil exactly as today); else the current cspan.Span path, byte-identical. EndSpan nil must change NOTHING (mirror the CallRe-nil guarantee and its TestSpecParserNoEdgesWithoutCallRe-style guard).

TESTS: cspan_test.go — KeywordSpan over lua-style (function/if/for/do..end) and ruby-style (def/class/module..end) snippets incl. nested blocks, one-line bodies, unterminated body (ok=false); langspec_test.go — a synthetic end-language Spec proves multi-line EndLine and ECall edges from the spanned body, plus the nil-EndSpan no-change guard.

Do NOT touch any per-language data file (a sibling task owns the five end-language files and starts after you merge). .spectackle files are server-owned.

VERIFY: go build ./... ; go test ./internal/cspan/... ./internal/langspec/... -race ; go test ./... ; go vet ./internal/cspan/... ./internal/langspec/... ; /home/user/spectackle/bin/spectackle lint.
ROLLBACK: additive function + optional field; revert restores brace-only. REPORT: the exact EndSpanSpec shape, test list with real output, anything deliberately not done.

## B-0007 parse-blob cache keyed by content hash only: parser upgrades silently serve stale nodes/edges until the cache is hand-cleared
kind: bug
state: active
created: 2026-07-25
refs: T-0125
targets: internal/store, internal/index

DEFECT
The persistent parse cache keys blobs by source-file content hash alone. When parser code changes (T-0125 live probe: goparser gained closure-var and generic-instantiation call edges), an unchanged file replays its pre-upgrade parse result — reindex printed the old edge set until .spectackle/cache/*.db was deleted by hand. Every parser fix in the ADR-0012 remediation waves has this property: repos indexed before the fix keep the stale graph indefinitely.

CAUSE
The cache key omits a parser-identity component; content immutability is assumed to imply result immutability, which only holds while the parser is frozen.

FIX (decision to make at implementation)
Fold a parser-version discriminator into the key or the cache generation stamp — e.g. a per-parser version string, or a build-binary identity component — so a parser change invalidates exactly its own language entries (or, cheapest, all entries). Needs a look at internal/store and the indexer gen-stamp rebuild logic before choosing.

VERIFY
Regression test: index a fixture, swap in a parser stub with different output, re-index, assert the new output wins without manual cache deletion.

ROLLBACK
Key/stamp change only; worst case is a one-time full re-parse per workspace.

## T-0127 parse and typed-call caches: mix a parser-identity component into both keys so upgrades self-invalidate
kind: task
state: approved
created: 2026-07-25
parent: B-0007
refs: B-0007, T-0125
grilled: 2026-07-25
targets: internal/index/indexer.go, internal/index/indexer_cache_test.go, internal/index/typespass.go, internal/index/typespass_test.go, internal/index/goparser.go, internal/index/asmparser.go, internal/index/cudaparser.go, internal/langspec/langspec.go, internal/langspec/langspec_test.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first; the design below is DECIDED, do not redesign it.

VERIFIED CURRENT STATE (orchestrator read this code; do not re-explore it)
  internal/index/indexer.go parseCached (sole caller indexer.go:154): hash := sha256.Sum256(src); s.Get(rel, hash) on hit gob-decodes a ParseResult; on miss it parses and s.Put(store.Entry{Path: rel, Hash: hash, Blob: gob}).
  internal/index/typespass.go ResolveTypedCalls: a SECOND instance of the same defect. Its whole-module edge cache lives under the sentinel path typedCallsCacheKey with key = moduleHashKey(root), module source content only, so a change to the typed pass itself (exactly what T-0125 shipped) is served stale.
  internal/store/store.go: Entry.Hash is [32]byte documented as sha256 of file contents; Get selects the one row per path and misses on hash mismatch, so a changed key overwrites rather than growing the store.
  index.LanguageParser is Lang() / Extensions() / Parse(path, src).
  langspec.Def{Kind, Re *regexp.Regexp, Name, Sig int}; langspec.Spec{Lang, Exts, Qual, Defs, CallRe, Stop, EndSpan}.
  Both cache paths already fall through to a recompute on a corrupt or shape-incompatible gob blob; leave that as is.
  internal/index/indexer_cache_test.go already has a counting fake parser and direct parseCached tests; extend that harness rather than building a new one.

PROBLEM
Both keys are content-only, so a parser or pass upgrade keeps serving pre-upgrade nodes and edges for unchanged sources. Confirmed live during T-0125 and every ADR-0012 remediation task: fixtures needed .spectackle/cache deleted by hand before a fix was observable. In a released binary this poisons every upgrade, users silently keep the old graph forever, and the typed Go call edges, the graph's highest-value edges, are affected too.

DESIGN
1. internal/index: optional interface CacheVersioner { CacheVersion() string }. Add a helper computing the parse-blob key: sha256 over the source bytes, then a domain separator, then the parser's version string when it implements CacheVersioner. A parser NOT implementing it must yield a key byte-identical to today's sha256.Sum256(src), a required test that keeps future and third-party parsers working unchanged.
2. internal/langspec: SpecParser implements CacheVersion by digesting its own Spec: every Def in order (regex source, Kind, Name and Sig indices), CallRe source, the Stop list, EndSpan Open and Close sources when set, Qual and Exts. Deterministic and order-stable: iterate slices, never a map. Any regex change then invalidates exactly that language.
3. Hand-written parsers (GoParser, AsmParser, CudaParser): CacheVersion returns a small explicit constant each, with a comment stating the bump rule (bump when the parser's output for unchanged input changes). T-0125 and T-0126 both changed these parsers, so the rule is not hypothetical.
4. typespass.go: mix an explicit typed-pass version constant into the module cache key the same way, same bump-rule comment, keeping the sentinel path and the [32]byte key shape.

Do NOT fold in the running binary's identity or build revision anywhere: that invalidates everything on every rebuild, which in this repository's own make dev loop means a full re-parse per iteration and on a large target repo throws away the entire point of the cache. Do NOT touch internal/store: the key stays [32]byte and the store keeps its content-agnostic contract.

SCOPE (lease exactly the nine target files)
Siblings currently hold internal/langspec language DATA files (javascript, typescript, dart, python, perl, php, shell, r). You own langspec.go and langspec_test.go only, never a language data file. On a reported lease conflict, stop and report.

TESTS
  index/parse: a fake parser reporting version v1 populates the cache; the same content parsed by a fake parser reporting v2 must re-parse and the NEW result must win with no cache deletion, the B-0007 regression proof. A parser without CacheVersion must produce the pre-fix key exactly (assert against sha256.Sum256 of the same bytes). A hit must still be a hit when version and content are both unchanged, so the optimization is not silently disabled.
  index/typed: changing the typed-pass version constant must miss the module cache and recompute. The existing typespass tests stub loadPackages, so assert the stub is invoked again after a version change and not invoked when nothing changed.
  langspec: two Specs differing in exactly one Def regex yield different CacheVersion; identical Specs yield identical CacheVersion; repeated calls on one Spec are stable.

VERIFY (run every one, real output, never predicted)
  go build ./...
  go test ./internal/index/... ./internal/langspec/... ./internal/store/... -race
  go test ./...
  go vet ./internal/index/... ./internal/langspec/...
  /home/user/spectackle/bin/spectackle lint
Then the live proof that closes B-0007, in a scratch workspace, WITHOUT ever deleting a cache directory: index a small fixture with your built binary, then edit one langspec regex so a previously missed construct now matches, rebuild, reindex the same fixture, and show the new node appearing. Paste the transcript. Revert that demonstration edit before submitting, or make it in a throwaway copy; the committed diff must contain only the cache work.

EXIT CRITERION
All listed tests green under -race, the no-CacheVersioner key proven byte-identical to today's, both cache paths covered, the whole suite green, vet and lint clean, and the live transcript showing a parser change taking effect with a warm cache.

ROLLBACK
One key helper, one optional interface, one digest method and four constants. Reverting restores content-only keying; no store schema, record format or blob shape changes, and a stale cache from either side simply misses and recomputes.
