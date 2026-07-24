// Command poc drives T-0040: it runs both C parser backends (the
// production cSpec oracle and the wazero/tree-sitter wasm side) over the
// synthetic + real-fixture corpus, and prints the parity and latency
// numbers docs/design-wasm-poc-c.md is built from. It intentionally prints
// plain, greppable text (not a UI) — this is a measurement tool, run once
// and captured, not a long-lived CLI.
package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/jxsl13/spectackle/poc/wasmparse/corpus"
	"github.com/jxsl13/spectackle/poc/wasmparse/cparser"
	"github.com/jxsl13/spectackle/poc/wasmparse/triple"
	"github.com/jxsl13/spectackle/poc/wasmparse/tswasm"
)

const seed = 42

// corpusFile is one file in the combined synthetic+real corpus.
type corpusFile struct {
	Path string
	Src  []byte
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "poc:", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := repoRoot()
	if err != nil {
		return err
	}

	files, err := buildCorpus(root)
	if err != nil {
		return err
	}
	totalLOC := 0
	for _, f := range files {
		totalLOC += strings.Count(string(f.Src), "\n") + 1
	}
	fmt.Printf("corpus: %d files, %d LOC\n\n", len(files), totalLOC)

	// ---- oracle (cSpec) ----
	oracle, err := cparser.New()
	if err != nil {
		return fmt.Errorf("cparser.New: %w", err)
	}

	const numPasses = 5
	oracleTriples := make([][]triple.Triple, len(files))
	oPasses := make([]time.Duration, numPasses)
	for pass := 0; pass < numPasses; pass++ {
		pStart := time.Now()
		for i, f := range files {
			tr, err := oracle.Parse(f.Path, f.Src)
			if err != nil {
				return fmt.Errorf("cparser parse %s: %w", f.Path, err)
			}
			if pass == 0 {
				oracleTriples[i] = tr
			}
		}
		oPasses[pass] = time.Since(pStart)
	}

	fmt.Println("=== (c) LATENCY: cSpec oracle ===")
	for i, d := range oPasses {
		label := "warm"
		if i == 0 {
			label = "cold (no engine to warm)"
		}
		fmt.Printf("pass %d (%s) : %v\n", i+1, label, d)
	}
	oMin, oMax := minMax(oPasses[1:])
	printScaledRange("cSpec", oMin, oMax, totalLOC)
	fmt.Println()

	// ---- wasm (tree-sitter) ----
	ctx := context.Background()
	engineStart := time.Now()
	ex, err := tswasm.New(ctx)
	if err != nil {
		return fmt.Errorf("tswasm.New: %w", err)
	}
	engineInit := time.Since(engineStart)

	wasmTriples := make([][]triple.Triple, len(files))
	wPasses := make([]time.Duration, numPasses)
	var firstFileDur time.Duration
	for pass := 0; pass < numPasses; pass++ {
		pStart := time.Now()
		for i, f := range files {
			fStart := time.Now()
			tr, err := ex.ParseFile(ctx, f.Src)
			if pass == 0 && i == 0 {
				firstFileDur = time.Since(fStart)
			}
			if err != nil {
				return fmt.Errorf("tswasm parse %s: %w", f.Path, err)
			}
			if pass == 0 {
				wasmTriples[i] = tr
			}
		}
		wPasses[pass] = time.Since(pStart)
	}

	fmt.Println("=== (c) LATENCY: wazero/tree-sitter wasm ===")
	fmt.Printf("engine compile+instantiate (cold, one-time) : %v\n", engineInit)
	fmt.Printf("first file parse (cold, post-engine-init)   : %v\n", firstFileDur)
	fmt.Printf("cold total = engine init + first file       : %v\n", engineInit+firstFileDur)
	for i, d := range wPasses {
		label := "full corpus, right after engine init"
		if i > 0 {
			label = "warm, full corpus"
		}
		fmt.Printf("pass %d (%s) : %v\n", i+1, label, d)
	}
	// "warm" is reported as a min-max range across passes 2-5, not a
	// single number: see the NOTE below — pass-to-pass variance here is
	// real and large (up to ~2x), driven by Go GC pauses, not measurement
	// noise (confirmed by disabling GC in a side experiment: with GC off,
	// per-pass wall time is flat and Go heap grows monotonically instead;
	// with GC on, heap stays bounded but collections spike wall time).
	wMin, wMax := minMax(wPasses[1:])
	printScaledRange("tree-sitter wasm", wMin, wMax, totalLOC)
	fmt.Println("NOTE: wasm-side pass latency varies ~2x run to run (see pass 2 vs 3 above).")
	fmt.Println("      Root cause (isolated by re-running the corpus with GC disabled): every")
	fmt.Println("      malivvan/tree-sitter host call (Kind/Child/ChildCount/StartByte/...)")
	fmt.Println("      returns a freshly allocated []uint64 from wazero's api.Function.Call, and")
	fmt.Println("      this PoC's full-tree walk (no field-based child access in the binding — see")
	fmt.Println("      tswasm.go's doc comment) issues many thousands of such calls per file. That")
	fmt.Println("      allocation churn is real Go-heap garbage; with GC on it's reclaimed but the")
	fmt.Println("      collections show up as latency spikes; with GC off heap grows unbounded and")
	fmt.Println("      passes get monotonically slower. Either way this is a cost of the specific")
	fmt.Println("      binding's calling convention, not of wazero or wasm parsing in general — a")
	fmt.Println("      hand-written adapter batching node reads (e.g. one call returning a whole")
	fmt.Println("      subtree) would not pay this tax.")
	fmt.Println()

	// ---- (a) PARITY ----
	fmt.Println("=== (a) PARITY ===")
	matched, cOnly, tOnly := comparePar(files, oracleTriples, wasmTriples)
	fmt.Printf("matched (within +/-1 line)      : %d\n", matched)
	fmt.Printf("cSpec-only (oracle regressions)  : %d\n", len(cOnly))
	if len(cOnly) > 0 {
		fmt.Println("cSpec-only examples:")
		for i, e := range cOnly {
			if i >= 10 {
				fmt.Printf("  ... and %d more\n", len(cOnly)-10)
				break
			}
			fmt.Printf("  %s\n", e)
		}
	}
	fmt.Printf("tree-sitter-only (fidelity gain) : %d\n", len(tOnly))
	if len(tOnly) > 0 {
		fmt.Println("tree-sitter-only examples (first 5):")
		for i, e := range tOnly {
			if i >= 5 {
				break
			}
			fmt.Printf("  %s\n", e)
		}
	}
	fmt.Println()

	// Content hash of the generated synthetic corpus, for reproducibility
	// cross-checking against corpus/gen_test.go's logged hash.
	synth := corpus.Generate(seed)
	fmt.Printf("synthetic corpus sha256 (seed=%d, %d files): %x\n", seed, len(synth), corpusSHA(synth))

	return nil
}

