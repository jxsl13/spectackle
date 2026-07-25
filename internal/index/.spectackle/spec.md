---
schema: v0
---

## intent
- P-0012 M3 slice 1: go/types call-edge upgrade pass (two-tier per R-0002): typed call edges live: +221 on this repo, Sweep shows preCall caller, ~340ms best-effort pass after IndexAll
- P-0013 M3 slice 2: module-hash cache for the typed-call pass: typed-call cache: module-hash key, cache hit skips packages.Load (sentinel-proven)
- T-0026 honor config.yaml ignore globs in IndexAll: config ignore globs honored in IndexAll, server passes Cfg.Ignore
- R-0003 graph node removal for true incremental IndexPaths: measure-first: incremental indexing not worth building; M4 gate already cleared 7-15x
- T-0125 go parser: call edges from closure-var bodies and explicit generic instantiations: go call edges now cover closure-var bodies and explicit generic instantiation in both the syntactic and typed passes; proven live on the fixture
- T-0126 asm + cuda parsers: linker-suffix symbol forms and kernel modifier orders: asm linker-suffix forms 7 to 11 of 11 symbols and cuda modifier orders 7 to 14 of 14; one silently broken go-to-asm edge restored

## IDX-001 {applies: go:index.LangOf,go:index.indexer.IndexAll}
The indexer SHALL treat `extLang` as the single source of truth for extension-to-language routing and flush the parse-blob store exactly once per `IndexAll` run.