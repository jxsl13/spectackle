package forge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
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
	Client     *http.Client // empty means http.DefaultClient
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
func (g *GitHub) request(method, path string, payload any) (status int, body []byte, err error) {
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
	path := fmt.Sprintf("/repos/%s/%s/commits/%s/check-runs", g.Owner, g.Repo, url.PathEscape(pr.Branch))
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
		return ChecksNone, nil
	}
	pending := false
	for _, r := range out.CheckRuns {
		if r.Status != "completed" {
			pending = true
			continue
		}
		switch r.Conclusion {
		case "success", "neutral", "skipped":
		default:
			return ChecksFailing, nil
		}
	}
	if pending {
		return ChecksPending, nil
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
