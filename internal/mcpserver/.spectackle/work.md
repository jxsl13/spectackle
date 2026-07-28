---
schema: v1
---

## B-01KYMCJG8HFYWBSFVG10KJP3NR knowledge path= resolves against the process cwd, not -root
kind: bug
state: done
created: 2026-07-28
refs: R-01KYMA7EXME6KAW9B77MJQ4MSD
targets: internal/mcpserver/knowledge.go, docs

Found by the R-01KYMA7EXME6K gap hunt (FAIL 3 + WARN 10). knowledge op=export path=kb-export.md wrote into the directory the PROCESS was started from - during the hunt that was the spectackle source repo itself, not the -root workspace; the hunter deleted it and verified a clean tree. Same for apply path= (reads from cwd). For a tool whose stated principle is that -root is the workspace and the LLM never names host paths, an undocumented cwd exception is a hazard, and a long-lived MCP server has a fixed cwd unrelated to the -root it serves. FIX: resolve a RELATIVE path against -root (absolute paths keep working verbatim); say so in the field description and docs. Fold in WARN 10 while here: an empty or whitespace-only artifact is diagnosed as schema mismatch (schema != v1) - detect and say empty artifact. TEST: export path=x.md with a cwd elsewhere lands under -root; absolute path unchanged; apply reads the root-relative file; empty artifact refuses naming emptiness. VERIFY: go build ./... && go test ./... -count=1.

## B-01KYMCKDD5FYBVW4Z3FYBTWG6E compact reports a cascade-archived child as a failure; docs omit validate and grill op=verdict
kind: bug
state: draft
created: 2026-07-28
refs: R-01KYMA7EXME6KAW9B77MJQ4MSD
targets: internal/mcpserver/tools.go, docs

Two render/doc honesty defects from the R-01KYMA7EXME6K gap hunt. (1) WARN 7: compact apply=true snapshots its done-item candidate list BEFORE archiving, so when archiving a task cascade-archives its linked ADR in the same call, the loop later tries to archive the already-archived child and renders c <dir> done-item <ADR> blocked: ! ARG E - lifecycle: unknown item <full-id>. Verified via get: the ADR was archived correctly a line earlier - no data loss, but the render says failure. FIX: re-check each candidate state immediately before archiving and skip the ones a cascade already closed (silently, or with a c ... already archived by parent line). (2) WARN 8: docs/tools.md documents 17 tools but NEVER mentions validate (registered, gates archive, source of the VALIDATE W/E lines the hunter had to chase into source to understand) and omits grill op=verdict entirely (its section shows only id/budget/cur, while pass/findings/waivers/lenses/panel/agent drive the review gate and the compaction keep-list). The file claims normativity via SPX-REPO-001. FIX: add the validate section and the grill verdict fields; re-check the tool-count sentence. TEST: extend the existing docs-vs-surface consistency test so a registered tool missing from the doc fails. VERIFY: go build ./... && go test ./internal/mcpserver/ -count=1.
