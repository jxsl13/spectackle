---
schema: v0
---

## T-0054 one span scanner: extract braceSpan to a leaf package, fix the FFI resolver's K&R-only copy, land the cpp:->c: crossing
kind: task
state: approved
created: 2026-07-24
parent: P-0026

ROOT CAUSE (docs/validation-ddnet.md re-validation, T-0053): internal/resolve/ffi.go duplicates the body-span logic as a private K&R-only ffiBraceSpan because resolve cannot import langspec (cycle: langspec -> index -> resolve). After T-0053, langspec.braceSpan understands Allman/prototypes/multi-line headers — the FFI copy does not, so the resolver still cannot see into ddnet's C++ bodies: 0 cpp:<->c: edges.

SCOPE: internal/cspan/ (NEW leaf package: cspan.go + cspan_test.go — zero non-stdlib imports), internal/langspec/langspec.go (replace braceSpan/braceSpanFrom internals with calls into cspan; keep the exported behavior and every existing test green UNCHANGED), internal/resolve/ffi.go (+ffi_test.go: replace ffiBraceSpan with cspan; extend tests with an Allman fixture), docs/validation-ddnet.md (append '## ffi re-validation' with fresh numbers).

DESIGN:
1. internal/cspan exports exactly what both consumers need, moved verbatim from langspec (T-0053's shipped semantics — K&R same-line via shared helper, trailing-';' prototype bail, ','/'(' multi-line param header scan, bounded 3-line Allman lookahead skipping blank//comment lines): e.g. func Span(lines []string, defLine int) (end int, ok bool) plus the Delta helper if needed. Table tests MOVE with it (langspec keeps only integration-level tests; TestBraceSpanAllman relocates to cspan_test.go, adjusted imports).
2. langspec.go delegates to cspan — NO behavior change; go test -race ./internal/langspec/ must stay green without editing its remaining tests.
3. ffi.go uses cspan for body scanning. Keep the mirrored Def/CallRe/Stop regexes as they are (that duplication is documented and acceptable; the SPAN logic is the bug).
4. ffi_test.go: add an Allman-style fixture — a .cpp file whose Allman-bodied method calls str_copy(...) while a .c file defines str_copy in Allman style too; assert the resolver emits cpp:<src> -> c:str_copy.

RE-VALIDATION (dogfooding): rebuild bin/spectacle, fresh-index the ddnet clone (rm -rf its .spectacle first; driver root = /tmp/claude-0/-home-user-spectacle/4c40537b-65eb-5824-86a6-6c853d4e1c78/scratchpad/ddnet), append to docs/validation-ddnet.md: total edge count, the count/sample of cross-language edges now present (probe c:str_format, c:net_init, c:io_open — base/system.c functions heavily called from C++; find scope=code + get depth=1 in-edges showing cpp: callers), cold/warm times. If ddnet still yields zero crossings, root-cause honestly (e.g. which functions actually live in system.h templates vs system.c) and prove the mechanism with the ffi_test fixture numbers instead.

ROLLBACK: single revert; pure refactor + one resolver fix.
EXIT CRITERION: go build ./... && go vet ./... && go test -race ./internal/cspan/ ./internal/langspec/ ./internal/resolve/ green; make lint-specs clean; docs appendix updated with real numbers. Constraints: never edit .spectacle/ directly; never commit/push; do NOT touch mcpserver/, index/, graph/.
