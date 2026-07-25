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

// TestGitHubReadyUsesGraphQLMutation.
//
// The previous version of this test stubbed a PATCH carrying draft:false and
// asserted the stub was called — and it passed, while production did not work
// at all: GitHub's REST pulls endpoint accepts that PATCH, answers 200, and
// leaves the pull request a draft. A stub that returns what you hope for tests
// your hope, not the API. Found live, on a pull request the automation reported
// as readied and which GitHub still showed as a draft.
//
// So this test pins the mechanism that actually works: the
// markPullRequestReadyForReview GraphQL mutation, addressed by node ID.
func TestGitHubReadyUsesGraphQLMutation(t *testing.T) {
	var gotQuery string
	var gotVars map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		gotQuery, gotVars = body.Query, body.Variables
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":{"markPullRequestReadyForReview":{"pullRequest":{"number":42,"isDraft":false,"url":"https://github.com/jxsl13/spectackle/pull/42"}}}}`))
	}))
	t.Cleanup(srv.Close)

	g := &GitHub{Owner: "jxsl13", Repo: "spectackle", Token: "t", GraphQLURL: srv.URL}
	pr, err := g.Ready(PR{Number: 42, Branch: "b", NodeID: "PR_node_42"})
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if pr.Draft {
		t.Fatalf("Ready did not clear draft: %+v", pr)
	}
	if !strings.Contains(gotQuery, "markPullRequestReadyForReview") {
		t.Fatalf("Ready did not use the mutation: %q", gotQuery)
	}
	if gotVars["id"] != "PR_node_42" {
		t.Fatalf("Ready addressed the wrong node: %v", gotVars)
	}
}

// TestGitHubReadyRefusesToClaimSuccessWhileStillDraft: if the forge answers
// that the pull request is STILL a draft, that is an error, not a success to
// report. The whole defect this replaced was the automation announcing a ready
// pull request that was not one.
func TestGitHubReadyRefusesToClaimSuccessWhileStillDraft(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":{"markPullRequestReadyForReview":{"pullRequest":{"number":42,"isDraft":true,"url":"u"}}}}`))
	}))
	t.Cleanup(srv.Close)

	g := &GitHub{Owner: "jxsl13", Repo: "spectackle", Token: "t", GraphQLURL: srv.URL}
	if _, err := g.Ready(PR{Number: 42, Branch: "b", NodeID: "n"}); err == nil {
		t.Fatal("Ready reported success while the PR was still a draft")
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

// TestGitHubMergeOtherErrorIsAnError: a status that is neither success nor a
// classified refusal is an error. The example used to be 409 — until two live
// merges showed 405 and 409 are ROUTINE seconds after a push (mergeability
// recompute, head-out-of-date), so both are now ReasonNotReady values a
// caller retries. 500 stands in for the genuinely broken case.
func TestGitHubMergeOtherErrorIsAnError(t *testing.T) {
	g := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{"message": "boom"})
	})

	res, err := g.Merge(PR{Number: 42, Branch: "agent/forge-client"})
	if err == nil {
		t.Fatalf("expected an error for an unclassified status, got result %+v", res)
	}
}

// TestGitHubMergeTransientStatusesAreNotReady pins the classification the
// retry loop depends on: 405 and 409 answer ReasonNotReady, not an error and
// never ReasonNoPermission.
func TestGitHubMergeTransientStatusesAreNotReady(t *testing.T) {
	for _, status := range []int{http.StatusMethodNotAllowed, http.StatusConflict} {
		g := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(map[string]any{"message": "transient"})
		})
		res, err := g.Merge(PR{Number: 42, Branch: "b"})
		if err != nil {
			t.Fatalf("status %d became an error: %v", status, err)
		}
		if res.Merged || res.Reason != ReasonNotReady {
			t.Fatalf("status %d classified as %+v, want ReasonNotReady", status, res)
		}
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
