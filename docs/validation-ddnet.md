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
