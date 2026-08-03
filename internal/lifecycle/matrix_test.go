package lifecycle

import (
	"errors"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/workspace"
)

// The exhaustive transition matrix: every state as a source, every state as a
// destination, all 64 cells, each with a stated outcome.
//
// The scenario tests elsewhere in this package walk the paths somebody thought
// to walk — forward skip, reopen, reject and revoke, archived terminal. What
// they cannot establish is the cross product, and the cross product is where a
// state machine rots: the cell nobody named is the cell that quietly starts
// behaving differently.
//
// Two properties are asserted, not one. The matrix pins what Move DOES, and
// TestAllowedAgreesWithMove pins that Allowed says the same thing. Allowed is
// advertised to callers — it composes the refusal messages and the tool surface
// reads it — so a cell where Allowed promises a transition Move refuses (or the
// reverse) is a real defect that a test exercising only one of the two cannot
// see.

// allStates is every state, in a fixed order. A slice and not a map: iteration
// order decides the order failures are reported in, and a matrix that reorders
// between runs is a matrix nobody can bisect.
var allStates = []string{
	item.StateDraft,
	item.StateSubmitted,
	item.StateApproved,
	item.StateActive,
	item.StateDone,
	item.StateArchived,
	item.StateRejected,
	item.StateBlocked,
}

// requireStates fails a test before its loop runs if allStates has emptied
// out. Every assertion in this file lives inside a `range allStates`, so an
// empty table would make the whole matrix report success while asserting
// nothing at all — the vacuous-test shape the workspace's own check flags
// (! VAC W). Called per test rather than once for the file because `go test
// -run` runs them one at a time.
func requireStates(t *testing.T) {
	t.Helper()
	if len(allStates) == 0 {
		t.Fatal("allStates is empty — every assertion in this file lives inside a range over it")
	}
}

// wantAllowed is the advertised transition set for each source state, written
// out as a literal rather than computed. Deriving it from the same rules the
// implementation uses would make this test agree with any bug that lives in
// those rules; spelling it out is the point.
var wantAllowed = map[string][]string{
	item.StateDraft: {
		item.StateSubmitted, item.StateApproved, item.StateActive,
		item.StateDone, item.StateArchived, item.StateRejected,
	},
	item.StateSubmitted: {
		item.StateApproved, item.StateActive, item.StateDone,
		item.StateArchived, item.StateRejected,
	},
	item.StateApproved: {
		item.StateActive, item.StateDone, item.StateArchived, item.StateRejected,
	},
	item.StateActive: {
		item.StateDone, item.StateArchived, item.StateRejected,
	},
	// done carries the one backward hop kept outside rejection: the reopen.
	item.StateDone: {
		item.StateArchived, item.StateActive, item.StateRejected,
	},
	// archived is terminal — not even rejection leaves it.
	item.StateArchived: {},
	// rejected is revocable into any state before done: a rejection made with
	// too little information must be undoable, but undoing it may not smuggle
	// an item into done or archived without passing the gates.
	item.StateRejected: {
		item.StateDraft, item.StateSubmitted, item.StateApproved, item.StateActive,
	},
	// blocked refuses every Move; only ResolveBlocked, driven by the linked
	// decision item's outcome, gets an item out of it.
	item.StateBlocked: {},
}

// matrixWorkspace builds a workspace whose feedback budget is far above
// anything the matrix consumes.
//
// done->active spends a round, and an exhausted budget makes Move return
// ErrRoundsExhausted while leaving the item on done — a legitimate behavior,
// tested by name in TestReopenIncrementsAndEscalates, and one that must never
// surface inside a matrix cell as a surprise. Determinism here means the cell's
// outcome depends on the transition alone, not on how many rounds earlier rows
// happened to burn.
func matrixWorkspace(t *testing.T) workspace.Root {
	t.Helper()
	root := ws(t)
	root.Cfg.Feedback.MaxRounds = 1000
	return root
}

// seedState returns a fresh workspace holding one item parked in state, plus
// its ID.
//
// A fresh workspace per cell, deliberately. archived and rejected items leave
// work.md, blocked is reachable only through Escalate, and a shared fixture
// would leak one row's writes into the next — so every cell starts from
// nothing and no row depends on the order rows run in.
func seedState(t *testing.T, state string) (workspace.Root, string) {
	t.Helper()
	root := matrixWorkspace(t)
	it, err := Draft(root, nil, "task", "matrix subject", "", "", "", nil)
	if err != nil {
		t.Fatalf("seed draft: %v", err)
	}
	id := it.ID // UUIDv7-derived since ADR-0013: captured, never a literal

	switch state {
	case item.StateDraft:
	case item.StateSubmitted, item.StateApproved, item.StateActive,
		item.StateDone, item.StateArchived:
		if _, err := Move(root, id, state, "seeding "+state); err != nil {
			t.Fatalf("seed %s: %v", state, err)
		}
	case item.StateRejected:
		if _, err := Move(root, id, item.StateRejected, "seeding rejected"); err != nil {
			t.Fatalf("seed rejected: %v", err)
		}
	case item.StateBlocked:
		// blocked is not a Move destination from anywhere: the only way in is
		// exhausting the reopen budget and escalating, so the fixture has to
		// drive that path explicitly.
		root.Cfg.Feedback.MaxRounds = 1
		if _, err := Move(root, id, item.StateDone, ""); err != nil {
			t.Fatalf("seed blocked (done): %v", err)
		}
		_, err := Move(root, id, item.StateActive, "")
		var exhausted ErrRoundsExhausted
		if !errors.As(err, &exhausted) {
			t.Fatalf("seed blocked: expected ErrRoundsExhausted, got %v", err)
		}
		if _, _, err := Escalate(root, nil, exhausted.Item); err != nil {
			t.Fatalf("seed blocked (escalate): %v", err)
		}
		root.Cfg.Feedback.MaxRounds = 1000
	default:
		t.Fatalf("seedState: unknown state %q", state)
	}
	assertState(t, root, id, state, "seed")
	return root, id
}

