package mcpserver

// The bench tool (P-01KYJMVX2Q): first-class benchmark records over
// internal/benchmark. Dense k=v / colon grammars in, m/f/u/d line records
// out — never JSON on either surface. Every put journals its delta against
// the outgoing head INCLUDING the superseded raw values (ADR-01KYJMWEWQ),
// so regressions survive the depth-1 trim.

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectackle/internal/benchmark"
	"github.com/jxsl13/spectackle/internal/budget"
	"github.com/jxsl13/spectackle/internal/ids"
	"github.com/jxsl13/spectackle/internal/journal"
)

type benchIn struct {
	Op      string `json:"op" jsonschema:"put|get|ls|rm|cmp"`
	Name    string `json:"name,omitempty" jsonschema:"benchmark name (put: required; ls: filter)"`
	Frame   string `json:"frame,omitempty" jsonschema:"dims as k=v, space/comma-separated; put requires os arch cpu ram gpu (value none: absent, any: irrelevant); ls: subset filter"`
	Results string `json:"results,omitempty" jsonschema:"put: impl[@src]: metric=value ...; entries ;-separated"`
	Metrics string `json:"metrics,omitempty" jsonschema:"declarations name:unit:dir[:noise] space/comma-separated; dir +|-|~; omitted = inherit the prior version"`
	ID      string `json:"id,omitempty" jsonschema:"M- record ID, short prefix ok (get/rm/cmp)"`
	ID2     string `json:"id2,omitempty" jsonschema:"cmp: second entry"`
	Tool    string `json:"tool,omitempty" jsonschema:"put: producing harness/fixture id"`
	Note    string `json:"note,omitempty" jsonschema:"put: freeform (batch, methodology)"`
	Dir     string `json:"dir,omitempty" jsonschema:"context dir; default root"`
	Budget  int    `json:"budget,omitempty" jsonschema:"ls: token budget, default 2000"`
	Cur     string `json:"cur,omitempty" jsonschema:"ls: resume cursor"`
}

// parseFrame reads "k=v k=v" (space or comma separated).
func parseFrame(s string) (map[string]string, error) {
	out := map[string]string{}
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' }) {
		k, v, ok := strings.Cut(tok, "=")
		if !ok || k == "" || v == "" {
			return nil, fmt.Errorf("frame token %q is not k=v", tok)
		}
		out[k] = v
	}
	return out, nil
}

// parseMetrics reads "name:unit:dir[:noise]" tokens.
func parseMetrics(s string) ([]benchmark.Metric, error) {
	var out []benchmark.Metric
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' }) {
		parts := strings.Split(tok, ":")
		if len(parts) < 3 || len(parts) > 4 {
			return nil, fmt.Errorf("metric %q is not name:unit:dir[:noise]", tok)
		}
		m := benchmark.Metric{Name: parts[0], Unit: parts[1], Dir: parts[2]}
		if len(parts) == 4 {
			nz, err := strconv.ParseFloat(parts[3], 64)
			if err != nil {
				return nil, fmt.Errorf("metric %q noise: %v", tok, err)
			}
			m.Noise = nz
		}
		out = append(out, m)
	}
	return out, nil
}

// parseResults reads "label[@src]: metric=value ..." entries, ;-separated.
func parseResults(s string) ([]benchmark.Impl, error) {
	var out []benchmark.Impl
	for _, entry := range strings.Split(s, ";") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		head, rest, ok := strings.Cut(entry, ":")
		if !ok {
			return nil, fmt.Errorf("results entry %q lacks the label: prefix", entry)
		}
		im := benchmark.Impl{Res: map[string]float64{}}
		if label, src, hasSrc := strings.Cut(strings.TrimSpace(head), "@"); hasSrc {
			im.Label, im.Src = label, src
		} else {
			im.Label = strings.TrimSpace(head)
		}
		for _, kv := range strings.Fields(rest) {
			mn, mv, ok := strings.Cut(kv, "=")
			if !ok {
				return nil, fmt.Errorf("result %q is not metric=value", kv)
			}
			v, err := strconv.ParseFloat(mv, 64)
			if err != nil {
				return nil, fmt.Errorf("result %q: %v", kv, err)
			}
			im.Res[mn] = v
		}
		if len(im.Res) == 0 {
			return nil, fmt.Errorf("results entry %q carries no metric=value pairs", entry)
		}
		out = append(out, im)
	}
	return out, nil
}

