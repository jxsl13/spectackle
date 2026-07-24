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

## P-0039 M6 slice: ObjC and Metal langspec parsers via the cookbook (parallel implementers)
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24

M6 lists ObjC/Metal/Vulkan support. extLang already claims .m/.mm (LangObjC) and .metal (LangMSL) but no parser exists for either — those files mint zero nodes today, so cross-language chains into ObjC/Metal are invisible. First slice: two langspec Specs per docs/cookbook-new-language.md (Lang constants and extLang entries already exist — cookbook steps 1-2 are no-ops, only step 3-4 apply). Two fully disjoint tasks (objc.go+objc_test.go vs metal.go+metal_test.go), one fresh implementer each, in parallel — dogfooding the tiered fan-out. Resolvers (objc bridging, gpupipe Vulkan) stay a later slice per RSV-001 layering. Exit criterion (explicit): both languages pass the cookbook five-test convention plus IndexAll E2E; full suite -race green; lint clean; a .m and a .metal file in a temp repo mint objc:/msl: nodes end-to-end. Rollback: revert additive files. Scope: internal/langspec/objc.go, objc_test.go, metal.go, metal_test.go (all new).

## T-0064 langspec objc: Spec + five tests + IndexAll E2E
kind: task
state: active
created: 2026-07-24
parent: P-0039

Scope (disjoint, exactly two NEW files): internal/langspec/objc.go, internal/langspec/objc_test.go. Recipe: docs/cookbook-new-language.md steps 3-4 only (graph.LangObjC and extLang .m/.mm entries already exist — do NOT touch graph.go or langs.go). Reference specs: internal/langspec/r.go (structure), internal/langspec/fortran.go (comment style, trap notes). Spec value objcSpec: Lang graph.LangObjC, Exts [".m", ".mm"], Qual QualFileStem, CallRe nil (first slice: definitions only), Defs exactly: (1) Kind KMethod, Re `^\s*[-+]\s*\([^)]*\)\s*(\w+)`, Name 1 — matches `- (void)viewDidLoad {` and `+ (instancetype)sharedInstance {`, capturing the first selector segment; (2) Kind KType, Re `^@(?:interface|implementation)\s+(\w+)`, Name 1 — matches `@interface Foo : NSObject` and `@implementation Foo`; (3) Kind KFunc, Re `^\s*(?:static\s+)?(?:[A-Za-z_][\w\s*]*?)\s+(\w+)\s*\(([^;{]*)\)\s*\{`, Name 1, Sig 2 — plain C functions defined in .m files (brace on same line, so prototypes ending in ; never match). init() appends objcSpec to registry. Tests mirror fortran_test.go exactly: TestObjcSpecLangExtensions, TestObjcSpecNodes (positive source with one -method, one +method, one @interface, one @implementation with different name, one C function; assert full NodeIDs objc:<stem>.<name>, Kind, Line), TestObjcSpecNegativeLines (`@end`, `[obj message];` call site, `// - (void)commented`, C prototype `int f(int);` mint nothing), TestObjcSpecRegisteredInAll, TestObjcSpecDeterministic, TestObjcSpecIndexAllE2E (real .m file through index.New+IndexAll, helper pattern copied from fortran_test.go TestFortranSpecIndexAllE2E). Verify: go vet ./internal/langspec/ && go test ./internal/langspec/ -run Objc -race. Constraints: no edits outside the two files; follow existing test helper nodesByID. Rollback: delete both files.

## T-0065 langspec metal: Spec + five tests + IndexAll E2E
kind: task
state: active
created: 2026-07-24
parent: P-0039

Scope (disjoint, exactly two NEW files): internal/langspec/metal.go, internal/langspec/metal_test.go. Recipe: docs/cookbook-new-language.md steps 3-4 only (graph.LangMSL and extLang .metal entry already exist — do NOT touch graph.go or langs.go). Reference specs: internal/langspec/r.go (structure), internal/langspec/fortran.go (comment style). Spec value metalSpec: Lang graph.LangMSL, Exts [".metal"], Qual QualFlat (entry points are globally named, like CUDA kernels — see cookbook Qual table), CallRe nil, Defs exactly: (1) Kind KKernel, Re `^\s*kernel\s+[\w:<>,*&\s]+?\b(\w+)\s*\(([^;{]*)\)`, Name 1, Sig 2 — matches `kernel void add_arrays(device const float* a [[buffer(0)]], ...)`; (2) Kind KKernel, Re `^\s*(?:vertex|fragment)\s+[\w:<>,*&\s]+?\b(\w+)\s*\(([^;{]*)\)`, Name 1, Sig 2 — matches `vertex float4 vertexShader(...)` and `fragment half4 fragmentShader(...)`; (3) Kind KFunc, Re `^\s*(?:static\s+|inline\s+)*(?:[A-Za-z_][\w:<>]*)\s+(\w+)\s*\(([^;{]*)\)\s*\{`, Name 1, Sig 2 — plain MSL helper functions with brace on the same line (prototypes with ; never match). Metal is case-sensitive C++ dialect: NO (?i). init() appends metalSpec to registry. Tests mirror fortran_test.go exactly: TestMetalSpecLangExtensions, TestMetalSpecNodes (positive source with one kernel, one vertex, one fragment, one helper; NodeIDs are msl:<name> — QualFlat, no stem prefix; assert Kind KKernel for the three entry points, KFunc for helper), TestMetalSpecNegativeLines (`float4 proto(float2 uv);` prototype, `// kernel void commented`, `return pos;` mint nothing), TestMetalSpecRegisteredInAll, TestMetalSpecDeterministic, TestMetalSpecIndexAllE2E (real .metal file through index.New+IndexAll, helper pattern from fortran_test.go). Verify: go vet ./internal/langspec/ && go test ./internal/langspec/ -run Metal -race. Constraints: no edits outside the two files. Rollback: delete both files.
