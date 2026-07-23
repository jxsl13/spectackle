---
prefix: SPX-ARC
---

# spectacle — global architecture contracts

These rules bind the whole repository. They cascade into every directory and
can only be suppressed by an explicit `overrides:` entry in a deeper spec file.

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