func (s *Server) bench(in benchIn) (*mcp.CallToolResult, any, error) {
	defer s.markDirty()
	path := s.ws.BenchPath(dirOf(in.Dir))
	st, err := benchmark.Load(path)
	if err != nil {
		return nil, nil, err
	}
	switch in.Op {
	case "put":
		return s.benchPut(in, st, path)
	case "get":
		return s.benchGet(in, st)
	case "ls":
		return s.benchLs(in, st)
	case "rm":
		return s.benchRm(in, st, path)
	case "cmp":
		return s.benchCmp(in, st)
	}
	return refuse("! ARG E - op must be put|get|ls|rm|cmp")
}

// benchResolve finds a record by (possibly short) M- ID. Ambiguity and
// no-match are DISTINCT errors (cross-val finding: a prefix matching two
// records must not read as "no benchmark record") — ids.ResolveRecordID
// names every candidate on ambiguity.
func benchResolve(st *benchmark.Store, id string) (benchmark.Record, error) {
	var known []string
	for _, key := range st.Keys() {
		for _, r := range st.Versions(key) {
			known = append(known, r.ID)
		}
	}
	full, err := ids.ResolveRecordID(id, known)
	if err != nil {
		return benchmark.Record{}, err
	}
	r, _ := st.ByID(full)
	return r, nil
}

func (s *Server) benchPut(in benchIn, st *benchmark.Store, path string) (*mcp.CallToolResult, any, error) {
	if in.Name == "" || in.Frame == "" || in.Results == "" {
		return refuse("! ARG E - put needs name, frame (os arch cpu ram gpu; none/any legal) and results (impl: metric=value ...)")
	}
	frame, err := parseFrame(in.Frame)
	if err != nil {
		return refuse("! ARG E - " + err.Error())
	}
	impls, err := parseResults(in.Results)
	if err != nil {
		return refuse("! ARG E - " + err.Error())
	}
	key, err := benchmark.CanonicalKey(in.Name, frame)
	if err != nil {
		return refuse("! ARG E - " + err.Error())
	}
	var metrics []benchmark.Metric
	if in.Metrics != "" {
		if metrics, err = parseMetrics(in.Metrics); err != nil {
			return refuse("! ARG E - " + err.Error())
		}
	} else if head, ok := st.Head(key); ok {
		metrics = head.Metrics // inherit the prior declaration
	} else {
		return refuse("! ARG E - first put for this key needs metrics declarations (name:unit:dir[:noise])")
	}
	if s.wtItem == "" {
		if res, out, err := s.blockedByLease([]string{dirOf(in.Dir)}); res != nil || err != nil {
			return res, out, err
		}
	}
	rec := benchmark.Record{
		ID: "M-" + ids.MintRecordID().String(), Name: in.Name, Key: key,
		Frame: benchmark.CanonicalFrame(frame), Metrics: metrics, Impls: impls,
		T: time.Now().UTC().Truncate(time.Second), Ag: s.agent, Tool: in.Tool, Note: in.Note,
	}
	stored, prev, changed, err := st.Put(rec, s.ws.Cfg.Benchmarks.History)
	if err != nil {
		return refuse("! ARG E - " + err.Error())
	}
	sid := shortDisplayID(stored.ID)
	if !changed {
		return text(fmt.Sprintf("ok m %s v%d %s unchanged\n", sid, stored.Ver, stored.Name))
	}
	if err := st.Save(path); err != nil {
		return nil, nil, err
	}
	var b strings.Builder
	better, worse, tie := 0, 0, 0
	var deltaLines, rawPrev []string
	if prev != nil {
		for _, m := range stored.Metrics {
			for _, im := range stored.Impls {
				nv, ok := im.Res[m.Name]
				if !ok {
					continue
				}
				pv, ok := prevValue(*prev, im.Label, m.Name)
				if !ok {
					continue
				}
				d := nv - pv
				verdict, mark := deltaVerdict(m, d)
				switch verdict {
				case "better":
					better++
				case "worse":
					worse++
				default:
					tie++
				}
				line := fmt.Sprintf("d %s %s %g %s Δ%+g%s", im.Label, m.Name, nv, m.Unit, d, mark)
				deltaLines = append(deltaLines, line)
				rawPrev = append(rawPrev, fmt.Sprintf("%s %s=%g", im.Label, m.Name, pv))
			}
		}
		for _, l := range deltaLines {
			b.WriteString(l + "\n")
		}
	}
	summary := fmt.Sprintf("ok m %s v%d %s impls=%d metrics=%d", sid, stored.Ver, stored.Name, len(stored.Impls), len(stored.Metrics))
	if prev != nil {
		summary += fmt.Sprintf(" better=%d worse=%d tie=%d trimmed=%d", better, worse, tie, prev.Ver)
	} else {
		summary += " new"
	}
	b.WriteString(summary + "\n")
	// The put delta is journaled WITH the superseded raw values
	// (ADR-01KYJMWEWQ: the user chose full regression forensics at depth
	// 1); EvBench rides the compaction keep-list verbatim.
	ev := journal.Event{
		Ev: journal.EvBench, ID: stored.ID, K: "bench", Ti: stored.Name,
		Dir: dirOf(in.Dir), Note: stored.Key,
		Sum: strings.Join(deltaLines, "; "),
	}
	if prev != nil {
		ev.Body = fmt.Sprintf("superseded v%d (%s): %s", prev.Ver, prev.ID, strings.Join(rawPrev, "; "))
	}
	if err := journal.Append(s.ws, dirOf(in.Dir), ev); err != nil {
		return nil, nil, err
	}
	return text(b.String())
}

