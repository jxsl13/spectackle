---
schema: v0
---

## P-0021 archived items stay referenceable: journal tombstones + decide option fidelity
kind: proposal
state: approved
created: 2026-07-24
grilled: 2026-07-24
targets: go:lifecycle.archive, go:item.Get, go:mcpserver.Server.decideAsk, go:mcpserver.Server.getItem, go:mcpserver.Server.openNeeds

Two lifecycle-integrity bugs found live (D-0002 flow):
(1) ARCHIVED REFS: lifecycle.archive removes the item from work.md, so item.Get no longer resolves it. Every reference site then rejects legitimate archived IDs: decide op=ask item=R-0004 -> '! ARG E - unknown item' although R-0004 is DONE work we must link decisions/provenance to. Archived is a rest position, not oblivion: the journal keeps an EvArchive event (compact fold retains archive events) carrying ID, K, Ti, Dir, Sum - enough to reconstruct a read-only tombstone. Fix: lifecycle.Tombstone(ws,id) resolving archived items from the journal; backpropagate to ALL reference sites: decide item= (record blocks: provenance, skip Needs backlink - nothing in work.md to write to), get <id> (render tombstone i-line instead of nf), draft parent= (allow archived parent as provenance), openNeeds/hasOpenNeeds (a need pointing at an archived item counts as satisfied, never open).
(2) OPTION FIDELITY: decideAsk stores 'options: a, b, c' comma-joined; decideOptions splits on comma -> options containing commas shatter and no byte-identical answer can ever match (hit live: D-0002 options contain '(tree-sitter-c + wazero, Parity-Oracle)'). Fix: store one 'option: <text>' line per option; decideOptions reads those first, keeps legacy 'options:' parsing for existing journals/items.

## T-0039 journal tombstones for archived refs + per-line decide options
kind: task
state: active
created: 2026-07-24
parent: P-0021
targets: go:lifecycle.archive, go:mcpserver.Server.decideAsk, go:mcpserver.Server.getItem, go:mcpserver.Server.openNeeds

SCOPE (disjoint from all langspec batches — touches only these files): internal/lifecycle/lifecycle.go, internal/lifecycle/lifecycle_test.go, internal/mcpserver/decide.go, internal/mcpserver/decide_test.go, internal/mcpserver/tools.go (getItem + openNeeds only), internal/mcpserver/prompts.go (hasOpenNeeds only), internal/mcpserver/prompts_test.go.

PART 1 — lifecycle.Tombstone (LCY-001): add to internal/lifecycle/lifecycle.go:
  func Tombstone(ws workspace.Root, id string) (item.Item, bool, error)
Scan journal.ReadAll(ws) (package internal/journal) for the LAST Event with Ev==journal.EvArchive && ID==id; reconstruct item.Item{ID: ev.ID, Kind: ev.K, Title: ev.Ti, Dir: ev.Dir, State: item.StateArchived, Body: ev.Sum}. Return ok=false if no archive event exists. Read-only: callers MUST NOT Upsert a tombstone (it has no work.md home). Note compact keeps EvArchive events (fold retention), so tombstones survive compaction.

PART 2 — backpropagate to reference sites:
(a) internal/mcpserver/decide.go decideAsk (~line 83): when item.Get misses, try lifecycle.Tombstone; if found, keep 'blocks: <id>' body line as provenance but SKIP the Needs backlink append+Upsert (hasBlocks stays false for the write path; introduce e.g. blocksID string for the body line). Output unchanged. Only if BOTH miss: '! ARG E - unknown item'.
(b) internal/mcpserver/tools.go getItem (line 282): on item.Get miss, try Tombstone; render the standard i-record via item.Record(it) plus a trailing ' (archived; journal tombstone)' note line instead of nf.
(c) internal/lifecycle/lifecycle.go Draft (line ~116): unknown parent currently errors — accept an archived parent (Tombstone hit) as provenance; keep erroring for truly unknown IDs.
(d) internal/mcpserver/tools.go openNeeds (line 845) and internal/mcpserver/prompts.go hasOpenNeeds (line 268), per LCY-002: a need whose ID resolves ONLY via Tombstone (archived) counts as satisfied (not open). A need that resolves nowhere stays OPEN (conservative: unknown != done).

PART 3 — decide option fidelity (MCP-001): in decide.go decideAsk, replace the single 'options: '+strings.Join(opts, ", ") body line with one 'option: <text>' line per option. In decideOptions, FIRST collect all 'option: ' lines (each line = one option verbatim); if none found, fall back to the legacy 'options: a, b, c' comma-split, then the outcome=a|b|c regex (both must keep working for existing items/journals — D-0002 in this repo is such a legacy item and must remain answerable via its comma-shattered fragments; do NOT migrate it).

TESTS (SPX-TST-001):
- lifecycle_test.go: archive an item, Tombstone returns it with State=archived+Kind+Title; unknown ID -> ok=false; Draft with archived parent succeeds, with unknown parent fails.
- decide_test.go: ask item=<archived id> succeeds, D body has 'blocks:', archived item untouched (no Needs write anywhere); ask with comma-containing options, then answer with the FULL byte-identical option string succeeds; legacy body with 'options: a, b' still answerable.
- prompts_test.go/tools tests: needs=[archived id] -> not open (next does not skip); needs=[nonexistent id] -> open.

ROLLBACK: purely additive fallback paths — reverting the commit restores old behavior; no data migration, no schema change (option: lines only appear on NEW decisions).

EXIT CRITERION: go build ./... && go vet ./... && go test -race ./internal/lifecycle/ ./internal/mcpserver/ && make lint-specs clean; then live: rebuild binary NOT required for the orchestrator (it verifies itself). After done: rule op=edit re-stamp anchors LCY-001/LCY-002/MCP-001 if check reports drift on edited funcs.

CONSTRAINTS: never commit/push (orchestrator does); do not touch registerTools, langspec, graph, index; keep the dense record grammar; comments follow existing file style (explain contracts, not change history).
