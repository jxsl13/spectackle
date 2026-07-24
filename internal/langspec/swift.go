package langspec

import (
	"regexp"

	"github.com/jxsl13/spectacle/internal/graph"
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
			// `convenience init() {}`. Captured as the literal name `init`.
			Re:   regexp.MustCompile(`^\s*(?:(?:public|private|fileprivate|internal|required|convenience)\s+)*(init)\s*\(`),
			Name: 1,
		},
	},
}

func init() { registry = append(registry, swiftSpec) }
