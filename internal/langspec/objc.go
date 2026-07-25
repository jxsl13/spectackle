package langspec

import (
	"regexp"

	"github.com/jxsl13/spectackle/internal/graph"
)

// objcSpec covers Objective-C files (.m and .mm). Qualification is
// QualFileStem: viewcontroller.m's `- (void)viewDidLoad` mints
// "objc:viewcontroller.viewDidLoad".
//
// Objective-C source embeds C, so objcSpec parses three shapes: Objective-C
// methods (both instance `-` and class `+`), Objective-C type definitions
// (`@interface` and `@implementation`), and plain C functions defined
// directly in the file (brace-delimited functions, not prototypes).
//
// CallRe captures Objective-C message send call sites (`[obj message]`),
// extracting the first selector segment (before any colons in multi-part
// selectors). Stop lists memory-management and identity keywords (alloc, init,
// release, retain, autorelease, copy, dealloc, self, super) whose syntax
// looks like a call but isn't an application-level call. Cross-file message
// sends dangle until a future bridging resolver wires them.
var objcSpec = Spec{
	Lang: graph.LangObjC,
	Exts: []string{".m", ".mm"},
	Qual: QualFileStem,
	Defs: []Def{
		{
			Kind: graph.KMethod,
			// `- (void)viewDidLoad {`, `+ (instancetype)sharedInstance {`
			// Captures the first selector segment (the method name before any
			// colons in multi-part selectors; single-part is most common).
			// After Name, the rest of the line must not cleanly end in a bare
			// `;` (a single-line declaration/prototype, e.g. `- (void)foo;`
			// inside an @interface/@protocol) — `[^;{}]*` only reaches `$`
			// when nothing forces a stop, i.e. when the line ends in `{...}`
			// (a real one-liner/K&R body) or in neither `;` nor `{` at all (a
			// selector that continues onto a following line — see
			// objc.go's package doc for why the multi-line case is only a
			// partial fix). This closes R-0005 objc.md's [high]
			// "@interface/@protocol method prototypes ... matched by the
			// same KMethod Def regex as real definitions" finding for every
			// single-line declaration (the common case).
			Re:   regexp.MustCompile(`^\s*[-+]\s*\([^)]*\)\s*(\w+)[^;{}]*(?:\{.*)?$`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `@interface Foo : NSObject ...` or `@implementation Foo`
			// (bare, or with a base-class/protocol-list tail starting with
			// `:`). Deliberately excludes the category shape (`@interface
			// Foo (Cat)`), which the dedicated Def below mints as a distinct
			// name instead — without this exclusion both Defs would fire on
			// a category line and mint two colliding "Foo" nodes.
			Re:   regexp.MustCompile(`^@(?:interface|implementation)\s+(\w+)\s*(?::|$)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// Category: `@interface Foo (Helpers)` / `@implementation Foo
			// (Helpers)` — captures the combined "Foo (Helpers)" text as one
			// distinct name so it no longer collides with the base class's
			// own "Foo" KType node (R-0005 objc.md's [medium] "Category
			// syntax ... captured by the same KType regex as the base
			// class" finding).
			Re:   regexp.MustCompile(`^@(?:interface|implementation)\s+(\w+\s*\([^)]*\))`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `@protocol SomeProtocol` — R-0005 objc.md's [high] "@protocol
			// declarations are not matched by any Def pattern" finding.
			Re:   regexp.MustCompile(`^@protocol\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// Plain C functions in .m files: `int foo(x) {`, `static void
			// bar() {`, and now also an Allman-style signature with the
			// opening `{` deferred to a following line (R-0005 objc.md's
			// [high] "C function definition with Allman-style brace"
			// finding) — the trailing alternation mirrors cSpec's Def:
			// balanced params + optional `;`/`{...}` tail. Prototypes
			// (ending in `;`) still never match.
			Re:   regexp.MustCompile(`^\s*(?:static\s+)?(?:[A-Za-z_][\w\s*]*?)\s+(\w+)\s*\(([^;{]*)\)\s*(?:\{.*)?$`),
			Name: 1,
			Sig:  2,
		},
	},
	// CallRe: SpecParser.callEdges always reads submatch 1 (see
	// langspec.go's callEdges), and Go's regexp/RE2 supports neither
	// lookaround nor branch-reset groups, so every alternative below must
	// share the *same* (and only) capturing group textually — the varying
	// part is expressed entirely as non-capturing context around it. This
	// single Def now covers three call shapes at once:
	//   - `[receiver message...]` / `[receiver.prop message...]`: a bracket
	//     message send whose receiver is a bare identifier, optionally with
	//     one dot-syntax property access (R-0005 objc.md's [high] "Message
	//     send whose receiver uses property dot-syntax" finding). No
	//     mandatory suffix is needed for these: the leading `[` context
	//     already makes them distinctive.
	//   - `[[...] message...]`: a nested/chained send whose receiver is
	//     itself a bracketed expression, e.g. `[[self view]
	//     addSubview:x]` — captures the *outer* message name (R-0005
	//     objc.md's [medium] "Nested/chained message sends" finding); the
	//     inner send's own receiver text is consumed as opaque,
	//     non-capturing context, so it is not separately reported by this
	//     same scan.
	//   - a bare identifier with no bracket context at all, gated on a
	//     mandatory trailing `(`, `]`, or `:` (R-0005 objc.md's [high]
	//     "Plain C function calling another plain C function ... never
	//     produces a call edge" finding — ordinary calls end in `(`; `]`/`:`
	//     cover a message name whose own bracket-context match happened to
	//     be absorbed as part of an outer send). Requiring one of these
	//     three characters immediately after is what keeps this branch from
	//     matching ordinary identifiers (e.g. local variable names) that
	//     aren't actually being called or sent a message.
	CallRe: regexp.MustCompile(`(?:\[\s*(?:\[[^\[\]]*\]|\w+(?:\.\w+)?)\s+)?(\w+)\s*(?:\(|[\]:])`),
	Stop:   append([]string{"alloc", "init", "release", "retain", "autorelease", "copy", "dealloc", "self", "super"}, cFamilyCallStop...),
}

func init() { registry = append(registry, objcSpec) }
