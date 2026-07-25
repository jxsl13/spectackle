package forge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// defaultAPIBase is GitHub's REST API root. Tests override GitHub.BaseURL
// with an httptest server so no test ever reaches the real network.
const defaultAPIBase = "https://api.github.com"

var _ Forge = (*GitHub)(nil)

// GitHub implements Forge over the REST pulls API using net/http only — no
// gh binary, no new module dependency. A second auth system plus a binary
// dependency is exactly what Token's credential-helper fallback exists to
// make unnecessary.
type GitHub struct {
	Owner   string
	Repo    string
	Token   string
	BaseURL string // API root; empty means defaultAPIBase
	// GraphQLURL is the GraphQL endpoint; empty means GitHub's own. Ready is
	// the only caller (see there), and tests point it at an httptest server.
	GraphQLURL string

	// workflow-existence cache, filled by hasWorkflows on first use: whether
	// this repository has any active workflow decides how an empty check-run
	// list reads (see Checks), and it does not change within a process's
	// lifetime often enough to be worth re-asking per merge.
	workflowsKnown bool
	workflowsExist bool
	Client         *http.Client // empty means http.DefaultClient
}

// NewGitHub builds a GitHub forge from a git remote URL (as returned by
// `git remote get-url origin`, either the https or the ssh form ParseRemote
// accepts) and resolves credentials via Token. The credential host is fixed
// to github.com: that is the only host this implementation ever talks to,
// GitHub Enterprise is out of scope here.
func NewGitHub(remote string, runner CredentialRunner) (*GitHub, error) {
	owner, repo, err := ParseRemote(remote)
	if err != nil {
		return nil, err
	}
	token, err := Token("github.com", runner)
	if err != nil {
		return nil, err
	}
	return &GitHub{Owner: owner, Repo: repo, Token: token}, nil
}

func (g *GitHub) baseURL() string {
	if g.BaseURL != "" {
		return g.BaseURL
	}
	return defaultAPIBase
}

func (g *GitHub) client() *http.Client {
	if g.Client != nil {
		return g.Client
	}
	return http.DefaultClient
}

