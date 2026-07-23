package index

import (
	"path/filepath"
	"strings"

	"github.com/jxsl13/spectacle/internal/graph"
)

// extLang maps file extensions to languages. This is the single source of
// truth for language detection; parsers additionally register their own
// extensions via LanguageParser.Extensions.
var extLang = map[string]graph.Lang{
	".go":    graph.LangGo,
	".c":     graph.LangC,
	".h":     graph.LangC, // headers default to C; resolvers may reclassify
	".cc":    graph.LangCpp,
	".cpp":   graph.LangCpp,
	".cxx":   graph.LangCpp,
	".hpp":   graph.LangCpp,
	".cu":    graph.LangCuda,
	".cuh":   graph.LangCuda,
	".s":     graph.LangAsm,
	".S":     graph.LangAsm,
	".m":     graph.LangObjC,
	".mm":    graph.LangObjC,
	".metal": graph.LangMSL,
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
