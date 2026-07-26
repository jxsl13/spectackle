---
schema: v1
---

## T-01KYEHVXBGEK1R0CJV0YJ657D3 the resident server restarts itself when its binary goes stale: the last manual step after every merge becomes mechanical
kind: task
state: active
created: 2026-07-26

Spun off from P-01KYDF as its only unlanded remainder — item four of its gap list: CONTRIBUTING mandates make dev after every merge and nobody automates it; the operator (or the orchestrating LLM) rebuilds and restarts by hand each landing, and a forgotten restart leaves a stale binary answering from code the tree moved past, which MCP-010s staleHint already detects and names but cannot act on. Change: serve gains an opt-in -self-restart flag (or config key): when the staleness detector that already powers the h binary-stale hint fires AND the workspace is the servers own source tree (the existing staleEligible gate), the server rebuilds the binary to a temp path, execs the new binary with identical arguments via syscall.Exec after gracefully draining in-flight requests (the pidfile machinery already brackets readiness), and refuses the swap loudly when the rebuild fails — never silently serving on. Constraints from the codebase: stdio mode must not restart mid-session (a JSON-RPC peer holds the pipe; hint-only there), only the HTTP resident mode; the pidfile is rewritten by the new process through the existing create-remove contract, which is atomic since B-01KYDR. VERIFY: e2e — start a resident server from a built binary, touch a source file, rebuild-trigger fires, the server comes back on the same port with the new version answering state, pidfile intact; stdio mode with the flag stays hint-only, pinned; a failing rebuild answers a loud record and keeps serving the old binary.
