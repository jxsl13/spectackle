// Package mcpclient is spectackle's own MCP client: it lets the binary make
// a headless tool call against a spectackle server (spawned over stdio, or a
// resident server reached over Streamable HTTP) without an external wrapper
// script (CLI-002). It is a thin, single-purpose layer over the go-sdk
// mcp.Client — transport selection, one Call method, and dense-record
// passthrough. No hand-rolled JSON-RPC framing.
package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectackle/internal/mcpserver"
)

// Config selects the transport and, for a spawned server, its workspace.
type Config struct {
	// Endpoint is the http(s) URL of a resident server (Streamable HTTP).
	// Empty means: spawn a fresh server over stdio instead.
	Endpoint string
	// Command is the executable to spawn when Endpoint is empty. Empty
	// means: re-exec the running binary (os.Executable()).
	Command string
	// Root is the workspace root passed to the spawned server (-root). Only
	// used for the stdio transport.
	Root string
}

// Call names a tool and its arguments, exactly as the MCP `tools/call`
// request takes them.
type Call struct {
	Name      string
	Arguments map[string]any
}

// ErrToolRefused marks a tool result that carried IsError: the rendered
// text holds the refusal itself, and callers that already surfaced the text
// (the CLI prints it to stdout) need only the exit-code signal, not a
// second prose rendition of the failure.
var ErrToolRefused = errors.New("tool refused")

// Session is a live connection to a spectackle MCP server, over whichever
// transport Dial picked.
type Session struct {
	cs    *mcp.ClientSession
	desc  string               // transport description, for error context
	tools map[string]*mcp.Tool // lazy tool-list cache for ShapeLine
}

// Dial connects to a spectackle server: Streamable HTTP when cfg.Endpoint is
// set, otherwise a freshly spawned `<command> serve -root <cfg.Root>` over
// stdio. Connect performs the MCP `initialize` handshake and the
// `initialized` notification (go-sdk mcp.Client.Connect) — no hand-rolled
// JSON-RPC framing.
func Dial(ctx context.Context, cfg Config) (*Session, error) {
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "spectackle",
		Version: mcpserver.ResolvedVersion(),
	}, nil)

	transport, desc, err := cfg.transport(ctx)
	if err != nil {
		return nil, fmt.Errorf("mcpclient: %s: %w", desc, err)
	}

	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("mcpclient: connect via %s: %w", desc, err)
	}
	return &Session{cs: cs, desc: desc}, nil
}

// transport builds the go-sdk Transport for cfg, plus a human-readable
// description of it (used to give spawn/connect failures enough context to
// name the transport and endpoint, per CLI-002's error requirement).
func (cfg Config) transport(ctx context.Context) (mcp.Transport, string, error) {
	if cfg.Endpoint != "" {
		return &mcp.StreamableClientTransport{Endpoint: cfg.Endpoint},
			fmt.Sprintf("http endpoint %s", cfg.Endpoint), nil
	}

	bin := cfg.Command
	if bin == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, "stdio spawn (resolving current executable)",
				fmt.Errorf("resolve executable for stdio spawn: %w", err)
		}
		bin = exe
	}
	root := cfg.Root
	if root == "" {
		root = "."
	}
	desc := fmt.Sprintf("stdio spawn %s serve -root %s", bin, root)
	cmd := exec.CommandContext(ctx, bin, "serve", "-root", root)
	return &mcp.CommandTransport{Command: cmd}, desc, nil
}

