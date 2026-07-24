---
schema: v0
prefix: SPX
---

# spectacle — living spec (root scope)

## intent
spectacle is a token-efficient, spec-driven MCP server: cross-language AST
maps, cascading EARS contracts and a git-native spec lifecycle, fused so an
LLM plans against structure and contracts instead of file contents.

## SPX-ARC-001
The spectacle server SHALL write only JSON-RPC 2.0 frames to stdout and route all log output to stderr.

Rationale: a single stray print corrupts the stdio MCP stream.

## SPX-ARC-002
WHEN a tool result exceeds the requested token budget, the server SHALL truncate the result at a record boundary and append a final `cur` record containing a resume cursor.

Rationale: token efficiency is the product; truncation must never split a record.

## SPX-ARC-003
IF a tool receives a symbol ID that is not present in the graph, THEN the server SHALL return the 3 closest matching IDs marked with an `nf` record instead of a JSON-RPC error.

Rationale: an error round-trip costs the caller more tokens than a correction.

## SPX-ARC-004
The input schema of every MCP tool SHALL declare a `default` value for each optional parameter.

Rationale: defaults let the LLM omit parameters, shrinking every call.

## SPX-ARC-005
The server SHALL confine every file write to `.spectacle` folders and keep the `.spectacle/cache` directory out of git via a server-written .gitignore file.

Rationale: the workspace belongs to the user; only the spec knowledge base is server territory, and caches must never be versioned.

## SPX-REPO-001
The repository SHALL keep every MCP tool schema in docs/tools.md consistent with the Go structs in internal/mcpserver/tools.go.

Rationale: docs are the normative schema mirror; drift breaks agent integrations.

## SPX-REPO-002
WHEN a new package is added under internal, the author SHALL add a scoped .spectacle/spec.md bundle or extend an ancestor spec bundle with rules for it.

Rationale: self-hosting requires every subsystem to be under contract.
