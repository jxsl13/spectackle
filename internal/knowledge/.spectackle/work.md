---
schema: v0
---

## P-0076 cross-repo knowledge: extract portable records, condense by recurrence, apply additively
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24
targets: internal/spec/cascade.go, internal/item/item.go

Goal: lift the reusable part of one repository's records into a portable artifact, merge several such artifacts into a condensate, and apply that condensate additively to every participating repository so a fleet converges on shared concepts, conventions, process models and library choices.

Central design decision, and the one that makes this buildable: extraction does NOT attempt to generalize prose. Rewriting an EARS sentence from repo-specific to generic is a natural-language judgment no mechanical pass can make reliably, and a tool that silently paraphrases contracts would corrupt the corpus it is meant to spread. Instead genericity is MEASURED, not asserted: extraction is a mechanical de-anchoring that keeps every sentence verbatim and attaches provenance, and merge treats RECURRENCE ACROSS REPOSITORIES as the genericity signal. A convention five of six repositories independently assert is a standard; one that appears once is local color. The condensate ranks by that count, so a human curates a ranked list instead of auditing paraphrases.

What travels: rules (EARS sentences), ADRs (question, options, decision, consequences, status) and intent prose. What does not: proposals, tasks and bugs. Those are work items describing one repository's transient state, not knowledge, and shipping them would drown the condensate in noise.

What is stripped at extraction, because it cannot mean anything in another repository: applies anchors (node IDs), anchor rows, rule ID prefixes and numbers, and lifecycle state. Consequence to state plainly rather than hide: an applied rule lands unanchored, so check reports it as a coverage gap until someone binds it to real nodes in the receiving repo. That is the correct behavior — the gap list is exactly the adoption worklist — but it means apply increases reported gaps on purpose.

Merge must never silently pick a winner. Two repositories asserting contradictory rules is a real disagreement about how they should work; auto-resolution would erase it. Contradictions surface as records for a human, and the natural home for the resolution is an ADR in the receiving repo.

Apply is additive and idempotent: it adds what is missing, never deletes, never overwrites a local specialization, and routes every write through the existing rule and decide paths so no new file-write path enters the codebase.

Format: markdown with front matter, the same family as spec.md, schema-stamped. A condensate exists to be read and curated by a human before it is pushed to N repositories, which rules out an opaque binary or a database; and the repo already owns a parser for exactly this shape.

Split: this proposal's first task delivers the portable format plus extraction plus merge as a standalone package with no tool wiring, mirroring how internal/mcpclient landed before its subcommand. Apply and the tool surface follow once that package is stable, so the tool count stays minimal and orthogonal — the intent is ONE tool with extract, merge and apply operations, not three tools.

## P-0079 knowledge extraction is LLM-driven end to end, including from repos with no records at all
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24
targets: internal/knowledge/artifact.go, internal/knowledge/merge.go

Corrects a design decision recorded in P-0076 and extends its reach.

What changes: P-0076 ranked merge output by cross-repo recurrence so that a HUMAN could curate the condensate. That role is removed. The LLM performs extraction, generalization, condensation and conflict resolution; the server supplies the mechanical half — parsing, normalization, dedup by content key, conflict detection, deterministic ordering, and the additive write path.

What does NOT change, and why: the original reasoning was that no mechanical pass can reliably generalize prose, so extraction must not paraphrase. That reasoning holds. The conclusion moves rather than dies — the generalization is done by the LLM, which can make that judgment, not by a regex. The server still never rewrites a sentence on its own. Recurrence ranking is likewise kept, demoted from a human-facing curation aid to a ranking signal the LLM consumes: a convention asserted by five of six repositories is still stronger evidence than one asserted once.

What this forces on the format: entries must be able to originate from the LLM, not only from a cascade walk. An LLM-authored generic entry has no source repository in the ordinary sense; it is a generalization over several, so provenance must be able to record derived-from alongside asserted-by. Conflicts likewise stop being a human's inbox and become an input the LLM resolves and records, which means a resolution has to be representable in the artifact rather than only returned in-band.

Second reach: extraction must work against a brownfield repository that has no .spectackle bundle at all and never practiced spec-driven development. There are no records to walk there, so the input is the code, the tests and the docs. The mechanism already exists in prose — the manifest's BROWNFIELD IMPORT paragraph describes fanning read-only subagents over disjoint subtrees and having the orchestrator mint contracts centrally. What is missing is that the resulting knowledge has no path into an artifact except by first minting a full record set in that repository. Direct authoring must be possible: the LLM surveys and emits entries, the server validates, normalizes and merges them exactly as it does extracted ones.

Also folded in, both found while harvesting T-0108 and deliberately left open there rather than hand-patched: ADR options must travel — they are the rejected alternatives, which this proposal's own record rules name as knowledge worth keeping, and they live as option: body lines rather than an Item field, so carrying them needs an exported parser instead of an invented field; and all whitelisted prose sections must travel, not only intent, which is harmless in this repository because it uses no others and wrong for any repository that does.

## T-0110 knowledge: LLM-authored entries, resolved conflicts, ADR options, all prose sections
kind: task
state: active
created: 2026-07-24
parent: P-0079
targets: internal/knowledge/artifact.go, internal/knowledge/artifact_test.go, internal/knowledge/extract.go, internal/knowledge/extract_test.go, internal/knowledge/merge.go, internal/knowledge/merge_test.go, internal/item/options.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first; do not explore beyond the files named here. The package already exists and is tested — read all six existing files before changing any of them.

