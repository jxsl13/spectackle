package knowledge

// FoldInto computes the additive delta of folding an incoming artifact into
// a workspace: every entry of incoming whose (Kind, Key) identity is not
// already present in current — current being the workspace's own artifact,
// exactly as Extract would produce it right now. This is pure data
// plumbing over two in-memory Artifacts; no workspace write happens here.
// The actual write step (internal/mcpserver's knowledge tool, op=apply)
// calls Extract on the live workspace to build current, calls FoldInto to
// learn what is missing, and then routes each missing entry through the
// existing rule/decide composers — never a new write path.
//
// Named FoldInto, not Apply: this package already exports an Apply
// (merge.go) that folds one conflict Resolution into an Artifact's own
// Entries/Resolutions — bookkeeping over a single artifact's conflict
// history, entirely in memory. "Fold an artifact into a live repository's
// spec/work files" is a different operation in every respect (what it
// reads, what it's for, who calls it) from "fold a resolved conflict back
// into an artifact's own entry list" — reusing the Apply name across two
// such different operations on the same package's public surface would
// make every call site ambiguous without a qualifying comment. FoldInto
// keeps the two apart by name alone.
//
// FoldInto is idempotent by construction: given the same current, a second
// call with the same incoming returns an empty Artifact once the first
// call's entries have actually been folded into the workspace (so a
// subsequent Extract of that workspace becomes the new current) — every key
// incoming carries is by then already present. Duplicate identities within
// incoming itself (e.g. a hand-assembled or concatenated artifact) also
// collapse to their first occurrence, so the result never carries the same
// (Kind, Key) twice even if the input did.
func FoldInto(current, incoming Artifact) Artifact {
	type identity struct {
		kind EntryKind
		key  string
	}
	have := make(map[identity]bool, len(current.Entries))
	for _, e := range current.Entries {
		have[identity{e.Kind, e.Key}] = true
	}
	var toAdd []Entry
	for _, e := range incoming.Entries {
		id := identity{e.Kind, e.Key}
		if have[id] {
			continue
		}
		have[id] = true
		toAdd = append(toAdd, e)
	}
	sortEntries(toAdd)
	return Artifact{Sources: incoming.Sources, Entries: toAdd}
}
