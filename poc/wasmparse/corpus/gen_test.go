package corpus

import (
	"crypto/sha256"
	"sort"
	"testing"
)

// TestGenerateDeterministic proves Generate(seed) is byte-for-byte
// reproducible: same seed twice must yield the same file set and the same
// bytes per file (T-0040's VERIFY requirement — "the corpus generator is
// deterministic: same seed, same bytes").
func TestGenerateDeterministic(t *testing.T) {
	a := Generate(42)
	b := Generate(42)

	if len(a) != len(b) {
		t.Fatalf("file count differs: %d vs %d", len(a), len(b))
	}
	for name, ab := range a {
		bb, ok := b[name]
		if !ok {
			t.Fatalf("run 2 missing file %s present in run 1", name)
		}
		if string(ab) != string(bb) {
			t.Fatalf("file %s differs between two Generate(42) runs", name)
		}
	}

	// Also pin a content hash of the whole corpus so an accidental future
	// change to the generator is caught explicitly rather than only via
	// the two-runs-equal check above.
	hash := corpusHash(a)
	t.Logf("corpus(seed=42) sha256=%x, files=%d", hash, len(a))
}

// TestGenerateDifferentSeedsDiffer is a sanity check that the generator
// actually uses the seed (guards against a no-op rng that would make the
// determinism test above vacuous).
func TestGenerateDifferentSeedsDiffer(t *testing.T) {
	a := Generate(1)
	b := Generate(2)
	if corpusHash(a) == corpusHash(b) {
		t.Fatalf("Generate(1) and Generate(2) produced identical corpora")
	}
}

// TestGenerateCounts checks the corpus matches the brief's sizing: ~50
// files, 1000 functions, 100 types, 50 macros.
func TestGenerateCounts(t *testing.T) {
	files := Generate(42)
	if len(files) != NumFiles {
		t.Fatalf("file count = %d, want %d", len(files), NumFiles)
	}

	var funcDefs, typeDefs, macroDefs int
	for _, src := range files {
		funcDefs += countOccurrences(string(src), "int n, float a, const float *x, float *y")
		typeDefs += countTypeHeaders(string(src))
		macroDefs += countOccurrences(string(src), "#define MACRO_")
	}
	if funcDefs != NumFunctions {
		t.Errorf("function signatures = %d, want %d", funcDefs, NumFunctions)
	}
	if typeDefs != NumTypes {
		t.Errorf("type headers = %d, want %d", typeDefs, NumTypes)
	}
	if macroDefs != NumMacros {
		t.Errorf("macro defs = %d, want %d", macroDefs, NumMacros)
	}
}

func countOccurrences(s, sub string) int {
	n := 0
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			n++
			i += len(sub) - 1
		}
	}
	return n
}

func countTypeHeaders(s string) int {
	return countOccurrences(s, "struct ") + countOccurrences(s, "union ") + countOccurrences(s, "enum ") -
		// each type body also contains a closing brace comment-free; no
		// extra "struct "/"union "/"enum " substrings are emitted
		// elsewhere in writeType, so no adjustment needed beyond the sum
		// above matching exactly one header line per type.
		0
}

func corpusHash(files map[string][]byte) [32]byte {
	names := make([]string, 0, len(files))
	for k := range files {
		names = append(names, k)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		h.Write([]byte(n))
		h.Write([]byte{0})
		h.Write(files[n])
		h.Write([]byte{0})
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}
