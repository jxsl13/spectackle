// Package forge is spectackle's one seam onto a pull-request host: open a
// draft PR for a branch, flip it ready, merge it, or find the one already
// open for a branch — nothing else. Two implementations share the Forge
// interface: GitHub (net/http against the REST pulls API — no gh binary, no
// new module dependency, see GitHub and Token) and Offline (no network at
// all; Merge performs a real local merge rather than a no-op, see Offline)
// so the lifecycle, and its tests, never need a live forge or a stub
// server.
//
// Merge method is always "merge" — NEVER squash (ADR-01KYDB, and the
// sibling never-squash policy: work.md T-01KYD8M955E9NBPH27D21E4J9J).
// Squashing a branch collapses every per-edge decision commit into one
// blob on main, destroying exactly the trail those commits exist to leave.
package forge

import "fmt"

// PR identifies one pull request across the four operations. Number is the
// host-assigned PR number for GitHub, or an in-process sequence number for
// Offline — either way it is what Ready and Merge take back to name "the
// same PR" without re-deriving it from the branch.
type PR struct {
	Number int    // PR number
	Branch string // head branch
	Base   string // target (base) branch
	URL    string // human-followable link; Offline gives a synthetic one
	Draft  bool
}

// ReasonNoPermission is the MergeResult.Reason set when Merge is blocked by
// insufficient permission. It exists so that outcome is a value a caller
// can branch on, not an error string a caller has to pattern-match to tell
// apart from a real bug.
const ReasonNoPermission = "no_permission"

// MergeResult reports what Merge actually did. Merged=false is not itself
// failure: a merge blocked by insufficient permission must leave the PR
// open and say so plainly (Reason=ReasonNoPermission), degrading rather
// than failing — the automation has to be safe to run as an identity that
// cannot merge, and the difference between "didn't merge because no
// permission" and "didn't merge because something broke" has to be
// visible, not silent.
type MergeResult struct {
	Merged bool
	SHA    string // merge commit sha; set only when Merged
	Reason string // empty when Merged; ReasonNoPermission otherwise
}

// Forge is the seam: open a draft, ready it, merge it, or find the PR
// already open for a branch. Find is what makes callers idempotent — a
// caller unsure whether it already opened a PR calls Find first and never
// Open blindly. Open itself always creates rather than deduping: GitHub's
// own create-PR endpoint 422s on a duplicate head/base pair, and Offline
// mirrors that error rather than silently returning the existing PR, so
// both implementations tell a misused caller the same story.
type Forge interface {
	// Open creates a draft pull request for branch onto base.
	Open(branch, base, title, body string) (PR, error)
	// Ready flips a draft PR to ready for review.
	Ready(pr PR) (PR, error)
	// Merge merges pr with a merge commit — never squash, never a bare
	// fast-forward (see package doc and MergeResult).
	Merge(pr PR) (MergeResult, error)
	// Find returns the pull request already open for branch, if any.
	Find(branch string) (PR, bool, error)
}

// notFoundErr is a small helper so every implementation phrases a missing
// tracked PR the same way (callers match on branch/number in error text
// during debugging, not on a sentinel — there being no PR is a normal,
// expected outcome surfaced through Find's bool, not an error type of its
// own).
func notFoundErr(branch string, number int) error {
	return fmt.Errorf("forge: no open PR #%d for %s", number, branch)
}
