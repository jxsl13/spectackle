package evidence

import (
	"strings"
	"testing"
)

// The calibration fixture the brief mandates: real excerpts of the
// gate/rule-inline twin — the duplication that survived every prose review
// in this repository's history. Copied, not read from tools.go: the twin
// is scheduled for de-duplication and the fixture must survive it.
const gateExcerpt = `
	return func(_ context.Context, _ *mcp.CallToolRequest, in T) (*mcp.CallToolResult, any, error) {
		s.inFlight.Add(1)
		defer s.inFlight.Add(-1)
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.preCall(); err != nil {
			return nil, nil, err
		}
		finish := s.beginEdgeCapture()
		res, out, err := h(in)
		finish(err != nil || (res != nil && res.IsError))
		return s.postCall(res), out, err
	}`

const ruleTwinExcerpt = `
	func(ctx context.Context, req *mcp.CallToolRequest, in ruleIn) (*mcp.CallToolResult, any, error) {
		s.inFlight.Add(1)
		defer s.inFlight.Add(-1)
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.preCall(); err != nil {
			return nil, nil, err
		}
		finish := s.beginEdgeCapture()
		res, out, err := s.rule(ctx, req, in)
		finish(err != nil || (res != nil && res.IsError))
		return s.postCall(res), out, err
	}`

const unrelatedExcerpt = `
	func snapshotHead(ctx context.Context, repoDir, head string) (string, error) {
		snap, err := os.MkdirTemp("", "spx-selfbuild-")
		if err != nil {
			return "", err
		}
		tarPath := filepath.Join(snap, "head.tar")
		if out, err := exec.CommandContext(ctx, "git", "-C", repoDir, "archive").CombinedOutput(); err != nil {
			os.RemoveAll(snap)
			return "", fmt.Errorf("git archive: %v: %s", err, out)
		}
		return snap, nil
	}`

// RED-RUN (T-01KYD9R, written first): the proving twin scores above the
// duplication threshold; an unrelated pair scores below.
func TestDupCalibrationPair(t *testing.T) {
	a := Fingerprint([]byte(gateExcerpt), true)
	b := Fingerprint([]byte(ruleTwinExcerpt), true)
	c := Fingerprint([]byte(unrelatedExcerpt), true)
	if sim := Similarity(a, b); sim < DupThreshold {
		t.Fatalf("the twin pair scores %.2f, below the %.2f threshold — the proving example must trip", sim, DupThreshold)
	}
	if sim := Similarity(a, c); sim >= DupThreshold {
		t.Fatalf("unrelated pair scores %.2f, at or above threshold", sim)
	}
}

// Intra-set duplication is caught; test-vs-production isolation holds both
// ways; determinism and caps hold; the generated marker excludes.
func TestDupProperties(t *testing.T) {
	body := gateExcerpt
	a := IndexedNode{ID: "go:a.One", File: "a/x.go", Print: Fingerprint([]byte(body), true)}
	b := IndexedNode{ID: "go:b.Two", File: "b/y.go", Print: Fingerprint([]byte(body), true)}
	tIdx := IndexedNode{ID: "go:c_test.Three", File: "c/x_test.go", Test: true, Print: Fingerprint([]byte(body), true)}
	index := []IndexedNode{a, b, tIdx}
	recs := Duplicates([]IndexedNode{a}, index)
	if len(recs) != 1 || !contains(recs[0], "go:a.One ~= go:b.Two") {
		t.Fatalf("intra-set duplicate missed or misattributed: %v", recs)
	}
	// production node must never match the test twin
	for _, r := range recs {
		if contains(r, "c_test.Three") {
			t.Fatalf("production matched a test node: %v", recs)
		}
	}
	// test node matches nothing (its only twin is production)
	if recs := Duplicates([]IndexedNode{tIdx}, index); len(recs) != 0 {
		t.Fatalf("test node crossed the isolation line: %v", recs)
	}
	// determinism
	x := Duplicates([]IndexedNode{a}, index)
	y := Duplicates([]IndexedNode{a}, index)
	if len(x) != len(y) || x[0] != y[0] {
		t.Fatal("nondeterministic dup output")
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && strings.Contains(s, sub) }
