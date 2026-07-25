---
schema: v1
---

## B-01KYD1G9RAEHWTK3SW3ZH3YFWS the stale-binary hint fires on released and packaged binaries, where its advice cannot be followed
kind: bug
state: draft
created: 2026-07-25
targets: internal/mcpserver/swarm.go

GitHub issue 29. This is a defect in the MCP-010 hint shipped by T-01KYB2318RFFGV6NA9WBWABMYB, found by field use of the released binary rather than by the development checkout it was built and tested in.

OBSERVED: every tool call prepends the hint naming make dev, including on a freshly installed release binary in a repository that contains no Makefile at all. The advice is unfollowable for anyone who installed spectackle rather than building it. It fires on every tool without exception.

WHY IT IS WORTH FIXING RATHER THAN TOLERATING, per the reporter: it sits on the token path of a server whose stated purpose is long-term token efficiency, costing a fixed tax on every result while carrying no information for the majority of users; and it trains callers to filter h lines wholesale, which is the same record class used for real signal such as commands op=detect reporting a detected harness. A noisy channel gets filtered, and the useful records go with it.

ROOT CAUSE IN OUR OWN TERMS: the check compares the executable's modification time against the newest source file under the workspace root. In a development checkout that is meaningful. For an installed binary the sources under the user's own repository are almost always newer, so the condition is permanently true and says nothing. The feature was verified only from the perspective it was written in.

FIX DIRECTION: fire only where the advice is actionable — a development checkout of spectackle itself, where a rebuild is both possible and relevant. The staleness check already stats the executable, so it has enough information to recognize that it is not running from a development build; consider also that the version stamp distinguishes a tagged release from a dev build.

VERIFY: an installed binary in an unrelated repository emits no hint on any tool; a development checkout with sources newer than the binary still emits exactly one per crossing; the existing debounce and once-per-crossing tests keep passing.

## P-01KYD47GZ7FAMAGM4NEF0BQS8T turn review from assertion into evidence: run the gate that exists, then make grill compute what it cannot fake
kind: proposal
state: draft
created: 2026-07-25
targets: internal/mcpserver/grill.go, internal/mcpserver/tools.go

R-0007 completed: six lenses, 63 mechanisms, 52 naming a real failure. The second pass verified its predecessors against the live server and the code, and it overturns the first synthesis on its top-ranked detector.

FINDINGS VERIFIED INDEPENDENTLY BY THE ORCHESTRATOR, not taken on report:
- The submit gate has executed ZERO commands in this repository's entire history. config.yaml carried no verify key and git log over all history shows no goal field ever added to any item, so runGate built its command list from two empty sources and returned success. All seven submit events passed a gate that ran nothing. Confirmed by reading config.yaml and by counting additions of goal across all branches.
- The mechanism the first synthesis ranked second — a monoculture scan over the target package's test files — would NOT have caught B-0004. The literal main lives in internal/wt/wt.go line 298, inside InitTestRepo, which is production code in a different package; before T-0130's retrofit it appeared in none of internal/mcpserver's test files. Naive literal frequency is noise: op appears 102 times, id 84, while main is not in the top 50. Four lenses converged on a mechanism that does not work as specified, and the first synthesis promoted it on the strength of that agreement. Recorded as the finding it is: convergence across lenses is not verification.
- grill is ceremony in practice, not only in principle. Twelve grill events exist; every one fired between zero and ninety-one seconds after its item's own create event, three within the same second, and no item body was ever revised in response to one. P-0088 was grilled three minutes and twenty-five seconds BEFORE its child briefs existed, so the stamp authorizing approval preceded the material it was supposed to critique. The briefs section is structurally near-dead: in at least seven of twelve grills no task item existed yet.
- check cannot report a contract gap in this repository. The root bundle is unscoped and carries sixteen rules, so ForPath never returns empty and the coverage-gap branch is unreachable. Twelve of twenty-four packages under internal carry no bundle at all while SPX-REPO-002 mandates one, and check answers ok. Six of the ten dogfooded defects landed in exactly those uncovered packages.

FIRST ACTION TAKEN, because it needed no new machinery: verify commands are now configured, so the gate runs go build and go test on both sides of every submit. Proven to bite before enabling, in a scratch workspace with a deliberately failing command: the submit refused with a GATE error rather than merging. Measured cost of the real commands is about fifteen seconds per run. The field, its parser, its executor and its documentation all already existed and had simply never been switched on.

