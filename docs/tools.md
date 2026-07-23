# MCP tool surface

The Go structs in `internal/mcpserver/tools.go` are the normative schema
source (SPX-REPO-001 keeps this file consistent with them).

## Design principles

- **Stable short IDs** (`go:saxpy.Saxpy`) are the currency: the LLM names
  code by ID, so calls and results stay tiny.
- **Flat params, enums, defaults** (SPX-ARC-004): the common call is
  `{"targets":["go:saxpy.Saxpy"]}` — everything else defaults.
- **Dense line records, not JSON, in results** (SPX-MCP-002): ~5× fewer
  tokens than pretty JSON for the same information.
- **Budgets and cursors everywhere** (SPX-ARC-002): results truncate at
  record boundaries; `cur` resumes.
- **Corrections instead of errors** (SPX-ARC-003): unknown ID → 3 nearest
  IDs as `nf` records, because an error round-trip costs more tokens.

## Output line grammar (shared by all tools)

```
n <id> <kind> <file>:<line> [sig=<sig>]      node   (kind: fn|method|type|var|kernel|asm|file|dir)
e <src> <ekind> <dst> [via=<file>:<line>]    edge   (ekind: def|call|incl|cgo|asm|launch|use|link)
r <ruleID> <P> <scopeDir> <text>             rule   (P: U|E|S|N|O|C; scopeDir "-" = global)
! <code> <sev> <file>:<line> <msg>           lint finding (sev: E|W)
g <kind> <ref> <msg>                         gap    (kind: uncovered|orphan|lint|none)
nf <id> <id> <id>                            not found — nearest matches
cur <token>                                  more results; pass back as "cur"
ok [<msg>]                                   success, nothing to report
```

## Tools

### 1. `sym` — resolve names → stable IDs (cheap entry point)

```json
{"type":"object","required":["q"],"properties":{
  "q":    {"type":"string","description":"name or ID fragment"},
  "k":    {"type":"integer","default":5},
  "kind": {"enum":["fn","type","kernel","asm","any"],"default":"any"}}}
```
→ `n` lines, best match first.

### 2. `map` — ranked cross-language repo map (Aider-style skeleton)

```json
{"type":"object","properties":{
  "path":  {"type":"string","description":"subtree, default root"},
  "focus": {"type":"array","items":{"type":"string"},"description":"IDs biasing the ranking"},
  "langs": {"type":"array","items":{"enum":["go","c","cpp","cu","asm","objc","msl"]}},
  "budget":{"type":"integer","default":2000},
  "cur":   {"type":"string"}}}
```
→ `n` lines in rank order; `cur` if truncated.

### 3. `impact` — cross-language impact radius, no file reads

```json
{"type":"object","required":["ids"],"properties":{
  "ids":   {"type":"array","items":{"type":"string"}},
  "depth": {"type":"integer","default":2},
  "dir":   {"enum":["out","in","both"],"default":"both"},
  "kinds": {"type":"array","items":{"enum":["call","incl","cgo","asm","launch","use"]}},
  "budget":{"type":"integer","default":1500},
  "cur":   {"type":"string"}}}
```
→ `n` + `e` lines in BFS order (each node once, at minimum distance —
SPX-GRA-002).

### 4. `contracts` — resolved cascading EARS rules for a scope

```json
{"type":"object","properties":{
  "ids":    {"type":"array","items":{"type":"string"}},
  "paths":  {"type":"array","items":{"type":"string"}},
  "inherit":{"type":"boolean","default":true},
  "budget": {"type":"integer","default":1500}}}
```
One of `ids`|`paths`. → deduped `r` lines in resolution order
(global → leaf).

### 5. `plan_change` — **the composite synergy tool** (call this first)

```json
{"type":"object","required":["targets"],"properties":{
  "targets":{"type":"array","items":{"type":"string"},"description":"node IDs or file[:line]"},
  "intent": {"type":"string","description":"one-line change description"},
  "depth":  {"type":"integer","default":2},
  "budget": {"type":"integer","default":3000}}}
```
→ three sections in one round trip:
```
#impact      n/e lines — the radius of the change
#contracts   r lines — every rule binding that radius
#gaps        g lines — radius nodes with zero rules, dirty spec files
```

### 6. `lint_ears` — validate rule text or a spec file

