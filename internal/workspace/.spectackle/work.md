---
schema: v1
---

## T-01KYDNNVCRFSDVF6ZCXM1XAJB2 the server recommends and scaffolds a pre-push hook running the local gates, so human pushes are gated like automated ones
kind: task
state: draft
created: 2026-07-25
refs: ADR-01KYDGXWH4FX9VQTG0G2CF8GQQ
grilled: 2026-07-26 open=0

User suggestion during the CI-minimization work: git pre-hooks as an MCP-recommended gate.

WHY IT COMPLETES THE PICTURE: the automation now gates itself — done runs the workspace verify commands locally before any runner fires. A HUMAN pushing to a task branch bypasses all of it and burns a runner on code that may not even build. A pre-push hook running the same verify commands closes that hole with the same commands from the same config, so there is exactly one definition of the local gate.

SHAPE, recommendation over imposition: the server does not silently install hooks into a repository it does not own. Instead: state's health section (or check) emits one hint record when the workspace has verify commands configured but .git/hooks/pre-push is absent or does not reference them — naming the one-line hook it recommends, which shells to the spectackle binary so the hook inherits config changes without rewriting. An explicit opt-in (config key git.hooks true, or a small subcommand) lets the server write the hook itself; EnsureScaffold stays hands-off by default because a hook write is a behavior change on every future push, which is exactly the class of change ADR-01KYDG wants said, not slipped in.

TESTS: the hint fires only when verify commands exist and the hook is absent; the opt-in write produces a hook that runs the verify commands and fails the push when they fail (asserted against a real local repository); the default path never writes into .git.

VERIFY: go build ./... ; go test ./... -race ; go vet ; gofmt -l (empty) ; spectackle lint . (POSITIONAL).
SCOPE: internal/workspace or internal/wt for the hook write, internal/mcpserver for the hint, docs.
ROLLBACK: hint plus opt-in are additive; removing them changes nothing for anyone who never opted in.