func prevValue(prev benchmark.Record, label, metric string) (float64, bool) {
	for _, im := range prev.Impls {
		if im.Label == label {
			v, ok := im.Res[metric]
			return v, ok
		}
	}
	return 0, false
}

// deltaVerdict classifies a delta under the metric's direction and noise:
// "" mark for ties within noise ("~"), " better"/" worse" otherwise.
func deltaVerdict(m benchmark.Metric, d float64) (verdict, mark string) {
	if m.Dir == "~" {
		return "tie", ""
	}
	abs := d
	if abs < 0 {
		abs = -abs
	}
	if abs <= m.Noise {
		return "tie", " ~"
	}
	improved := (m.Dir == "+" && d > 0) || (m.Dir == "-" && d < 0)
	if improved {
		return "better", " better"
	}
	return "worse", " worse"
}

func (s *Server) benchGet(in benchIn, st *benchmark.Store) (*mcp.CallToolResult, any, error) {
	if in.ID == "" {
		return refuse("! ARG E - get needs id (M-...)")
	}
	r, err := benchResolve(st, in.ID)
	if err != nil {
		return refuse("! ARG E - " + err.Error())
	}
	var b strings.Builder
	fmt.Fprintf(&b, "m %s v%d %s %s", shortDisplayID(r.ID), r.Ver, r.Name, r.T.Format("2006-01-02"))
	if r.Tool != "" {
		b.WriteString(" tool=" + r.Tool)
	}
	b.WriteString("\n")
	var dims []string
	for k, v := range r.Frame {
		dims = append(dims, k+"="+v)
	}
	sort.Strings(dims)
	b.WriteString("f " + strings.Join(dims, " ") + "\n")
	for _, m := range r.Metrics {
		winner, winVal := "", 0.0
		if m.Dir != "~" && len(r.Impls) > 1 {
			for _, im := range r.Impls {
				v, ok := im.Res[m.Name]
				if !ok {
					continue
				}
				if winner == "" || (m.Dir == "+" && v > winVal) || (m.Dir == "-" && v < winVal) {
					winner, winVal = im.Label, v
				}
			}
		}
		for _, im := range r.Impls {
			v, ok := im.Res[m.Name]
			if !ok {
				continue
			}
			star := ""
			if im.Label == winner {
				star = " *"
			}
			fmt.Fprintf(&b, "u %s %s %g %s%s\n", im.Label, m.Name, v, m.Unit, star)
		}
	}
	if r.Note != "" {
		b.WriteString("note " + r.Note + "\n")
	}
	return text(b.String())
}