RANKED REMAINDER, by verified failures per unit of cost:
2. Server-computed environment differential at grill: print the live value of a fixed axis list beside what the item's tests construct. Four of ten defects in one section, roughly thirty lines of code. Its own limit is honest — the axis list is hindsight-fitted, and its real test is whether axis six gets added before defect eleven.
3. grill stamps a verdict bound to what it read, and move gates on that verdict rather than on a non-empty date. This is P-0060's adjudicated principle applied to the reviewer: a refresh is not a verification.
4. Package-local contract coverage: make the coverage gap fire per package instead of inheriting the root scope. Twelve violations are visible today. Weakest of the top five because an EARS sentence silences it; mitigate by requiring the silencing rule to bind a node via applies.
5. Blast radius and irreversibility computed from targets. T-0135 declared four files and landed fifteen; T-0137 rewrote journal history and passed the rollback word-check with a well-formed paragraph.
6. Declared-but-unconsumed sweep. B-0009's title is literally the finding.
7. Caller-divergence sweep: print only minority argument shapes among a callee's call sites. B-0003 was one against twenty.
8. Server-executed mutant-kill gate at submit. Strongest evidence generator after the first, ranked here only for its measured eighty-second tax on the package where most work happens.
9. Independent-oracle recall for recognizers, as a ratchet rather than a threshold. Would have caught R-0005 wholesale; also the only mechanism with a maintenance tail, and the property that carries it — the corpus is not the implementer's — is the one thing no lint can enforce.

TO DELETE RATHER THAN ADD: grill's word-presence questions and the brief substring heuristics. They cannot fail for a determined author, they train bodies to grow padding, and they occupy the slot where a computed check belongs.

Scope for the follow-up tasks is disjoint by file: grill.go for the env differential and the verdict stamp, tools.go and research.go for coverage, a new sweep for callers. Rollback for each is the removal of one section or one predicate.

## T-01KYD8HC02EDSVB104TRJS23Y1 the stale-binary hint fires only for a development build serving its own tree
kind: task
state: active
created: 2026-07-25
parent: P-01KYD8DK52EG5A1AKC1KSRR4Z2
refs: B-01KYD1G9RAEHWTK3SW3ZH3YFWS
targets: internal/mcpserver/swarm.go

Closes GitHub issue 29 (see B-01KYD1G9R for the field report).

ROOT CAUSE IN OUR OWN TERMS: binaryStale compares the executable's modification time against the newest .go file under the workspace root. In a development checkout of spectackle that is meaningful. For an INSTALLED binary the sources under the user's own repository are almost always newer, so the condition is permanently true and the hint says nothing — while naming `make dev`, in repositories that frequently contain no Makefile at all. The feature was verified only from the perspective it was written in.

WHY IT IS WORTH FIXING RATHER THAN TOLERATING, both reasons from the reporter and both about the token path: it costs a fixed tax on every single tool result of a server whose stated purpose is long-term token efficiency, carrying no information for the majority of users; and it trains callers to filter h records wholesale, which is the same class used for real signal such as commands op=detect reporting a detected harness. A noisy channel gets filtered and the useful records go with it.

DECISION ALREADY TAKEN: fire only where the advice is actionable — a development build of spectackle serving spectackle's own tree. Rejected alternative: a config flag to silence it. A noisy default that every user must individually silence is the defect, not the fix.

VERIFIED GROUND, do not re-derive. internal/mcpserver/server.go line 37 has `var Version = "0.2.0-dev"`, ldflags-stamped for a release, so the running binary already knows whether it is a dev build. Line 54 has `const modulePath = "github.com/jxsl13/spectackle"` and moduleRepoURLFrom already reads the main module path from build info. The staleness walk itself (binaryStale, and staleHint above it with its 30s debounce and once-per-crossing staleHinted latch) is in internal/mcpserver/swarm.go and is otherwise fine — keep it.

WHAT TO BUILD: a precondition on the hint, evaluated before the walk so an installed binary does not even pay for the traversal. Both of these must hold, and say in your report how you tested each: the running binary is a development build (Version carries a dev marker rather than a stamped release version), AND the workspace being served is spectackle's own module (compare the workspace's go.mod module path against modulePath). Either one alone is insufficient — a dev build serving an unrelated repository would still give unfollowable advice, and a released binary serving a spectackle checkout cannot act on `make dev` either.

TESTS
  a released Version serving any tree: no hint, on any tool, ever — and assert the walk did not run.
  a dev Version serving an unrelated module: no hint.
  a dev Version serving spectackle's own tree with sources newer than the binary: exactly one hint per crossing, which is today's behavior and must be preserved.
  the existing debounce and once-per-crossing tests keep passing unchanged — if you have to edit them, you have probably changed behavior you should not have.

VERIFY: go build ./... ; go test ./internal/mcpserver/... -race ; go test ./... ; go vet ; gofmt -l (empty) ; spectackle lint . (POSITIONAL).

SCOPE: internal/mcpserver/swarm.go and its tests, plus a read-only reference to the Version and modulePath already in server.go. Do NOT touch internal/mcpserver/state.go or the check path (a sibling owns those), nor internal/workspace, internal/index, internal/ignore, nor any .spectackle file.
ROLLBACK: remove the precondition; the hint returns to firing as it does today.
REPORT BACK: how you detected each of the two conditions, how you tested the released-binary case without a release build, each test's real result, anything deliberately not done.
