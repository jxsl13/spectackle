---
schema: v0
---

## T-0117 surface item Refs: draft accepts them, get renders them, grill demands deliberation
kind: task
state: approved
created: 2026-07-25
parent: P-0078
targets: internal/mcpserver/tools.go, internal/mcpserver/tools_test.go, internal/mcpserver/grill.go, internal/mcpserver/grill_test.go, internal/lifecycle/lifecycle.go, internal/lifecycle/lifecycle_test.go, docs/tools.md

IMPLEMENTER IN OWN WORKTREE. Read this whole body first; do not explore beyond the files named here. Storage already exists and is archived work (T-0109): item.Refs round-trips through work.md, item.UnknownRefs(selfID, refs, known) validates a proposed set. This task is the wiring only.

GOAL
Make deliberation chains usable: a caller can cite items when drafting, readers see the citations, and grill asks for a recorded weighing on proposals that have none.

SCOPE (lease exactly these seven)
  internal/mcpserver/tools.go        draftIn field + draft handler validation + getItem rendering
  internal/mcpserver/tools_test.go
  internal/mcpserver/grill.go        the new question in grillQuestions
  internal/mcpserver/grill_test.go
  internal/lifecycle/lifecycle.go    Draft carries refs to the persisted item
  internal/lifecycle/lifecycle_test.go
  docs/tools.md                      draft entry: the new field
Do NOT touch internal/item (finished), internal/knowledge, internal/mcpserver/knowledge.go, commands.go or templates. .spectackle files are server-owned: never edit them by hand.

DRAFT INPUT
Add Refs []string to draftIn (jsonschema: item IDs this item cites — research/ADR/proposal, any kind) and thread it through lifecycle.Draft to the persisted item in the same single Upsert that Draft already performs — do not draft-then-patch with a second write. Validation happens in the handler before persisting, via item.UnknownRefs: the known set is every live item ID (item.LoadAll) plus archived IDs still answerable as journal tombstones (lifecycle.Tombstone answers per-ID; build the archived set the way the tombstone path reads the journal — read that code, do not re-derive the journal format). Unknown refs refuse with the ! ARG E prefix the handler already uses for lifecycle.Draft errors, naming the unknown IDs; a citation to an archived item is legitimate and must pass. Self-reference cannot happen at draft time (the ID does not exist yet) but UnknownRefs guards it anyway — keep that guard.

GET RENDERING
getItem (tools.go, the parent/targets/rules block): render refs on one line — refs <id> <id> ... — after rules, only when non-empty, matching the existing style exactly. The tombstone early-return path stays untouched.

GRILL QUESTION
grillQuestions (grill.go): for kind=proposal only, when the item records no weighing, append: q no deliberation recorded: no ADR/research ref and no rejected alternative. No weighing means: no ref whose ID starts with ADR- or R- (kind is encoded in the ID prefix — no lookup needed), and the body never mentions a rejected alternative (case-insensitive "rejected" or "rejected:" content, same cheap substring style as the existing three questions). grillQuestions currently reads only it — it already receives the full item, so Refs and Kind are at hand; keep it a pure function.

TESTS
  tools_test.go: draft with refs to a live item persists them and get renders the refs line; draft with an unknown ref refuses ! ARG naming the ID and persists nothing; draft with a ref to an archived (tombstoned) item succeeds; get on an item without refs renders no refs line (byte-stable output for existing fixtures).
  grill_test.go: proposal with no refs and no rejected-prose gets the new q line; proposal with an ADR- or R- ref does not; proposal whose body contains a rejected alternative does not; task-kind item never gets it.
  lifecycle_test.go: Draft persists refs in the single write (extend the existing Draft tests table style).

VERIFY (run every one; report real output, never predicted)
  go build ./...
  go test ./internal/lifecycle/... ./internal/mcpserver/... -race
  go test ./...
  go vet ./internal/lifecycle/... ./internal/mcpserver/...
  ./bin/spectackle lint
Then live: rebuild, drive draft-with-refs, get, and grill on a proposal without refs via the call subcommand against a scratch workspace; paste the real transcript.

EXIT CRITERION
All listed tests green under -race, ./... green, vet clean, lint clean, docs/tools.md consistent with draftIn (SPX-REPO-001), live transcript showing the refs line and the new grill question.

ROLLBACK
One input field, one render line, one question, one parameter threading. Reverting the seven files restores prior behavior; the storage layer (T-0109) is untouched and keeps working; no schema stamp, record format or anchor changes.

REPORT BACK
The final draftIn field and its schema text, the known-set construction you chose for archived IDs, each test's real output, the live transcript, and anything you deliberately did NOT do.
