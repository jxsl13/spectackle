// Command sizewith is a minimal binary that embeds and instantiates the
// wazero/tree-sitter wasm engine, for T-0040 part 3(b)'s size-delta
// measurement. It does real, non-elidable work (parses one string) so the
// linker cannot dead-code-eliminate the wasm engine or its 4.2MB embedded
// blob.
package main

import (
	"context"
	"fmt"

	"github.com/jxsl13/spectackle/poc/wasmparse/tswasm"
)

func main() {
	ctx := context.Background()
	ex, err := tswasm.New(ctx)
	if err != nil {
		panic(err)
	}
	triples, err := ex.ParseFile(ctx, []byte("int main(void) { return 0; }\n"))
	if err != nil {
		panic(err)
	}
	fmt.Println(len(triples))
}
