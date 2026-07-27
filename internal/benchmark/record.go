// Package benchmark implements the first-class benchmark record type
// (P-01KYJMVX2Q): entries compare implementations against each other on a
// FRAME SYSTEM, keyed uniquely by canonicalized name+frame, with bounded
// per-key version history (default 1 — the latest is what the codebase
// cares about; benchmarks.history raises it).
//
// The store is a keyed last-writer-wins map serialized as ndjson
// (.spectackle/bench.ndjson per context, union-merged like the journal),
// NOT an append-only log: a benchmark is re-measured constantly and its
// identity is its key, never its position.
package benchmark

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// RequiredDims are the frame keys every record must carry (user
// requirement: os/arch/cpu/ram/gpu at minimum). Values may be real host
// facts, or the sentinels: "none" (hardware genuinely absent — no GPU) and
// "any" (dimension irrelevant to this benchmark — machine-independent
// measurements share one key across hosts, ADR-01KYJMWE1N).
var RequiredDims = []string{"os", "arch", "cpu", "ram", "gpu"}

// Metric declares one measured quantity, shared by every Impl in a record.
type Metric struct {
	Name string `json:"n"` // compare join key; folded [a-z0-9._/-]+
	// Unit is free-form ("ns/op", "ops/s", "B", "token/s") and is
	// byte-compared on cmp — NEVER converted or summed across units.
	Unit string `json:"u"`
	// Dir is the comparability direction: "+" higher is better, "-" lower
	// is better, "~" diagnostic (no better/worse verdicts).
	Dir string `json:"d"`
	// Noise is the absolute jitter floor: deltas within ±Noise render "~"
	// (tie) instead of a direction verdict. 0 = every delta counts.
	Noise float64 `json:"nz,omitempty"`
}

// Impl is one implementation's measured results within a record.
type Impl struct {
	Label string             `json:"l"`             // "go", "python", "cuda-v2"
	Src   string             `json:"src,omitempty"` // provenance: commit, package, variant note
	Res   map[string]float64 `json:"res"`           // metric name -> value
}

// Record is one retained version of one benchmark entry.
type Record struct {
	ID      string            `json:"id"`   // "M-" + record ID, minted fresh per version
	Name    string            `json:"name"` // verbatim benchmark name (folded only inside Key)
	Key     string            `json:"key"`  // CanonicalKey(Name, Frame) — stored AND verified at load
	Ver     int               `json:"ver"`  // 1-based, monotonic per Key
	Frame   map[string]string `json:"frame"`
	Metrics []Metric          `json:"metrics"` // declaration order = render order
	Impls   []Impl            `json:"impls"`   // order = presentation order
	T       time.Time         `json:"t"`
	Ag      string            `json:"ag,omitempty"`   // producing agent
	Tool    string            `json:"tool,omitempty"` // producing harness/fixture
	Note    string            `json:"note,omitempty"`
	Dir     string            `json:"-"` // context dir, backfilled at load
}

// forbidden characters inside names, dim keys, dim values, metric names
// and impl labels: they are the grammar's own separators.
const forbidden = "=|;:"

