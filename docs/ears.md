# EARS notation — the semantic pillar

Every rule sentence in a spec file must match exactly one EARS pattern
(Easy Approach to Requirements Syntax). Keywords are **uppercase and
case-sensitive** — `if (i < n)` inside a code fragment never triggers a
pattern (SPX-EARS-003). Vague prose is a lint **error**, not a warning.

## The six patterns

| P | Pattern | Template | Example |
|---|---|---|---|
| U | Ubiquitous | `The <system> SHALL <response>.` | The graph SHALL identify every node by an ID of the form `lang:name`. |
| E | Event-driven | `WHEN <trigger>, the <system> SHALL <response>.` | WHEN a kernel launch statement returns, the host wrapper SHALL check cudaGetLastError. |
| S | State-driven | `WHILE <state>, the <system> SHALL <response>.` | WHILE serving a tool call, the server SHALL keep cumulative file reads under 1 MiB. |
| N | Unwanted behaviour | `IF <condition>, THEN the <system> SHALL <response>.` | IF a rule sentence lacks the response keyword, THEN the linter SHALL report finding E001. |
| O | Optional feature | `WHERE <feature>, the <system> SHALL <response>.` | WHERE CUDA support is enabled, the indexer SHALL parse `.cu` files. |
| C | Complex | `WHERE …, WHILE …, WHEN/IF …, the <system> SHALL <response>.` | WHERE CUDA support is enabled, WHEN a launch is found, the resolver SHALL add a `launch` edge. |

Anti-example (rejected): *"The server should handle errors appropriately."*
→ E001 (no SHALL), E004 (`handle`, `appropriately`).

## Linter codes

| Code | Sev | Meaning |
|---|---|---|
| E001 | E | no `SHALL` response keyword |
| E002 | E | matches no EARS pattern (or rule has no sentence) |
| E003 | E | more than one `SHALL` — a rule has exactly one response clause |
| E004 | E | vague term (appropriate, properly, fast, efficient, robust, handle, as needed, etc., …) |
| E005 | E | rule heading is not a valid ID (`PREFIX-SEG-042` form) |
| E006 | E | duplicate rule ID across the cascade |
| W001 | W | response clause names nothing verifiable (no number, identifier, API, error code, or artifact) |
| W002 | W | sentence longer than 40 words — split it |

Grammar checks are regexes over the whitespace-normalized sentence
(`internal/ears/ears.go`); a Complex rule needs ≥ 2 condition keywords at
clause starts.

## Why EARS here

The synergy depends on it: because every contract is a single conditioned,
measurable clause, an LLM can translate it **deterministically** into each
language of the impact radius — `WHEN … SHALL check cudaGetLastError` becomes
an `if ((err = cudaGetLastError()) != cudaSuccess)` in the `.cu` wrapper and
an error-wrap branch on the Go side, with no interpretation latitude and no
rework loop.

## Authoring

End users never write EARS by hand. The `rule` MCP tool composes
sentences deterministically from structured slots (system, response, and the
pattern's condition clause), eliciting missing slots from the user through
the MCP client where supported — see docs/spec-cascade.md, "Authoring". The
linter below is the gate that every composed (and every hand-smuggled)
sentence must pass.

## Running the linter

```sh
spectackle lint .        # CI mode: exit 1 on E-severity findings
```

or via MCP: the `check` tool lints every spec bundle (returns `ok` or `!`
finding records), and the `rule` tool lints each sentence before writing.
`go test ./...` also fails if any committed spec bundle in this repo lints
dirty.
