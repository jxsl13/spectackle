# Cookbook: adding a language

A language in spectackle is **data, not code** (SPX-LSP-001, P-0019): one
`langspec.Spec` value plus two one-line registrations. The indexing
pipeline (`internal/index`) is never touched. This recipe was proven by
adding Fortran (`internal/langspec/fortran.go`) — every step below names
the exact file the Fortran diff touched, and nothing else.

Total surface: **4 files** (2 new, 2 one-line edits) + tests.

## 1. Mint the language tag — `internal/graph/graph.go`

Add one `Lang` constant to the const block. The tag is the NodeID prefix
(`f90:solver.det`), so keep it short and unambiguous:

```go
LangF90 Lang = "f90"
```

## 2. Claim the extensions — `internal/index/langs.go`

Add the file extensions to `extLang` (the single source of truth for
language detection). Lowercase only — both `index.LangOf` and the
indexer's parser routing lowercase unknown-case extensions before lookup,
so `.F90` files reach the `.f90` entry without a second row (pinned by
`TestRSpecIndexAllUppercaseExtension`):

```go
".f90": graph.LangF90,
".f95": graph.LangF90,
```

## 3. Write the Spec — `internal/langspec/<lang>.go` (new file)

One `Spec` value + one `init()` line. The reference specs: `python.go`
(simplest), `r.go` (extension-case notes), `c.go` (CallRe + brace bodies),
`fortran.go` (case-insensitive keyword language).

```go
var fortranSpec = Spec{
    Lang: graph.LangF90,
    Exts: []string{".f90", ".f95", ".f03", ".f08"},
    Qual: QualFileStem,
    Defs: []Def{ /* regexes, see below */ },
}

func init() { registry = append(registry, fortranSpec) }
```

### Choosing `Qual` — how names become NodeIDs

| Mode | Qualifier | Pick when | Example |
|---|---|---|---|
| `QualFileStem` | file base name | one file ≈ one module (Python, R, Fortran) | `solver.f90` → `f90:solver.det` |
| `QualDirPkg` | directory base name | a directory is the namespace (Go asm layouts) | `mat/ops.s` → `asm:mat.mulVec` |
| `QualFlat` | none | globally-named entry points (CUDA kernels) | `cu:saxpy_kernel` |

### Writing Defs — the regex conventions

- One `Def` per symbol shape: `Kind` (usually `KFunc`; `KType` for
  module/class/struct headers), `Re` with `Name` = submatch group index of
  the symbol name, optional `Sig` group for a compact signature.
- **Case-insensitive languages get `(?i)`** on every regex (Fortran, SQL).
- **The end-keyword trap**: in keyword-delimited languages, `end function
  foo` matches a naive `function\s+(\w+)`. Go's regexp has **no
  lookahead**, so you cannot write "not preceded by end". Instead anchor
  the match at line start through an alternation of the *only legal prefix
  words* — attributes and type keywords — so a line starting with any
  other word (like `end`) can never reach the keyword literal:

  ```go
  // pure elemental real(kind=8) function det(m)  -> matches
  // end function det                             -> cannot match: "end"
  //                                                 is not in the prefix set
  `(?i)^\s*(?:(?:pure|impure|elemental|recursive|module)\s+)*` +
      `(?:(?:integer|real|logical|...)\s*(?:\([^)]*\))?\s+)?function\s+(\w+)`
  ```
- Pin the bare form with `\s*$` when a keyword has longer look-alike forms
  (`module linalg` yes, `module procedure solve` no).

### `CallRe` — usually nil

`CallRe` (call-edge capture, LSP-001) is bounded by **brace-counted body
spans**. Set it only for brace languages (see `c.go`, `cpp.go`). A
keyword-delimited language (Fortran, Ruby) has no brace body to bound the
scan, so leave `CallRe` nil — the framework then emits definition nodes
only, which is the intended degradation (`TestSpecParserNoEdgesWithoutCallRe`).

## 4. Tests — `internal/langspec/<lang>_test.go` (new file)

Mirror `fortran_test.go` / `r_test.go`, five tests:

1. `Lang()`/`Extensions()` round-trip.
2. Positive nodes: every Def shape, asserted by full NodeID (qualification
   is part of the contract), Kind, Line.
3. Negative lines: comments, call sites, and **every `end ...` form** mint
   nothing.
4. Registered in `All()` (catches a forgotten `init`).
5. Determinism: two `Parse` runs, `reflect.DeepEqual`.

Plus one end-to-end test through `index.New` + `IndexAll` over a real file
on disk — this is what proves steps 1–3 actually connected.

## 5. Dogfood gate

`internal/langspec` already carries its spec bundle (SPX-REPO-002), so no
new `.spectackle/` work is needed for a language added there. Finish with:

```sh
go test ./internal/langspec/ ./internal/index/ -race
make build && ./bin/spectackle lint .
```

If the language needs **cross-language edges** (a foreign-function
boundary like cgo/CUDA), that is a resolver, not a parser — see
`internal/resolve` (RSV-001: resolvers only connect nodes the parsers
minted, they never mint their own). Parser and resolver stay separate
layers; most languages never need a resolver.
