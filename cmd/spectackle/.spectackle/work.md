---
schema: v1
---

## B-01KYD4HBHQE9ZV9CJP3S6KQF0Z the call subcommand exits 0 on every tool refusal, so a scripted gate cannot detect one
kind: bug
state: active
created: 2026-07-25

DEFECT
README's headless-quickstart contract states that a refusal still prints its text to stdout but exits non-zero, and instructs callers to script against the exit code rather than the prose. No refusal does. Reproduced on a freshly built binary against a scratch workspace, with no pipe anywhere in the invocation (redirect to /dev/null, then read $?):
  call draft '{"kind":"epic",...}'      -> ! ARG E - lifecycle: unknown kind "epic"          exit 0
  call move  '{...,"to":"rejected"}'      -> ! ARG E - rejection requires a note              exit 0
  call get   '{"id":"<ambiguous prefix>"}' -> ! ARG E <p> ambiguous prefix - 2 records: ...    exit 0

CAUSE
mcpclient.Call decides the exit code from res.IsError, and main's call path propagates that faithfully. The server never sets it: every refusal is composed by mcpserver's text() helper, which builds a CallToolResult with Content only. So IsError is false for the entire dense-refusal grammar - ! ARG E, ! GATE E, ! ROUNDS E, ! WT E, ! GRILL E - and the client cannot distinguish a refusal from a success without parsing the prose the contract says not to parse.

NOT B-01KYCYCFERF4KVN04NTA2K32EC
B-01KYCYCFERF4KVN04NTA2K32EC claimed the lint subcommand exits 0 on an unreadable path and was rejected as a false alarm - the reporter had read the exit code through a pipe. Re-verified here with no pipe: lint on a nonexistent path exits 1, correctly. This is a different subcommand and a different cause, and the reproduction above deliberately avoids the trap that invalidated B-01KYCYCFERF4KVN04NTA2K32EC.

IMPACT
Silent, and it defeats the documented CI story: a Makefile target or agent script that gates on the exit code of a call treats every refusal as a pass. It also affects the swarm loop, where a lease conflict or a gate failure is supposed to be detectable mechanically.

FIX (decision at implementation)
Either set IsError on the results text() builds for the ! ... E grammar - which means separating the refusal constructor from the success one, since text() serves both - or have the call subcommand classify by the leading record tag. The former keeps the knowledge server-side, where the grammar is defined; the latter duplicates the grammar into the client. Prefer the former.

VERIFY
A CLI-level regression per refusal family: each prints its text on stdout unchanged AND exits non-zero, while a successful call still exits 0 - the existing byte-identical-stdout guarantee must not move.

ROLLBACK
The IsError flag on the refusal constructor; reverting restores exit 0 everywhere.
