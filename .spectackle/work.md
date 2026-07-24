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

## P-0042 M6 ObjC message-send call edges: QualMode-aware CallRe callees + objcSpec.CallRe
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24

Last implementable M6 resolver piece. langspec callEdges mints Dst flat (ids.Mint(lang, callee)) — correct for QualFlat CallRe users (c, cpp), but for a QualFileStem language every callee edge would dangle forever since nodes are stem-qualified. Fix in two parts, one task: (1) framework: callEdges mints Dst via p.qualify(path, callee) — byte-identical behavior for QualFlat (qualify returns the bare name), and for QualFileStem it resolves same-file calls exactly while cross-file calls dangle (tolerated, same as Go's syntactic pass; a future resolver may bridge). (2) objcSpec gains CallRe matching the first selector segment of a message send \[\s*\w+\s+(\w+) with a Stop list for memory-management noise (alloc init release retain autorelease copy dealloc self super) — ObjC bodies are brace-delimited so cspan bounds them already. Exit criterion (explicit): extended objc_test.go proves [self helperMethod] inside a method body yields ECall objc:<stem>.<caller> -> objc:<stem>.helperMethod; Stop-listed sends and own-name recursion yield no edge; full langspec suite -race green proving c/cpp behavior unchanged (their CallRe tests must not change); lint clean; cover >= 70. Rollback: revert diff. Scope (disjoint): internal/langspec/langspec.go (callEdges + doc), internal/langspec/objc.go, internal/langspec/objc_test.go.

## T-0068 QualMode-aware callEdges + objcSpec.CallRe + message-send tests
kind: task
state: active
created: 2026-07-24
parent: P-0042

Contract: the LSP rule just minted plus existing LSP-001. Scope (disjoint, three files): internal/langspec/langspec.go, internal/langspec/objc.go, internal/langspec/objc_test.go. EDIT 1 — langspec.go callEdges: change `Dst: graph.NodeID(ids.Mint(string(p.S.Lang), callee))` to `Dst: graph.NodeID(ids.Mint(string(p.S.Lang), p.qualify(path, callee)))` and extend the callEdges doc comment sentence 'Destinations are minted in the same language and may be dangling' with ', qualified per the Spec QualMode (QualFlat: bare name, unchanged; QualFileStem: same-file resolution, cross-file dangles)'. EDIT 2 — objc.go: add to objcSpec: CallRe: regexp.MustCompile(`\[\s*\w+\s+(\w+)`) capturing the first selector segment of a message send, and Stop: []string{"alloc", "init", "release", "retain", "autorelease", "copy", "dealloc", "self", "super"}; replace the existing 'CallRe stays nil' doc paragraph with one explaining message-send capture, the Stop list, and that cross-file sends dangle until a future bridging resolver. EDIT 3 — objc_test.go: extend the positive fixture with a helper method the main method calls via [self helperMethod] and a Stop-listed [obj retain]; add TestObjcSpecMessageSendEdges asserting exactly the expected ECall edges: caller -> objc:<stem>.helperMethod present; no edge for retain; no self-recursion edge; assert via pr.Edges. IMPORTANT: adding CallRe changes EndLine semantics for objc KFunc/KMethod nodes (body span instead of def line) — existing TestObjcSpecNodes may assert Line==EndLine; adjust its EndLine expectations to the brace-counted span (see how c_test.go handles spans) and note this in the report. Verify: go vet ./internal/langspec/ && go test ./internal/langspec/ -race (FULL package suite — c/cpp CallRe tests must pass unchanged). Rollback: revert diff.
