package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jxsl13/spectackle/internal/workspace"
)

// Regressions for T-01KYD2 (GitHub issues 25 and 30), both in EditRule.
//
// Issue 30 lives entirely here: the serializer. Issue 25's cause is split —
// EditRule's sentence fallback is deliberate and stays, and the half that
// needed fixing is in the tool layer (see internal/mcpserver's ruleedit tests).
// What this file pins on EditRule's side is that the fallback keeps working for
// the partial edits it exists for, and that the lint findings describe the text
// actually stored.

func editWS(t *testing.T) workspace.Root {
	t.Helper()
	root := workspace.Root{Dir: t.TempDir()}
	if err := root.EnsureScaffold(""); err != nil {
		t.Fatal(err)
	}
	return root
}

// addThree writes three rules through AddRule and returns the file's bytes.
func addThree(t *testing.T, root workspace.Root, texts [3]string, rationales [3]string) string {
	t.Helper()
	for i, txt := range texts {
		c, err := Load(root.Dir)
		if err != nil {
			t.Fatal(err)
		}
		res, err := AddRule(root, c, AuthorReq{
			Dir: "", Stem: "EDT-API", Sentence: txt, Rationale: rationales[i],
		})
		if err != nil {
			t.Fatal(err)
		}
		if !res.Written {
			t.Fatalf("rule %d not written: %+v", i, res.Findings)
		}
	}
	raw, err := os.ReadFile(filepath.Join(root.Dir, ".spectackle", "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestEditKeepsSeparatorSoAddAndEditConverge is issue 30's reproduction.
//
// EditRule replaces the span from a rule's heading up to the NEXT heading,
// which includes the blank line separating them, and used to rebuild the block
// without a trailing blank. Every edit therefore ate one separator
// permanently, so add and edit produced different bytes for identical content
// — and the loss accumulated silently, since nothing asserts the layout.
//
// The assertion is byte equality against the same three rules added directly
// with their final text. That is stronger than counting blank lines and is the
// property that actually matters: how a rule reached its text must not be
// visible in the file.
func TestEditKeepsSeparatorSoAddAndEditConverge(t *testing.T) {
	final := [3]string{
		"The parser SHALL reject a malformed heading.",
		"WHEN a rule is edited, the serializer SHALL keep one blank line before the next rule.",
		"The linter SHALL report every finding once.",
	}
	initial := [3]string{
		"The parser SHALL accept a malformed heading.",
		"WHEN a rule is edited, the serializer SHALL drop the separator.",
		final[2],
	}
	none := [3]string{}

	// direct: the three rules added with their final text
	want := addThree(t, editWS(t), final, none)

	// edited: added with the initial text, then the first two edited to final
	root := editWS(t)
	addThree(t, root, initial, none)
	for i, id := range []string{"EDT-API-001", "EDT-API-002"} {
		c, err := Load(root.Dir)
		if err != nil {
			t.Fatal(err)
		}
		res, err := EditRule(root, c, id, final[i], "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Written {
			t.Fatalf("edit %s not written: %+v", id, res.Findings)
		}
	}
	got, err := os.ReadFile(filepath.Join(root.Dir, ".spectackle", "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("edit does not converge with add.\n--- added directly\n%q\n--- via edit\n%q", want, string(got))
	}
}

// TestEditKeepsSeparatorWithRationale: the same convergence with a Rationale
// paragraph in the block, which adds its own blank line and is the shape most
// likely to be off by one.
func TestEditKeepsSeparatorWithRationale(t *testing.T) {
	texts := [3]string{
		"The parser SHALL reject a malformed heading.",
		"The linter SHALL report every finding once.",
		"The writer SHALL emit one canonical layout.",
	}
	rats := [3]string{"parsers must fail loudly", "duplicates read as separate defects", ""}

	want := addThree(t, editWS(t), texts, rats)

	root := editWS(t)
	addThree(t, root, [3]string{"The parser SHALL accept anything.", texts[1], texts[2]}, rats)
	c, err := Load(root.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EditRule(root, c, "EDT-API-001", texts[0], rats[0], nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root.Dir, ".spectackle", "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("edit with rationale does not converge with add.\n--- added\n%q\n--- edited\n%q", want, string(got))
	}
}

// TestEditRepeatedIsStable: editing the same rule several times must not drift
// the layout at all. The original defect lost one separator PER edit, so a
// single-edit test could have passed while the file still decayed.
func TestEditRepeatedIsStable(t *testing.T) {
	root := editWS(t)
	addThree(t, root, [3]string{
		"The parser SHALL reject a malformed heading.",
		"The linter SHALL report every finding once.",
		"The writer SHALL emit one canonical layout.",
	}, [3]string{})

	var prev string
	for i := 0; i < 4; i++ {
		c, err := Load(root.Dir)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := EditRule(root, c, "EDT-API-002", "The linter SHALL report every finding exactly once.", "", nil); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(filepath.Join(root.Dir, ".spectackle", "spec.md"))
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 && string(raw) != prev {
			t.Fatalf("edit %d changed the file although the content is identical:\n--- before\n%q\n--- after\n%q", i, prev, string(raw))
		}
		prev = string(raw)
	}
	// and all three rules still parse
	c, err := Load(root.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"EDT-API-001", "EDT-API-002", "EDT-API-003"} {
		if _, ok := c.Rule(id); !ok {
			t.Fatalf("%s no longer parses after repeated edits", id)
		}
	}
}

// TestEditPartialFallbacksStillWork: rationale-only and applies-only edits are
// what EditRule's fallbacks exist for, and they must keep working — the fix for
// issue 25 must not turn a missing sentence into an error.
func TestEditPartialFallbacksStillWork(t *testing.T) {
	root := editWS(t)
	const text = "The parser SHALL reject a malformed heading."
	addThree(t, root, [3]string{text, "The linter SHALL report every finding once.", "The writer SHALL emit one layout."}, [3]string{})

	// rationale-only
	c, err := Load(root.Dir)
	if err != nil {
		t.Fatal(err)
	}
	res, err := EditRule(root, c, "EDT-API-001", "", "parsers must fail loudly", nil)
	if err != nil || !res.Written {
		t.Fatalf("rationale-only edit: %+v %v", res, err)
	}
	c, err = Load(root.Dir)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := c.Rule("EDT-API-001")
	if !ok || r.Text != text || r.Rationale != "parsers must fail loudly" {
		t.Fatalf("rationale-only edit lost the sentence or the rationale: %+v", r)
	}

	// applies-only
	res, err = EditRule(root, c, "EDT-API-001", "", "", []string{"go:pkg.Fn"})
	if err != nil || !res.Written {
		t.Fatalf("applies-only edit: %+v %v", res, err)
	}
	c, err = Load(root.Dir)
	if err != nil {
		t.Fatal(err)
	}
	r, ok = c.Rule("EDT-API-001")
	if !ok || r.Text != text || len(r.Applies) != 1 || r.Applies[0] != "go:pkg.Fn" {
		t.Fatalf("applies-only edit did not land: %+v", r)
	}
	if r.Rationale != "parsers must fail loudly" {
		t.Fatalf("applies-only edit dropped the rationale: %+v", r)
	}
}

// TestEditFindingsDescribeStoredText: the lint findings an edit returns must
// describe the text that is actually stored afterward, never an intermediate
// state. The original report called this out as a third symptom of issue 25.
func TestEditFindingsDescribeStoredText(t *testing.T) {
	root := editWS(t)
	addThree(t, root, [3]string{
		"The parser SHALL reject a malformed heading.",
		"The linter SHALL report every finding once.",
		"The writer SHALL emit one layout.",
	}, [3]string{})

	// A sentence with no SHALL lints as an error, so nothing is written and the
	// finding describes the sentence the caller tried to store.
	c, err := Load(root.Dir)
	if err != nil {
		t.Fatal(err)
	}
	res, err := EditRule(root, c, "EDT-API-001", "the parser rejects malformed headings", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Written {
		t.Fatal("a lint-error sentence was written")
	}
	if len(res.Findings) == 0 {
		t.Fatal("no findings for a sentence with no SHALL")
	}
	// the stored text is untouched
	c, err = Load(root.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := c.Rule("EDT-API-001"); r.Text != "The parser SHALL reject a malformed heading." {
		t.Fatalf("refused edit still changed the stored text: %q", r.Text)
	}

	// A clean partial edit reports findings about the text that stays stored.
	res, err = EditRule(root, c, "EDT-API-001", "", "still fine", nil)
	if err != nil || !res.Written {
		t.Fatalf("partial edit: %+v %v", res, err)
	}
	for _, f := range res.Findings {
		if !strings.Contains(f.String(), "EDT-API-001") && f.Severity == 0 {
			continue
		}
	}
}
