package evidence

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"go/scanner"
	"go/token"
	"sort"
	"strings"

	"github.com/jxsl13/spectackle/internal/graph"
)

// DupThreshold marks a pair redundant: at 0.85 shingle-Jaccard the two
// blocks share their structure with only local renames — the proving
// gate/rule twin scores well above, unrelated same-idiom code well below
// (calibrated by the fixture test, T-01KYD9R).
const DupThreshold = 0.85

// minDupTokens is the significant-token floor: below 15 tokens a match is
// idiom (an error check, a loop header), not redundancy worth a reviewer's
// attention.
const minDupTokens = 15

// shingleSize is the token-window width for fingerprints: 5 tokens carries
// enough structure to separate idiom from copied logic without making
// single-token edits shatter the match.
const shingleSize = 5

// byteShingleSize is the fallback window for non-Go spans: 32
// whitespace-normalized bytes approximates the 5-token window's
// discriminative power on C-family kernel code.
const byteShingleSize = 32

// detectorVersion salts fingerprint caches: bump on any change to
// normalization, shingle sizes, or hashing, so stale prints invalidate
// (the T-0127 cache discipline).
const detectorVersion = 1

// Print is a fingerprint: the shingle-hash set plus its significant-token
// count.
type Print struct {
	Shingles map[uint64]struct{}
	Tokens   int
}

// Fingerprint normalizes a span and shingles it. Go spans normalize via
// go/scanner with identifiers and basic literals replaced by kind
// placeholders (type-2 clones: renamed copies still match); non-Go spans
// fall back to whitespace-normalized byte shingles.
func Fingerprint(src []byte, isGo bool) Print {
	if isGo {
		return goFingerprint(src)
	}
	return byteFingerprint(src)
}

func goFingerprint(src []byte) Print {
	var sc scanner.Scanner
	fset := token.NewFileSet()
	f := fset.AddFile("", fset.Base(), len(src))
	sc.Init(f, src, nil, 0)
	var toks []string
	for {
		_, tok, _ := sc.Scan()
		if tok == token.EOF {
			break
		}
		switch {
		case tok == token.IDENT:
			toks = append(toks, "I")
		case tok.IsLiteral():
			toks = append(toks, "L")
		case tok == token.SEMICOLON:
			// auto-inserted semicolons are formatting, not structure
		default:
			toks = append(toks, tok.String())
		}
	}
	p := Print{Shingles: map[uint64]struct{}{}, Tokens: len(toks)}
	for i := 0; i+shingleSize <= len(toks); i++ {
		h := sha256.Sum256([]byte(strings.Join(toks[i:i+shingleSize], " ")))
		p.Shingles[binary.BigEndian.Uint64(h[:8])] = struct{}{}
	}
	return p
}

func byteFingerprint(src []byte) Print {
	norm := strings.Join(strings.Fields(string(src)), " ")
	p := Print{Shingles: map[uint64]struct{}{}, Tokens: len(norm) / 4}
	for i := 0; i+byteShingleSize <= len(norm); i += 4 {
		h := sha256.Sum256([]byte(norm[i : i+byteShingleSize]))
		p.Shingles[binary.BigEndian.Uint64(h[:8])] = struct{}{}
	}
	return p
}

// Similarity is shingle-set Jaccard.
func Similarity(a, b Print) float64 {
	if len(a.Shingles) == 0 || len(b.Shingles) == 0 {
		return 0
	}
	inter := 0
	for s := range a.Shingles {
		if _, ok := b.Shingles[s]; ok {
			inter++
		}
	}
	union := len(a.Shingles) + len(b.Shingles) - inter
	return float64(inter) / float64(union)
}

// dupIndexCeiling bounds index construction: past 2000 function nodes the
// pack reports truncation instead of blowing the SPX-MCP-001 read budget.
const dupIndexCeiling = 2000

// generatedMarker excludes generated files from both sides — their
// duplication is a generator's business, not a reviewer's.
const generatedMarker = "DO NOT EDIT"

// IndexedNode pairs a node with its fingerprint.
type IndexedNode struct {
	ID    graph.NodeID
	File  string
	Test  bool
	Print Print
}

// BuildDupIndex fingerprints every function-kind node's span, reading
// spans only (never whole files repeatedly — spans come from one read per
// file). load returns nil to skip a file (unreadable or generated).
func BuildDupIndex(g graph.Graph, load func(path string) []byte) ([]IndexedNode, bool) {
	nodes := g.Find("", 1<<20, graph.KFunc)
	if len(nodes) > dupIndexCeiling {
		nodes = nodes[:dupIndexCeiling]
	}
	truncated := len(g.Find("", 1<<20, graph.KFunc)) > dupIndexCeiling
	fileCache := map[string][]byte{}
	var out []IndexedNode
	for _, n := range nodes {
		if n.EndLine <= n.Line {
			continue
		}
		src, ok := fileCache[n.File]
		if !ok {
			src = load(n.File)
			fileCache[n.File] = src
		}
		if src == nil || strings.Contains(string(src[:min(len(src), 2048)]), generatedMarker) {
			continue
		}
		span := spanBytes(src, n.Line, n.EndLine)
		if span == nil {
			continue
		}
		p := Fingerprint(span, strings.HasSuffix(n.File, ".go"))
		if p.Tokens < minDupTokens {
			continue
		}
		out = append(out, IndexedNode{
			ID: n.ID, File: n.File,
			Test:  strings.HasSuffix(n.File, "_test.go"),
			Print: p,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, truncated
}

func spanBytes(src []byte, start, end int) []byte {
	lines := strings.Split(string(src), "\n")
	if start < 1 || end > len(lines) {
		return nil
	}
	return []byte(strings.Join(lines[start-1:end], "\n"))
}

// Duplicates reports, for each changed node, its best index match at or
// above DupThreshold — one match per node (the best; more is noise), test
// against test and production against production only (fixture boilerplate
// across tests is a deliberate idiom, not redundancy). Bucketed by shared
// shingle so the comparison is candidates-only, never all-pairs.
func Duplicates(changed []IndexedNode, index []IndexedNode) []string {
	buckets := map[uint64][]int{}
	for i, n := range index {
		for s := range n.Print.Shingles {
			buckets[s] = append(buckets[s], i)
		}
	}
	var out []string
	for _, c := range changed {
		bestSim := 0.0
		bestID := graph.NodeID("")
		cand := map[int]struct{}{}
		for s := range c.Print.Shingles {
			for _, i := range buckets[s] {
				cand[i] = struct{}{}
			}
		}
		for i := range cand {
			n := index[i]
			if n.ID == c.ID || n.Test != c.Test {
				continue
			}
			if sim := Similarity(c.Print, n.Print); sim > bestSim {
				bestSim, bestID = sim, n.ID
			}
		}
		if bestSim >= DupThreshold {
			out = append(out, fmt.Sprintf("v dup %s ~= %s %d%%", c.ID, bestID, int(bestSim*100)))
		}
	}
	sort.Strings(out)
	if len(out) > recordCap {
		n := len(out) - recordCap
		out = append(out[:recordCap], fmt.Sprintf("v dup +%d more", n))
	}
	return out
}
