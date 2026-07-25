package forge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestGitHub wires a GitHub forge at an httptest server standing in for
// the real REST API, so these tests never touch the network.
func newTestGitHub(t *testing.T, handler http.HandlerFunc) *GitHub {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &GitHub{Owner: "jxsl13", Repo: "spectackle", Token: "test-token", BaseURL: srv.URL}
}

func TestGitHubOpenCreatesDraft(t *testing.T) {
	var gotAuth, gotMethod, gotPath string
	var gotBody map[string]any
	g := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotMethod, gotPath = r.Header.Get("Authorization"), r.Method, r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"number": 42, "html_url": "https://github.com/jxsl13/spectackle/pull/42", "draft": true,
		})
	})

	pr, err := g.Open("agent/forge-client", "main", "add forge client", "body text")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if pr.Number != 42 || !pr.Draft || pr.URL == "" {
		t.Fatalf("Open PR = %+v", pr)
	}
	if gotMethod != http.MethodPost || gotPath != "/repos/jxsl13/spectackle/pulls" {
		t.Fatalf("Open request = %s %s", gotMethod, gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("Authorization header = %q", gotAuth)
	}
	if draft, _ := gotBody["draft"].(bool); !draft {
		t.Fatalf("Open request body did not request draft:true: %v", gotBody)
	}
	if head, _ := gotBody["head"].(string); head != "agent/forge-client" {
		t.Fatalf("Open request body head = %v", gotBody["head"])
	}
}

func TestGitHubReadyClearsDraft(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	g := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"number": 42, "html_url": "https://github.com/jxsl13/spectackle/pull/42", "draft": false,
		})
	})

	pr, err := g.Ready(PR{Number: 42, Branch: "agent/forge-client"})
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if pr.Draft {
		t.Fatalf("Ready did not clear draft: %+v", pr)
	}
	if gotMethod != http.MethodPatch || gotPath != "/repos/jxsl13/spectackle/pulls/42" {
		t.Fatalf("Ready request = %s %s", gotMethod, gotPath)
	}
	if draft, ok := gotBody["draft"].(bool); !ok || draft {
		t.Fatalf("Ready request body did not send draft:false: %v", gotBody)
	}
}

func TestGitHubMergeUsesMergeMethodNeverSquash(t *testing.T) {
	var gotBody map[string]any
	g := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"merged": true, "sha": "abc123"})
	})

	res, err := g.Merge(PR{Number: 42, Branch: "agent/forge-client"})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if !res.Merged || res.SHA != "abc123" || res.Reason != "" {
		t.Fatalf("Merge result = %+v", res)
	}
	if m, _ := gotBody["merge_method"].(string); m != "merge" {
		t.Fatalf("merge_method = %q, want merge (never squash)", gotBody["merge_method"])
	}
}

func TestGitHubMergeForbiddenDegradesNotFails(t *testing.T) {
	g := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]any{"message": "Resource not accessible by integration"})
	})

	res, err := g.Merge(PR{Number: 42, Branch: "agent/forge-client"})
	if err != nil {
		t.Fatalf("Merge should degrade, not error, on 403: %v", err)
	}
	if res.Merged {
		t.Fatalf("Merge reported success on a 403: %+v", res)
	}
	if res.Reason != ReasonNoPermission {
		t.Fatalf("Merge Reason = %q, want %q", res.Reason, ReasonNoPermission)
	}
}

func TestGitHubMergeOtherErrorIsAnError(t *testing.T) {
	g := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict) // e.g. merge conflict / not mergeable
		json.NewEncoder(w).Encode(map[string]any{"message": "not mergeable"})
	})

	res, err := g.Merge(PR{Number: 42, Branch: "agent/forge-client"})
	if err == nil {
		t.Fatalf("expected an error for a non-403 non-200 status, got result %+v", res)
	}
	if !strings.Contains(err.Error(), "409") && !strings.Contains(err.Error(), "Conflict") {
		t.Fatalf("error does not name the status: %v", err)
	}
}

func TestGitHubFindReturnsExistingPR(t *testing.T) {
	var gotPath string
	g := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]map[string]any{
			{"number": 42, "html_url": "https://github.com/jxsl13/spectackle/pull/42", "draft": true,
				"base": map[string]any{"ref": "main"}},
		})
	})

	pr, ok, err := g.Find("agent/forge-client")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if !ok {
		t.Fatal("Find did not report the existing PR")
	}
	if pr.Number != 42 || pr.Base != "main" {
		t.Fatalf("Find PR = %+v", pr)
	}
	if !strings.Contains(gotPath, "head=jxsl13%3Aagent%2Fforge-client") {
		t.Fatalf("Find did not scope the query to owner:branch: %s", gotPath)
	}
}

func TestGitHubFindNoneOpen(t *testing.T) {
	g := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]map[string]any{})
	})

	_, ok, err := g.Find("agent/forge-client")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if ok {
		t.Fatal("Find reported a PR when none is open")
	}
}
