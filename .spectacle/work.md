---
schema: v0
---

## D-0002 R-0004 empfiehlt: wasm/tree-sitter-Parser DEFERREN; Erst-Slice waere ein C-PoC (tree-sitter-c via wazero) mit Parity-Oracle gegen die Regex-Kette, Binärgrößen- und Latenzbudget innerhalb der M4-Envelope (docs/design-wasm-parsers.md). Wie verfahren wir mit wazero?
kind: decision
state: done
created: 2026-07-24

kind: radio
options: defer — langspec-Regex-Kette bleibt der Weg, wasm-PoC erst bei echtem Parity-Bedarf, c-poc — jetzt einen C-PoC-Task minten (tree-sitter-c + wazero, Parity-Oracle), no-go — wasm-Pfad endgültig verwerfen
choice: c-poc — jetzt einen C-PoC-Task minten (tree-sitter-c + wazero
