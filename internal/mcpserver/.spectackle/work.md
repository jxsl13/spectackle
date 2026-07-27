---
schema: v1
---

## B-01KYHC7APAEH1TA430A8MW5JW3 work op=abort by a foreign identity destroys a dead holders dirty worktree without refusal or force
kind: bug
state: draft
created: 2026-07-27
targets: internal/mcpserver/swarm.go

FOUND by cross-val-wipe2 round-2 audit of B-01KYH8JBB (H3): workAbort gates only LIVE holders; a rotated identity running the common abort-then-start reset on a DEAD-reading holders worktree removes the tree with uncommitted files, no dirty-guard, no holder named. abort is an explicit discard op, so this is not silent-at-start class, but it is the same loss one habit away and inconsistent with the op=start guard landed in B-01KYH8JBB. EXPECTED: foreign abort on a dead holders worktree with dirty non-.spectackle files refuses naming file count, holder, and work op=abort force=true; the holder itself may always abort its own tree (explicit intent); force discards. REUSE: wt.DirtyFiles + the exact filter/refusal shape from workStart (extract a shared helper if trivial). Fail closed on DirtyFiles error, mirroring workStart. TEST: wipeguard_test.go sibling using startWorkFixtureLive short TTL - foreign abort refuses and preserves; force aborts; holder self-abort still succeeds dirty. VERIFY: go build ./... && go test ./internal/mcpserver/ -race -run 'Abort|Orphan' -count=1 && gofmt -l . empty. SCOPE: workAbort only. ROLLBACK: revert.

## B-01KYHCHRN0FYK93YJEP6JA6NAD rule slot elicitation and commands harness elicitation pop native user forms for agent-authorable input
kind: bug
state: draft
created: 2026-07-27
targets: internal/mcpserver/tools.go, internal/mcpserver/commands.go

USER REPORT (2026-07-27, Claude CLI): with missing rule slots the MCP opens a native input form addressed to the HUMAN; the agent - the author of the rule - should be told to supply the slots instead. MECHANISM: elicitSlots (internal/mcpserver/tools.go:1260) calls req.Session.Elicit for missing rule op=add slots before falling back to need records (tools.go:1323); commands gen resolves its harness list the same way (native checkbox form). Elicitation in Claude CLI always targets the user - it cannot target the model - so both call sites mis-route authoring input to the human, per ELICIT-001 elicitation is reserved for decide op=ask user decisions. FIX: delete the Elicit leg from the rule path (elicitSlots and its call site) so missing slots ALWAYS return the existing need records naming each missing slot with its expected shape; commands gen with no arg and no detection leaves its adr item open (that leg already exists - remove only the elicitation attempt before it). decide op=ask stays untouched (its Elicit IS the feature, ASK-SURFACE-001). Update the rule and commands tool descriptions (elicited if missing / elicitation wording) to name the need-record path. TEST: rule op=add with missing trigger over a session WITHOUT elicitation capability already passes need-record tests - add the inverse pin: a session WITH elicitation capability still gets need records and no ElicitParams call is made (fake session recording Elicit calls, assert zero); same for commands gen. VERIFY: go build ./... && go test ./internal/mcpserver/ -count=1 && gofmt -l . empty. SCOPE: the two Elicit call sites outside decide + descriptions + tests. ROLLBACK: revert.
