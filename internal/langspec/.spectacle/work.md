---
schema: v0
---

## T-0034 langspec framework + python/javascript reference specs
kind: task
state: done
created: 2026-07-24
parent: P-0019

Scope ONLY: new internal/langspec/ (langspec.go, python.go, javascript.go, langspec_test.go, .spectacle rule via driver), internal/graph/graph.go (Lang consts LangPy py, LangJS js ONLY), internal/index/langs.go (extLang entries .py/.js/.mjs). Design: type Def struct{Kind graph.NodeKind; Re *regexp.Regexp; Name int; Sig int}; type Spec struct{Lang graph.Lang; Exts []string; Qual QualMode; Defs []Def}; QualMode enum QualFileStem|QualDirPkg|QualFlat; type SpecParser struct{S Spec} implements index.LanguageParser (Lang/Extensions/Parse); Parse: line scan, per match mint ids.Mint(string(S.Lang), qual) with qual per mode (filestem: base name without ext + '.' + name; dirpkg: dir base + '.'; flat: name), Node{Kind,Lang,File,Line,EndLine:line,Sig: optional group}, deterministic, sha256 hash, no edges; func All() []index.LanguageParser returns SpecParser for every registered spec. python.go: defs ^\s*def\s+(\w+)\s*\( KFunc (indented = methods too, same kind v0), ^class\s+(\w+) KType. javascript.go (.js/.mjs): ^(?:export\s+)?(?:async\s+)?function\s*\*?\s+(\w+) KFunc, ^(?:export\s+)?class\s+(\w+) KType, ^(?:export\s+)?(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s*)?(?:\(|\w+\s*=>) KFunc arrow. Tests: per-spec fixture strings -> expected node IDs/kinds/lines; determinism; e2e via index.New(graph.NewMem(), store.NewMem(), append([]index.LanguageParser{index.GoParser{}}, langspec.All()...), resolve.Default().All()) on temp tree with .py/.js -> nodes py:mod.func js:mod.Class present. Add EARS rule via driver: rule op=add dir=internal/langspec pattern=U system='the langspec registry' response='define a language purely as one `Spec` data value so adding a language never modifies the indexing pipeline' stem SPX-LSP applies=['go:langspec.SpecParser.Parse'] item=P-0019 (AFTER implementation + make build so the anchor resolves). Verify: go build ./... && go vet ./internal/langspec/ ./internal/index/ ./internal/graph/ && go test ./internal/langspec/ ./internal/index/ ./internal/graph/ ./internal/mcpserver/ -race green. NO server.go changes.
