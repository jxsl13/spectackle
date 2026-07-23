package mcpserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectacle/internal/budget"
	"github.com/jxsl13/spectacle/internal/ears"
	"github.com/jxsl13/spectacle/internal/index"
	"github.com/jxsl13/spectacle/internal/spec"
)

// The tool surface is documented in docs/tools.md; the structs below are the
// normative schemas (SPX-REPO-001 requires both to stay consistent). Results
// use the dense line grammar from docs/tools.md, not JSON — that is where the
// token savings come from.

const stubLine = "stub milestone=M1 see docs/roadmap.md"

// ---- input structs (JSON Schemas are inferred from these) ----

type symIn struct {
	Q    string `json:"q" jsonschema:"name or ID fragment to resolve"`
	K    int    `json:"k,omitempty" jsonschema:"max results, default 5"`
	Kind string `json:"kind,omitempty" jsonschema:"fn|type|kernel|asm|any, default any"`
}

type mapIn struct {
	Path   string   `json:"path,omitempty" jsonschema:"subtree to map, default repo root"`
	Focus  []string `json:"focus,omitempty" jsonschema:"node IDs biasing the ranking"`
	Langs  []string `json:"langs,omitempty" jsonschema:"go|c|cpp|cu|asm|objc|msl, default all"`
	Budget int      `json:"budget,omitempty" jsonschema:"token budget, default 2000"`
	Cur    string   `json:"cur,omitempty" jsonschema:"resume cursor from a previous call"`
}

type impactIn struct {
	IDs    []string `json:"ids" jsonschema:"seed node IDs"`
	Depth  int      `json:"depth,omitempty" jsonschema:"BFS depth, default 2"`
	Dir    string   `json:"dir,omitempty" jsonschema:"out|in|both, default both"`
	Kinds  []string `json:"kinds,omitempty" jsonschema:"call|incl|cgo|asm|launch|use, default all"`
	Budget int      `json:"budget,omitempty" jsonschema:"token budget, default 1500"`
	Cur    string   `json:"cur,omitempty" jsonschema:"resume cursor"`
}

type contractsIn struct {
	IDs     []string `json:"ids,omitempty" jsonschema:"node IDs to load contracts for"`
	Paths   []string `json:"paths,omitempty" jsonschema:"repo-relative paths to load contracts for"`
	Inherit bool     `json:"inherit,omitempty" jsonschema:"include inherited rules, default true"`
	Budget  int      `json:"budget,omitempty" jsonschema:"token budget, default 1500"`
}

type planChangeIn struct {
	Targets []string `json:"targets" jsonschema:"node IDs or repo-relative file paths the change starts at"`
	Intent  string   `json:"intent,omitempty" jsonschema:"one-line description of the change"`
	Depth   int      `json:"depth,omitempty" jsonschema:"impact BFS depth, default 2"`
	Budget  int      `json:"budget,omitempty" jsonschema:"token budget, default 3000"`
}

type lintEarsIn struct {
	Text string `json:"text,omitempty" jsonschema:"rule markdown to lint (exclusive with path)"`
	Path string `json:"path,omitempty" jsonschema:"spec file to lint (exclusive with text)"`
}

type coverageIn struct {
	Path   string `json:"path,omitempty" jsonschema:"subtree to report on, default repo root"`
	Budget int    `json:"budget,omitempty" jsonschema:"token budget, default 1000"`
}

type linkIn struct {
	Rule string `json:"rule" jsonschema:"rule ID, e.g. CUDA-KRN-001"`
	ID   string `json:"id" jsonschema:"node ID, e.g. cu:saxpy_kernel"`
	Rm   bool   `json:"rm,omitempty" jsonschema:"remove the link instead, default false"`
}

type reindexIn struct {
	Paths []string `json:"paths,omitempty" jsonschema:"files to re-index, default full re-index"`
}

