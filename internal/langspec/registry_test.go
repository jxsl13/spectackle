package langspec

import (
	"fmt"
	"sort"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
)

// This file holds registry-wide invariants: properties asserted over every
// Spec in the registry rather than over one language's fixture. A registry
// of ~30 languages means each new entry can silently omit something the
// engine needs, and a per-language fixture only ever covers the language
// whose author remembered to write it. Assert the property over the
// registry and the language somebody adds next month is covered without
// anyone writing anything (P-0087, T-0128).
//
// Every invariant is a table-driven subtest named by graph.Lang, so a
// failure names the offending language in its test path directly.

// registrySpecs returns the registry in a deterministic order for
// subtesting. registry itself is assembled by per-file init() functions, so
// its order is package-file order — stable in practice, but sorting by Lang
// makes `go test -run` paths reproducible regardless.
func registrySpecs(t *testing.T) []Spec {
	t.Helper()
	if len(registry) == 0 {
		t.Fatal("registry is empty — every invariant below would vacuously pass")
	}
	out := make([]Spec, len(registry))
	copy(out, registry)
	sort.Slice(out, func(i, j int) bool { return out[i].Lang < out[j].Lang })
	return out
}

// reviewedNonSpanKinds are NodeKinds a Spec may declare in its Defs while
// setting CallRe, even though spanEligibleKinds excludes them: each entry is
// a decision that was made and written down, not an omission.
//
// Invariant C exists to catch omissions — the B-0008 shape, where a
// language declares a kind the gate silently drops and nobody notices no
// edges ever mint from it. A kind that has been looked at and deliberately
// left out is not that shape, so it is recorded here with its reason rather
// than papered over by widening the gate. Adding an entry here should
// require the same argument that adding one to spanEligibleKinds does.
//
// KType: a class/struct/trait/enum is a container, not a callable unit —
// the calls in its body belong to its member functions, which mint their own
// KFunc/KMethod nodes and edges. Admitting it was tried in T-0128 and
// rejected: it mints a bogus cpp:Shape -> cpp:area ECall for a pure-virtual
// declaration (the case TestCppSpecNoEdges pins) and moves every class
// node's EndLine off the contract ~15 per-language tests assert.
//
// KVar: a binding, not a unit of execution. The registry's only KVar
// declarer (rustSpec's const/static Def) requires a `NAME:` type annotation
// and so only ever matches `;`-terminated lines, which cspan.Span already
// refuses — admitting it would change nothing observable and widen the gate
// on speculation.
var reviewedNonSpanKinds = map[graph.NodeKind]string{
	graph.KType: "container kind: calls belong to its members, not to it",
	graph.KVar:  "binding kind: rustSpec's const/static Defs are always ;-terminated",
}

// TestRegistryInvariantC_CallReKindsAreSpanEligible is invariant C, the
// generalization of B-0008 (langspec.go's Parse gated body spans and call
// edges on `KFunc || KMethod`, so a KKernel def — metal/glsl entry points —
// never minted an edge however well its Spec was configured; T-0122
// observed it live on the gap-metal fixture).
//
// The class: a Spec that sets CallRe has declared "scan my bodies for
// calls", but that declaration is only honored for kinds the gate admits.
// Any kind outside the gate is a promise the framework silently drops.
// Asserting it over the registry means the next language to introduce a new
// kind fails here loudly instead of shipping zero edges quietly.
func TestRegistryInvariantC_CallReKindsAreSpanEligible(t *testing.T) {
	for _, s := range registrySpecs(t) {
		s := s
		t.Run(string(s.Lang), func(t *testing.T) {
			if s.CallRe == nil {
				t.Skip("CallRe unset — Parse emits no edges by design, nothing to gate")
			}
			logged := map[graph.NodeKind]bool{}
			for i, d := range s.Defs {
				if spanEligible(d.Kind) {
					continue
				}
				if why, ok := reviewedNonSpanKinds[d.Kind]; ok {
					if !logged[d.Kind] {
						logged[d.Kind] = true
						t.Logf("kind %v declared here is deliberately not span-eligible (%s) "+
							"— see reviewedNonSpanKinds", d.Kind, why)
					}
					continue
				}
				t.Errorf("Defs[%d] declares kind %v while CallRe is set, but %v is "+
					"neither in spanEligibleKinds nor in reviewedNonSpanKinds: defs of "+
					"this kind get no body span and mint no call edges, and nothing else "+
					"reports it (B-0008). Either add the kind to spanEligibleKinds in "+
					"langspec.go or record why it is excluded in reviewedNonSpanKinds. "+
					"Regex: %s", i, d.Kind, d.Kind, d.Re)
			}
		})
	}
}

