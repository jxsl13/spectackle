package forge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestGitHub wires a GitHub forge at an httptest server standing in for
// the real REST API, so these tests never touch the network.
func newTestGitHub(t *testing.T, handler http.HandlerFunc) *GitHub {
	t.Helper()
	// Every test runs with millisecond retry budgets: a test that answers 5xx
	// on purpose would otherwise sit in the PRODUCTION five-minute transport
	// retry — found when one such test quietly stretched the package run to
	// four minutes.
	shrinkRetryBudgets(t)
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

// TestGitHubChecksZeroRunsDisambiguates: an empty check-run list means
// "not started yet" in a repository that has workflows, and "no CI" only in
// one that has none. Observed live: the merge gate polled seconds after a
// push, got zero runs, read it as no-CI and merged before Actions started.
func TestGitHubChecksZeroRunsDisambiguates(t *testing.T) {
	for _, tc := range []struct {
		workflows string
		want      CheckState
	}{
		{`{"workflows":[{"state":"active"}]}`, ChecksPending},
		{`{"workflows":[{"state":"disabled_manually"}]}`, ChecksNone},
		{`{"workflows":[]}`, ChecksNone},
	} {
		wfCalls := 0
		g := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			if strings.Contains(r.URL.Path, "/actions/workflows") {
				wfCalls++
				w.Write([]byte(tc.workflows))
				return
			}
			w.Write([]byte(`{"check_runs":[]}`))
		})
		got, err := g.Checks(PR{Number: 1, Branch: "b"})
		if err != nil {
			t.Fatalf("Checks: %v", err)
		}
		if got != tc.want {
			t.Fatalf("workflows %s: Checks = %s, want %s", tc.workflows, got, tc.want)
		}
		// cached: a second ask must not re-query the workflows endpoint
		if _, err := g.Checks(PR{Number: 1, Branch: "b"}); err != nil {
			t.Fatal(err)
		}
		if wfCalls != 1 {
			t.Fatalf("workflows endpoint queried %d times, want 1 (cached)", wfCalls)
		}
	}
}

// TestGitHubChecksRealRunsUnaffected: actual runs still reduce as before —
// the disambiguation only ever fires on an empty list.
func TestGitHubChecksRealRunsUnaffected(t *testing.T) {
	g := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/actions/workflows") {
			t.Fatal("workflows endpoint consulted although runs exist")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"check_runs":[{"status":"completed","conclusion":"success"}]}`))
	})
	got, err := g.Checks(PR{Number: 1, Branch: "b"})
	if err != nil || got != ChecksPassing {
		t.Fatalf("Checks = %s, %v; want passing", got, err)
	}
}

// shrinkRetryBudgets makes the transport retry run at test speed and restores
// production values afterwards.
func shrinkRetryBudgets(t *testing.T) {
	t.Helper()
	budget, start, cap0 := transportRetryBudget, transportBackoffStart, transportBackoffCap
	transportRetryBudget, transportBackoffStart, transportBackoffCap = 50*time.Millisecond, time.Millisecond, 4*time.Millisecond
	t.Cleanup(func() {
		transportRetryBudget, transportBackoffStart, transportBackoffCap = budget, start, cap0
	})
}

// TestTransportRetryRecoversFrom5xx: two 503s then success — the operation
// succeeds and the caller never sees the flakes (the user's requirement:
// retry with backoff before passing any error through).
func TestTransportRetryRecoversFrom5xx(t *testing.T) {
	shrinkRetryBudgets(t)
	calls := 0
	g := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"check_runs":[{"status":"completed","conclusion":"success"}]}`))
	})
	state, err := g.Checks(PR{Number: 1, Branch: "b"})
	if err != nil || state != ChecksPassing {
		t.Fatalf("flaky 503s not absorbed: %s, %v", state, err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 attempts, got %d", calls)
	}
}

