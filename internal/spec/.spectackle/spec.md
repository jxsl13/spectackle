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
WHEN `rule` lacks required slots, the server SHALL return `need` records naming each missing slot plus the 1 assembled-call shape line to the calling agent; slots are agent-authored, never user-elicited (ELICIT-001).

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

## intent
- B-01KYD1G9SJFE8B1CGF6THPX6J4 rule op=edit drops the blank-line separator before the next rule, the loss accumulates, and lint does not notice: fixed under T-01KYD2: add and edit share one canonical block serializer that ends every rule block with exactly one blank line, so the two paths produce byte-identical files and no edit eats a separator
- B-01KYFSZ7A6FZV9QY6SK88AMJ9V rule composer accepts a trigger already starting with WHEN and emits WHEN WHEN: validated pass by cross-val-when diff b105a2d3dd9a :: [correctness] re-ran the composer table (own-keyword stripped case-insensitively per pattern, mid-sentence keywords survive) and the W003 pair from the diff. [completeness] both ends covered - normalization prevents new damage, lint surfaces old damage; the live probe rule EARS-001 exercised the exact reported repro and doubles as the ears packages first contract. [refute] tried: stripping could eat a trigger that legitimately BEGINS with the word when as prose (when-clauses of the requirement itself) - that is precisely the callers intent to compose a WHEN clause, so stripping is correct by construction. Zero waivers. — EARS composer damage is prevented and detectable: clause() strips the clauses own leading keyword case-insensitively (table-pinned incl. mid-sentence survival), LintSentence W003 flags doubled keywords in already-damaged files, and the live probe composed a single-WHEN sentence from a WHEN-leading trigger on this repo - kept as EARS-001, the ears packages first binding contract, itself recomposed once after tripping W001. Remaining queue: T-01KYDNN (pre-push hook), the node/edge principle rule (user directive 2026-07-27), v0.2.0 release mechanics.