func (s *Server) benchLs(in benchIn, st *benchmark.Store) (*mcp.CallToolResult, any, error) {
	var filter map[string]string
	if in.Frame != "" {
		f, err := parseFrame(in.Frame)
		if err != nil {
			return refuse("! ARG E - " + err.Error())
		}
		filter = benchmark.CanonicalFrame(f)
	}
	var lines []string
	for _, key := range st.Keys() {
		head, _ := st.Head(key)
		if in.Name != "" && !strings.Contains(strings.ToLower(head.Name), strings.ToLower(in.Name)) {
			continue
		}
		match := true
		for k, v := range filter {
			if head.Frame[k] != v {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		lines = append(lines, fmt.Sprintf("m %s v%d %s impls=%d metrics=%d %s",
			shortDisplayID(head.ID), head.Ver, head.Name, len(head.Impls), len(head.Metrics), head.T.Format("2006-01-02")))
	}
	// never silent: quarantined lines are preserved on save but SAID here —
	// FIRST, so budget truncation can never page the warning out of sight
	// (cross-val finding: appended last, a fat page starved it to page N)
	if q := len(st.Quarantine); q > 0 {
		lines = append([]string{fmt.Sprintf("! QUAR W - %d quarantined line(s) in bench.ndjson preserved verbatim — inspect the file", q)}, lines...)
	}
	if len(lines) == 0 {
		return text("ok no benchmarks match\n")
	}
	if in.Budget <= 0 {
		in.Budget = 2000
	}
	kept, cur := budget.TruncateRecords(lines, budget.Resume(in.Cur), in.Budget)
	return text(budget.Render(kept, cur))
}

func (s *Server) benchRm(in benchIn, st *benchmark.Store, path string) (*mcp.CallToolResult, any, error) {
	if in.ID == "" {
		return refuse("! ARG E - rm needs id (M-...)")
	}
	r, err := benchResolve(st, in.ID)
	if err != nil {
		return refuse("! ARG E - " + err.Error())
	}
	if s.wtItem == "" {
		if res, out, err := s.blockedByLease([]string{dirOf(in.Dir)}); res != nil || err != nil {
			return res, out, err
		}
	}
	n := st.Remove(r.Key)
	if err := st.Save(path); err != nil {
		return nil, nil, err
	}
	// the removal journals: a later put restarts at v1 and the journal
	// records the restart — no silent resurrection
	if err := journal.Append(s.ws, dirOf(in.Dir), journal.Event{
		Ev: journal.EvBench, ID: r.ID, K: "bench", Ti: r.Name, Dir: dirOf(in.Dir),
		Note: r.Key, Sum: fmt.Sprintf("removed %d version(s); a later put restarts at v1", n),
	}); err != nil {
		return nil, nil, err
	}
	return text(fmt.Sprintf("ok m %s removed %d version(s) of %s\n", shortDisplayID(r.ID), n, r.Name))
}

func (s *Server) benchCmp(in benchIn, st *benchmark.Store) (*mcp.CallToolResult, any, error) {
	if in.ID == "" || in.ID2 == "" {
		return refuse("! ARG E - cmp needs id and id2")
	}
	a, err := benchResolve(st, in.ID)
	if err != nil {
		return refuse("! ARG E - " + err.Error())
	}
	c, err := benchResolve(st, in.ID2)
	if err != nil {
		return refuse("! ARG E - " + err.Error())
	}
	var b strings.Builder
	fmt.Fprintf(&b, "m cmp %s v%d vs %s v%d\n", shortDisplayID(a.ID), a.Ver, shortDisplayID(c.ID), c.Ver)
	compared := 0
	for _, ma := range a.Metrics {
		var mc *benchmark.Metric
		for i := range c.Metrics {
			if c.Metrics[i].Name == ma.Name {
				mc = &c.Metrics[i]
				break
			}
		}
		if mc == nil {
			continue
		}
		if mc.Unit != ma.Unit {
			return refuse(fmt.Sprintf("! ARG E - metric %s units differ (%s vs %s) — units are never converted", ma.Name, ma.Unit, mc.Unit))
		}
		for _, ia := range a.Impls {
			va, ok := ia.Res[ma.Name]
			if !ok {
				continue
			}
			vc, ok := cmpValue(c, ia.Label, ma.Name)
			if !ok {
				continue
			}
			d := vc - va
			_, mark := deltaVerdict(ma, d)
			fmt.Fprintf(&b, "d %s %s %g -> %g %s Δ%+g%s\n", ia.Label, ma.Name, va, vc, ma.Unit, d, mark)
			compared++
		}
	}
	if compared == 0 {
		return refuse("! ARG E - the two entries share no (impl, metric) pair to compare")
	}
	return text(b.String())
}

func cmpValue(r benchmark.Record, label, metric string) (float64, bool) {
	for _, im := range r.Impls {
		if im.Label == label {
			v, ok := im.Res[metric]
			return v, ok
		}
	}
	return 0, false
}
