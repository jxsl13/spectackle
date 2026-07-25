---
schema: v0
---

## B-0008 langspec Parse gates body spans and call edges on KFunc/KMethod only, so kernel-originating calls (KKernel) never mint edges
kind: bug
state: draft
created: 2026-07-25
refs: T-0122, ADR-0012
targets: internal/langspec/langspec.go

DEFECT
SpecParser.Parse invokes the body-span computation and callEdges only when def.Kind is KFunc or KMethod. A KKernel def (metal kernel/vertex/fragment, glsl entry points, analogous shapes) therefore never gets a body span beyond its def line and never emits call edges, even with CallRe and (since T-0118) EndSpan configured. Observed empirically in T-0122: gap-metal fixture, shade_pixels (kernel) calls computeNormal — no edge mints; the same class exists for any language whose entry points are a distinct node kind.

CAUSE
The kind gate predates kernel-bearing langspec languages; it encodes the assumption that only functions and methods have bodies worth scanning.

FIX (decision at implementation)
Include KKernel in the span/edge gate (likely just widening the kind set), plus a regression test with a kernel calling a helper. Check whether other kinds with brace bodies (e.g. KType for languages minting callable types) warrant inclusion — decide there, not here.

VERIFY
go test ./internal/langspec/... -race with the new kernel-edge test; re-probe the gap-metal fixture: shade_pixels->computeNormal edge appears.

ROLLBACK
One kind-set widening; revert restores prior gating.
