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

## P-0041 M6 gpupipe resolver, Metal slice: newFunctionWithName string binding -> ELaunch edges
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24

The GpuPipeResolver stub documents its own detection contract 'implemented in M6'; with objcSpec and metalSpec now minting objc:/msl: nodes (P-0039), the Metal half is implementable: scan LangObjC files, track the enclosing host symbol (ObjC method or C function) via the same brace-depth walk cuda.go uses, and for every string literal argument of newFunctionWithName:@"name" inside a host body emit ELaunch from the host node (objc:<filestem>.<name>, matching objcSpec QualFileStem qualification) to msl:<name> (QualFlat), deduped per (host, kernel, file) like cuda.go launchKey. Vulkan half stays future (no SPIR-V/GLSL entry-point parsing yet); the stub's confidence/Rank-penalty note is out of scope — RSV-001 forbids resolvers mutating nodes, edges only. Exit criterion (explicit): gpupipe_test.go mirrors cuda_test.go: a fixture .m file with one method-hosted and one C-function-hosted newFunctionWithName launch plus one launch outside any body (must NOT edge) and one duplicated launch (must dedupe) yields exactly the two expected ELaunch edges with correct Src qualification; resolver_test.go registry test keeps passing; full suite -race green; lint clean. Rollback: revert gpupipe.go to stub, delete test. Scope (disjoint): internal/resolve/gpupipe.go, internal/resolve/gpupipe_test.go.

## T-0067 gpupipe Metal slice: host tracking + launch regex + fixture test
kind: task
state: active
created: 2026-07-24
parent: P-0041

Contract: the RSV rule just minted (anchor resolves after implementation). Scope (disjoint, two files): internal/resolve/gpupipe.go (replace stub Resolve), internal/resolve/gpupipe_test.go (new). Reference files (read, do not modify): internal/resolve/cuda.go (brace-depth host tracking, launchKey dedupe, braceDelta — REUSE braceDelta, do not redefine), internal/resolve/cuda_test.go + internal/resolve/resolver_test.go (FileSet fixture conventions), internal/langspec/objc.go (the two host-def regexes to mirror). Implementation: keep type/Name/Langs unchanged; keep the doc comment but move the Vulkan bullet under a 'still future' note and drop the confidence/Rank-penalty sentence (RSV-001: edges only). Resolve: for each path in fs.ByLang(graph.LangObjC): scan lines; host begin when line matches objc method regexp ^\s*[-+]\s*\([^)]*\)\s*(\w+) OR C-function regexp ^\s*(?:static\s+)?(?:[A-Za-z_][\w\s*]*?)\s+(\w+)\s*\(([^;{]*)\)\s*\{ (name = group 1); track brace depth from that line with braceDelta; host ends at depth<=0. Inside a host body, launch regexp newFunctionWithName:\s*@"(\w+)" (FindAllStringSubmatchIndex, may hit multiple per line); per hit emit graph.Edge{Src: ids.Mint("objc", stem+"."+host), Dst: ids.Mint("msl", kernel), Kind: graph.ELaunch, File: path, Line: lineNo} where stem = filepath.Base(path) minus extension; dedupe with a launchKey-style struct per (host, kernel, path). Note: ObjC method lines may open the brace on the SAME line or the NEXT line — after a host-begin match with braceDelta(line)==0, do NOT close the host immediately (unlike cuda.go): treat depth 0 as 'body not yet opened' until the first '{' appears, then close at depth<=0. Test gpupipe_test.go: fixture .m source with (1) method -(void)runKernel calling newFunctionWithName:@"add_arrays" inside its body; (2) C function static void setupPipeline(...) { ... newFunctionWithName:@"scale_rows" ... }; (3) a top-level line newFunctionWithName:@"orphan_kernel" outside any body -> must NOT edge; (4) the add_arrays call duplicated on a second line in the same method -> exactly one edge. Assert exactly two edges: objc:fixture.runKernel -launch-> msl:add_arrays and objc:fixture.setupPipeline -launch-> msl:scale_rows, Kind ELaunch, correct File. Use the FileSet test helper pattern from cuda_test.go/resolver_test.go. Verify: go vet ./internal/resolve/ && go test ./internal/resolve/ -race. Rollback: git checkout the two files.
