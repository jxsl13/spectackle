package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jxsl13/spectackle/internal/mcpserver"
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

func TestRunHTTPListenerGracefulShutdown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	addr := ln.Addr().String()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runHTTPListener(ctx, ln, handler)
	}()

	// Confirm the server is actually accepting connections before we ask it
	// to shut down.
	if resp, err := http.Get("http://" + addr); err != nil {
		t.Fatalf("GET before shutdown: %v", err)
	} else {
		_ = resp.Body.Close()
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runHTTPListener returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runHTTPListener did not return within 2s of ctx cancellation")
	}

	// The listener should now be closed: new connections must be refused.
	if _, err := net.DialTimeout("tcp", addr, 500*time.Millisecond); err == nil {
		t.Fatal("expected connection to be refused after shutdown, but dial succeeded")
	}
}
