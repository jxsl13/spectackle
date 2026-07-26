package ears

// Composer keyword normalization + the W003 doubled-keyword lint
// (B-01KYFSZ7): a slot value leading with its own keyword no longer
// doubles it; already-damaged sentences surface as findings.

import (
	"strings"
	"testing"
)

func TestComposeStripsOwnKeyword(t *testing.T) {
	cases := []struct {
		p    Pattern
		s    Slots
		want string
	}{
		{PEvent, Slots{System: "server", Response: "log the event to audit.log", Trigger: "WHEN a request arrives"},
			"WHEN a request arrives, the server SHALL log the event to audit.log."},
		{PEvent, Slots{System: "server", Response: "log the event to audit.log", Trigger: "when a request arrives"},
			"WHEN a request arrives, the server SHALL log the event to audit.log."},
		{PState, Slots{System: "server", Response: "hold the lock file open", State: "WHILE serving requests"},
			"WHILE serving requests, the server SHALL hold the lock file open."},
		{PUnwanted, Slots{System: "server", Response: "refuse with error E001", Condition: "IF the input is malformed"},
			"IF the input is malformed, THEN the server SHALL refuse with error E001."},
		// mid-sentence keywords survive untouched
		{PEvent, Slots{System: "server", Response: "log the event to audit.log", Trigger: "a request arrives even if malformed"},
			"WHEN a request arrives even if malformed, the server SHALL log the event to audit.log."},
	}
	for _, c := range cases {
		got, err := Compose(c.p, c.s)
		if err != nil {
			t.Fatalf("compose: %v", err)
		}
		if got != c.want {
			t.Errorf("got  %q\nwant %q", got, c.want)
		}
	}
}

func TestLintDoubledKeyword(t *testing.T) {
	fs := LintSentence("WHEN WHEN a request arrives, the server SHALL log it to audit.log.", "f", 1)
	found := false
	for _, f := range fs {
		if f.Code == "W003" && strings.Contains(f.Msg, "WHEN WHEN") {
			found = true
		}
	}
	if !found {
		t.Fatalf("doubled keyword not flagged: %v", fs)
	}
	for _, f := range LintSentence("WHEN a request arrives, the server SHALL log it to audit.log.", "f", 1) {
		if f.Code == "W003" {
			t.Fatalf("clean sentence flagged W003: %v", f)
		}
	}
}
