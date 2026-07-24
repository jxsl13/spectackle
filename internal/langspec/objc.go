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
			Re:   regexp.MustCompile(`^\s*[-+]\s*\([^)]*\)\s*(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KType,
			// `@interface Foo : NSObject` or `@implementation Foo`
			// Matches the class name after the @interface/@implementation keyword.
			Re:   regexp.MustCompile(`^@(?:interface|implementation)\s+(\w+)`),
			Name: 1,
		},
		{
			Kind: graph.KFunc,
			// Plain C functions in .m files: `int foo(x) {`, `static void bar() {`
			// Captures name and parameter list. Prototypes (ending in `;`) never match
			// because they require a `{` on the same line.
			Re:   regexp.MustCompile(`^\s*(?:static\s+)?(?:[A-Za-z_][\w\s*]*?)\s+(\w+)\s*\(([^;{]*)\)\s*\{`),
			Name: 1,
			Sig:  2,
		},
	},
	CallRe: regexp.MustCompile(`\[\s*\w+\s+(\w+)`),
	Stop:   []string{"alloc", "init", "release", "retain", "autorelease", "copy", "dealloc", "self", "super"},
}

func init() { registry = append(registry, objcSpec) }
