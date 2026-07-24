package index

import (
	"path/filepath"
	"strings"

	"github.com/jxsl13/spectackle/internal/graph"
)

// extLang maps file extensions to languages. This is the single source of
// truth for language detection; parsers additionally register their own
// extensions via LanguageParser.Extensions.
var extLang = map[string]graph.Lang{
	".go":     graph.LangGo,
	".c":      graph.LangC,
	".h":      graph.LangC, // headers default to C; resolvers may reclassify
	".cc":     graph.LangCpp,
	".cpp":    graph.LangCpp,
	".cxx":    graph.LangCpp,
	".hpp":    graph.LangCpp,
	".cu":     graph.LangCuda,
	".cuh":    graph.LangCuda,
	".s":      graph.LangAsm,
	".S":      graph.LangAsm,
	".m":      graph.LangObjC,
	".mm":     graph.LangObjC,
	".metal":  graph.LangMSL,
	".py":     graph.LangPy,
	".js":     graph.LangJS,
	".mjs":    graph.LangJS,
	".ts":     graph.LangTS,
	".tsx":    graph.LangTS,
	".rs":     graph.LangRs,
	".java":   graph.LangJava,
	".rb":     graph.LangRb,
	".php":    graph.LangPHP,
	".kt":     graph.LangKt,
	".kts":    graph.LangKt,
	".swift":  graph.LangSwift,
	".cs":     graph.LangCs,
	".scala":  graph.LangScala,
	".sh":     graph.LangSh,
	".bash":   graph.LangSh,
	".lua":    graph.LangLua,
	".zig":    graph.LangZig,
	".pl":     graph.LangPerl,
	".pm":     graph.LangPerl,
	".dart":   graph.LangDart,
	".groovy": graph.LangGroovy,
	".ex":     graph.LangEx,
	".exs":    graph.LangEx,
	".erl":    graph.LangErl,
	".hrl":    graph.LangErl,
	".jl":     graph.LangJl,
	".hs":     graph.LangHs,
	".ml":     graph.LangMl,
	".mli":    graph.LangMl,
	".r":      graph.LangR,
	".f90":    graph.LangF90,
	".f95":    graph.LangF90,
	".f03":    graph.LangF90,
	".f08":    graph.LangF90,
	".comp":   graph.LangGLSL,
	".vert":   graph.LangGLSL,
	".frag":   graph.LangGLSL,
	".geom":   graph.LangGLSL,
	".tesc":   graph.LangGLSL,
	".tese":   graph.LangGLSL,
	".glsl":   graph.LangGLSL,
}

// LangOf returns the language for a path, or "" if unrecognized.
func LangOf(path string) graph.Lang {
	ext := filepath.Ext(path)
	if l, ok := extLang[ext]; ok {
		return l
	}
	// .S (uppercase, preprocessed asm) on case-insensitive lookups
	if l, ok := extLang[strings.ToLower(ext)]; ok {
		return l
	}
	return ""
}
