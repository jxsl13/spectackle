---
schema: v1
---

## T-01KYJN4CCEFMGTGEX2M41HMHJQ dogfood: the bench-curves ledger measurements become first-class bench records
kind: task
state: draft
created: 2026-07-27
parent: P-01KYJMVX2QES89YTP3KXSJPA7J
targets: docs/bench-curves.md

Task 3 of P-01KYJMVX2Q - the acceptance fixture: the repos own measurements enter the new type. Using the shipped bench tool (tasks 1+2 merged first): put the offline-collapse A/B (name=lifecycle-tokens, frame os=any arch=any cpu=any ram=any gpu=any impl=offline-theater vs commit-only, metrics bytes:B:- tokens:tok:-, values 3558/889 vs 2765/691, tool=spectackle-bench-v3-fixture, note citing T-01KYJ4Q8DT) and the outcome judge batch (name=outcome-navigation, frame any, impls per judge or one aggregate - implementers call, note citing T-01KYJ58DBA with the 1/3-valid caveat verbatim-compacted). Then docs/bench-curves.md gains a short section stating that NEW measurements land as bench records first and the ledger table cites their M- IDs - the prose ledger stays for narrative, the records become the source of truth. Assert the round trip in an e2e: put both, find scope=bench matches, get renders the winner stars per direction. VERIFY: go build ./... && go test ./... -count=1 && gofmt -l . empty; the two M- IDs pasted in the archive note. SCOPE: records + one docs section. ROLLBACK: revert.
