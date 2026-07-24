---
schema: v0
---

## T-0023 M5 prep: GitHub Actions CI + CONTRIBUTING with the lifecycle loop
kind: task
state: done
created: 2026-07-24

Scope ONLY: new .github/workflows/ci.yml + new CONTRIBUTING.md. CI: on push to main + pull_request; single job ubuntu-latest; actions/checkout@v4, actions/setup-go@v5 with go-version-file go.mod + cache true; steps exactly: make build, go vet ./..., go test -race ./..., ./bin/spectacle lint ., make smoke; then the self-hosting gate: ./bin/spectacle serve is NOT needed - instead run the check tool headlessly via a 10-line inline python heredoc (initialize handshake + tools/call check, fail job if result text is not ok-only) mirroring the README headless quickstart driver. CONTRIBUTING.md: short - the two roles (orchestrator/implementer, link docs/agent-workflow.md), every change starts with find scope=rejection + draft (link README loop), forward-skip move semantics one-liner, make all must be green before any PR, .spectacle files are server-written only. Verify: yamllint-free by eye (no tabs), make all locally still green (you touch no Go code).

## T-0024 docs nits: transcript rule count + README triple-hop demo with real output
kind: task
state: done
created: 2026-07-24

Scope ONLY: docs/example-go-cuda.md, README.md. (1) example-go-cuda.md says 'The cascade loaded five rules' but the #contracts block shows 4 r-lines - recount the r-lines in the transcript block above that sentence and fix the word (likely four; verify in-file, do not guess). (2) README: after the 'Status' table add a short '## The chain, live' section: 3-4 intro words + fenced block with the REAL current output of get id=go:saxpy.Saxpy depth=2 - obtain it via the driver (SPECTACLE_AGENT=agent-docs2, README headless quickstart pattern) and paste verbatim (n/e records incl. launch edge); one closing sentence that this is produced by the shipped binary on this repo, no mockup. Keep both edits minimal.
