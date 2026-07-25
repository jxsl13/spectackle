package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// swiftSpec covers .swift files. Qualification is QualFileStem, mirroring
// pythonSpec and rustSpec: one file is treated as one module, so `func run`
// in App.swift mints "swift:App.run".
var swiftSpec = Spec{
	Lang: graph.Lang("swift"),
	Exts: []string{".swift"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KFunc,
			// `func run(x: Int) -> Int {`, `public static func make() {}`,
			// `override func viewDidLoad() {}`, `@objc func handleTap() {}`.
			Re:   regexp.MustCompile(`^\s*(?:(?:public|private|fileprivate|internal|open|static|class|override|final|mutating|nonmutating|convenience|required|@\w+)\s+)*func\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `class Foo {`, `struct Point {`, `enum Color {`,
			// `protocol Shape {`, `actor Counter {`, `extension Foo {}`.
			Re:   regexp.MustCompile(`^\s*(?:(?:public|private|fileprivate|internal|open|final|indirect|@\w+)\s+)*(?:class|struct|enum|protocol|actor|extension)\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// `init(x: Int) {`, `public required init?(coder: NSCoder) {}`,
			// `convenience init() {}`, `override init(name: String) {}`
			// (R-0005: `override` was missing from the modifier whitelist —
			// a subclass initializer override is one of the most common init
			// shapes in idiomatic Swift). Captured as the literal name
			// `init`. The `[?!]?` between `init` and `(` (R-0005) admits
			// failable (`init?`) and implicitly-unwrapped-failable (`init!`)
			// initializers, which the old `(init)\s*\(` shape (nothing
			// allowed between the literal and the paren) rejected outright.
			Re:   regexp.MustCompile(`^\s*(?:(?:public|private|fileprivate|internal|required|convenience|override)\s+)*(init)[?!]?\s*\(`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// `subscript(index: Int) -> Double { ... }` (R-0005: no Def
			// matched subscript declarations at all).
			Re:   regexp.MustCompile(`^\s*(?:(?:public|private|fileprivate|internal|static|final)\s+)*(subscript)\s*\(`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// `deinit { ... }` (R-0005, low severity but free to add
			// alongside subscript above: same one-line Def shape).
			Re:   regexp.MustCompile(`^\s*(deinit)\s*\{`),
			Name: 1,
		},
	},

	// CallRe/Stop (LSP-001): Swift function/init/type bodies are
	// brace-delimited exactly like C/C++/Java, so the same brace-counted
	// cspan.Span machinery applies directly (R-0005: swiftSpec previously
	// left CallRe nil, which meant zero call edges, ever, for any Swift
	// file, and also meant every node's span collapsed to its declaration
	// line regardless of body length).
	CallRe: regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\(`),
	Stop:   swiftCallStop,
}

// swiftCallStop lists Swift control-flow keywords whose `name (` syntax
// structurally matches CallRe but is never a call into a repo symbol.
var swiftCallStop = []string{
	"if", "for", "while", "switch", "catch", "guard", "return",
}

func init() { registry = append(registry, swiftSpec) }