type addRuleIn struct {
	Dir       string   `json:"dir,omitempty" jsonschema:"target directory the rule scopes, default repo root"`
	Global    bool     `json:"global,omitempty" jsonschema:"write to .spectacle/global.ears.md instead, default false"`
	Pattern   string   `json:"pattern,omitempty" jsonschema:"U|E|S|N|O|C; elicited from the user if missing"`
	System    string   `json:"system,omitempty" jsonschema:"the acting system, e.g. 'host wrapper'"`
	Response  string   `json:"response,omitempty" jsonschema:"what it SHALL do; must name something verifiable"`
	Trigger   string   `json:"trigger,omitempty" jsonschema:"WHEN clause (pattern E/C)"`
	State     string   `json:"state,omitempty" jsonschema:"WHILE clause (pattern S/C)"`
	Condition string   `json:"condition,omitempty" jsonschema:"IF clause (pattern N/C)"`
	Feature   string   `json:"feature,omitempty" jsonschema:"WHERE clause (pattern O/C)"`
	Stem      string   `json:"stem,omitempty" jsonschema:"ID stem, e.g. CUDA-KRN; default: stem of last rule in target file"`
	Rationale string   `json:"rationale,omitempty" jsonschema:"optional rationale paragraph"`
	Applies   []string `json:"applies,omitempty" jsonschema:"node IDs the rule is pinned to"`
}

type rmRuleIn struct {
	Rule string `json:"rule" jsonschema:"rule ID to remove, e.g. CUDA-KRN-003"`
}

func text(s string) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}, nil, nil
}

func (s *Server) registerTools() {
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "sym",
		Description: "Resolve names to stable node IDs (cheap entry point before impact/plan_change)."},
		func(ctx context.Context, req *mcp.CallToolRequest, in symIn) (*mcp.CallToolResult, any, error) {
			return text(fmt.Sprintf("%s\nq=%s", stubLine, in.Q))
		})

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "map",
		Description: "Ranked cross-language repo map (Aider-style skeleton) within a token budget."},
		func(ctx context.Context, req *mcp.CallToolRequest, in mapIn) (*mcp.CallToolResult, any, error) {
			return text(stubLine)
		})

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "impact",
		Description: "Cross-language impact radius (BFS over call/cgo/asm/launch edges) without reading files."},
		func(ctx context.Context, req *mcp.CallToolRequest, in impactIn) (*mcp.CallToolResult, any, error) {
			return text(fmt.Sprintf("%s\nids=%s", stubLine, strings.Join(in.IDs, ",")))
		})

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "contracts",
		Description: "Resolved cascading EARS rules for nodes or paths (global -> nearest dir, overrides applied)."},
		func(ctx context.Context, req *mcp.CallToolRequest, in contractsIn) (*mcp.CallToolResult, any, error) {
			return s.contracts(in)
		})

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "plan_change",
		Description: "Composite: impact radius + applicable EARS contracts + coverage gaps in one round trip. Call this first for any change task."},
		func(ctx context.Context, req *mcp.CallToolRequest, in planChangeIn) (*mcp.CallToolResult, any, error) {
			return s.planChange(in)
		})

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "lint_ears",
		Description: "Validate EARS rule text or a spec file; empty result means conformant."},
		func(ctx context.Context, req *mcp.CallToolRequest, in lintEarsIn) (*mcp.CallToolResult, any, error) {
			return s.lintEars(in)
		})

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "coverage",
		Description: "Spec coverage report: directories/nodes without rules, rules without code."},
		func(ctx context.Context, req *mcp.CallToolRequest, in coverageIn) (*mcp.CallToolResult, any, error) {
			return s.coverage(in)
		})

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "link",
		Description: "Bind an EARS rule to a graph node (writes .spectacle/links.tsv for traceability)."},
		func(ctx context.Context, req *mcp.CallToolRequest, in linkIn) (*mcp.CallToolResult, any, error) {
			return s.link(in)
		})

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "add_rule",
		Description: "Create an EARS contract from structured slots (never hand-write spec files). Missing slots are elicited from the user when the client supports it; the composed sentence is linted before anything is written; IDs are assigned automatically."},
		func(ctx context.Context, req *mcp.CallToolRequest, in addRuleIn) (*mcp.CallToolResult, any, error) {
			return s.addRule(ctx, req, in)
		})

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "rm_rule",
		Description: "Remove an EARS contract by ID from its spec file."},
		func(ctx context.Context, req *mcp.CallToolRequest, in rmRuleIn) (*mcp.CallToolResult, any, error) {
			return s.rmRule(in)
		})

	mcp.AddTool(s.mcp, &mcp.Tool{Name: "reindex",
		Description: "Re-index changed files (or everything) and refresh the graph."},
		func(ctx context.Context, req *mcp.CallToolRequest, in reindexIn) (*mcp.CallToolResult, any, error) {
			return text(stubLine)
		})
}

