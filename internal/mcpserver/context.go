package mcpserver

import (
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectackle/internal/graph"
	"github.com/jxsl13/spectackle/internal/item"
	"github.com/jxsl13/spectackle/internal/journal"
	"github.com/jxsl13/spectackle/internal/spec"
)

type contextClass string

const (
	contextPhysical        contextClass = "physical"
	contextHistoricalOnly  contextClass = "historical-only"
	contextRecordsOnlyLive contextClass = "records-only-live"
)

type contextStatus struct {
	Dir      string
	Class    contextClass
	Mode     string
	Rules    int
	Open     int
	Unbound  []string
	Material bool
}

func (s *Server) contextStatuses(c *spec.Cascade, items []item.Item) ([]contextStatus, error) {
	ctxs, err := s.ws.ContextDirs()
	if err != nil {
		return nil, err
	}
	out := make([]contextStatus, 0, len(ctxs))
	for _, dir := range ctxs {
		st := contextStatus{Dir: dir}
		if sf, ok := c.File(dir); ok {
			st.Mode = sf.ContextMode
			st.Rules = len(sf.Rules)
			for _, rule := range sf.Rules {
				resolved := false
				for _, target := range rule.Applies {
					if _, ok := s.g.Node(graph.NodeID(target)); ok {
						resolved = true
						break
					}
				}
				if !resolved {
					st.Unbound = append(st.Unbound, rule.ID)
				}
			}
		}
		for _, it := range items {
			if it.Dir == dir && contextItemOpen(it.State) {
				st.Open++
			}
		}
		st.Material, err = s.ws.ContextHasMaterial(dir)
		if err != nil {
			return nil, err
		}
		switch {
		case dir == "" || st.Material:
			st.Class = contextPhysical
		case st.Rules == 0 && st.Open == 0:
			st.Class = contextHistoricalOnly
		default:
			st.Class = contextRecordsOnlyLive
		}
		out = append(out, st)
	}
	return out, nil
}

func contextItemOpen(state string) bool {
	switch state {
	case item.StateDone, item.StateArchived, item.StateRejected:
		return false
	default:
		return true
	}
}

func contextModeLabel(mode string) string {
	if mode == "" {
		return "-"
	}
	return mode
}

func contextStatusByDir(statuses []contextStatus, dir string) (contextStatus, bool) {
	for _, st := range statuses {
		if st.Dir == dir {
			return st, true
		}
	}
	return contextStatus{}, false
}

func contextCheckFindings(statuses []contextStatus) []string {
	var lines []string
	for _, st := range statuses {
		if st.Dir == "" {
			continue
		}
		ref := orDot(st.Dir)
		if st.Mode == spec.ContextModeLogical && st.Class != contextRecordsOnlyLive {
			lines = append(lines, fmt.Sprintf("! CTX W %s context-logical-stale class=%s", ref, st.Class))
		}
		if st.Class != contextRecordsOnlyLive {
			continue
		}
		if st.Mode != spec.ContextModeLogical {
			lines = append(lines, fmt.Sprintf("! CTX E %s context-records-only-live mode=%s rules=%d open=%d", ref, contextModeLabel(st.Mode), st.Rules, st.Open))
			continue
		}
		if st.Open > 0 {
			lines = append(lines, fmt.Sprintf("! CTX E %s context-logical-open-work open=%d", ref, st.Open))
		}
		for _, id := range st.Unbound {
			lines = append(lines, fmt.Sprintf("! CTX E %s context-logical-unbound-rule %s", ref, id))
		}
	}
	sort.Strings(lines)
	return lines
}

type contextIn struct {
	Op   string `json:"op" jsonschema:"set"`
	Dir  string `json:"dir" jsonschema:"non-root context dir"`
	Mode string `json:"mode" jsonschema:"logical|physical; physical or empty clears the declaration"`
}

func (s *Server) context(in contextIn) (*mcp.CallToolResult, any, error) {
	if in.Op != "set" {
		return refuse("! ARG E - context op must be set\nshape: context {op*:set, dir*, mode*:logical|physical}\n")
	}
	dir := strings.Trim(strings.TrimSpace(in.Dir), "/")
	if dir == "" || dir == "." {
		return refuse("! CTX E . root context is always physical\n")
	}
	mode := strings.TrimSpace(in.Mode)
	if mode == "physical" {
		mode = ""
	}
	if mode != "" && mode != spec.ContextModeLogical {
		return refuse("! ARG E - context mode must be logical|physical\nshape: context {op*:set, dir*, mode*:logical|physical}\n")
	}

	c, err := spec.Load(s.ws.Dir)
	if err != nil {
		return nil, nil, err
	}
	items, err := item.LoadAll(s.ws)
	if err != nil {
		return nil, nil, err
	}
	statuses, err := s.contextStatuses(c, items)
	if err != nil {
		return nil, nil, err
	}
	st, ok := contextStatusByDir(statuses, dir)
	if !ok {
		return refuse(fmt.Sprintf("! CTX E %s unknown context\n", dir))
	}
	if mode == spec.ContextModeLogical {
		if st.Class != contextRecordsOnlyLive {
			return refuse(fmt.Sprintf("! CTX E %s context-logical-stale class=%s\n", dir, st.Class))
		}
		if st.Open > 0 {
			return refuse(fmt.Sprintf("! CTX E %s context-logical-open-work open=%d\n", dir, st.Open))
		}
		if len(st.Unbound) > 0 {
			return refuse(fmt.Sprintf("! CTX E %s context-logical-unbound-rule rules=%s\n", dir, strings.Join(st.Unbound, ",")))
		}
	}
	if _, hasSpec := c.File(dir); !hasSpec {
		if mode == "" {
			return text(fmt.Sprintf("ok context %s mode=- class=%s\n", dir, st.Class))
		}
		return refuse(fmt.Sprintf("! CTX E %s logical context requires spec.md\n", dir))
	}
	if err := spec.SetContextMode(s.ws, dir, mode); err != nil {
		return nil, nil, err
	}
	if err := journal.Append(s.ws, dir, journal.Event{
		Ev: journal.EvContext, Dir: dir, Op: "set", Mode: mode,
	}); err != nil {
		return nil, nil, err
	}
	return text(fmt.Sprintf("ok context %s mode=%s class=%s\n", dir, contextModeLabel(mode), st.Class))
}
