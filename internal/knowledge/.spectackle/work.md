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

## T-0108 internal/knowledge: portable artifact format, extraction, recurrence-based merge
kind: task
state: active
created: 2026-07-24
parent: P-0076
targets: internal/knowledge/artifact.go, internal/knowledge/artifact_test.go, internal/knowledge/extract.go, internal/knowledge/extract_test.go, internal/knowledge/merge.go, internal/knowledge/merge_test.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first; do not explore beyond the files named here plus the read-only references it names.

GOAL
A standalone package that lifts the reusable part of one repository's records into a portable artifact and condenses several such artifacts into one. No tool wiring, no apply step — both follow in a later task, exactly as internal/mcpclient landed before its subcommand.

SCOPE (the package is NEW; lease exactly these six files)
  internal/knowledge/artifact.go + artifact_test.go   format: types, Marshal, Parse
  internal/knowledge/extract.go  + extract_test.go    one workspace -> one Artifact
  internal/knowledge/merge.go    + merge_test.go      N Artifacts -> one condensate
Read-only references you WILL need: internal/spec (Cascade, SpecFile, ResolvedRule), internal/ears (Rule, Section, FrontMatter, StripFrontMatter, ParseRules, ParseSections, Pattern), internal/item (Item and its ADR fields), internal/workspace (SchemaStamp). Do NOT modify any of them.
Do NOT touch internal/drift, internal/mcpserver, cmd/spectackle (a sibling task owns all three right now), README.md or docs/. Do NOT add a go.mod dependency. .spectackle files are server-owned: never edit them by hand.

THE DESIGN DECISION THAT SHAPES EVERYTHING — read before writing code
Extraction does NOT generalize prose. It never paraphrases, rewrites or templatizes an EARS sentence. Rewriting a repo-specific contract into a generic one is a natural-language judgment, and a tool that silently paraphrases contracts would corrupt the very corpus it exists to spread.
Genericity is MEASURED instead: merge treats recurrence across repositories as the signal. A convention five of six repositories independently assert is a standard; one appearing once is local color. Your merge output ranks by that count so a human curates a ranked list rather than auditing machine paraphrases.
If you find yourself writing a regex that rewrites sentence content, stop — that is out of scope by design, not an oversight.

WHAT TRAVELS / WHAT DOES NOT
Travels: rules (EARS sentences), ADRs (question, options, decision, consequences, status), intent prose sections.
Does not: proposals, tasks, bugs. They describe one repository's transient state, not knowledge, and would drown the condensate.
Stripped at extraction because it cannot mean anything elsewhere: applies anchors (node IDs), anchor rows, rule ID prefixes and numbers, lifecycle state, file paths inside anchors. Sentences keep their own words verbatim, including any repo-specific nouns — provenance lets a human judge portability.

FORMAT (artifact.go)
Markdown with YAML front matter, the same family as spec.md, schema-stamped with workspace.SchemaStamp — a condensate exists to be read and curated by a human before it is applied to N repositories, which rules out an opaque encoding, and the repo already parses this shape.
Suggested shape (adjust names if the existing code makes a better fit obvious, and say what you changed):
  front matter: schema, kind: knowledge, sources: [<module path or repo label>, ...]
  one section per entry, carrying: entry kind (rule|adr|intent), the payload, a stable content key, and provenance (which sources asserted it, and the count).
Content key: derive it from the NORMALIZED text so that trivial whitespace differences do not split an entry. internal/drift.NormHash already normalizes exactly this way (line-trailing whitespace trimmed, blank first/last lines dropped) — either use it or document why you did not.
Marshal and Parse must round-trip: Parse(Marshal(a)) deep-equals a. That is a required test, not a nicety, because the artifact is the interchange format between repositories.
Stable ordering everywhere: sort entries deterministically (by kind, then content key) before marshaling. Two runs over the same input must produce byte-identical output — a condensate that reshuffles on every run is unreviewable in a diff.

EXTRACTION (extract.go)
  func Extract(c *spec.Cascade, items []item.Item, source string) (Artifact, error)
source is the repository label recorded as provenance (the caller passes the module path). Walk the cascade for rules and intent sections; filter items to ADRs only. Record the originating context dir per entry as metadata — it is a portability hint for the human, not a merge key.

MERGE (merge.go)
  func Merge(as ...Artifact) (Artifact, []Conflict, error)
Group entries by content key; union their provenance; the count of DISTINCT sources is the recurrence rank. Sort the result by that count descending, then by content key for determinism.
Conflicts are never auto-resolved and never dropped. A Conflict is returned, not silently merged, when two entries share an identity but disagree in substance — at minimum: two ADRs with the same question but different decisions. Model Conflict as a small struct naming the identity, the disagreeing sources and their values. Two repositories disagreeing about how they should work is a real disagreement; erasing it would be the worst possible behavior for this feature.
Merge must be associative in effect and idempotent: Merge(a) equals a modulo provenance normalization, and Merge(a, a) must not double-count a as two sources. Both are required tests.

TESTS
  artifact_test.go: round-trip equality; byte-identical output across two Marshal calls; schema stamp rejected when it does not match workspace.SchemaStamp.
  extract_test.go: build a small in-memory or temp-dir cascade (internal/spec/cascade_test.go's buildTree shows the shape) plus a few items; assert rules, ADRs and intent are extracted, that proposals/tasks/bugs are NOT, and that applies anchors and rule ID numbering do not survive.
  merge_test.go: recurrence ranking across three sources; idempotence under Merge(a, a); same-question-different-decision ADRs surface as a Conflict rather than a silent pick; deterministic ordering across repeated runs.

VERIFY (run every one; report real output, never predicted)
  go build ./...
  go test ./internal/knowledge/... -race -v
  go test ./...
  go vet ./internal/knowledge/...
  /home/user/spectackle/bin/spectackle lint
  git diff go.mod go.sum      (must be empty)

EXIT CRITERION
All tests green under -race, round-trip and determinism proven by test rather than asserted in prose, conflicts surfaced rather than resolved, ./... green, vet clean, lint clean, go.mod untouched.

ROLLBACK
One new package imported by nothing until the later wiring task. Deleting the directory restores the prior state exactly — no schema, stored format, record, anchor or dependency change anywhere else.

REPORT BACK
The final API, the artifact format with one small real example, each test's real output, any place you deviated from the suggested shape and why, and anything you deliberately did NOT do. If part of this brief contradicts what the existing packages actually offer, STOP and report rather than improvising a different design.
