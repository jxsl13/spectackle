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

## P-0043 examples/metalcompute: live ObjC->Metal chain demo (saxpy analog)
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24

The new ObjC/Metal axis (P-0039/P-0041/P-0042) has no in-repo proof like saxpy's Go->C->CUDA chain. Add examples/metalcompute: Renderer.m (ObjC class, dispatch method message-sends [self buildPipeline], buildPipeline does newFunctionWithName:@"add_arrays"), shaders.metal (kernel void add_arrays + vertex/fragment pair), server-authored spec bundle (rule op=add dir=examples/metalcompute, stem MTC-API: one rule applies msl:add_arrays, one applies objc:Renderer.buildPipeline — the LLM never writes spec files), README.md with the real get transcript. Exit criterion (explicit): after reindex, get objc:Renderer.dispatch depth=2 shows the full chain — ECall dispatch->buildPipeline (message-send edge) and ELaunch buildPipeline->msl:add_arrays (gpupipe) — captured verbatim into the example README; ./bin/spectackle lint . clean including the new bundle; full suite -race green (self-spec lint test covers the bundle); check ok with the two new anchors. Rollback: delete examples/metalcompute + rule op=retire the two rules. Scope (disjoint): examples/metalcompute/** only.

## T-0069 metalcompute example: sources + server-authored bundle + live transcript
kind: task
state: active
created: 2026-07-24
parent: P-0043

Scope (disjoint): examples/metalcompute/** only — nothing outside. Files: (1) examples/metalcompute/Renderer.m — @interface Renderer : NSObject / @implementation Renderer with exactly two methods: - (void)buildPipeline { id fn = [library newFunctionWithName:@"add_arrays"]; (void)fn; } and - (void)dispatch { [self buildPipeline]; [encoder dispatchThreadgroups]; } (plain compilable-looking ObjC, no imports needed — this is an index fixture, not a buildable app; keep braces on the def lines). (2) examples/metalcompute/shaders.metal — kernel void add_arrays(device const float* a [[buffer(0)]], device float* out [[buffer(1)]], uint i [[thread_position_in_grid]]) { out[i] = a[i] + 1.0; } plus a vertex float4 passthrough(uint vid [[vertex_id]]) { return float4(0); }. (3) After creating both files run make build (binary may be stale) then ./bin/spectackle reindex, then author the spec bundle VIA THE SERVER (never write spec.md by hand): driver rule op=add dir=examples/metalcompute stem MTC-API pattern E system 'the compute pipeline' trigger 'add_arrays is dispatched' response 'process each thread index exactly once so out[i] depends only on a[i]' applies ["msl:add_arrays"]; second rule op=add same dir stem MTC-API pattern U system 'the Renderer' response 'bind the kernel by the literal name add_arrays through newFunctionWithName' applies ["objc:Renderer.buildPipeline"]. (4) examples/metalcompute/README.md — three sentences (what it demos: ObjC->Metal chain via message-send + string-binding launch edges) plus the VERBATIM output of driver get objc:Renderer.dispatch depth=2 in a code fence. Driver: python3 /tmp/claude-0/-home-user-spectackle/d0b8e016-f097-5792-857b-fd9ea4a8a781/scratchpad/mcp_orch.py /home/user/spectackle with heredoc lines like {"name": "get", "arguments": {"id": "objc:Renderer.dispatch", "depth": 2}}. Exit criterion: transcript shows n objc:Renderer.dispatch, e objc:Renderer.dispatch call objc:Renderer.buildPipeline, e objc:Renderer.buildPipeline launch msl:add_arrays, n msl:add_arrays kernel; ./bin/spectackle lint . clean; go test ./internal/spec/ -run TestSelfSpecs -race green. Verify commands as listed. Rollback: rule op=retire both MTC rules, delete examples/metalcompute.
