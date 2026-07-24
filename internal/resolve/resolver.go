// Package resolve defines the cross-language binding resolvers — the piece
// that turns per-language symbol islands into one connected graph. Each
// resolver detects one FFI boundary kind and emits edges (ECgo, EAsm,
// ELaunch). New languages are added by implementing BindingResolver and
// registering it; nothing else in the pipeline changes.
package resolve

import (
	"context"

	"github.com/jxsl13/spectacle/internal/graph"
)

// FileSet gives resolvers read access to the indexed source files without
// binding them to the filesystem layout (tests use in-memory sets).
type FileSet interface {
	ByLang(l graph.Lang) []string
	Read(path string) ([]byte, error)
}

// BindingResolver inspects the graph plus the source files of its languages
// and returns cross-language edges to be added.
type BindingResolver interface {
	Name() string        // "cgo", "plan9asm", "cuda", "gpupipe"
	Langs() []graph.Lang // languages whose file changes require a re-run
	Resolve(ctx context.Context, g graph.Graph, fs FileSet) ([]graph.Edge, error)
}

// Registry holds the active resolvers in registration order.
type Registry struct{ rs []BindingResolver }

func (r *Registry) Register(b BindingResolver) { r.rs = append(r.rs, b) }
func (r *Registry) All() []BindingResolver     { return r.rs }

// Default returns the built-in resolver set.
func Default() *Registry {
	r := &Registry{}
	r.Register(CgoResolver{})
	r.Register(Plan9AsmResolver{})
	r.Register(CudaResolver{})
	r.Register(GpuPipeResolver{})
	return r
}
