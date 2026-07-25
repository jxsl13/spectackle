---
schema: v0
---

## ADR-0012 R-0005 found major parser gaps in 30 of 32 languages. Which remediation path should spectackle take?
kind: adr
state: submitted
created: 2026-07-25
context: Empirical per-language probes show three fixable classes: missing/over-strict langspec Def regexes (data-only fixes, biggest recall wins: JS/TS class methods, Java constructors, Python async def, C# Allman braces, Rust const fn, Swift override init, plus asm/cuda/go hand-parser fixes), an engine gap for end-terminated languages (no body spans, some with no call edges: ruby, lua, elixir, julia, fortran, perl, php, r, shell), and the standing wazero/tree-sitter alternative (ADR-0010/0011: latency-red, availability-gated for CUDA/ObjC, but would fix span+edge+Def classes wholesale where grammars exist). Correctness-first axis per prior user steer.
status: proposed

kind: radio
option: harden-langspec: fix Def/Call regexes per language + hand-parser fixes, keep pure-Go chain
option: engine-endspan: harden regexes AND add end-keyword span/edge support to the langspec engine
option: wazero-partial: adopt wazero/tree-sitter for the worst offenders where grammars exist, langspec for the rest
option: record-only: keep findings as research, no implementation now
