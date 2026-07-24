---
schema: v0
---

## T-0012 plan9 asm: internal/plan9 scanner pkg + AsmParser + EAsm resolver
kind: task
state: done
created: 2026-07-24
parent: P-0007

Scope files ONLY: git mv internal/index/plan9asm.go -> internal/plan9/scan.go (package plan9) and internal/index/plan9asm_test.go -> internal/plan9/scan_test.go; new internal/index/asmparser.go + asmparser_test.go; rewrite internal/resolve/plan9asm.go + new plan9asm_test.go. Do NOT touch internal/mcpserver (orchestrator registers the parser). AsmParser: Lang=LangAsm, Ext .s/.S, nodes asm:<dirbase>.<name> (dirbase=basename of file dir; root files: asm:<name>), TEXT->KAsmProc, GLOBL->KVar, Line=EndLine=sym line, Sig=frame. Resolver: fs.ByLang(LangAsm) files, plan9.Scan each, TEXT syms where g.Node(go:<dirbase>.<name>) AND g.Node(asm-id) both exist -> EAsm edge go->asm at TEXT site. make all green.