// Call invokes a tool and renders its result the way every other spectackle
// surface does: the concatenation of the result's text content blocks,
// joined by newline, with no added framing, prefixes or JSON — the dense
// line-record grammar (docs/tools.md) passes through byte-identically.
// Non-text content blocks are skipped.
//
// When the tool result carries IsError, Call returns the rendered text
// AND a non-nil error, so a caller can tell a refusal (`! GATE ...`,
// `! REJECTED ...`) from success without parsing prose.
func (s *Session) Call(ctx context.Context, c Call) (string, error) {
	res, err := s.cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      c.Name,
		Arguments: c.Arguments,
	})
	if err != nil {
		return "", fmt.Errorf("mcpclient: call %q via %s: %w", c.Name, s.desc, err)
	}

	var parts []string
	for _, content := range res.Content {
		if tc, ok := content.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	rendered := strings.Join(parts, "\n")

	if res.IsError {
		// Refusals carry their whole reason in the rendered text; the
		// transport description here was ~430 bytes of absolute paths
		// repeated on EVERY refusal a CLI-driven agent read — 68-78% of the
		// first live judges' entire tool-output diet (T-01KYE0). Transport
		// context stays on transport errors above, where no result text
		// exists to explain anything.
		return rendered, fmt.Errorf("mcpclient: tool %q: %w", c.Name, ErrToolRefused)
	}
	return rendered, nil
}

// ShapeLine renders one dense argument-shape line for a tool, from the
// live session's own tool list (cached after the first fetch): property
// names with required ones starred and enum-shaped descriptions inlined.
// Built for the CLI's schema-refusal path (B-01KYE69): the SDK validation
// message names only the REJECTED property, and a live judge burned
// fourteen refusals cycling invented decide arguments because nothing ever
// named the accepted vocabulary — `choose` cannot be guessed, it has to be
// said once.
func (s *Session) ShapeLine(ctx context.Context, name string) (string, error) {
	if s.tools == nil {
		lt, err := s.cs.ListTools(ctx, nil)
		if err != nil {
			return "", fmt.Errorf("mcpclient: list tools for shape: %w", err)
		}
		s.tools = map[string]*mcp.Tool{}
		for _, t := range lt.Tools {
			s.tools[t.Name] = t
		}
	}
	t, ok := s.tools[name]
	if !ok || t.InputSchema == nil {
		return "", fmt.Errorf("mcpclient: no schema for tool %q", name)
	}
	// The SDK types InputSchema as any; a JSON round trip into the two
	// fields this line needs keeps us independent of its concrete type.
	rawSchema, err := json.Marshal(t.InputSchema)
	if err != nil {
		return "", fmt.Errorf("mcpclient: marshal schema for %q: %w", name, err)
	}
	var sch struct {
		Required   []string `json:"required"`
		Properties map[string]struct {
			Description string `json:"description"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(rawSchema, &sch); err != nil {
		return "", fmt.Errorf("mcpclient: decode schema for %q: %w", name, err)
	}
	req := map[string]bool{}
	for _, r := range sch.Required {
		req[r] = true
	}
	var names []string
	for p := range sch.Properties {
		names = append(names, p)
	}
	// Required first (schema order), then the rest alphabetically: the
	// star and the enum belong at the front of the reader's eye.
	sort.Slice(names, func(i, j int) bool {
		ri, rj := req[names[i]], req[names[j]]
		if ri != rj {
			return ri
		}
		return names[i] < names[j]
	})
	var parts []string
	for _, p := range names {
		part := p
		if req[p] {
			part += "*"
			// Enum-shaped descriptions (a|b|c, short) are the value grammar
			// the caller cannot guess; prose descriptions stay out — the
			// line teaches vocabulary, not the manual.
			if d := sch.Properties[p].Description; strings.Contains(d, "|") && len(d) <= 32 && !strings.Contains(d, " ") {
				part += ":" + d
			}
		}
		parts = append(parts, part)
	}
	return "shape: " + name + " {" + strings.Join(parts, ", ") + "}", nil
}

// Instructions returns the server's instructions string, captured from the
// initialize handshake (mcp.InitializeResult.Instructions). This is the
// self-bootstrapping manifest that teaches a connecting agent the whole
// lifecycle loop and tool order; dropping it silently is what an earlier
// hand-rolled wrapper got wrong during this project's own development.
func (s *Session) Instructions() string {
	ir := s.cs.InitializeResult()
	if ir == nil {
		return ""
	}
	return ir.Instructions
}

// Close is idempotent and releases the transport: for stdio, it closes the
// child's stdin and waits for it to exit (escalating to SIGTERM/SIGKILL if
// it doesn't); for http, it closes the client connection.
func (s *Session) Close() error {
	return s.cs.Close()
}
