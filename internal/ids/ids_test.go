package ids

import "testing"

func TestMintParseRoundtrip(t *testing.T) {
	id := Mint("go", "saxpy.Saxpy")
	if id != "go:saxpy.Saxpy" || !Valid(id) {
		t.Fatalf("Mint = %q (valid=%v)", id, Valid(id))
	}
	lang, qual, err := Parse(id)
	if err != nil || lang != "go" || qual != "saxpy.Saxpy" {
		t.Fatalf("Parse = %q %q %v", lang, qual, err)
	}
	// whitespace is normalized away
	if got := Mint("cu", " saxpy kernel "); got != "cu:saxpy_kernel" {
		t.Fatalf("Mint with spaces = %q", got)
	}
}

func TestCollisionSuffix(t *testing.T) {
	id := WithCollision("c:init", 2)
	if id != "c:init~2" || !Valid(id) {
		t.Fatalf("WithCollision = %q (valid=%v)", id, Valid(id))
	}
	if _, _, err := Parse(id); err != nil {
		t.Fatalf("Parse collision ID: %v", err)
	}
}

func TestValid(t *testing.T) {
	for id, want := range map[string]bool{
		"go:pkg.Fn":     true,
		"asm:mat.mul":   true,
		"sec:gpu#intent": true,
		"go:pkg.Fn~2":   true,
		"go:":           false,
		"pkg.Fn":        false,
		"GO:pkg.Fn":     false, // lang tag is lowercase
		"go:a b":        false, // no whitespace
		"go:x~1":        false, // collision suffixes start at 2
	} {
		if got := Valid(id); got != want {
			t.Errorf("Valid(%q) = %v, want %v", id, got, want)
		}
	}
}