// ghPull mirrors the subset of GitHub's pull-request JSON object this
// package reads. Only the fields Open/Ready/Merge/Find actually use are
// named — everything else GitHub sends is silently ignored by encoding/json.
type ghPull struct {
	Number  int    `json:"number"`
	NodeID  string `json:"node_id"` // GraphQL identifier; Ready needs it (see Ready)
	HTMLURL string `json:"html_url"`
	Draft   bool   `json:"draft"`
	Base    struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

// graphql sends one GraphQL call. Separate from request because the endpoint,
// the method and the body shape all differ, and only Ready needs it — GitHub
// exposes no REST way to take a pull request out of draft (see Ready).
func (g *GitHub) graphql(payload any) (status int, body []byte, err error) {
	return withTransportRetry("graphql", func() (int, []byte, error) {
		return g.graphqlOnce(payload)
	})
}

func (g *GitHub) graphqlOnce(payload any) (status int, body []byte, err error) {
	b, merr := json.Marshal(payload)
	if merr != nil {
		return 0, nil, fmt.Errorf("forge: marshal graphql: %w", merr)
	}
	endpoint := g.GraphQLURL
	if endpoint == "" {
		endpoint = "https://api.github.com/graphql"
	}
	req, rerr := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(b))
	if rerr != nil {
		return 0, nil, rerr
	}
	req.Header.Set("Authorization", "Bearer "+g.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, derr := http.DefaultClient.Do(req)
	if derr != nil {
		return 0, nil, derr
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw, nil
}

// request sends one REST call and returns the raw status and body. Status
// codes are deliberately NOT turned into Go errors here: Merge needs to
// tell a 403 (no permission — degrade) apart from every other non-2xx
// (fail), so that judgment call is left to each method.
// Transport-retry budgets. The user's requirement, stated twice: retry with
// backoff for up to five minutes when network problems occur, and only then
// pass the error through. Package variables so tests shrink them to
// milliseconds; production never mutates them.
var (
	transportRetryBudget  = 5 * time.Minute
	transportBackoffStart = time.Second
	transportBackoffCap   = time.Minute
)

// withTransportRetry runs one HTTP exchange, retrying transport failures
// (refused connections, resets, DNS) and 5xx answers with doubling backoff
// inside the budget. It sits at the transport seam so every forge operation
// inherits it without per-call-site duplication.
//
// 4xx is deliberately NOT retried: those are semantic answers the callers
// classify (403 permission, 405/409 transient merge states, 422 validation),
// and retrying a validation refusal five minutes long would turn a caller bug
// into a hang. A failure that survives the budget surfaces as ONE error
// naming the attempt count and the budget, so the operator sees it was
// retried rather than dropped on first contact — the never-silent boundary
// (ADR-01KYDG): a success after retries is a succeeded action and carries no
// extra record, agreed and recorded rather than overlooked.
func withTransportRetry(what string, do func() (int, []byte, error)) (int, []byte, error) {
	deadline := time.Now().Add(transportRetryBudget)
	backoff := transportBackoffStart
	attempt := 0
	for {
		attempt++
		status, body, err := do()
		if err == nil && status < 500 {
			return status, body, nil
		}
		if time.Now().Add(backoff).After(deadline) {
			if err == nil {
				err = fmt.Errorf("status %d", status)
			}
			return status, body, fmt.Errorf("forge: %s failed after %d attempts within %s: %w",
				what, attempt, transportRetryBudget, err)
		}
		time.Sleep(backoff)
		if backoff *= 2; backoff > transportBackoffCap {
			backoff = transportBackoffCap
		}
	}
}

func (g *GitHub) request(method, path string, payload any) (status int, body []byte, err error) {
	return withTransportRetry(method+" "+path, func() (int, []byte, error) {
		return g.requestOnce(method, path, payload)
	})
}

func (g *GitHub) requestOnce(method, path string, payload any) (status int, body []byte, err error) {
	var reader io.Reader
	if payload != nil {
		b, merr := json.Marshal(payload)
		if merr != nil {
			return 0, nil, fmt.Errorf("forge: marshal %s %s: %w", method, path, merr)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, g.baseURL()+path, reader)
	if err != nil {
		return 0, nil, fmt.Errorf("forge: build %s %s: %w", method, path, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "spectackle-forge") // GitHub 403s any request without one
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if g.Token != "" {
		req.Header.Set("Authorization", "Bearer "+g.Token)
	}

	resp, err := g.client().Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("forge: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("forge: read %s %s response: %w", method, path, err)
	}
	return resp.StatusCode, body, nil
}

// Open creates a draft pull request for branch onto base.
func (g *GitHub) Open(branch, base, title, body string) (PR, error) {
	payload := map[string]any{
		"title": title,
		"head":  branch,
		"base":  base,
		"body":  body,
		"draft": true,
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls", g.Owner, g.Repo)
	status, raw, err := g.request(http.MethodPost, path, payload)
	if err != nil {
		return PR{}, err
	}
	if status != http.StatusCreated {
		return PR{}, fmt.Errorf("forge: open PR for %s: %s: %s", branch, http.StatusText(status), strings.TrimSpace(string(raw)))
	}
	var p ghPull
	if err := json.Unmarshal(raw, &p); err != nil {
		return PR{}, fmt.Errorf("forge: decode open-PR response: %w", err)
	}
	return PR{Number: p.Number, Branch: branch, Base: base, URL: p.HTMLURL, Draft: p.Draft}, nil
}

// Ready flips a draft PR to ready for review (PATCH draft:false — GitHub
// has no dedicated "ready" endpoint, only this backdoor through the update
// call).
// Ready flips a draft pull request to ready for review, via GRAPHQL.
//
// It has to be GraphQL, and this is the trap: the REST pulls endpoint accepts
// a PATCH carrying "draft": false, answers 200, and leaves the pull request a
// draft. Nothing fails, nothing warns — the field is simply not writable there.
// The first version of this used that PATCH, reported success, and the pull
// request stayed a draft; the automation then claimed to have readied it, which
// is the one thing this package must never do.
//
// markPullRequestReadyForReview is the only supported way, and it takes the
// GraphQL node ID rather than the number, which is why ghPull carries NodeID.
//
// The result is verified rather than assumed: if the pull request is still a
// draft after the mutation, that is an error, not a success to report.
func (g *GitHub) Ready(pr PR) (PR, error) {
	nodeID := pr.NodeID
	if nodeID == "" {
		p, err := g.pull(pr.Number)
		if err != nil {
			return PR{}, err
		}
		nodeID = p.NodeID
	}
	body := map[string]any{
		"query":     "mutation($id:ID!){markPullRequestReadyForReview(input:{pullRequestId:$id}){pullRequest{number isDraft url}}}",
		"variables": map[string]any{"id": nodeID},
	}
	status, raw, err := g.graphql(body)
	if err != nil {
		return PR{}, err
	}
	if status != http.StatusOK {
		return PR{}, fmt.Errorf("forge: mark PR #%d ready: %s: %s", pr.Number, http.StatusText(status), strings.TrimSpace(string(raw)))
	}
	var out struct {
		Data struct {
			Mark struct {
				PullRequest struct {
					Number  int    `json:"number"`
					IsDraft bool   `json:"isDraft"`
					URL     string `json:"url"`
				} `json:"pullRequest"`
			} `json:"markPullRequestReadyForReview"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return PR{}, fmt.Errorf("forge: decode ready-PR response: %w", err)
	}
	if len(out.Errors) > 0 {
		return PR{}, fmt.Errorf("forge: mark PR #%d ready: %s", pr.Number, out.Errors[0].Message)
	}
	if out.Data.Mark.PullRequest.IsDraft {
		return PR{}, fmt.Errorf("forge: PR #%d is still a draft after markPullRequestReadyForReview", pr.Number)
	}
	pr.Draft = false
	if u := out.Data.Mark.PullRequest.URL; u != "" {
		pr.URL = u
	}
	return pr, nil
}

// hasWorkflows reports whether the repository has any active workflow, cached
// after the first answer (see Checks for why and for the staleness argument).
func (g *GitHub) hasWorkflows() (bool, error) {
	if g.workflowsKnown {
		return g.workflowsExist, nil
	}
	status, raw, err := g.request(http.MethodGet, fmt.Sprintf("/repos/%s/%s/actions/workflows", g.Owner, g.Repo), nil)
	if err != nil {
		return false, err
	}
	if status != http.StatusOK {
		return false, fmt.Errorf("forge: list workflows: %s: %s", http.StatusText(status), strings.TrimSpace(string(raw)))
	}
	var out struct {
		Workflows []struct {
			State string `json:"state"`
		} `json:"workflows"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return false, fmt.Errorf("forge: decode workflows: %w", err)
	}
	g.workflowsKnown = true
	for _, w := range out.Workflows {
		if w.State == "active" {
			g.workflowsExist = true
			break
		}
	}
	return g.workflowsExist, nil
}

// pull fetches one pull request, for the fields a caller did not already have
// (Ready needs NodeID when it was handed a PR that came from somewhere else).
func (g *GitHub) pull(number int) (ghPull, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", g.Owner, g.Repo, number)
	status, raw, err := g.request(http.MethodGet, path, nil)
	if err != nil {
		return ghPull{}, err
	}
	if status != http.StatusOK {
		return ghPull{}, fmt.Errorf("forge: get PR #%d: %s: %s", number, http.StatusText(status), strings.TrimSpace(string(raw)))
	}
	var p ghPull
	if err := json.Unmarshal(raw, &p); err != nil {
		return ghPull{}, fmt.Errorf("forge: decode PR #%d: %w", number, err)
	}
	return p, nil
}

// Merge merges pr with merge_method=merge — NEVER squash (ADR-01KYDB and
// the sibling never-squash policy; see package doc). A 403 means the
// credential lacks merge permission: that is a degrade, not a failure —
// the PR is left open and MergeResult reports ReasonNoPermission so the
// caller can act on it instead of mistaking it for a bug.
func (g *GitHub) Merge(pr PR) (MergeResult, error) {
	payload := map[string]any{"merge_method": "merge"}
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/merge", g.Owner, g.Repo, pr.Number)
	status, raw, err := g.request(http.MethodPut, path, payload)
	if err != nil {
		return MergeResult{}, err
	}
	switch status {
	case http.StatusOK:
		var m struct {
			Merged bool   `json:"merged"`
			SHA    string `json:"sha"`
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			return MergeResult{}, fmt.Errorf("forge: decode merge response: %w", err)
		}
		return MergeResult{Merged: m.Merged, SHA: m.SHA}, nil
	case http.StatusForbidden:
		return MergeResult{Merged: false, Reason: ReasonNoPermission}, nil
	case http.StatusMethodNotAllowed, http.StatusConflict:
		// Transient by design on GitHub's side, and both observed live within
		// seconds of a push to the head: 405 while mergeability is being
		// recomputed, 409 when the head advanced under the merge call. A
		// value, not an error, so the caller retries this and nothing else.
		return MergeResult{Merged: false, Reason: ReasonNotReady}, nil
	default:
		return MergeResult{}, fmt.Errorf("forge: merge PR #%d: %s: %s", pr.Number, http.StatusText(status), strings.TrimSpace(string(raw)))
	}
}

// Checks reduces the head's check runs to one CheckState.
//
// The reduction order matters: any failed run makes the whole verdict
// Failing even while others still run — a red that is already certain must
// not be waited on; any still-running run with no failure yet is Pending;
// all-concluded-benign is Passing; and zero runs is None, the
// repository-without-CI case, which must be distinguishable from Passing
// because it is the caller's cue that there is nothing to wait FOR.
func (g *GitHub) Checks(pr PR) (CheckState, error) {
	// The SHA when the caller pinned one, the branch name only as fallback:
	// see PR.HeadSHA for why the branch ref is the wrong oracle right after a
	// push.
	ref := pr.HeadSHA
	if ref == "" {
		ref = pr.Branch
	}
	path := fmt.Sprintf("/repos/%s/%s/commits/%s/check-runs", g.Owner, g.Repo, url.PathEscape(ref))
	status, raw, err := g.request(http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("forge: check runs for %s: %s: %s", pr.Branch, http.StatusText(status), strings.TrimSpace(string(raw)))
	}
	var out struct {
		CheckRuns []struct {
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		} `json:"check_runs"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("forge: decode check runs: %w", err)
	}
	if len(out.CheckRuns) == 0 {
		// Zero runs is ambiguous, and the two readings demand opposite
		// behavior: no CI configured (proceed) versus CI not started yet
		// (wait). Observed live: the merge gate polled seconds after the
		// records push, Actions had not yet spun up a run for the new head,
		// and the gate merged before CI ever started. Disambiguate by asking
		// whether the repository HAS active workflows — if it does, zero runs
		// on a fresh head means not-started, which is Pending; only a
		// repository with no workflows at all answers None. The workflow
		// lookup is cached per client: it changes on the timescale of
		// repository administration, not of a merge.
		hasCI, err := g.hasWorkflows()
		if err != nil {
			return "", err
		}
		if hasCI {
			return ChecksPending, nil
		}
		return ChecksNone, nil
	}
	pending := false
	realGreen := false
	for _, r := range out.CheckRuns {
		if r.Status != "completed" {
			pending = true
			continue
		}
		switch r.Conclusion {
		case "success", "neutral":
			realGreen = true
		case "skipped":
			// benign, but NOT evidence of green: the draft-skip workflow
			// concludes every during-work run as skipped, and a head whose
			// runs are all skipped has never been tested.
		default:
			return ChecksFailing, nil
		}
	}
	if pending {
		return ChecksPending, nil
	}
	if !realGreen {
		// Every run concluded skipped. That is what a draft-phase head looks
		// like, and reading it as passing would merge untested work on the
		// strength of runs that deliberately did nothing — the predecessor-
		// verdict defect (B-01KYDN) in a new costume. With workflows active a
		// real run is expected once the pull request is ready, so this is
		// Pending; without any workflow it is the ordinary no-CI case.
		hasCI, err := g.hasWorkflows()
		if err != nil {
			return "", err
		}
		if hasCI {
			return ChecksPending, nil
		}
		return ChecksNone, nil
	}
	return ChecksPassing, nil
}

// Find returns the pull request already open for branch, if any — the
// lookup that makes a second Open call unnecessary (see Forge doc).
func (g *GitHub) Find(branch string) (PR, bool, error) {
	q := url.Values{}
	q.Set("state", "open")
	q.Set("head", fmt.Sprintf("%s:%s", g.Owner, branch))
	path := fmt.Sprintf("/repos/%s/%s/pulls?%s", g.Owner, g.Repo, q.Encode())
	status, raw, err := g.request(http.MethodGet, path, nil)
	if err != nil {
		return PR{}, false, err
	}
	if status != http.StatusOK {
		return PR{}, false, fmt.Errorf("forge: find PR for %s: %s: %s", branch, http.StatusText(status), strings.TrimSpace(string(raw)))
	}
	var list []ghPull
	if err := json.Unmarshal(raw, &list); err != nil {
		return PR{}, false, fmt.Errorf("forge: decode find-PR response: %w", err)
	}
	if len(list) == 0 {
		return PR{}, false, nil
	}
	p := list[0]
	return PR{Number: p.Number, Branch: branch, Base: p.Base.Ref, URL: p.HTMLURL, Draft: p.Draft}, true, nil
}
