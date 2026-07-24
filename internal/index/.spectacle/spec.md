---
schema: v0
---

## intent
- P-0012 M3 slice 1: go/types call-edge upgrade pass (two-tier per R-0002): typed call edges live: +221 on this repo, Sweep shows preCall caller, ~340ms best-effort pass after IndexAll
- P-0013 M3 slice 2: module-hash cache for the typed-call pass: typed-call cache: module-hash key, cache hit skips packages.Load (sentinel-proven)
- T-0026 honor config.yaml ignore globs in IndexAll: config ignore globs honored in IndexAll, server passes Cfg.Ignore
