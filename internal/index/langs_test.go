package index

import (
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
)

func TestLangOf(t *testing.T) {
	for path, want := range map[string]graph.Lang{
		"a/b.go":       graph.LangGo,
		"x.c":          graph.LangC,
		"x.h":          graph.LangC,
		"x.cu":         graph.LangCuda,
		"x.cuh":        graph.LangCuda,
		"x.cpp":        graph.LangCpp,
		"mat_amd64.s":  graph.LangAsm,
		"boot.S":       graph.LangAsm,
		"v.m":          graph.LangObjC,
		"shader.metal": graph.LangMSL,
		"README.md":    "",
		"noext":        "",
	} {
		if got := LangOf(path); got != want {
			t.Errorf("LangOf(%q) = %q, want %q", path, got, want)
		}
	}
}
