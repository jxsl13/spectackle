package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectacle/internal/mcpserver"
)

func TestServeHTTPListTools(t *testing.T) {
	s, err := mcpserver.New(t.TempDir())
	if err != nil {
		t.Fatalf("mcpserver.New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return s.MCP()
	}, nil)

	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: ts.URL}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer sess.Close()

	res, err := sess.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := make(map[string]bool, len(res.Tools))
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"draft", "work"} {
		if !names[want] {
			t.Errorf("ListTools: missing tool %q, got %v", want, names)
		}
	}
}