// TestTransportRetryBudgetSpentNamesAttempts: an always-503 endpoint survives
// the budget and the surfaced error names attempts and budget — retried, not
// dropped on first contact.
func TestTransportRetryBudgetSpentNamesAttempts(t *testing.T) {
	shrinkRetryBudgets(t)
	g := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	_, err := g.Checks(PR{Number: 1, Branch: "b"})
	if err == nil {
		t.Fatal("permanent 503 did not surface")
	}
	if !strings.Contains(err.Error(), "attempts within") {
		t.Fatalf("error does not name attempts and budget: %v", err)
	}
}

// TestTransportRetryNever4xx: a 422 is a semantic answer, not a flake —
// exactly one attempt, asserted by call count.
func TestTransportRetryNever4xx(t *testing.T) {
	shrinkRetryBudgets(t)
	calls := 0
	g := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"message":"nope"}`))
	})
	if _, err := g.Open("b", "main", "t", ""); err == nil {
		t.Fatal("422 must surface as an error")
	}
	if calls != 1 {
		t.Fatalf("4xx was retried: %d attempts", calls)
	}
}

// TestTransportRetryNetworkError: a genuinely dead endpoint (closed listener)
// is retried and then surfaced with the retry accounting.
func TestTransportRetryNetworkError(t *testing.T) {
	shrinkRetryBudgets(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // now every dial fails at the transport
	g := &GitHub{Owner: "o", Repo: "r", Token: "t", BaseURL: srv.URL}
	_, err := g.Checks(PR{Number: 1, Branch: "b"})
	if err == nil {
		t.Fatal("dead endpoint did not surface")
	}
	if !strings.Contains(err.Error(), "attempts within") {
		t.Fatalf("transport failure lacks retry accounting: %v", err)
	}
}

// TestGitHubChecksAllSkippedIsPendingWithCI: a head whose runs all concluded
// skipped — the draft-skip workflow's signature — has never been tested and
// must read Pending while workflows exist, or the await after the ready flip
// would merge untested work on the strength of runs that deliberately did
// nothing (B-01KYDN's predecessor-verdict defect in a new costume).
// TestGitHubChecksAllSkippedIsUnavailableWithCI: every run concluded, and
// every conclusion was skipped. That is not Passing (skipped tested nothing)
// and it is not Pending either — Pending promises that waiting resolves it,
// and nothing is in flight to resolve. It is Unavailable: only an event (the
// PR leaving draft, a new push) can produce a verdict for this head.
//
// This test previously asserted ChecksPending, and that assertion is what made
// every done edge poll to its deadline for a verdict that could not arrive
// (B-01KYZB4QA9FF4). The distinguishing case is
// TestGitHubChecksInFlightRunStaysPending below: keep both, because the whole
// value of the split is that these two inputs answer differently.
func TestGitHubChecksAllSkippedIsUnavailableWithCI(t *testing.T) {
	for _, tc := range []struct {
		workflows string
		want      CheckState
	}{
		{`{"workflows":[{"state":"active"}]}`, ChecksUnavailable},
		{`{"workflows":[]}`, ChecksNone},
	} {
		g := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			if strings.Contains(r.URL.Path, "/actions/workflows") {
				w.Write([]byte(tc.workflows))
				return
			}
			w.Write([]byte(`{"check_runs":[{"status":"completed","conclusion":"skipped"},{"status":"completed","conclusion":"skipped"}]}`))
		})
		got, err := g.Checks(PR{Number: 1, Branch: "b"})
		if err != nil || got != tc.want {
			t.Fatalf("all-skipped with workflows=%s: %s, %v; want %s", tc.workflows, got, err, tc.want)
		}
	}
}

// TestGitHubChecksInFlightRunStaysPending is the other half of the
// Pending/Unavailable split: a run that has NOT reached "completed" is coming,
// so the caller must still spend its budget waiting. If this ever answers
// Unavailable, the archive gate stops waiting for real CI and merges on the
// first poll — far worse than the stall the split was introduced to remove.
// The skipped sibling is present deliberately: a mixed head must be read by
// the in-flight run, not by the concluded one.
func TestGitHubChecksInFlightRunStaysPending(t *testing.T) {
	g := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if strings.Contains(r.URL.Path, "/actions/workflows") {
			w.Write([]byte(`{"workflows":[{"state":"active"}]}`))
			return
		}
		w.Write([]byte(`{"check_runs":[{"status":"completed","conclusion":"skipped"},{"status":"in_progress","conclusion":""}]}`))
	})
	got, err := g.Checks(PR{Number: 1, Branch: "b"})
	if err != nil || got != ChecksPending {
		t.Fatalf("in-flight run among skipped: %s, %v; want pending", got, err)
	}
}

// TestGitHubChecksRealGreenAmongSkippedPasses: one genuine success among
// skipped stubs is real evidence — Passing.
func TestGitHubChecksRealGreenAmongSkippedPasses(t *testing.T) {
	g := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"check_runs":[{"status":"completed","conclusion":"skipped"},{"status":"completed","conclusion":"success"}]}`))
	})
	got, err := g.Checks(PR{Number: 1, Branch: "b"})
	if err != nil || got != ChecksPassing {
		t.Fatalf("real green among skipped: %s, %v; want passing", got, err)
	}
}

