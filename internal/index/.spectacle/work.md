---
schema: v0
---

## P-0013 M3 slice 2: module-hash cache for the typed-call pass
kind: proposal
state: approved
created: 2026-07-24
targets: internal/index/typespass.go

ResolveTypedCalls costs ~340ms per reindex; on a resident service reindex runs per reroot. Cache the produced ECall edge list in the existing parse-blob store under a module-state key: sha256 over go.mod bytes + sorted (path,sha256) of every .go file the walk sees. Hit -> re-add cached edges (dedupe logic already guards), skip packages.Load entirely. Signature: ResolveTypedCalls(ctx, g, root, s store.Store) - nil store = uncached (tests unaffected). Server wiring (pass s.blobs) orchestrator-owned.

## T-0022 module-hash cache for ResolveTypedCalls
kind: task
state: done
created: 2026-07-24
parent: P-0013

Scope ONLY: internal/index/typespass.go + typespass_test.go. Change signature to ResolveTypedCalls(ctx, g, root, s store.Store); nil s = today's behavior. Module key: walk root exactly like IndexAll (same ignoreDirs) collecting .go files sorted; key = sha256 over go.mod contents + each (relpath, sha256(file)) pair; cache entry Path="__typedcalls__" Hash=key Blob=gob([]graph.Edge). Hit: decode, run edges through the SAME existing-dedupe-and-add path as fresh results, return count, NO packages.Load. Miss: current behavior + Put after success (best-effort). Update existing tests for new arg (nil); new test: run with store -> run again with fresh graph+same store and a counting hook proving packages.Load was skipped (e.g. corrupt go.mod after first run: cached second run still succeeds BECAUSE Load is skipped... careful, key includes go.mod - instead prove via timing-independent trick: delete one .go file's content? that changes key. Best proof: after first cached run, second run with same tree returns same count AND a sentinel: make loadPackages an injectable package-level var func so the test can swap it with a failing stub for run 2). go build/vet/test -race for internal/index green. NO server.go changes.