GOAL
The human curator is removed from the knowledge pipeline. The LLM extracts, generalizes, condenses and resolves conflicts; this package supplies the mechanical half — parsing, normalization, dedup by content key, conflict detection, deterministic ordering. Four concrete changes follow from that, plus two gaps left open when the package first landed.

WHAT DOES NOT CHANGE, so you do not undo it: the package still never paraphrases a sentence on its own. The original reasoning stands — no mechanical pass generalizes prose reliably. What moved is WHO generalizes: the LLM, which can make that judgment, not a regex in this package. If you find yourself writing sentence-rewriting code, stop; that is still out of scope.
Recurrence ranking also stays. It is demoted from a human curation aid to a ranking signal the LLM consumes, but a convention asserted by five of six repositories is still stronger evidence than one asserted once. Do not remove Count or the ordering.

SCOPE (lease exactly these seven)
  internal/knowledge/*.go and their tests (six existing files)
  internal/item/options.go   NEW — the exported option parser, see change 3
Do NOT touch internal/mcpserver (a sibling task holds tools.go, server.go, state.go), internal/drift, internal/lifecycle, cmd/spectackle, internal/item/item.go (another sibling task is adding a field there — put your new code in a NEW file in that package, never in item.go), README.md or docs/. No new go.mod dependency. .spectackle files are server-owned: never edit them by hand.

CHANGE 1 — entries may originate from the LLM, not only from a cascade walk
An LLM-authored generic entry is a generalization over several repositories, so it has no single asserting source. Provenance must distinguish ASSERTED-BY (this repository literally contains this text) from DERIVED-FROM (this generalization was drawn from these sources). Extend Provenance or Entry accordingly, keep both representable in the marshaled form, and keep the round-trip test passing.
Add a constructor or validating entry point so an LLM-supplied entry gets the same treatment as an extracted one: content key computed by the package (never supplied by the caller — a caller-chosen key would break dedup), kind validated, required payload fields present for that kind. Reject rather than silently normalize a malformed entry.

CHANGE 2 — conflict resolutions are representable in the artifact
Today Merge returns []Conflict in-band and nothing can record how it was settled. Since the LLM now resolves conflicts, the resolution must survive into the artifact: which identity was in conflict, which value won, and which values lost. Losing values are not deleted — the rejected alternatives are exactly the knowledge this system exists to preserve.
Keep Merge's conflict detection and its return value unchanged for callers that just want to know. Add the representation plus a way to apply a resolution, and test that a resolved artifact round-trips with its losing values intact.

CHANGE 3 — ADR options must travel
Options are the rejected alternatives, which this project's own record rules name as knowledge worth keeping; an ADR without them loses half its value. They are NOT an item.Item field — they live as `option: <text>` body lines (and two legacy forms). internal/mcpserver/decide.go's decideOptions parses all three, but internal/knowledge must not import internal/mcpserver.
Create internal/item/options.go exporting that parser, handling the same three forms decideOptions does — read it first and match its behavior exactly, including the fallback order. Do NOT edit decide.go to use it in this task (mcpserver is held); leaving the duplication for one task is deliberate, and your report should name it so the follow-up removes it.
Carry the parsed options on ADR entries, and keep the ADR content key hashing the QUESTION only, so two repositories answering the same question differently still land in the same identity bucket and surface as a conflict.

CHANGE 4 — all whitelisted prose sections travel
Extraction currently takes only `## intent`. ears.IsProseSection whitelists more; take all of them. This repository uses only intent, so the change is invisible here and wrong to omit for any repository that uses the others.

TESTS — extend the existing test files, do not create parallel ones
  Provenance: an entry with derived-from sources round-trips; asserted-by and derived-from do not collapse into each other.
  LLM entry: a supplied entry gets a package-computed key identical to the key the same text would get through Extract — assert that equality directly, it is what makes LLM-authored and extracted entries merge with each other. Malformed entries rejected.
  Conflict resolution: resolve a same-question-different-decision conflict, marshal, parse, assert the losing values are still present.
  Options: an ADR item whose body carries option: lines yields those options on the entry; the legacy comma-joined form yields the same; the ADR key still depends only on the question.
  Prose: a section other than intent survives extraction.

VERIFY (run every one; report real output, never predicted)
  go build ./...
  go test ./internal/knowledge/... ./internal/item/... -race -v
  go test ./...
  go vet ./internal/knowledge/... ./internal/item/...
  /home/user/spectackle/bin/spectackle lint
  git diff go.mod go.sum      (must be empty)

EXIT CRITERION
All new and pre-existing tests green under -race, key equality between LLM-authored and extracted entries proven by test, resolutions round-tripping with losers intact, ./... green, vet clean, lint clean, go.mod untouched, and internal/item/item.go byte-unchanged.

ROLLBACK
Additive throughout: new fields written only when non-empty, one new file in internal/item, no change to Extract's or Merge's existing signatures beyond what change 2 adds. Reverting is a git checkout of the seven files. No schema stamp change and no stored-record migration — a knowledge artifact written before this change still parses.

REPORT BACK
The final API deltas, the artifact format with one real example showing derived-from provenance and one showing a resolved conflict, each test's real output, the duplication you left in decide.go for the follow-up, and anything you deliberately did NOT do. If any part of this brief contradicts what the existing package or internal/item actually offers, STOP and report rather than improvising.