// assertState checks where the item actually is, reading it back through the
// same resolution order Move uses: live work.md first, then the reject snapshot,
// then the archive tombstone. Anything else would report "gone" for the two
// states that legitimately leave work.md.
func assertState(t *testing.T, root workspace.Root, id, want, ctx string) {
	t.Helper()
	if got, ok, err := item.Get(root, id); err != nil {
		t.Fatalf("%s: item.Get: %v", ctx, err)
	} else if ok {
		if got.State != want {
			t.Fatalf("%s: item is %s, want %s", ctx, got.State, want)
		}
		return
	}
	if rej, ok, err := lastReject(root, id); err != nil {
		t.Fatalf("%s: lastReject: %v", ctx, err)
	} else if ok && rej.State == want {
		return
	}
	if _, ok, err := Tombstone(root, id); err != nil {
		t.Fatalf("%s: Tombstone: %v", ctx, err)
	} else if ok && want == item.StateArchived {
		return
	}
	t.Fatalf("%s: item is not resolvable in state %s", ctx, want)
}

// TestTransitionMatrix drives all 64 (from, to) pairs.
//
// Each cell asserts both halves of the outcome: an allowed transition lands and
// the stored item follows, and a refused one errors AND leaves the item exactly
// where it was. The second half is the one worth having — a refused Move that
// wrote anyway is the worst failure this machine can have, and an
// error-only assertion would never notice.
func TestTransitionMatrix(t *testing.T) {
	requireStates(t)
	for _, from := range allStates {
		for _, to := range allStates {
			t.Run(from+"_to_"+to, func(t *testing.T) {
				root, id := seedState(t, from)
				want := allowedByTable(from, to)

				note := ""
				if to == item.StateRejected {
					note = "matrix rejection note" // rejections require one
				}
				_, err := Move(root, id, to, note)

				if want {
					if err != nil {
						t.Fatalf("%s -> %s refused, want allowed: %v", from, to, err)
					}
					assertState(t, root, id, to, "after allowed move")
					return
				}
				if err == nil {
					t.Fatalf("%s -> %s allowed, want refused", from, to)
				}
				// refused: the item must not have moved
				assertState(t, root, id, from, "after refused move")
			})
		}
	}
}

// allowedByTable answers the matrix from wantAllowed — the literal table, not
// the implementation.
func allowedByTable(from, to string) bool {
	for _, s := range wantAllowed[from] {
		if s == to {
			return true
		}
	}
	return false
}

// TestAllowedMatchesTable pins the advertised transition set per source state.
// Allowed's result is not merely internal: it composes the "allowed: ..." tail
// of every refusal and is what the tool surface reads, so its exact content and
// ORDER are contract.
func TestAllowedMatchesTable(t *testing.T) {
	requireStates(t)
	for _, from := range allStates {
		got := Allowed(from)
		want := wantAllowed[from]
		if len(got) != len(want) {
			t.Fatalf("Allowed(%s) = %v, want %v", from, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("Allowed(%s) = %v, want %v (differs at %d)", from, got, want, i)
			}
		}
	}
}

// TestAllowedAgreesWithMove is the second property, and the reason the matrix
// is worth more than a list of scenarios: Allowed and Move must answer the same
// question the same way in all 64 cells.
//
// They are separate code paths — Allowed builds a slice from the state order,
// Move consults it and then applies its own guards — so they can diverge. A
// divergence is a live defect either way round: Allowed promising a transition
// Move refuses sends a caller down a path that cannot work, and Allowed
// omitting one Move accepts hides a legal move from every caller that reads the
// advertised set.
func TestAllowedAgreesWithMove(t *testing.T) {
	requireStates(t)
	for _, from := range allStates {
		for _, to := range allStates {
			t.Run(from+"_to_"+to, func(t *testing.T) {
				root, id := seedState(t, from)
				note := ""
				if to == item.StateRejected {
					note = "agreement note"
				}
				_, err := Move(root, id, to, note)
				moveAccepts := err == nil

				if allowedByTable(from, to) != moveAccepts {
					t.Fatalf("Allowed(%s) says %v for %s, Move says %v",
						from, allowedByTable(from, to), to, moveAccepts)
				}
			})
		}
	}
}