// ---- real M0 implementations ----

func (s *Server) loadCascade() (*spec.Cascade, error) {
	return spec.Load(s.root)
}

func ruleLine(r spec.ResolvedRule) string {
	return fmt.Sprintf("r %s %s %s %s", r.ID, r.Pattern, r.ScopeDir, r.Text)
}

func (s *Server) contracts(in contractsIn) (*mcp.CallToolResult, any, error) {
	if len(in.IDs) == 0 && len(in.Paths) == 0 {
		return text("! ARG E - pass ids or paths")
	}
	c, err := s.loadCascade()
	if err != nil {
		return nil, nil, err
	}
	if in.Budget == 0 {
		in.Budget = 1500
	}
	var lines []string
	seen := map[string]bool{}
	for _, p := range in.Paths {
		for _, r := range c.ForPath(p) {
			if !seen[r.ID] {
				seen[r.ID] = true
				lines = append(lines, ruleLine(r))
			}
		}
	}
	if len(in.IDs) > 0 {
		lines = append(lines, stubLine+" (id->path mapping needs the graph; pass paths meanwhile)")
	}
	if len(lines) == 0 {
		lines = []string{"ok no applicable rules"}
	}
	kept, cur := budget.TruncateRecords(lines, 0, in.Budget)
	return text(budget.Render(kept, cur))
}

func (s *Server) planChange(in planChangeIn) (*mcp.CallToolResult, any, error) {
	if len(in.Targets) == 0 {
		return text("! ARG E - pass targets")
	}
	c, err := s.loadCascade()
	if err != nil {
		return nil, nil, err
	}
	if in.Budget == 0 {
		in.Budget = 3000
	}
	var b strings.Builder
	b.WriteString("#impact\n")
	b.WriteString(stubLine + "\n")

	b.WriteString("#contracts\n")
	seen := map[string]bool{}
	var lines []string
	for _, t := range in.Targets {
		p, ok := targetPath(t)
		if !ok {
			continue // node IDs need the graph (M1); path targets work now
		}
		for _, r := range c.ForPath(p) {
			if !seen[r.ID] {
				seen[r.ID] = true
				lines = append(lines, ruleLine(r))
			}
		}
	}
	if len(lines) == 0 {
		lines = []string{"ok no applicable rules (or targets were node IDs; graph lands in M1)"}
	}
	kept, cur := budget.TruncateRecords(lines, 0, in.Budget)
	b.WriteString(budget.Render(kept, cur))

	b.WriteString("#gaps\n")
	gaps := 0
	for _, f := range c.Findings() {
		b.WriteString(fmt.Sprintf("g lint %s:%d %s %s\n", f.File, f.Line, f.Code, f.Msg))
		gaps++
	}
	for _, t := range in.Targets {
		if p, ok := targetPath(t); ok && len(c.ForPath(p)) == 0 {
			b.WriteString("g uncovered " + p + " no rules apply\n")
			gaps++
		}
	}
	if gaps == 0 {
		b.WriteString("g none -\n")
	}
	return text(b.String())
}

// targetPath decides whether a plan_change target is a file path (as opposed
// to a node ID) and strips an optional ":line" suffix.
func targetPath(t string) (string, bool) {
	if i := strings.IndexByte(t, ':'); i > 0 {
		// "go:pkg.Fn" is an ID; "dir/file.go:42" is a path with line
		if !strings.ContainsAny(t[:i], "./") {
			return "", false
		}
		return t[:i], true
	}
	return t, strings.ContainsAny(t, "./")
}