// TestGitHubChecksPollsPinnedSHA (B-01KYDN): with HeadSHA set, the verdict is
// asked for that exact commit — a branch ref polled seconds after a push can
// still resolve to the previous head, whose concluded green would merge work
// that was never tested. Without a pin, the branch fallback stays.
func TestGitHubChecksPollsPinnedSHA(t *testing.T) {
	var gotPath string
	g := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"check_runs":[{"status":"completed","conclusion":"success"}]}`))
	})
	if _, err := g.Checks(PR{Number: 1, Branch: "b", HeadSHA: "deadbeef42"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotPath, "/commits/deadbeef42/check-runs") {
		t.Fatalf("pinned SHA not polled: %s", gotPath)
	}
	if _, err := g.Checks(PR{Number: 1, Branch: "feature/x"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotPath, "/commits/feature/x/check-runs") {
		t.Fatalf("branch fallback lost: %s", gotPath)
	}
}

// TestGitHubDraftUsesGraphQLMutation pins the reopen mirror (T-01KYDKNR8):
// convertPullRequestToDraft, addressed by node ID — REST cannot un-ready a
// pull request any more than it can un-draft one, and the Ready lesson
// (B-01KYDE: a stubbed PATCH tested the hope, not the API) applies verbatim
// in this direction.
func TestGitHubDraftUsesGraphQLMutation(t *testing.T) {
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
		w.Write([]byte(`{"data":{"convertPullRequestToDraft":{"pullRequest":{"number":42,"isDraft":true,"url":"https://github.com/jxsl13/spectackle/pull/42"}}}}`))
	}))
	t.Cleanup(srv.Close)

	g := &GitHub{Owner: "jxsl13", Repo: "spectackle", Token: "t", GraphQLURL: srv.URL}
	pr, err := g.Draft(PR{Number: 42, Branch: "b", NodeID: "PR_node_42"})
	if err != nil {
		t.Fatalf("Draft: %v", err)
	}
	if !pr.Draft {
		t.Fatalf("Draft did not set draft: %+v", pr)
	}
	if !strings.Contains(gotQuery, "convertPullRequestToDraft") {
		t.Fatalf("Draft did not use the mutation: %q", gotQuery)
	}
	if gotVars["id"] != "PR_node_42" {
		t.Fatalf("Draft addressed the wrong node: %v", gotVars)
	}
}

// TestGitHubDraftRefusesToClaimSuccessWhileStillReady: a 200 whose payload
// still shows isDraft false is an error, never a claimed success.
func TestGitHubDraftRefusesToClaimSuccessWhileStillReady(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":{"convertPullRequestToDraft":{"pullRequest":{"number":42,"isDraft":false,"url":"u"}}}}`))
	}))
	t.Cleanup(srv.Close)

	g := &GitHub{Owner: "jxsl13", Repo: "spectackle", Token: "t", GraphQLURL: srv.URL}
	if _, err := g.Draft(PR{Number: 42, Branch: "b", NodeID: "n"}); err == nil {
		t.Fatal("Draft reported success while the PR was still ready")
	}
}
