# Validation — ddnet, the real thing

Not a fixture: [ddnet/ddnet](https://github.com/ddnet/ddnet) @
`0ab9158ff59d0d8f64f9dc72a3680aed7042dd65` (2026-07-23), shallow-cloned to a
scratchpad, indexed via the driver against the clone root (its scaffolded
`.spectacle` is a scratchpad artifact, not repo state). 779 files under
`src/` (335 `.cpp`, 335 `.h`, 24 `.c`, 18 `.rs`, rest build/vendor), ~339K
lines — measured via `find src -type f`, not the brief's "~2300" estimate.

## Measured numbers

The driver launches one fresh `spectacle serve -root <clone>` subprocess per
shell call (mcp_call.py never persists a server across calls), so every
timing below is a true process-boundary cold/warm pair.

| run | `.spectacle/cache/` | wall time (`time`, real) |
|---|---|---|
| cold (cache deleted first) | absent → rebuilt | 0.604s |
| warm (immediately after) | parse.db present | 0.329s |
| warm (again) | parse.db present | 0.325s |

`state` graph line, identical on every run: `ok graph nodes=10524 edges=506`.
Cold/warm ratio ≈ 1.85x — parse.db's content-hash cache (`internal/store`)
skips re-parsing all 779 files warm; the ~0.33s floor is process start +
graph rebuild from cached blobs + MCP handshake, not disk parsing. `state`
exposes no separate memory metric.

## The three probes (verbatim driver output)

**(a) `c:` node with genuine in-edges from another file** (see Limits below
for why no surviving C++ caller exists to probe instead):

```
> get {"id":"c:fill_window","depth":2}
n c:fill_window fn src/engine/external/zlib/deflate.c:251-368
...
e c:fill_window call c:zmemcpy via=src/engine/external/zlib/deflate.c:279
...
e c:read_buf call c:adler32 via=src/engine/external/zlib/deflate.c:228
e c:read_buf call c:crc32 via=src/engine/external/zlib/deflate.c:232
```

`c:zmemcpy` is defined in `zutil.c`, a different translation unit than the
`deflate.c` call site; `c:fill_window` has 20+ in-edges from
`deflate_fast`/`deflate_slow`/`deflate_rle`/`deflate_huff`. A real
multi-file call radius from regex + brace-counting alone, file:line on
every edge.

**(b) `n` record with a real file:start-end span** (310 lines;
`deflate.c:1255` is the function's actual closing brace, checked by hand):

```
> get {"id":"c:deflate","depth":2}
n c:deflate fn src/engine/external/zlib/deflate.c:946-1255
```

**(c) attempted `cpp:` seed reaching into `c:` — negative result,
root-caused:**

```
> find {"q":"GetItemName","scope":"code"}
n cpp:GetItemName fn src/game/client/gameclient.cpp:96

> get {"id":"cpp:GetItemName","depth":2}
n cpp:GetItemName fn src/game/client/gameclient.cpp:96
```

Zero edges at depth 2, despite line 96 being a real one-line call:
`const char *CGameClient::GetItemName(int Type) const { return
m_NetObjHandler.GetObjName(Type); }`. This is the **only** call site among
**all 57** same-line-brace C++ method bodies in the entire non-vendored
ddnet tree (re-derived exhaustively with the mirrored regexes from
`internal/langspec/cpp.go` / `internal/resolve/ffi.go`: 0 plain-function
hits, 57 method hits, 1 real call, 0 survivors) — and even it doesn't
survive `indexer.go`'s same-language filter (`present[e.Src] &&
present[e.Dst]`): `cpp:GetObjName` was never independently minted (a
`.`-member call; no `Foo::GetObjName(` def line exists anywhere in the
tree). No `cpp:`→`c:` edge exists anywhere in this graph.

## What the regex chain does NOT see (this codebase)

- **Allman brace style defeats the body scanner almost entirely.** Both the
  primary pass (`langspec.braceSpan`) and the FFI resolver
  (`resolve.ffiBraceSpan`) require `{` on the *same line* as the def match.
  ddnet puts the brace on the next line everywhere (0 same-line-brace plain
  defs across all 335 `.cpp`; 0 across non-vendored `.h`; only 57/4773
  out-of-line method defs, all one-liners). Defs still mint nodes; bodies
  just never get scanned (`EndLine == Line`, no outgoing edges).
- **A call edge is dropped if its destination was never independently
  minted**, even when the caller's body is scanned. Member calls
  (`obj.Method(...)`) mint a flat callee that only resolves if some *other*
  file defines `Foo::Method(` verbatim — virtual dispatch and most member
  calls never satisfy this.
- **Header prototype vs. `.cpp` definition split by extension, not
  semantics.** `str_copy`'s prototype in `str.h` mints `c:str_copy`; its
  body in `str.cpp` mints `cpp:str_copy` (which itself doesn't survive
  brace-scanning). Two IDs, one function — the FFI-bridging chain the task
  hoped to demonstrate needs a caller *and* callee that both survive
  brace-scanning on the C++ side, and that combination never occurs once
  across 335 `.cpp` files.
- **Virtual dispatch, templates, macros are invisible by construction** —
  `str_copy`'s callers mostly go through a `template<int N>` forwarder; the
  regex has no notion of overload resolution or instantiation.
- **506 edges / 10524 nodes (4.8%)** is not underindexing so much as an
  accurate reflection of what a single-line, brace-adjacent regex parser
  recovers from Allman-style C++: nearly every real edge traces to
  self-contained K&R-style vendored C libs (zlib, wavpack, md5,
  json-parser), not to ddnet's own code.

## Verdict

On real, unmodified ddnet the indexer builds a genuine, file:line-accurate,
multi-file call graph inside brace-adjacent C code (zlib's `deflate`/
`fill_window`/`zmemcpy` chain, probes a/b) in well under a second warm —
but it finds **zero** surviving `cpp:`→`c:` FFI-boundary edges in ddnet's
own ~335-file C++ tree, because ddnet's universal brace-on-next-line style
defeats the same-line brace-span heuristic both passes depend on, and the
one call site that does survive body-scanning is still pruned by the
dangling-destination filter — a precise, reproducible limit, not a vague
"C++ is hard."

## re-validation

T-0053 extended `internal/langspec/langspec.go`'s `braceSpan` (and only
`braceSpan` + its call site) to look past the def line for the opening
`{`: same-line (K&R, byte-identical to before), a bounded 3-line lookahead
past blank/`//`-comment lines (Allman — the opening brace on a following
line), and a multi-line-parameter-list header (scan forward to the line
closing `)`, then apply the same same-line-or-Allman search from there).
Prototypes/macros (a def line, or a multi-line header's closing line,
ending in `;`) still bail immediately with `ok=false`, untouched. Full
semantics and the six-case regression table are in `langspec_test.go`'s
`TestBraceSpanAllman`; `c_test.go`/`cpp_test.go` add one C-side and one
C++-side regression each (see below).

Same clone, same commit, re-indexed fresh (`.spectacle/` deleted, cold
run) via the driver against
`/tmp/claude-0/-home-user-spectacle/4c40537b-65eb-5824-86a6-6c853d4e1c78/scratchpad/ddnet`
(`0ab9158ff59d0d8f64f9dc72a3680aed7042dd65`, unchanged; `find src -type f`
still reports 779 files: 367 `.cpp`, 335 `.h`, 24 `.c`, 18 `.rs`, rest
build/vendor — the 367 vs. the original write-up's "335 `.cpp`" is this
run's own recount, not a clone difference).

### Measured numbers (post-fix)

| run | wall time (`time`, real) | nodes | edges |
|---|---|---|---|
| cold (`.spectacle` deleted first) | 1.059s | 10524 | 30627 |
| warm (immediately after) | 0.417s | 10524 | 30627 |
| warm (again) | 0.453s | 10524 | 30627 |

Nodes are unchanged (10524 → 10524): T-0053 is scoped to `braceSpan`
alone, not the Def regexes that decide *whether* a line mints a node.
Edges rose **60.5x** (506 → 30627), all from bodies that previously
collapsed to `EndLine == Line` with zero outgoing edges now being
depth-counted correctly. Cold time roughly doubled (0.604s → 1.059s) —
expected: `braceEnd` and `callEdges` now do real work (bounded lookahead +
brace-counting + `CallRe` scanning) over tens of thousands of newly-found
bodies instead of returning `ok=false` on the def line alone. Warm time is
within noise of the pre-fix run (0.417s/0.453s vs. 0.329s/0.325s) since
`.spectacle/cache/parse.db`'s content-hash cache still skips re-parsing
unchanged file bytes; the small warm delta is graph-rebuild-from-cache
work proportional to the now much larger edge set, not re-parsing.

### Probes (verbatim driver output)

**(a) the exact node the original write-up used as its negative example —
`cpp:GetItemName` — now has real depth-2 neighbors** (its own span is
still `96-96`, a one-line K&R method body, byte-identical to before; the
change is in its *callers*, whose Allman bodies are now scanned):

```
> find {"q":"GetItemName","scope":"code"}
n cpp:GetItemName fn src/game/client/gameclient.cpp:96

> get {"id":"cpp:GetItemName","depth":2}
n cpp:GetItemName fn src/game/client/gameclient.cpp:96
n cpp:RenderDebug fn src/engine/client/client.cpp:947-1072
...
e cpp:RenderDebug call cpp:GetItemName via=src/engine/client/client.cpp:1027
e cpp:RenderDebug call cpp:GetItemName via=src/engine/client/client.cpp:1060
e cpp:RenderServerbrowserTypesFilter call cpp:GetItemName via=src/game/client/components/menus_browser.cpp:1108
...
```

`cpp:RenderDebug` (`client.cpp:947-1072`, a 126-line Allman method body)
alone contributes 60+ outgoing call edges in this one `get` — all real,
file:line-accurate, same-language `cpp:`→`cpp:` edges that did not exist
before this fix (`RenderDebug`'s `EndLine` was `947 == Line` pre-fix, zero
edges).

**(b) the K&R zlib chain (probes a/b from the original write-up) is
unchanged, confirming no regression:**

```
> get {"id":"c:fill_window","depth":2}
n c:fill_window fn src/engine/external/zlib/deflate.c:251-368
...
e c:fill_window call c:zmemcpy via=src/engine/external/zlib/deflate.c:279
...
e c:read_buf call c:adler32 via=src/engine/external/zlib/deflate.c:228
e c:read_buf call c:crc32 via=src/engine/external/zlib/deflate.c:232

> get {"id":"c:deflate","depth":1}
n c:deflate fn src/engine/external/zlib/deflate.c:946-1255
...
```

Same spans, same edges as the pre-fix run (zlib is K&R throughout, so
`braceSpan`'s K&R branch — untouched semantics — governs it exactly as
before).

**(c) the `cpp:`→`c:` FFI crossing still does not materialize — root
cause confirmed, not the same one as before:**

The original write-up's own example (`cpp:GetItemName` → `GetObjName`)
isn't reachable anymore as a probe (its callee is a `.`-member call that
was never independently minted), so this run root-caused the crossing
exhaustively instead of by hand: a throwaway diagnostic (`go test`, run
and deleted before this task finished — not part of the committed diff)
ran the real indexing pipeline over the clone and walked every `cpp:`/`c:`
`KFunc` node's outgoing `ECall` edges looking for a same-name-space
crossing:

```
ZZZ nodes=10524 edges=30627
ZZZ sampled 9923 KFunc node ids via Find
ZZZ cross-language cpp<->c ECall edges found: 0
```

Zero, confirmed programmatically across the full graph (not just the one
hand-picked probe the original write-up used). Root cause: **T-0053 fixed
`internal/langspec/langspec.go`'s `braceSpan`, but
`internal/resolve/ffi.go`'s `FFIResolver` — the pass actually responsible
for emitting `cpp:`→`c:`/`c:`→`cpp:` bridging edges — carries its own,
independent, *unfixed* copy of the same K&R-only brace-span logic:**

```go
// internal/resolve/ffi.go
// ffiBraceSpan mirrors langspec.braceSpan: if lines[start] has no '{' at
// all, the def has no body (ok=false). Otherwise depth-counts forward...
func ffiBraceSpan(lines []string, start int) (end int, ok bool) {
	depth, opened := ffiBraceDelta(lines[start])
	if !opened {
		return 0, false
	}
	...
```

`ffi.go`'s own doc comment already says why it's a separate copy rather
than a shared call: "resolve is a lower layer than internal/index, which
internal/langspec imports, so resolve cannot depend on langspec." That
architectural boundary means T-0053's fix — scoped to `langspec.go` only,
per its brief, and `internal/resolve/` is explicitly out of scope for this
task — does not reach `ffiBraceSpan`. Concretely: `FFIResolver.Resolve`
mirrors `cppSpec`'s out-of-line-method Def (`ffiMethodRe`, no end-anchor,
so it does find e.g. `cpp:Reset`'s def line) and correctly requires the
def's node to already exist (`g.Node(srcID)` — it does, the primary pass
now mints and spans it correctly), but then calls its own `ffiBraceSpan`
to find the body to scan for calls — and for any Allman-style def
(effectively all of ddnet's own C++, per the original write-up's 57/4773
same-line-brace count), that still returns `ok=false` immediately, so the
resolver silently skips scanning the body and never sees the `str_copy`/
`mem_zero`/`dbg_msg`-shaped call sites inside it at all. The bottleneck
that blocked the crossing pre-fix (only 57 same-line-brace method defs
even have a body `ffiBraceSpan` can find) is numerically unchanged
post-fix, because the code path that would need the Allman extension was
never touched.

Two additional, independent blockers were confirmed while root-causing
this (neither is the primary one above, but both would still block the
crossing even if `ffiBraceSpan` were fixed):

- **`str_copy`/`mem_zero` have no `cpp:` definition to bridge to/from in
  the first place.** Both are header-only (`src/base/str.h`,
  `src/base/mem.h`), `.h`-only per `cSpec.Exts`, so any C++-side
  `str_copy(...)`/`mem_zero(...)` call site mints a dangling `cpp:str_copy`
  edge whose destination (`get {"id":"cpp:str_copy"}` → `nf - - -`, not
  found) was never independently minted by any pass — RSV-001 requires
  `FFIResolver` to bridge only to nodes that already exist in the sibling
  language, and `c:str_copy` exists but `cpp:str_copy` does not, so the
  *outgoing* direction (`cpp:` def calling `str_copy`) needs the sibling
  check to find `c:str_copy`, which it would (that part works) — but only
  once `ffiBraceSpan` can see into the Allman `cpp:` body in the first
  place, which is exactly the blocker above.
- **`dbg_msg` mints no node in either language at all**: its prototype
  (`src/base/dbg.h:121`) starts with a `[[gnu::format(...)]]` attribute,
  which is not `[A-Za-z_]` at column zero, so `cSpec`'s function Def
  (anchored `^(?:[A-Za-z_]...)`) never matches that line; its definition
  (`src/base/dbg.cpp:57`) is a plain (non-method) function in Allman
  style, and `cppSpec`'s plain-function Def carries the *same*
  `[;{]\s*$` trailing anchor as `cSpec`'s (see `c.go`/`cpp.go`, both
  untouched by this task) — a bare Allman signature line ending in `)`
  never satisfies that anchor regardless of `braceSpan`. `find
  {"q":"dbg_msg","scope":"code"}` → `ok no code matches`, confirmed.

None of the above is fixed here — `internal/resolve/`, `internal/index/`,
and the plain-function Def regex in `c.go`/`cpp.go` are all out of
T-0053's scope by its own brief. The concrete next slice, in priority
order: (1) extend `ffiBraceSpan` in `internal/resolve/ffi.go` with the
same Allman lookahead as this task's `braceSpan` (same shape, different
package, would immediately unblock the `cpp:Reset`-class crossings that
already have both a spanned body and a real sibling-language node to
bridge to); (2) drop the `[;{]\s*$` trailing anchor from `cSpec`'s and
`cppSpec`'s plain-function Def (mirroring how the method Def already has
no such anchor) so Allman-style plain functions mint nodes at all — this
is the `dbg_msg`/template-`str_copy` blocker and is a larger, riskier
change since the anchor is also what keeps indented control-flow/call
statements from false-positiving as defs (see `c.go`'s Def comment).

### Test tail (this task's suite)

```
$ go test -race ./internal/langspec/...
ok  	github.com/jxsl13/spectacle/internal/langspec	1.242s
$ go vet ./...
$ go build ./...
$ make lint-specs
CGO_ENABLED=0 go build -o bin/spectacle ./cmd/spectacle
./bin/spectacle lint .
14 spec files, 50 rules, 0 findings (0 errors)
```

### Verdict (post-T-0053)

`braceSpan`'s Allman extension does exactly what it was scoped to do: the
primary pass's same-language call graph inside ddnet's own ~367-file C++
tree went from structurally invisible (0 bodies scanned, 506 edges total,
nearly all from vendored K&R C) to a real, dense, file:line-accurate
`cpp:`→`cpp:` graph (30627 edges, 60.5x) in ~1s cold / ~0.4s warm — still
well under a second. It does **not**, by itself, produce a single new
`cpp:`↔`c:` FFI-boundary edge, because that bridging is `internal/resolve
.FFIResolver`'s job, and `FFIResolver` carries its own unfixed, K&R-only
brace-span copy (`ffiBraceSpan`) by architectural necessity (`resolve`
cannot import `langspec`). This is a precise, reproducible, and now
fully root-caused gap — not a vague "C++ is hard" — and the fix is a
same-shaped, small, separate follow-up task in `internal/resolve/ffi.go`,
explicitly out of scope here.

## ffi re-validation

T-0054 did three things: (1) extracted `braceSpan`/`braceSpanFrom`/
`braceDelta` out of `internal/langspec/langspec.go` verbatim into a new
leaf package, `internal/cspan` (`Span`/`SpanFrom`/`Delta`, zero
non-stdlib imports — no semantic change, byte-for-byte the same T-0053
logic, just relocated so a lower layer can reach it); (2) switched
`langspec.go`'s `SpecParser.Parse` to call `cspan.Span` instead of the
now-deleted local copy; (3) deleted `internal/resolve/ffi.go`'s
independent, unfixed, K&R-only `ffiBraceSpan`/`ffiBraceDelta` and switched
`ffiScanFile` to call `cspan.Span` too — the exact follow-up the previous
section's Verdict named. `internal/resolve/`'s mirrored Def/CallRe/Stop
regexes (`ffiPlainFuncRe`, `ffiMethodRe`, `ffiCallRe`, `ffiStop`) are
untouched, per the task's scope — only the span logic was the bug.

Same clone, same commit, unchanged since the last re-validation
(`0ab9158ff59d0d8f64f9dc72a3680aed7042dd65`; `find src -type f` still
reports 779 files: 367 `.cpp`, 335 `.h`, 24 `.c`, 18 `.rs`). Re-indexed
fresh (`.spectacle/` deleted, cold run) via the same driver against
`/tmp/claude-0/-home-user-spectacle/4c40537b-65eb-5824-86a6-6c853d4e1c78/scratchpad/ddnet`.

### Measured numbers (post-T-0054)

| run | wall time (`time`, real) | nodes | edges |
|---|---|---|---|
| cold (`.spectacle` deleted first) | 1.313s | 10524 | 35080 |
| warm (immediately after) | 0.708s | 10524 | 35080 |
| warm (again) | 0.665s | 10524 | 35080 |

Nodes are unchanged (10524 → 10524, same as T-0053): this task never
touches a Def regex, only body-span scanning. Edges rose from 30627
(post-T-0053, pre-fix `FFIResolver`) to **35080** — a **+4453** delta —
and, unlike the primary pass's 60.5x jump, this entire delta is new
`cpp:`↔`c:` bridging edges emitted by `FFIResolver`, since RSV-001
guarantees resolvers only add and nothing else in the pipeline changed.
Cold time crept up slightly (1.059s → 1.313s: `FFIResolver` now actually
scans thousands of previously-skipped Allman bodies instead of bailing on
the def line); warm times (0.708s/0.665s vs. 0.417s/0.453s) show the same
small, expected increase — graph-rebuild-from-cache work proportional to
the larger edge set, not re-parsing (`.spectacle/cache/parse.db`'s
content-hash cache still skips unchanged file bytes). Still well under
1.5s cold, well under a second warm.

### Programmatic cross-language edge count

A throwaway diagnostic (`cmd/zzzdiag/main.go` — built, run, and deleted
before this task finished; not part of the committed diff) instantiated
the exact same pipeline `internal/mcpserver.Server.reindex` uses
(`index.New` with `index.GoParser{}, index.AsmParser{}, index.CudaParser{}`
plus `langspec.All()`, resolved with `resolve.Default().All()`) directly
over the ddnet clone, then walked every `KFunc` node's outgoing `ECall`
edges (`g.Find("", 0, graph.KFunc)` + `g.Neighbors(id, Out, [ECall])`,
the same `Find("")`-samples-everything technique T-0053's re-validation
used) counting same-name-space crossings:

```
ZZZ nodes=10524 edges=35080 files=807 skipped=0
ZZZ sampled 9923 KFunc node ids via Find
ZZZ sample: cpp:Render~12 -call-> c:str_copy via=src/game/editor/envelope_editor.cpp:487
ZZZ sample: cpp:Render~12 -call-> c:str_copy via=src/game/editor/envelope_editor.cpp:570
ZZZ sample: cpp:Render~12 -call-> c:str_copy via=src/game/editor/envelope_editor.cpp:583
...
ZZZ sample: cpp:RenderPopupFullscreen -call-> c:time_get via=src/game/client/components/menus.cpp:1241
ZZZ cross-language cpp<->c ECall edges found: 4453 (cpp->c=4453, c->cpp=0)
ZZZ c:str_copy: 612 total in-edges, 612 from cpp:
ZZZ c:io_open: 3 total in-edges, 3 from cpp:
ZZZ c:net_init: 0 total in-edges, 0 from cpp:
ZZZ c:str_format_int: 0 total in-edges, 0 from cpp:
```

4453, matching the edge-count delta above exactly (35080 − 30627 =
4453) — confirmation that every new edge in this run is a `FFIResolver`
bridge, none of them going the other direction (`c:` → `cpp:`): ddnet's
own C-family headers/prototypes never call into C++ definitions, so
`cpp->c=4453, c->cpp=0` is the expected shape, not a bug.

### The three named probes (verbatim driver output)

**`c:str_copy`** — the task brief's own running example, and by far the
largest single bridge target: `str_copy`'s prototype lives in `str.h`
(mints `c:str_copy`, per `cSpec.Exts` claiming `.h`), its definition in
`str.cpp` (mints `cpp:str_copy`, which — being itself K&R/Allman-shaped —
doesn't matter for this direction). `get {"id":"c:str_copy","depth":1}`
returns 100+ real `cpp:` callers before the response's own pagination
cursor cuts it off (`cur b2ZmPTExOA`); the diagnostic's precise count is
612 total in-edges, all from `cpp:`, e.g.:

```
> get {"id":"c:str_copy","depth":1}
n c:str_copy fn src/base/str.h:45
n cpp:InitOpenGL fn src/engine/client/backend/opengl/backend_opengl.cpp:325-606
n cpp:RconAuth fn src/engine/client/client.cpp:288-311
n cpp:Connect fn src/engine/client/client.cpp:617-728
n cpp:ProcessServerPacket fn src/engine/client/client.cpp:1592-2378
...
```

**`c:io_open`** — a smaller, hand-checkable bridge: `io_open`'s prototype
lives in `io.h`, three real out-of-line-method callers survive
brace-scanning and bridge:

```
> get {"id":"c:io_open","depth":1}
n c:io_open fn src/base/io.h:83
n cpp:Dump~2 fn src/engine/shared/assertion_logger.cpp:54-76
n cpp:BeforeInit fn src/engine/shared/http.cpp:122-149
n cpp:OnCompletionInternal fn src/engine/shared/http.cpp:374-481
e cpp:Dump~2 call c:io_open via=src/engine/shared/assertion_logger.cpp:61
e cpp:BeforeInit call c:io_open via=src/engine/shared/http.cpp:141
e cpp:OnCompletionInternal call c:io_open via=src/engine/shared/http.cpp:443
```

**`c:net_init`** — a negative result, root-caused rather than left as a
mystery: `find {"q":"net_init","scope":"code"}` shows `c:net_init` (its
`net.h` prototype), but `get {"id":"c:net_init","depth":1}` returns zero
neighbors:

```
> find {"q":"net_init","scope":"code"}
n c:net_init fn src/base/net.h:162

> get {"id":"c:net_init","depth":1}
n c:net_init fn src/base/net.h:162
```

`net_init()`'s only non-test, non-tool call site in the tree is
`src/engine/shared/engine.cpp:74`, inside `CEngine`'s constructor — but
that constructor is defined **inline inside the class body**
(`CEngine(bool Test, ...) : m_pFutureLogger(...) { ... net_init(); ... }`),
not as an out-of-line `CEngine::CEngine(...)` definition. `ffi.go`'s
mirrored def shapes (`ffiMethodRe` for `Foo::bar(`, `ffiPlainFuncRe` for
a plain function) — deliberately unchanged by this task, mirroring
`cppSpec`'s own Def list — have no shape for an in-class inline method or
constructor body, so that call site is never scanned by `FFIResolver` at
all (independent of `cspan.Span`, which never gets a chance to run on it).
This is the same class of pre-existing, out-of-scope limitation as
`c:str_format`: `str_format`'s own prototype in `str.h` is prefixed with
a `[[gnu::format(printf, 3, 4)]]` attribute, so `cSpec`'s column-zero-
anchored Def never matches that line at all —
`find {"q":"str_format","scope":"code"}` mints only `c:str_format_int`
(the unattributed overload), never `c:str_format` itself:

```
> find {"q":"str_format","scope":"code"}
n c:str_format_int fn src/base/str.h:159
```

`c:str_format_int` does exist as a node but has zero in-edges (checked
programmatically above) — not further root-caused here, since neither of
these gaps is `cspan`/`ffi.go`'s span-scanning logic (this task's exit
criterion), they are the def-regex-anchor and in-class-method blockers the
previous section's Verdict already named as separate, larger, riskier
follow-ups.

### Test tail (this task's suite)

```
$ go test -race ./internal/cspan/ ./internal/langspec/ ./internal/resolve/
ok  	github.com/jxsl13/spectacle/internal/cspan	1.014s
ok  	github.com/jxsl13/spectacle/internal/langspec	1.105s
ok  	github.com/jxsl13/spectacle/internal/resolve	1.021s
$ go vet ./...
$ go build ./...
$ make lint-specs
CGO_ENABLED=0 go build -o bin/spectacle ./cmd/spectacle
./bin/spectacle lint .
14 spec files, 50 rules, 0 findings (0 errors)
```

### Verdict (post-T-0054)

The last blocker named by the previous section's Verdict is closed:
`internal/resolve/ffi.go` no longer carries its own K&R-only brace-span
copy, it calls the same `internal/cspan.Span` the primary `langspec` pass
uses, and `FFIResolver` now emits real, file:line-accurate `cpp:`→`c:`
bridging edges on real, unmodified ddnet — **4453 of them**, verified two
independent ways (the `state` edge-count delta and a from-scratch
programmatic walk of every cross-language `ECall` edge in the graph), not
one hand-picked probe. `c:str_copy` alone accounts for 612 of them — a
single C-family helper called from 612 real C++ call sites across the
tree, exactly the FFI-boundary chain the original write-up (T-0051) set
out to demonstrate and the T-0053 re-validation confirmed was blocked by
this exact resolver. The two residual gaps this task did not touch
(`net_init`'s only call site being an in-class inline constructor body,
and `str_format`'s attribute-prefixed prototype defeating the def-regex
anchor) are both `internal/resolve/`'s mirrored Def-shape limitations —
architecturally separate from `cspan`'s span-scanning contract, and
explicitly out of this task's scope, same as the previous section left
them.
