---
schema: v1
---

## B-01KYEEJKQ0FCNTSJ6DDM2FVT1H detached-head fast-forward at submit advances no named branch while the result claims merged to main
kind: bug
state: done
created: 2026-07-26

Minor finding of the same review: when vacateBranch falls through every candidate and detaches the main checkout, a later submit fast-forwards the DETACHED head — no named branch receives the work although the result says merged to main. Self-healing in practice (the archive merge lands the branch into the real base, online via the PR and offline via the forge double), so deferred rather than lost, but the claim is wrong while it lasts. Fix direction: FFMainPreservingRecords detects a detached head and answers a distinct loud line naming the deferral instead of the merged claim; alternatively vacateBranch refuses detach when a submit is known to follow. Low urgency: requires a repo with no resolvable base at all.
