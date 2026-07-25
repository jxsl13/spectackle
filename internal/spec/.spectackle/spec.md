---
schema: v1
prefix: SPX-SPC
---

# Spec authoring and lifecycle contracts

Spec bundles are server-managed: contracts and lifecycle items are created
through the MCP tools, never hand-written by end users.

## SPX-SPC-001
The `rule` tool SHALL be the only write path for `spec.md` bundles, composing each sentence deterministically from structured slots.

## SPX-SPC-002
IF a composed sentence produces an error-severity finding, THEN the server SHALL reject the `rule` call and write 0 bytes to the spec bundle.

## SPX-SPC-003
WHEN `rule` lacks required slots, the server SHALL elicit the missing values from the user or return `need` records naming each missing slot.

Rationale: guided input keeps end users out of EARS syntax entirely.

## SPX-SPC-004
The server SHALL assign rule IDs automatically as `STEM-NNN` with the lowest unused 3-digit number across the whole cascade.

Rationale: hand-picked IDs are the main source of E006 duplicates.

## SPX-SPC-005
WHEN an item is moved to rejected, the server SHALL persist a full item snapshot in the `reject` journal event so the rejection stays searchable and revocable after every compaction.

Rationale: the rejection corpus is how the LLM learns from past failures; revocation needs the full item back.

## SPX-SPC-006
WHEN an item is archived, the server SHALL merge its outcome into the `intent` section of the scoped spec bundle and remove the item from work.md.

Rationale: archive is the OpenSpec delta-merge moment; work.md stays bounded.

## SPX-SPC-007 {applies: go:spec.Cascade.ForNode}
WHEN ForNode resolves a node ID with a known file, the cascade SHALL return the rules whose applies list names the node first, then the file's cascade rules, deduplicated by rule ID.