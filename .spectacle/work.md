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