// TestRegistryInvariantD_CaptureGroupIndicesInRange is invariant D: every
// Def's Name group index is within its own regex's capture groups, and the
// same for Sig when set.
//
// The class: Def.Name and Def.Sig are bare ints into a submatch slice that
// belongs to a regex sitting a few lines away. An index past the regex's
// NumSubexp is guarded at runtime (Parse's `def.Name >= len(m)` skips the
// match), so the failure mode is not a panic — it is the Def silently never
// minting a node, forever, for every file in that language. The 27-language
// hardening waves rewrote most of these regexes by hand, and a rewrite that
// drops or reorders a group leaves the index behind. Nothing else in the
// tree checks the two halves against each other.
func TestRegistryInvariantD_CaptureGroupIndicesInRange(t *testing.T) {
	for _, s := range registrySpecs(t) {
		s := s
		t.Run(string(s.Lang), func(t *testing.T) {
			for i, d := range s.Defs {
				n := d.Re.NumSubexp()
				if d.Name < 1 || d.Name > n {
					t.Errorf("Defs[%d].Name = %d, want 1..%d (the regex's capture-group "+
						"count): out-of-range means Parse skips every match of this Def "+
						"and it mints nothing, silently. Regex: %s",
						i, d.Name, n, d.Re)
				}
				if d.Sig != 0 && (d.Sig < 1 || d.Sig > n) {
					t.Errorf("Defs[%d].Sig = %d, want 0 (no signature) or 1..%d: an "+
						"out-of-range Sig silently yields an empty Node.Sig. Regex: %s",
						i, d.Sig, n, d.Re)
				}
				if d.Sig != 0 && d.Sig == d.Name {
					t.Errorf("Defs[%d] Name and Sig both point at group %d, so Node.Sig "+
						"would duplicate the symbol name. Regex: %s", i, d.Name, d.Re)
				}
			}
		})
	}
}

// TestRegistryInvariantE_SpecIsWellFormed is invariant E: every Spec
// declares a non-empty Lang tag, at least one extension, and at least one
// Def.
//
// The class: each of the three is load-bearing and each fails silently when
// missing. No Exts means index.New's byExt map never routes a file to the
// parser, so the language is registered and dead. No Defs means the parser
// runs over every matching file and mints nothing. An empty Lang tag makes
// ids.Mint produce ":name" NodeIDs that collide across every such language
// at once. A Spec can be wrong in all three ways and every existing test
// still passes, because per-language tests only ever run the specs their
// authors wrote fixtures for.
func TestRegistryInvariantE_SpecIsWellFormed(t *testing.T) {
	for i, s := range registrySpecs(t) {
		s, i := s, i
		name := string(s.Lang)
		if name == "" {
			name = fmt.Sprintf("unnamed-%d", i)
		}
		t.Run(name, func(t *testing.T) {
			if s.Lang == "" {
				t.Error("Lang is empty: ids.Mint would produce \":name\" NodeIDs that " +
					"collide with every other tagless language")
			}
			if len(s.Exts) == 0 {
				t.Error("Exts is empty: index.New's extension map never routes a file " +
					"here, so the Spec is registered but can never parse anything")
			}
			for j, e := range s.Exts {
				if e == "" || e[0] != '.' {
					t.Errorf("Exts[%d] = %q, want a leading-dot suffix such as \".py\" "+
						"(index.New keys on filepath.Ext output)", j, e)
				}
			}
			if len(s.Defs) == 0 {
				t.Error("Defs is empty: the parser would run over every matching file " +
					"and mint no nodes at all")
			}
			for j, d := range s.Defs {
				if d.Re == nil {
					t.Errorf("Defs[%d].Re is nil: Parse would panic on the first line "+
						"of any file in this language", j)
				}
			}
			if s.EndSpan != nil && (s.EndSpan.Open == nil || s.EndSpan.Close == nil) {
				t.Error("EndSpan is set but Open and/or Close is nil: cspan.KeywordSpan " +
					"would panic on the first eligible def")
			}
		})
	}
}

// allowedExtOverlaps records extensions two Specs are known and intended to
// share, keyed by extension. index.New builds a single ext -> parser map, so
// whichever Spec is registered later wins outright and the other is dead
// weight for that extension — intent, if any, has to be written down.
//
// Empty today: the registry has no overlap. An entry here is a claim that
// the overlap is deliberate AND that losing the second parser for that
// extension is acceptable; it is not a way to silence the check.
var allowedExtOverlaps = map[string]string{}

// TestRegistryInvariantF_NoUnintendedExtensionOverlap is invariant F.
//
// The class: registration is last-write-wins through index.New's byExt map,
// and nothing anywhere warns about the loser. Two Specs claiming ".m" (say,
// Objective-C and MATLAB) would leave one language wired up, tested by its
// own per-language fixtures — which call SpecParser directly and so keep
// passing — and completely absent from the real graph. This reports every
// overlap unconditionally so it is visible in `go test -v`, and fails only
// on overlaps nobody has written an intent for.
func TestRegistryInvariantF_NoUnintendedExtensionOverlap(t *testing.T) {
	specs := registrySpecs(t)
	byExt := map[string][]graph.Lang{}
	for _, s := range specs {
		for _, e := range s.Exts {
			byExt[e] = append(byExt[e], s.Lang)
		}
	}
	exts := make([]string, 0, len(byExt))
	for e := range byExt {
		exts = append(exts, e)
	}
	sort.Strings(exts)

	overlaps := 0
	for _, e := range exts {
		langs := byExt[e]
		if len(langs) < 2 {
			continue
		}
		overlaps++
		t.Logf("extension %q is claimed by %v — index.New keeps only the "+
			"last-registered parser for it", e, langs)
		if why, ok := allowedExtOverlaps[e]; ok {
			t.Logf("  ...intended: %s", why)
			continue
		}
		t.Errorf("extension %q claimed by %d Specs (%v) with no recorded intent: "+
			"index.New's ext map keeps exactly one, so the others never parse a "+
			"file even though their own per-language tests (which call SpecParser "+
			"directly) still pass. Give one of them the extension, or record why "+
			"the overlap is deliberate in allowedExtOverlaps.", e, len(langs), langs)
	}
	t.Logf("scanned %d Specs over %d distinct extensions, %d overlapping",
		len(specs), len(exts), overlaps)
}
