---
schema: v1
---

## T-01KYE3XEK2EE1VJQ9VKE5TZKJB manifest-aware judges: agent-prep optionally prepends the connect-time manifest to the brief, and the score reports session cost
kind: task
state: done
created: 2026-07-26

The manifest diet (T-01KYE3) re-priced connect-time guidance but nothing measures what that guidance BUYS: CLI judges have always run zero-doc, so whether the 3473-byte core reduces wandering — or pays nothing on flows the tool outputs already carry — is an open question, and the honest answer decides whether further core trims are safe. Change: bench -agent-prep DIR -with-manifest prepends the composed manifest to brief.md under a header naming it as the connect-time server instructions an MCP session receives, and writes its byte count to a manifest.size sidecar; ScoreAgentRun reads the sidecar when present into a ManifestBytes field, and AgentReport adds a session line — agent session=<total>B (manifest <m> + tools <t>) — so the session-shaped cost is visible next to the tool diet without corrupting the tool-bytes comparison the aggregate spreads run on. Prep without the flag stays byte-identical. Tests: with-manifest brief carries the manifest text and the sidecar matches its length; the plain prep produces neither; the score report carries the session line exactly when the sidecar exists. VERIFY: go test ./internal/bench/ ./cmd/spectackle/ -count=1 green; the experiment — three fresh manifest judges against the current build, aggregate-scored in one command, compared with the zero-doc trio at 12/14/14 calls and 1038/1119/1135 tool bytes — recorded with its conclusion in the archive note, whichever way it lands.
