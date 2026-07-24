---
schema: v0
---

## R-0004 wazero x tree-sitter feasibility: pure-Go WASM parser runtime (M6 architecture concept)
kind: research
state: done
created: 2026-07-24

Deliverable: docs/design-wasm-parsers.md (100-140 lines, style of docs/architecture.md). Questions to answer with WEB research (pkg.go.dev, github): (1) does a maintained pure-Go tree-sitter runtime over wazero exist (candidates to verify, not assume: wasilibs projects, malivvan/*, anything importing wazero + tree-sitter); (2) are official grammar .wasm builds published per language (web-tree-sitter artifacts, tree-sitter CLI build --wasm) and what ABI do they expect (emscripten imports?) - can wazero satisfy it or is a WASI build needed; (3) realistic integration shape for spectacle: one LanguageParser adapter owning a wazero runtime + N grammar modules, symbol extraction via tags queries (.scm) - which pieces exist vs must be written; (4) cost estimate (binary size per grammar, parse latency vs our line scanners); (5) recommendation: go/no-go/defer + first-slice cut (e.g. ONE language e2e as PoC) + exit criterion. Also state the fallback clearly: the langspec declarative scanner framework (P-0019) covers breadth today; wazero adds fidelity, not reach.

## P-0019 langspec: declarative multi-language support - a language is data, not code
kind: proposal
state: approved
created: 2026-07-24
grilled: 2026-07-24
targets: internal/index/langs.go, internal/graph/graph.go

New internal/langspec package: a language = one Spec value (Lang tag, extensions, definition regex patterns with kind+name-group, qualification mode dirpkg|filestem|flat) executed by ONE generic SpecParser implementing index.LanguageParser. Adding a language touches exactly one spec file + one extLang entry - the M6 cookbook goal, deliverable today in pure Go. Foundation ships the framework + python and javascript as reference specs + tests; language batches then parallelize across implementer agents (disjoint per-language files). Wiring (server reindex parser list) orchestrator-owned.
