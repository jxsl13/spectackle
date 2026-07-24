---
schema: v0
---

## P-0091 ULID item IDs behind the existing kind prefix, accepted alongside the legacy form
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24
targets: internal/item/item.go

ADR-0012 removed the central counter as the coordination point, with the distributed-database half explicitly dropped: solving collisions is the goal, running a cluster is not. ULID is the concrete carrier — 128 bits, 48-bit millisecond timestamp plus randomness, lexicographically sortable, coordination-free, and 26 Crockford base32 characters against UUIDv7 hex's 36 for identical guarantees. Crockford's alphabet ascends, so string order equals byte order equals creation order.

The kind prefix stays. A ULID carries order and uniqueness but not type; without P, T, B, R or ADR in front, no reader can tell a proposal from a task without a lookup, and find scope=proposal, item.IDRe and kindLetter all key on it. Two to four characters buy a resolution avoided on every single read.

Both forms must resolve. Every record, anchor, journal line and test in this repository carries the four-digit form; a grammar that rejected it would orphan the corpus wholesale. Acceptance is therefore additive and permanent, not a migration window.

Scope deliberately excludes rule IDs. The collision that motivated this was in fact a rule id, but rules differ in the two ways that matter: they are authored deliberately rather than minted concurrently by racing agents, and they live in spec.md, which exists to be read and reviewed by humans in a diff. MCP-013 stays legible as a heading in a way MCP-01J8ZQK3XYZ would not, and the concurrency exposure that makes item minting racy simply is not there. If rules turn out to need it, that is its own decision with its own trade-off.

Rejected: UUIDv7 in canonical hex. Same 128 bits and same properties for ten more characters per id, in a system whose output diet is a stated feature.

Rejected: xid at 20 characters. Second-granularity timestamps and host/pid components mean a 3-byte counter carries uniqueness for agents minting inside the same second, which is exactly the case here.

This first task delivers generation and dual-form validation as pure functions, with nothing switched over. The minter change is a separate act, because flipping it rewrites what every subsequent record looks like and deserves its own verification.

## T-0125 item: ULID generation and dual-form ID validation, nothing switched over
kind: task
state: active
created: 2026-07-24
parent: P-0091
targets: internal/item/ulid.go, internal/item/ulid_test.go, internal/item/item.go, internal/item/item_test.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first. Read internal/item/item.go before writing anything, especially IDRe, kindLetter, NextID and Num.

GOAL
Generate ULID-based item IDs behind the existing kind prefix, and accept both that form and the legacy four-digit form. NOTHING SWITCHES OVER in this task: the minter keeps producing legacy IDs. This is the pure, testable half.

SCOPE (lease exactly these four)
  internal/item/ulid.go        NEW  generation + encoding
  internal/item/ulid_test.go   NEW
  internal/item/item.go        IDRe and Num only
  internal/item/item_test.go
Do NOT touch internal/lifecycle (that is where the minter lives and it is NOT part of this task), internal/coord, internal/mcpserver, internal/knowledge, internal/journal, internal/drift, cmd/, docs/, README.md. Do NOT add a go.mod dependency: implement ULID directly, it is small and the repo is dependency-frugal. .spectackle files are server-owned: never edit them by hand.

CONTRACT
ITM-001 (read it with get id=ITM-001): the item ID grammar SHALL accept both the legacy <KIND>-NNNN form and a <KIND>-<26-char Crockford base32 ULID> form, so records written before and after the switch both resolve.

WHAT A ULID IS, precisely
128 bits: 48-bit big-endian millisecond Unix timestamp, then 80 bits of randomness. Encoded as 26 characters of Crockford base32 (alphabet 0123456789ABCDEFGHJKMNPQRSTVWXYZ — no I, L, O or U). The alphabet ascends, so lexicographic string order equals byte order equals creation order. That sortability is the whole point; a test must prove it rather than assume it.

WHAT TO BUILD (ulid.go)
  A generator producing the 26-char encoding, taking its time source and randomness as parameters or injectable fields so tests are deterministic. Do NOT read the clock from an untestable global.
  Uppercase output. Crockford decoding is case-insensitive and treats I/L as 1 and O as 0, but generation must be canonical.
  A function minting a full item ID as <KIND>-<ULID> using the existing kindLetter map, so the prefix stays the single source of truth for kinds.

WHAT TO CHANGE (item.go — only these two)
  IDRe: currently ^(?:ADR|[PTBRD])-\d{4}$. Extend it to also accept the 26-char Crockford form after the same prefixes. Keep D accepted: it is legacy-only and existing journals carry it.
  Num: it currently parses the numeric suffix and is used for ordering and for the minter's floor. Decide what it returns for a ULID id and document the choice in a comment — returning 0 is defensible if every caller treats it as a floor, but VERIFY that by reading the callers rather than assuming. If a caller would misbehave, say so in your report instead of quietly changing that caller; it is out of scope.

TESTS
  ulid_test.go:
    1. length is exactly 26 and every character is in the Crockford alphabet.
    2. monotonic sortability: ids generated at increasing timestamps sort in generation order as plain strings. Test at least three timestamps.
    3. same-millisecond ids differ (randomness is doing its job) and still both decode to that millisecond.
    4. the encoded timestamp round-trips: decode the first 10 characters back to the millisecond you put in.
    5. determinism: a fixed time source and a fixed randomness source produce a byte-identical id twice.
  item_test.go:
    6. IDRe accepts P-0084, ADR-0012, D-0001 (legacy) and the new P-<ULID> form; it rejects a 25-char and a 27-char body, and rejects a body containing I, L, O or U.
    7. Num on both forms behaves as you documented.

VERIFY (run every one; report real output, never predicted)
  go build ./...
  go test ./internal/item/... -race -v
  go test ./...
  go vet ./internal/item/...
  /home/user/spectackle/bin/spectackle lint
  git diff go.mod go.sum   (must be empty)
go test ./... green WITHOUT touching another package is the signal that acceptance really is additive. If widening IDRe breaks a caller, report it — do not fix the caller here.

EXIT CRITERION
Seven tests green under -race, sortability and round-trip proven by test rather than asserted, ./... green with no changes outside internal/item, vet clean, lint clean, go.mod untouched.

ROLLBACK
One new file plus a widened regex. Nothing mints ULIDs yet, so reverting is deleting the file and narrowing the regex; no record on disk changes either way.

REPORT BACK
The API, what Num returns for a ULID id and which callers you checked to justify it, each test's real output, and anything you deliberately did NOT do.
