---
schema: v0
---

## P-0084 langspec engine: end-keyword body spans, so end-terminated languages get real spans and call edges
kind: proposal
state: active
created: 2026-07-25
refs: ADR-0012, R-0005
grilled: 2026-07-25
targets: internal/cspan/cspan.go, internal/langspec/langspec.go

Per ADR-0012 (engine-endspan) resolving R-0005. The langspec engine bounds bodies exclusively by brace counting (cspan.Span), so end-terminated languages (lua, ruby, elixir, julia, fortran) ship EndLine==Line for every symbol and can never set CallRe — impact BFS is blind there. Add a keyword-counting span: Spec gains an optional EndSpan config (open/close regexes); Parse uses it in place of cspan.Span when set, feeding the existing callEdges loop unchanged. cspan gains KeywordSpan alongside Span, same leaf-package discipline. Default nil = byte-identical current behavior (same guarantee CallRe made when introduced). Then per-language data updates switch the five end-terminated languages onto it. Rejected: indentation-based spans for python/haskell in this pass (different mechanism, separate proposal if the hardened regexes prove insufficient); tree-sitter adoption (ADR-0012 kept the pure-Go chain, wazero stays gated per ADR-0010). Scope disjoint: engine task owns internal/cspan + langspec.go; data task owns the five language files and runs only after the engine task merges. Exit: engine tests green including a no-behavior-change guard for nil EndSpan; the five languages report multi-line spans and call edges over the R-0005 scratch fixtures. Rollback: the field and KeywordSpan are additive; reverting restores brace-only behavior.

## P-0086 hand-written parser fixes: go call-edge coverage, asm linker-suffix symbols, cuda kernel modifiers
kind: proposal
state: active
created: 2026-07-25
refs: ADR-0012, R-0005
grilled: 2026-07-25
targets: internal/index

