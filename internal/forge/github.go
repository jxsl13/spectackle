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
	BaseURL string       // API root; empty means defaultAPIBase
	Client  *http.Client // empty means http.DefaultClient
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
	HTMLURL string `json:"html_url"`
	Draft   bool   `json:"draft"`
	Base    struct {
		Ref string `json:"ref"`
	} `json:"base"`
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
func (g *GitHub) Ready(pr PR) (PR, error) {
	payload := map[string]any{"draft": false}
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", g.Owner, g.Repo, pr.Number)
	status, raw, err := g.request(http.MethodPatch, path, payload)
	if err != nil {
		return PR{}, err
	}
	if status != http.StatusOK {
		return PR{}, fmt.Errorf("forge: mark PR #%d ready: %s: %s", pr.Number, http.StatusText(status), strings.TrimSpace(string(raw)))
	}
	var p ghPull
	if err := json.Unmarshal(raw, &p); err != nil {
		return PR{}, fmt.Errorf("forge: decode ready-PR response: %w", err)
	}
	pr.Draft = p.Draft
	pr.URL = p.HTMLURL
	return pr, nil
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
	default:
		return MergeResult{}, fmt.Errorf("forge: merge PR #%d: %s: %s", pr.Number, http.StatusText(status), strings.TrimSpace(string(raw)))
	}
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