// foldToken lowercases and trims one token.
func foldToken(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// validToken refuses empty tokens, separator characters and inner spaces.
func validToken(s string) error {
	if s == "" {
		return fmt.Errorf("benchmark: empty token")
	}
	if strings.ContainsAny(s, forbidden) {
		return fmt.Errorf("benchmark: token %q carries a separator character (%s)", s, forbidden)
	}
	if strings.ContainsAny(s, " \t\n\r") {
		return fmt.Errorf("benchmark: token %q carries whitespace", s)
	}
	return nil
}

// CanonicalKey derives the unique key from a benchmark name and its frame:
// folded name, then the folded dims as k=v joined sorted by key with "|".
// Deterministic across insertion order; every RequiredDim must be present
// (sentinels are legal values); forbidden separator characters refuse.
func CanonicalKey(name string, frame map[string]string) (string, error) {
	n := foldToken(name)
	if err := validToken(strings.ReplaceAll(n, " ", "-")); err != nil {
		return "", err
	}
	if strings.ContainsAny(n, forbidden) {
		return "", fmt.Errorf("benchmark: name %q carries a separator character", name)
	}
	for _, d := range RequiredDims {
		if _, ok := frame[d]; !ok {
			return "", fmt.Errorf("benchmark: frame is missing the required dim %q (use none for absent hardware, any for an irrelevant dim)", d)
		}
	}
	dims := make([]string, 0, len(frame))
	for k, v := range frame {
		fk, fv := foldToken(k), foldToken(v)
		if err := validToken(fk); err != nil {
			return "", fmt.Errorf("benchmark: dim key: %w", err)
		}
		if fv == "" {
			return "", fmt.Errorf("benchmark: dim %q has an empty value", k)
		}
		if strings.ContainsAny(fv, forbidden) {
			return "", fmt.Errorf("benchmark: dim %q value %q carries a separator character", k, v)
		}
		dims = append(dims, fk+"="+strings.ReplaceAll(fv, " ", "-"))
	}
	sort.Strings(dims)
	return strings.ReplaceAll(n, " ", "-") + "|" + strings.Join(dims, "|"), nil
}

// CanonicalFrame returns the folded copy of a frame, the form Records
// store — key derivation and storage must agree byte-for-byte.
func CanonicalFrame(frame map[string]string) map[string]string {
	out := make(map[string]string, len(frame))
	for k, v := range frame {
		out[foldToken(k)] = strings.ReplaceAll(foldToken(v), " ", "-")
	}
	return out
}

// Validate checks a record's internal consistency: key matches its
// name+frame, metrics well-formed with legal directions, impl results
// reference declared metrics only.
func (r Record) Validate() error {
	key, err := CanonicalKey(r.Name, r.Frame)
	if err != nil {
		return err
	}
	if key != r.Key {
		return fmt.Errorf("benchmark: stored key %q does not match name+frame (%q)", r.Key, key)
	}
	if len(r.Metrics) == 0 {
		return fmt.Errorf("benchmark: record %s declares no metrics", r.ID)
	}
	seen := map[string]bool{}
	for _, m := range r.Metrics {
		if err := validToken(m.Name); err != nil {
			return fmt.Errorf("benchmark: metric name: %w", err)
		}
		if m.Unit == "" {
			return fmt.Errorf("benchmark: metric %q has no unit", m.Name)
		}
		switch m.Dir {
		case "+", "-", "~":
		default:
			return fmt.Errorf("benchmark: metric %q direction %q (want +, - or ~)", m.Name, m.Dir)
		}
		if seen[m.Name] {
			return fmt.Errorf("benchmark: metric %q declared twice", m.Name)
		}
		seen[m.Name] = true
	}
	if len(r.Impls) == 0 {
		return fmt.Errorf("benchmark: record %s carries no implementations", r.ID)
	}
	labels := map[string]bool{}
	for _, im := range r.Impls {
		if err := validToken(im.Label); err != nil {
			return fmt.Errorf("benchmark: impl label: %w", err)
		}
		if labels[im.Label] {
			return fmt.Errorf("benchmark: impl %q listed twice", im.Label)
		}
		labels[im.Label] = true
		for mn := range im.Res {
			if !seen[mn] {
				return fmt.Errorf("benchmark: impl %q reports undeclared metric %q", im.Label, mn)
			}
		}
	}
	return nil
}

// SameContent reports whether two records measure identically — the
// idempotent-replay check: key, metrics, impls and values equal (ID, Ver,
// T, Ag, Tool, Note excluded — provenance may differ on a re-put).
func SameContent(a, b Record) bool {
	if a.Key != b.Key || len(a.Metrics) != len(b.Metrics) || len(a.Impls) != len(b.Impls) {
		return false
	}
	for i := range a.Metrics {
		if a.Metrics[i] != b.Metrics[i] {
			return false
		}
	}
	for i := range a.Impls {
		if a.Impls[i].Label != b.Impls[i].Label || a.Impls[i].Src != b.Impls[i].Src ||
			len(a.Impls[i].Res) != len(b.Impls[i].Res) {
			return false
		}
		for k, v := range a.Impls[i].Res {
			if bv, ok := b.Impls[i].Res[k]; !ok || bv != v {
				return false
			}
		}
	}
	return true
}
