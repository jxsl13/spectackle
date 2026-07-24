package resolve

import (
	"context"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
)

func TestDefaultRegistry(t *testing.T) {
	r := Default()
	want := map[string]bool{"cgo": false, "ffi": false, "plan9asm": false, "cuda": false, "gpupipe": false, "vulkan": false}
	for _, b := range r.All() {
		if _, ok := want[b.Name()]; !ok {
			t.Errorf("unexpected resolver %q", b.Name())
			continue
		}
		want[b.Name()] = true
		if len(b.Langs()) == 0 {
			t.Errorf("resolver %q declares no languages (sync engine needs them for invalidation)", b.Name())
		}
		// M0 contract: stubs resolve to nothing, never to an error
		edges, err := b.Resolve(context.Background(), graph.NewMem(), nil)
		if err != nil || edges != nil {
			t.Errorf("resolver %q stub contract broken: %v %v", b.Name(), edges, err)
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("resolver %q not registered", name)
		}
	}
}