func (s *Server) lintEars(in lintEarsIn) (*mcp.CallToolResult, any, error) {
	if (in.Text == "") == (in.Path == "") {
		return text("! ARG E - pass exactly one of text or path")
	}
	var fs []ears.Finding
	if in.Text != "" {
		body, start := ears.StripFrontMatter(in.Text)
		if strings.Contains(body, "## ") || strings.HasPrefix(body, "## ") {
			_, fs = ears.ParseRules(body, "input", start)
		} else {
			fs = ears.LintSentence(body, "input", start)
		}
	} else {
		var err error
		fs, err = ears.LintFile(filepath.Join(s.root, filepath.FromSlash(in.Path)))
		if err != nil {
			return nil, nil, err
		}
	}
	if len(fs) == 0 {
		return text("ok")
	}
	var b strings.Builder
	for _, f := range fs {
		b.WriteString(f.String())
		b.WriteByte('\n')
	}
	return text(b.String())
}

func (s *Server) coverage(in coverageIn) (*mcp.CallToolResult, any, error) {
	c, err := s.loadCascade()
	if err != nil {
		return nil, nil, err
	}
	if in.Budget == 0 {
		in.Budget = 1000
	}
	sub := filepath.Join(s.root, filepath.FromSlash(in.Path))

	// M0 half: directories containing recognized source files but reachable
	// by zero rules. The node-level half (uncovered exported symbols, orphan
	// rules with dead scopes) needs the graph and lands in M3.
	uncovered := map[string]bool{}
	err = filepath.WalkDir(sub, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			n := d.Name()
			if n == ".git" || n == "node_modules" || n == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if index.LangOf(p) == "" {
			return nil
		}
		rel, _ := filepath.Rel(s.root, p)
		rel = filepath.ToSlash(rel)
		if len(c.ForPath(rel)) == 0 {
			uncovered[filepath.ToSlash(filepath.Dir(rel))] = true
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	var lines []string
	for d := range uncovered {
		lines = append(lines, "g uncovered "+d+" source files with zero applicable rules")
	}
	sort.Strings(lines)
	for _, f := range c.Findings() {
		lines = append(lines, fmt.Sprintf("g lint %s:%d %s %s", f.File, f.Line, f.Code, f.Msg))
	}
	if len(lines) == 0 {
		lines = []string{"ok all source directories covered"}
	}
	kept, cur := budget.TruncateRecords(lines, 0, in.Budget)
	return text(budget.Render(kept, cur))
}

// slotQuestions drive both the elicitation form and the `need` fallback
// records for guided contract authoring.
var slotQuestions = map[string]string{
	"pattern":   "EARS pattern: U ubiquitous, E event (WHEN), S state (WHILE), N unwanted (IF/THEN), O optional (WHERE), C complex",
	"system":    "Which system/component does the rule bind (e.g. 'host wrapper')?",
	"response":  "What SHALL it do? Name something verifiable (number, API, error code, artifact).",
	"trigger":   "WHEN — the triggering event",
	"state":     "WHILE — the state during which the rule holds",
	"condition": "IF — the unwanted condition",
	"feature":   "WHERE — the optional feature gating the rule",
}

// missingSlots returns the unfilled inputs required for the requested pattern.
func missingSlots(in addRuleIn) []string {
	if ears.PatternFromString(in.Pattern) == ears.PInvalid {
		return []string{"pattern"}
	}
	return ears.MissingSlots(ears.PatternFromString(in.Pattern), slotsOf(in))
}

func slotsOf(in addRuleIn) ears.Slots {
	return ears.Slots{
		System: in.System, Response: in.Response, Trigger: in.Trigger,
		State: in.State, Condition: in.Condition, Feature: in.Feature,
	}
}

// elicit asks the end user for the missing slots through the MCP client
// (elicitation). Returns false when the client cannot or the user declines —
// the caller then falls back to `need` records the agent can relay.
func elicitSlots(ctx context.Context, req *mcp.CallToolRequest, in *addRuleIn, missing []string) bool {
	props := map[string]any{}
	for _, m := range missing {
		p := map[string]any{"type": "string", "description": slotQuestions[m]}
		if m == "pattern" {
			p["enum"] = []string{"U", "E", "S", "N", "O", "C"}
		}
		props[m] = p
	}
	res, err := req.Session.Elicit(ctx, &mcp.ElicitParams{
		Message: "spectacle: complete the EARS contract",
		RequestedSchema: map[string]any{
			"type": "object", "properties": props, "required": missing,
		},
	})
	if err != nil || res.Action != "accept" {
		return false
	}
	get := func(k string) string { v, _ := res.Content[k].(string); return v }
	for _, m := range missing {
		switch m {
		case "pattern":
			in.Pattern = get(m)
		case "system":
			in.System = get(m)
		case "response":
			in.Response = get(m)
		case "trigger":
			in.Trigger = get(m)
		case "state":
			in.State = get(m)
		case "condition":
			in.Condition = get(m)
		case "feature":
			in.Feature = get(m)
		}
	}
	return true
}

func (s *Server) addRule(ctx context.Context, req *mcp.CallToolRequest, in addRuleIn) (*mcp.CallToolResult, any, error) {
	// guided flow: elicit missing slots from the user; iterate because the
	// pattern answer can introduce new required slots (e.g. E needs trigger).
	for range 3 {
		missing := missingSlots(in)
		if len(missing) == 0 {
			break
		}
		if elicitSlots(ctx, req, &in, missing) {
			continue
		}
		var b strings.Builder
		for _, m := range missing {
			b.WriteString("need " + m + " " + slotQuestions[m] + "\n")
		}
		return text(b.String())
	}
	if missing := missingSlots(in); len(missing) > 0 {
		return text("! ARG E - still missing: " + strings.Join(missing, ", "))
	}

	p := ears.PatternFromString(in.Pattern)
	sentence, err := ears.Compose(p, slotsOf(in))
	if err != nil {
		return text("! ARG E - " + err.Error())
	}
	c, err := s.loadCascade()
	if err != nil {
		return nil, nil, err
	}
	res, err := spec.AddRule(s.root, c, spec.AuthorReq{
		Dir: in.Dir, Global: in.Global, Stem: in.Stem,
		Sentence: sentence, Rationale: in.Rationale, Applies: in.Applies,
	})
	if err != nil {
		return text("! ARG E - " + err.Error())
	}
	var b strings.Builder
	for _, f := range res.Findings {
		b.WriteString(f.String())
		b.WriteByte('\n')
	}
	if !res.Written {
		b.WriteString("! REJECTED E - fix the slots and retry; nothing was written\n")
		return text(b.String())
	}
	scopeDir := in.Dir
	if in.Global {
		scopeDir = "-"
	} else if scopeDir == "" {
		scopeDir = "."
	}
	fmt.Fprintf(&b, "ok %s %s\nr %s %s %s %s\n", res.ID, res.Path, res.ID, p, scopeDir, sentence)
	return text(b.String())
}

func (s *Server) rmRule(in rmRuleIn) (*mcp.CallToolResult, any, error) {
	c, err := s.loadCascade()
	if err != nil {
		return nil, nil, err
	}
	file, err := spec.RemoveRule(s.root, c, in.Rule)
	if err != nil {
		return text("! ARG E - " + err.Error())
	}
	return text(fmt.Sprintf("ok %s removed from %s", in.Rule, file))
}

func (s *Server) link(in linkIn) (*mcp.CallToolResult, any, error) {
	if in.Rule == "" || in.ID == "" {
		return text("! ARG E - pass rule and id")
	}
	c, err := s.loadCascade()
	if err != nil {
		return nil, nil, err
	}
	if _, ok := c.Rule(in.Rule); !ok {
		return text("! ARG E - unknown rule " + in.Rule)
	}
	linksPath := filepath.Join(s.root, ".spectacle", "links.tsv")
	existing := map[string]bool{}
	var order []string
	if raw, err := os.ReadFile(linksPath); err == nil {
		for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			if l != "" && !existing[l] {
				existing[l] = true
				order = append(order, l)
			}
		}
	}
	entry := in.Rule + "\t" + in.ID
	if in.Rm {
		delete(existing, entry)
	} else if !existing[entry] {
		existing[entry] = true
		order = append(order, entry)
	}
	var b strings.Builder
	for _, l := range order {
		if existing[l] {
			b.WriteString(l)
			b.WriteByte('\n')
		}
	}
	if err := os.MkdirAll(filepath.Dir(linksPath), 0o755); err != nil {
		return nil, nil, err
	}
	if err := os.WriteFile(linksPath, []byte(b.String()), 0o644); err != nil {
		return nil, nil, err
	}
	verb := "->"
	if in.Rm {
		verb = "-/->"
	}
	return text(fmt.Sprintf("ok %s %s %s", in.Rule, verb, in.ID))
}