func minMax(ds []time.Duration) (min, max time.Duration) {
	min, max = ds[0], ds[0]
	for _, d := range ds[1:] {
		if d < min {
			min = d
		}
		if d > max {
			max = d
		}
	}
	return min, max
}

func printScaledRange(label string, warmMin, warmMax time.Duration, totalLOC int) {
	if totalLOC == 0 {
		return
	}
	scale := func(d time.Duration) time.Duration {
		return time.Duration(float64(d) / float64(totalLOC) * 100000)
	}
	sMin, sMax := scale(warmMin), scale(warmMax)
	headroomMin := float64(5*time.Second) / float64(sMax) // worst-case pass -> worst-case headroom
	headroomMax := float64(5*time.Second) / float64(sMin)
	fmt.Printf("%s: warm %v-%v for %d LOC -> scaled to 100k LOC: %v-%v (M4 budget 5s, headroom %.1fx-%.1fx)\n",
		label, warmMin, warmMax, totalLOC, sMin, sMax, headroomMin, headroomMax)
}

// comparePar does a per-file, greedy +/-1-line match between oracle and
// wasm triples, per T-0040 part 3(a).
func comparePar(files []corpusFile, oracleTriples, wasmTriples [][]triple.Triple) (matched int, cOnly, tOnly []string) {
	for fi, f := range files {
		oT := oracleTriples[fi]
		wT := wasmTriples[fi]
		used := make([]bool, len(wT))
		for _, ot := range oT {
			found := -1
			for wi, wt := range wT {
				if used[wi] {
					continue
				}
				if wt.Key() != ot.Key() {
					continue
				}
				d := wt.Line - ot.Line
				if d < 0 {
					d = -d
				}
				if d <= 1 {
					found = wi
					break
				}
			}
			if found >= 0 {
				used[found] = true
				matched++
			} else {
				cOnly = append(cOnly, fmt.Sprintf("%s: %s", f.Path, ot))
			}
		}
		for wi, wt := range wT {
			if !used[wi] {
				tOnly = append(tOnly, fmt.Sprintf("%s: %s", f.Path, wt))
			}
		}
	}
	sort.Strings(cOnly)
	sort.Strings(tOnly)
	return matched, cOnly, tOnly
}

func corpusSHA(files map[string][]byte) [32]byte {
	names := make([]string, 0, len(files))
	for k := range files {
		names = append(names, k)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		h.Write([]byte(n))
		h.Write(files[n])
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// buildCorpus combines the deterministic synthetic corpus with this repo's
// real C-adjacent fixtures (examples/saxpy/saxpy/kernels/saxpy.h and any
// other .c/.h under examples/), per T-0040 part 1.
func buildCorpus(root string) ([]corpusFile, error) {
	synth := corpus.Generate(seed)
	var out []corpusFile
	for _, name := range corpus.SortedNames(synth) {
		out = append(out, corpusFile{Path: "corpus/" + name, Src: synth[name]})
	}

	examplesDir := filepath.Join(root, "examples")
	err := filepath.Walk(examplesDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if ext != ".c" && ext != ".h" {
			return nil
		}
		src, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		out = append(out, corpusFile{Path: filepath.ToSlash(rel), Src: src})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %s: %w", examplesDir, err)
	}
	return out, nil
}

// repoRoot locates the spectackle repo root from this source file's own
// location (poc/wasmparse/cmd/poc/main.go is three directories below root),
// independent of the process's current working directory.
func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller failed")
	}
	// .../poc/wasmparse/cmd/poc/main.go -> repo root is 4 levels up.
	root := filepath.Join(filepath.Dir(file), "..", "..", "..", "..")
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(abs, "go.mod")); err != nil {
		return "", fmt.Errorf("resolved repo root %s does not contain go.mod: %w", abs, err)
	}
	return abs, nil
}
