---
schema: v0
---

## T-0018 graceful shutdown for serve -http (SIGINT/SIGTERM)
kind: task
state: done
created: 2026-07-24

Scope ONLY: cmd/spectacle/main.go + cmd/spectacle/http_test.go. Extract runHTTP(ctx, addr, handler) helper: http.Server, goroutine ListenAndServe, on ctx.Done -> Shutdown with 5s timeout, return first error (http.ErrServerClosed maps to nil). serve() wires signal.NotifyContext(SIGINT, SIGTERM) around it in -http mode; stdio path untouched. Test: runHTTP on 127.0.0.1:0 listener... if addr:0 port unknown use net.Listen first and add a variant taking net.Listener (runHTTPListener) so the test can dial; cancel ctx, assert clean return within 2s and connections refused after.