```json
{"type":"object","properties":{
  "text":{"type":"string"},
  "path":{"type":"string"}}}
```
Exactly one of `text`|`path`. → `!` lines or `ok`.

### 7. `coverage` — spec coverage report

```json
{"type":"object","properties":{
  "path":  {"type":"string","description":"subtree, default root"},
  "budget":{"type":"integer","default":1000}}}
```
→ `g uncovered …` (code without rules), `g orphan …` (rules whose scope
matches no code, M3), `g lint …`.

### 8. `link` — bind a rule to a node (traceability)

```json
{"type":"object","required":["rule","id"],"properties":{
  "rule":{"type":"string","description":"e.g. CUDA-KRN-001"},
  "id":  {"type":"string","description":"e.g. cu:saxpy_kernel"},
  "rm":  {"type":"boolean","default":false}}}
```
Writes `.spectacle/links.tsv`. → `ok CUDA-KRN-001 -> cu:saxpy_kernel`.

### 9. `add_rule` — create a contract from structured slots (the only write path)

End users and agents **never hand-write EARS sentences**. They fill slots;
the server composes the canonical sentence, lints it (errors reject the call
before anything is written — SPX-SPC-002), assigns the next free ID
(SPX-SPC-004) and appends to the target spec file, creating it with front
matter when needed.

```json
{"type":"object","properties":{
  "dir":      {"type":"string","description":"target directory, default repo root"},
  "global":   {"type":"boolean","default":false,"description":"write to .spectacle/global.ears.md"},
  "pattern":  {"enum":["U","E","S","N","O","C"]},
  "system":   {"type":"string","description":"the acting system"},
  "response": {"type":"string","description":"what it SHALL do; must name something verifiable"},
  "trigger":  {"type":"string","description":"WHEN clause (E/C)"},
  "state":    {"type":"string","description":"WHILE clause (S/C)"},
  "condition":{"type":"string","description":"IF clause (N/C)"},
  "feature":  {"type":"string","description":"WHERE clause (O/C)"},
  "stem":     {"type":"string","description":"ID stem e.g. CUDA-KRN; default: stem of last rule in target"},
  "rationale":{"type":"string"},
  "applies":  {"type":"array","items":{"type":"string"}}}}
```

**Guided flow**: missing slots are requested from the *end user* via MCP
**elicitation** (a flat form with one question per slot; the `pattern` field
is an enum). If the client has no elicitation support or the user declines,
the tool returns `need <slot> <question>` records for the agent to relay
(SPX-SPC-003). → on success:

```
ok CUDA-KRN-003 cuda_kernels/.spectacle.ears.md
r CUDA-KRN-003 E cuda_kernels WHEN stride parameters are supplied, the kernel SHALL …
```

on lint failure: `!` lines + `! REJECTED E - …` (nothing written).

### 10. `rm_rule` — remove a contract by ID

```json
{"type":"object","required":["rule"],"properties":{
  "rule":{"type":"string","description":"e.g. CUDA-KRN-003"}}}
```
→ `ok CUDA-KRN-003 removed from cuda_kernels/.spectacle.ears.md`.

### 11. `reindex` — refresh the graph

```json
{"type":"object","properties":{
  "paths":{"type":"array","items":{"type":"string"},"description":"default: full re-index"}}}
```
→ stats line.

## Token cost: dense records vs JSON

The same 5-node impact result:

```
n go:saxpy.Saxpy fn saxpy/saxpy.go:18
e go:saxpy.Saxpy cgo c:launch_saxpy via=saxpy/saxpy.go:24
n c:launch_saxpy fn saxpy/kernels/saxpy.cu:19
e c:launch_saxpy launch cu:saxpy_kernel via=saxpy/kernels/saxpy.cu:23
n cu:saxpy_kernel kernel saxpy/kernels/saxpy.cu:5
```
≈ 90 tokens. The equivalent pretty-printed JSON object graph (`{"nodes":
[{"id":…,"kind":…,"file":…,"line":…}],"edges":[…]}`) ≈ 450 tokens — a 5×
difference on every call, before any file-read savings.

## M0 status

`contracts`, `plan_change` (`#contracts`/`#gaps` halves), `lint_ears`,
`coverage`, `link`, `add_rule` and `rm_rule` are live. `sym`, `map`,
`impact`, `reindex` and `#impact` return `stub milestone=M1 see
docs/roadmap.md` until the indexer lands.
