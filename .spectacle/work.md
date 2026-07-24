---
schema: v0
---

## P-0007 Plan 9 asm chain live: AsmParser nodes + go<->asm EAsm edges
kind: proposal
state: active
created: 2026-07-24
targets: internal/index/plan9asm.go, internal/resolve/plan9asm.go

Move ScanPlan9Asm into a dependency-free internal/plan9 package (resolve must not import index - cycle). New index.AsmParser (LanguageParser, .s/.S) minting asm:<dirpkg>.<name> nodes from TEXT/GLOBL. Implement resolve.Plan9AsmResolver: for each TEXT sym with an existing go:<pkg>.<name> node, emit EAsm edge go->asm at the TEXT site.

## P-0009 forward-skip state machine: every forward jump is one move call
kind: proposal
state: active
created: 2026-07-24
targets: internal/lifecycle/lifecycle.go, README.md

States stay (each carries meaning: submitted=review queue, approved=swarm backlog, done=await archive/compact) but no hop is ever mandatory: allowed(from,to) becomes a total-order comparison draft<submitted<approved<active<done<archived, any forward skip legal in ONE move call; rejected reachable from every non-terminal (note required); revocation from rejected back up to active; active->archived implies done; open-children guard on archive unchanged. Standard proposal drops from 5 moves to 2 (draft->active->archived). README gets the automaton as a Mermaid stateDiagram-v2; docs/lifecycle.md, docs/tools.md move section and the server instructions manifest updated to teach forward-skips.