// TestRejectionRequiresNoteFromEveryLegalSource: the note is not a formality —
// it becomes the searchable rejection corpus, which is a product feature — so
// every source that may reject must refuse a noteless one, and refuse it
// without moving the item.
func TestRejectionRequiresNoteFromEveryLegalSource(t *testing.T) {
	requireStates(t)
	for _, from := range allStates {
		if !allowedByTable(from, item.StateRejected) {
			continue
		}
		t.Run(from, func(t *testing.T) {
			root, id := seedState(t, from)
			for _, note := range []string{"", "   ", "\t\n"} {
				_, err := Move(root, id, item.StateRejected, note)
				if err == nil {
					t.Fatalf("%s -> rejected accepted a blank note %q", from, note)
				}
				if !strings.Contains(err.Error(), "note") {
					t.Fatalf("refusal does not name the missing note: %v", err)
				}
				assertState(t, root, id, from, "after noteless rejection")
			}
			// and with a note it goes through
			if _, err := Move(root, id, item.StateRejected, "a real reason"); err != nil {
				t.Fatalf("%s -> rejected with a note: %v", from, err)
			}
		})
	}
}

// TestSelfTransitionsAlwaysRefused states the diagonal of the matrix on its own.
// It is covered by TestTransitionMatrix too, but a state machine that quietly
// accepts X -> X re-runs that state's effects — archive would merge its delta
// twice, reject would append a second snapshot — so the diagonal is worth a
// named test rather than only an implied one.
func TestSelfTransitionsAlwaysRefused(t *testing.T) {
	requireStates(t)
	for _, s := range allStates {
		t.Run(s, func(t *testing.T) {
			root, id := seedState(t, s)
			note := ""
			if s == item.StateRejected {
				note = "self note"
			}
			if _, err := Move(root, id, s, note); err == nil {
				t.Fatalf("%s -> %s (self) was accepted", s, s)
			}
			assertState(t, root, id, s, "after refused self-transition")
		})
	}
}

// TestArchivedAfterRevokedRejectionIsTerminal is the archived row of the
// matrix again, seeded the one way seedState cannot seed it: draft ->
// rejected -> draft (revoke) -> archived, rather than the direct forward hop.
//
// The distinction is the whole defect (B-01KYS6ZJQSE1E). An archived item is
// absent from work.md, so Move falls back to the journal — and the fallback
// used to ask for the last REJECTION, which for this history is the stale
// snapshot from before the revoke. It answered state=rejected, and rejected is
// revocable into draft/submitted/approved/active, so four of these eight cells
// returned err == nil and pulled the record back into work.md while its
// tombstone and its spec.md `## intent` line stayed exactly where they were:
// one record, live and archived at once. Every cell here must refuse, and the
// record must still be resolvable only as a tombstone afterwards.
//
// item.Get is asserted DIRECTLY, on top of assertState, because the resurrection
// is a WRITE: a test that only checked the error would have gone green the day
// the refusal was worded correctly and the record was still put back.
func TestArchivedAfterRevokedRejectionIsTerminal(t *testing.T) {
	requireStates(t)
	for _, to := range allStates {
		t.Run("to_"+to, func(t *testing.T) {
			root := matrixWorkspace(t)
			it, err := Draft(root, nil, "task", "revoked, then archived", "", "", "", nil)
			if err != nil {
				t.Fatalf("seed draft: %v", err)
			}
			id := it.ID
			if _, err := Move(root, id, item.StateRejected, "rejected with too little information"); err != nil {
				t.Fatalf("seed reject: %v", err)
			}
			if _, err := Move(root, id, item.StateDraft, "revoked: the rejection was uninformed"); err != nil {
				t.Fatalf("seed revoke: %v", err)
			}
			if _, err := Move(root, id, item.StateArchived, "shipped after all"); err != nil {
				t.Fatalf("seed archive: %v", err)
			}
			assertState(t, root, id, item.StateArchived, "seed")

			note := ""
			if to == item.StateRejected {
				note = "post-archive rejection note"
			}
			if _, err := Move(root, id, to, note); err == nil {
				t.Fatalf("archived -> %s was accepted; archived is terminal", to)
			}
			// The write, not the wording: work.md must not have gained the
			// record back, whatever the refusal said.
			if got, ok, err := item.Get(root, id); err != nil {
				t.Fatalf("item.Get: %v", err)
			} else if ok {
				t.Fatalf("archived -> %s put the record back into work.md as %s", to, got.State)
			}
			assertState(t, root, id, item.StateArchived, "after refused move")
		})
	}
}

// TestBlockedIsNeverAMoveDestination: blocked never appears in any source's
// allowed set, so no sequence of Move calls can reach it. Escalate is the only
// way in, which is what makes the escalation exits (rescope, reject,
// override-once) the only way out.
func TestBlockedIsNeverAMoveDestination(t *testing.T) {
	requireStates(t)
	for _, from := range allStates {
		if allowedByTable(from, item.StateBlocked) {
			t.Fatalf("blocked is reachable by Move from %s — only Escalate may set it", from)
		}
	}
}