Per ADR-0012 resolving R-0005. Three empirically confirmed hand-parser gaps. Go (nodes perfect, two edge-coverage holes): callEdges/typedCallEdges walk only *ast.FuncDecl bodies, skipping package-level var F = func(){} initializer bodies, and the callee switch handles only *ast.Ident/*ast.SelectorExpr, dropping explicit generic instantiation Foo[T]() (*ast.IndexExpr/*ast.IndexListExpr). Asm: TEXT/GLOBL patterns miss file-local <> suffixed symbols, <ABIInternal>-tagged symbols (pervasive since Go 1.17), and quoted method-shaped linker symbols. CUDA: static-qualified __global__ kernels (and sibling modifier-order forms) mint no node. Fix in the hand parsers with the R-0005 scratch fixtures as regression inputs. Two disjoint tasks: go edge coverage; asm+cuda symbol patterns. Exit: previously-missed forms produce nodes/edges over the fixtures, package tests green under -race. Rollback: bounded pattern/switch-case additions, revertible per file.

## B-0009 search cache staleness: bundle freshness is decided by mtime and size alone, while the files table's sha column is never written or read
kind: bug
state: draft
created: 2026-07-25
refs: B-0007
targets: internal/sync/sync.go, internal/cache/cache.go

DEFECT
sync.Scanner decides whether a .spectackle bundle needs re-feeding by comparing os.Stat mtime (UnixNano) and size against cache.FileStat. A content change that preserves both is therefore invisible, and every FTS-backed surface keeps answering from the pre-change docs: find scope=rule|spec|proposal|task|history|rejection, the research pack, and grill's rejection lookup. The cache DDL already declares files(path, mtime, size, sha) but nothing ever writes or reads sha, which is evidence the content-hash check was designed and then not wired.

REPRODUCTION (scratch workspace, observed)
Mint a rule containing a marker word, search it (hit). Replace the marker with a same-length word directly in spec.md and restore the file's original mtime. Search again: the old marker is still returned, and the word actually on disk returns no matches. Note the reproduction restores mtime by hand to isolate the mechanism; in the field the same window opens by itself wherever mtime granularity is coarser than a nanosecond (HFS+, several network and container filesystems) or wherever tooling preserves timestamps across a write (rsync with times, tar with permissions, cp -p, image layers, CI cache restore).

CAUSE
Freshness is inferred from metadata that a same-size, timestamp-preserving write leaves unchanged, rather than from the content the cache actually indexes. Same defect class as B-0007, one layer up: there the cached artifact outlived the producer, here it outlives the input.

FIX (decision at implementation)
Write and compare the sha column the schema already carries. Keep mtime and size as the cheap first gate so the common path stays a stat, and hash only when that gate says unchanged, or hash unconditionally if measurement shows the read is affordable for bundle-sized files. Bump the cache gen stamp so existing caches rebuild once with the column populated.

VERIFY
Regression test: feed a bundle, rewrite it with equal length and a restored mtime, re-scan, and assert the new content is searchable and the old is not. Plus the existing sync tests unchanged, and a check that an untouched bundle still short-circuits without re-feeding.

ROLLBACK
One column write, one comparison and a gen bump; reverting restores metadata-only comparison and costs one cache rebuild.

## P-0087 class-level regression invariants: catch the next instance of each defect family, not just the nine already fixed
kind: proposal
state: active
created: 2026-07-25
refs: B-0003, B-0007, B-0008, B-0009
grilled: 2026-07-25
targets: internal/langspec, internal/mcpserver, internal/sync

Nine defects were found by dogfooding this repository with its own server (B-0001 through B-0009). Each fix carried a regression test pinned to its own call site, which proves that one line stays fixed and nothing more. The defects were not independent, though: they fall into a few classes, and the class-level invariant is what catches the NEXT instance, including in code nobody has written yet.

FOUR CLASSES WORTH GENERALIZING

1. Producer identity in cache keys and registry hygiene (B-0007, B-0008). A cached artifact outlives its producer, so every cache key must carry the producer's identity; and a registry of 30 languages means a new entry can silently omit that identity, declare a Def kind the engine's gate excludes, or point a capture group at a group its own regex does not have. These are registry-wide properties, checkable without writing a fixture per language, and they cover every language added later for free.

2. Context dirs versus item IDs on the journal boundary (B-0003). One call site passed an item ID where a context dir was expected and scaffolded a bogus directory at the repo root that then read as a context dir. The invariant is not about that call site: after exercising the whole mutating tool surface, no bundle directory may exist whose parent is not a legitimate context dir.

3. Worktree submit under conditions that are ordinary in the field but absent from the test bed (B-0002, B-0004, B-0005, B-0006). Four separate defects hid behind the same gap: the tests only ever exercised a repository whose primary branch was literally named main, whose main never advanced during a worktree's life, whose worktrees carried no live record state, and whose submit happened in the process that opened the worktree. Every one of those assumptions is false in real use, and one end-to-end test that violates all four at once would have caught all four defects before release.

4. Freshness must follow content, not metadata (B-0009). A cache that decides staleness from mtime and size cannot see a same-size timestamp-preserving write. The property is per bundle kind, not per file, so it generalizes across every doc kind the scanner feeds.

NOT GENERALIZED, DELIBERATELY
B-0001 (journal events predating the eid field) is a one-time historical shape, not a recurring class: its targeted tests are the right size, and an invariant asserting the absence of a format that can no longer be produced would only be ceremony.

TWO OPEN FIXES ARE IN SCOPE, BECAUSE THEY MUST BE
Classes 1 and 4 assert properties the code currently violates (B-0008's kind gate, B-0009's metadata-only freshness). A test that encodes a known defect as acceptable is worse than no test, and a test committed red is not acceptable either, so those two fixes ship with their invariants. Both fix directions were already decided in their own bug records.

Rejected: one generic table-driven test per bug, mechanically derived. It would restate the specific tests already committed at each site and add maintenance weight without adding a single new detection. Also rejected: fuzzing the tool surface for class 2. Property assertions over a deterministic tool surface are cheaper to read and cannot flake, and the failure mode here is structural, not input-dependent.

Scope is disjoint by package and file: registry invariants in internal/langspec and internal/index, tool-surface invariants and the worktree end-to-end in two separate new files under internal/mcpserver, freshness in internal/sync and internal/cache. Exit criterion: each invariant fails against the pre-fix behavior it encodes and passes after, the full suite is green under -race, and vet and lint stay clean. Rollback: new test files plus two bounded fixes, each revertible on its own.

## T-0128 registry invariants for parsers and Specs, plus the B-0008 kind-gate fix
kind: task
state: approved
created: 2026-07-25
parent: P-0087
refs: B-0007, B-0008
targets: internal/langspec/langspec.go, internal/langspec/registry_test.go, internal/index/parsers_test.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first.

CLASS: a registry of 30 languages means every new entry can silently omit something the engine needs. Assert the properties over the registry itself, so a language added next month is covered without anyone writing a fixture for it.

FIRST, THE FIX (B-0008, read it with get id=B-0008)
internal/langspec/langspec.go's Parse computes a body span and scans call edges only when def.Kind is KFunc or KMethod. A KKernel def (metal, glsl entry points) therefore never mints edges even with CallRe and EndSpan configured; T-0122 confirmed it live on the gap-metal fixture. Widen the gate to the kinds that actually carry bodies. Decide from the code which kinds qualify and say why in the commit; KKernel is required, anything else is your call with a stated reason.

THEN THE INVARIANTS (new files; do NOT add them to an existing per-language test file)
internal/index/parsers_test.go
  A. Every parser the server actually assembles — langspec.All() plus the hand-written GoParser, AsmParser, CudaParser — implements index.CacheVersioner. A parser without it silently reverts to content-only keying, which is exactly B-0007; this catches the next parser added without a version.
  B. No two of those parsers report the same CacheVersion. Equal versions across languages would let one language's cached blob satisfy another's key.
internal/langspec/registry_test.go, over every Spec in the registry:
  C. If a Spec sets CallRe, every Kind appearing in its Defs must be span/edge eligible under the gate you widened. This is B-0008 as a property: a language declaring a kind the gate drops gets no edges from those defs and nobody notices.
  D. Every Def's Name group index is >= 1 and <= its own regex's NumSubexp, and the same for Sig when non-zero. A capture index pointing past the regex's groups mints empty names or panics; the 27-language hardening rewrote most of these regexes by hand.
  E. Every Spec declares at least one extension and at least one Def, and its Lang tag is non-empty.
  F. Report, do not necessarily fail, on two Specs claiming the same extension: the first parser registered wins and the second is dead weight. If the current registry already has an intentional overlap, encode the intent (allow-list it with a comment) rather than bending the registry to the test.

Use table-driven subtests named by language so a failure names the offender directly.

VERIFY (run every one, real output, never predicted)
  go build ./...
  go test <your packages> -race
  go test ./...
  go vet <your packages>
  /home/user/spectackle/bin/spectackle lint
PROVE THE TEST BITES: for each invariant, temporarily reintroduce the defect it encodes (revert the guard, restore the old gate, whatever is minimal), show the test failing, restore, show it passing. A green invariant that would also be green against the broken code is worthless; paste both transcripts.

ROLLBACK: new test files plus the bounded fix named above; each revertible on its own.
REPORT BACK: each invariant with the class it generalizes, the failing-then-passing transcript, real verify output, and anything you deliberately did NOT do.

## T-0131 freshness follows content: fix B-0009 and assert it per bundle kind
kind: task
state: done
created: 2026-07-25
parent: P-0087
refs: B-0009
targets: internal/sync/sync.go, internal/sync/sync_test.go, internal/cache/cache.go, internal/cache/cache_test.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first.

FIRST THE FIX (B-0009, read it with get id=B-0009 — it carries the reproduction and the decided direction)
sync.Scanner decides whether a bundle needs re-feeding from os.Stat mtime and size alone, so a same-size timestamp-preserving write is invisible and every FTS-backed surface keeps answering from stale docs. The cache DDL already declares files(path, mtime, size, sha) and nothing writes or reads sha. Wire it: keep mtime and size as the cheap first gate so the common path stays a stat, and consult the content hash when that gate says unchanged. Bump the cache gen stamp so existing caches rebuild once with the column populated, and update the gen constant's own comment if its stated bump rule no longer covers the reason.

THEN THE INVARIANT (the class: freshness must follow content, not metadata)
For EVERY bundle kind the scanner feeds — spec, work, journal — assert the same property rather than testing one file: feed it, rewrite it with identical byte length while restoring the original mtime exactly, re-scan, and assert the new content is searchable and the old content is not. Drive it through the real Scanner and Cache, not a stub, because the defect lived in the interaction between them.
Assert the other direction too, in the same table: an untouched bundle must still short-circuit without re-feeding, so the fix cannot quietly turn every scan into a full rebuild. Measure that by observing that the feed function is not invoked, not by timing.

VERIFY (run every one, real output, never predicted)
  go build ./...
  go test <your packages> -race
  go test ./...
  go vet <your packages>
  /home/user/spectackle/bin/spectackle lint
PROVE THE TEST BITES: for each invariant, temporarily reintroduce the defect it encodes (revert the guard, restore the old gate, whatever is minimal), show the test failing, restore, show it passing. A green invariant that would also be green against the broken code is worthless; paste both transcripts.

ROLLBACK: new test files plus the bounded fix named above; each revertible on its own.
REPORT BACK: each invariant with the class it generalizes, the failing-then-passing transcript, real verify output, and anything you deliberately did NOT do.
