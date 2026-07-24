// Command sizewithout is the size-delta baseline for T-0040 part 3(b): a
// minimal Go binary with no wasm/tree-sitter import at all (and, for a fair
// delta, no other PoC-specific package either — cmd/sizewith imports only
// tswasm, so this baseline must be equally bare to isolate just the wasm
// engine's marginal cost, not internal/index's own transitive deps such as
// modernc.org/sqlite).
package main

import "fmt"

func main() {
	fmt.Println("no wasm engine linked in")
}
