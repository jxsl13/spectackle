---
schema: v0
---

## D-0003 Rebranding spectacle→spectackle: wie tief? Der Go-Modulpfad (github.com/jxsl13/spectacle) muss zum GitHub-Repo-Namen passen — ein kompletter Modulpfad-Rebrand setzt voraus, dass du das Repo auf GitHub in spectackle umbenennst. Der .spectacle-Workspace-Ordner ist zudem das persistierte Format bestehender Nutzer-Repos.
kind: decision
state: done
created: 2026-07-24

kind: radio
option: full — Binary+Servername+Docs+Modulpfad+.spectackle-Ordner (mit .spectacle-Legacy-Fallback); du benennst das GitHub-Repo um
option: brand — Binary spectackle + MCP-Servername + Docs/README/goreleaser; Modulpfad und .spectacle-Ordner bleiben (nicht-brechend)
option: brand+dir — wie brand plus .spectackle-Ordner mit Legacy-Fallback; nur Modulpfad bleibt
choice: full — Binary+Servername+Docs+Modulpfad+.spectackle-Ordner (mit .spectacle-Legacy-Fallback); du benennst das GitHub-Repo um

## P-0028 full rebrand spectacle → spectackle (D-0003: full)
kind: proposal
state: approved
created: 2026-07-24
targets: go.mod, cmd/spectacle, internal/workspace/workspace.go

User decision D-0003 = full: binary, MCP server name, docs, Go module path AND workspace folder. Scope:
(1) Module path github.com/jxsl13/spectacle → github.com/jxsl13/spectackle: go.mod + every import in-tree + poc/wasmparse (own go.mod + replace ../..) + .goreleaser.yaml + CI workflows. USER ACTION AFTER MERGE: rename the GitHub repo to spectackle (redirects keep old clones working).
(2) Binary + dirs: cmd/spectacle → cmd/spectackle, Makefile bin/spectackle, goreleaser project/binary name, smoke targets.
(3) MCP identity: server Name/Title 'spectackle', instructions text, prompts (/mcp__spectackle__*), .claude/commands/spectacle*.md → spectackle*.md (content updated; old files deleted).
(4) Workspace folder: .spectackle preferred, .spectacle legacy fallback (Detect/EnsureScaffold/paths: if .spectacle exists and .spectackle does not, keep using .spectacle — zero migration forced; new scaffolds create .spectackle). This repo's own folders get git mv'd to .spectackle in the same change.
(5) Env var: SPECTACKLE_AGENT preferred, SPECTACLE_AGENT fallback.
(6) Docs/README/CONTRIBUTING: full text rebrand; rule IDs (SPX-*) and node IDs unchanged (data, not brand).
SEQUENCING: runs ALONE as the final wave — conflicts with every other open scope by construction.
Rollback: single revert commit restores everything; legacy fallbacks mean external users see no break either way.

## T-0050 full rebrand spectacle → spectackle — hard rename, NO fallbacks (pre-release)
kind: task
state: approved
created: 2026-07-24
parent: P-0028

AMENDMENT to P-0028 (user, 2026-07-24): NO legacy fallbacks anywhere — we are pre-release, there is nothing to fall back to. This supersedes the proposal body's fallback clauses.

SCOPE: the whole repo — this task runs ALONE, no sibling agents.

(1) Go module path: go.mod module github.com/jxsl13/spectackle; rewrite EVERY in-tree import (all packages, all tests), poc/wasmparse/go.mod (module path + replace directive + its imports). USER ACTION AFTER MERGE: rename the GitHub repo to spectackle.
(2) Binary/dirs: git mv cmd/spectacle cmd/spectackle; Makefile (bin/spectackle, all targets), .goreleaser.yaml (project_name, binary, archives), .github/workflows/*.yml (paths, binary names, smoke).
(3) MCP identity: internal/mcpserver server Name/Title 'spectackle', instructions text, prompt names; version var comment.
(4) Workspace folder: HARD rename — all code paths .spectacle → .spectackle (workspace.go ScaffoldDir/paths, sync, docs, .gitattributes template, EVERYTHING that mentions the folder name); git mv ALL .spectacle dirs in this repo (root + every internal/* + cmd + examples) to .spectackle. NO fallback code: Detect only knows .spectackle.
(5) Env var: SPECTACLE_AGENT → SPECTACKLE_AGENT, hard, no fallback (server.go + docs + .claude/commands + README).
(6) Text rebrand: README.md, CONTRIBUTING.md, docs/*.md, .claude/commands/spectacle.md→spectackle.md and spectacle-state.md→spectackle-state.md (names AND content; old files deleted), instructions manifest. Rule IDs (SPX-*) stay — they are data, not brand. binary references in scripts/tests (smoke, TestCheckOnOwnRepo paths if any).
(7) Self-consistency: after the rename the repo's own workspace must load — rebuild bin/spectackle and run the driver once against '.' to prove state/check work on the renamed .spectackle folders.

ROLLBACK: single revert commit.
EXIT CRITERION: go build ./... && go vet ./... && go test -race ./... green; make all green (Makefile renamed targets); grep -ri 'spectacle' --include='*.go' returns ZERO hits outside historical .spectackle journal/spec content (journals are history — do NOT rewrite past journal lines/rule texts recording the old name); bin/spectackle serve driver smoke on this repo renders #version 'spectackle'.
Constraints: never edit .spectackle/.spectacle journals' historical content; lifecycle writes only via the server; never commit/push (orchestrator does).
