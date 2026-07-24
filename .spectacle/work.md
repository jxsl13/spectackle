---
schema: v0
---

## P-0019 langspec: declarative multi-language support - a language is data, not code
kind: proposal
state: approved
created: 2026-07-24
grilled: 2026-07-24
targets: internal/index/langs.go, internal/graph/graph.go

New internal/langspec package: a language = one Spec value (Lang tag, extensions, definition regex patterns with kind+name-group, qualification mode dirpkg|filestem|flat) executed by ONE generic SpecParser implementing index.LanguageParser. Adding a language touches exactly one spec file + one extLang entry - the M6 cookbook goal, deliverable today in pure Go. Foundation ships the framework + python and javascript as reference specs + tests; language batches then parallelize across implementer agents (disjoint per-language files). Wiring (server reindex parser list) orchestrator-owned.

## D-0002 R-0004 empfiehlt: wasm/tree-sitter-Parser DEFERREN; Erst-Slice waere ein C-PoC (tree-sitter-c via wazero) mit Parity-Oracle gegen die Regex-Kette, Binärgrößen- und Latenzbudget innerhalb der M4-Envelope (docs/design-wasm-parsers.md). Wie verfahren wir mit wazero?
kind: decision
state: done
created: 2026-07-24

kind: radio
options: defer — langspec-Regex-Kette bleibt der Weg, wasm-PoC erst bei echtem Parity-Bedarf, c-poc — jetzt einen C-PoC-Task minten (tree-sitter-c + wazero, Parity-Oracle), no-go — wasm-Pfad endgültig verwerfen
choice: c-poc — jetzt einen C-PoC-Task minten (tree-sitter-c + wazero
