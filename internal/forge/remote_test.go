package forge

import "testing"

func TestParseRemote(t *testing.T) {
	cases := []struct {
		remote      string
		owner, repo string
	}{
		{"https://github.com/jxsl13/spectackle", "jxsl13", "spectackle"},
		{"https://github.com/jxsl13/spectackle.git", "jxsl13", "spectackle"},
		{"git@github.com:jxsl13/spectackle.git", "jxsl13", "spectackle"},
		{"git@github.com:jxsl13/spectackle", "jxsl13", "spectackle"},
	}
	for _, c := range cases {
		owner, repo, err := ParseRemote(c.remote)
		if err != nil {
			t.Fatalf("ParseRemote(%q): %v", c.remote, err)
		}
		if owner != c.owner || repo != c.repo {
			t.Fatalf("ParseRemote(%q) = %q, %q; want %q, %q", c.remote, owner, repo, c.owner, c.repo)
		}
	}
}

func TestParseRemoteRejectsUnrecognizedForm(t *testing.T) {
	for _, remote := range []string{
		"",
		"not-a-remote-at-all",
		"ftp://example.com/owner/repo",
		"https://github.com/onlyowner",
	} {
		if _, _, err := ParseRemote(remote); err == nil {
			t.Fatalf("ParseRemote(%q): expected an error, got nil", remote)
		}
	}
}
