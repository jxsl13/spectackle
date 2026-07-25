// This file holds parser-set invariants: properties asserted over every
// parser the server actually assembles, rather than over one parser's
// fixture. It lives in package index_test rather than package index because
// it imports internal/langspec, which imports internal/index — a legal
// dependency only for an external test package.
//
// The set under test mirrors internal/mcpserver.BuildGraph's parsers slice
// (the single place both the server's reindex and the `spectackle reindex`
// CLI subcommand build their parser list): the three hand-written parsers,
// then every langspec-registered Spec. Adding a language is a data file in
// internal/langspec and is picked up here automatically; adding a
// hand-written parser means adding it to serverParsers below, which is the
// same edit BuildGraph needs.
package index_test

import (
	"fmt"
	"sort"
	"testing"

	"github.com/jxsl13/spectackle/internal/index"
	"github.com/jxsl13/spectackle/internal/langspec"
)

// serverParsers is the parser set internal/mcpserver.BuildGraph wires into
// index.New, reproduced here so the invariants below run over the real
// production set instead of a hand-picked sample.
func serverParsers() []index.LanguageParser {
	return append(
		[]index.LanguageParser{index.GoParser{}, index.AsmParser{}, index.CudaParser{}},
		langspec.All()...)
}

// parserName labels a parser for a subtest path: the language tag, plus a
// disambiguating ordinal if two parsers ever claim the same tag.
func parserName(p index.LanguageParser, i int) string {
	l := string(p.Lang())
	if l == "" {
		return fmt.Sprintf("unnamed-%d", i)
	}
	return l
}

// TestParsersInvariantA_AllImplementCacheVersioner is invariant A, the
// generalization of B-0007 (the parse-blob cache was keyed by content hash
// alone, so a parser upgrade kept serving pre-upgrade nodes and edges for
// every unchanged file until somebody hand-cleared the cache).
//
// The class: T-0127 fixed that by mixing a parser-identity component into
// the key — but only for parsers that opt in. index.parseCacheKey falls back
// to plain sha256.Sum256(src) for any parser that does not implement
// CacheVersioner, and that fallback is deliberate and untyped: it is not a
// compile error, it is not a runtime error, and the only symptom is stale
// results months later. So the opt-in has to be asserted from outside. A
// parser added next month without a CacheVersion is B-0007 again, silently,
// for that one language.
func TestParsersInvariantA_AllImplementCacheVersioner(t *testing.T) {
	parsers := serverParsers()
	if len(parsers) == 0 {
		t.Fatal("no parsers assembled — this invariant would vacuously pass")
	}
	for i, p := range parsers {
		p, i := p, i
		t.Run(parserName(p, i), func(t *testing.T) {
			cv, ok := p.(index.CacheVersioner)
			if !ok {
				t.Fatalf("%T does not implement index.CacheVersioner: its parse blobs "+
					"key on source bytes alone, so every upgrade to it keeps serving "+
					"pre-upgrade nodes and edges for unchanged files until the cache is "+
					"hand-cleared (B-0007). A langspec.Spec gets this for free via "+
					"SpecParser.CacheVersion; a hand-written parser needs a "+
					"CacheVersion() string method plus a bumpable constant (see "+
					"index.GoParser).", p)
			}
			if cv.CacheVersion() == "" {
				t.Errorf("%T.CacheVersion() is empty: parseCacheKey would mix in nothing, "+
					"which is the content-only keying this invariant exists to prevent", p)
			}
		})
	}
}

// TestParsersInvariantB_CacheVersionsAreDistinct is invariant B, the other
// half of the same B-0007 class.
//
// The class: the parse-blob key is a hash of (source bytes, parser version).
// Two parsers reporting the same version therefore produce the same key for
// the same bytes — so one language's cached nodes and edges satisfy the
// other language's lookup. That needs no identical file to bite: a shared
// header, a vendored snippet, or any file two parsers are both asked about
// is enough, and the result is a graph carrying another language's symbols.
// Nothing else in the tree compares versions across parsers; a copy-pasted
// `const fooParserCacheVersion = "go-2"` in a new parser would ship green.
func TestParsersInvariantB_CacheVersionsAreDistinct(t *testing.T) {
	parsers := serverParsers()
	seen := map[string][]string{}
	for i, p := range parsers {
		cv, ok := p.(index.CacheVersioner)
		if !ok {
			// Invariant A owns this failure; skip so B reports only collisions.
			continue
		}
		v := cv.CacheVersion()
		seen[v] = append(seen[v], fmt.Sprintf("%s (%T)", parserName(p, i), p))
	}

	versions := make([]string, 0, len(seen))
	for v := range seen {
		versions = append(versions, v)
	}
	sort.Strings(versions)

	for _, v := range versions {
		owners := seen[v]
		if len(owners) < 2 {
			continue
		}
		t.Errorf("CacheVersion %q is reported by %d parsers (%v): parse blobs are keyed "+
			"on (source bytes, CacheVersion), so these parsers share a cache key for "+
			"identical bytes and one language's cached nodes and edges can satisfy "+
			"another's lookup (B-0007's identity class). Give each parser a version "+
			"unique to it — langspec.SpecParser derives one from the Spec itself; a "+
			"hand-written parser needs its own constant.", v, len(owners), owners)
	}
	t.Logf("%d parsers, %d distinct CacheVersions", len(parsers), len(versions))
}
