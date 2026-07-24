---
schema: v0
---

## P-0015 harness-agnostic orchestration language + fanout in the workflow prompt
kind: proposal
state: approved
created: 2026-07-24
targets: README.md, internal/mcpserver/prompts.go

The orchestrator/implementer split is a MODEL-TIER pattern, not a vendor feature: a complex model plans, reviews, merges and writes exhaustive briefs; simpler models execute one disjoint task each from that brief - keeping the complex model's context unpolluted and saving tokens long-run. All product surfaces must say it that way: README workflow section, docs/agent-workflow.md, CONTRIBUTING.md, the instructions manifest ORCHESTRATION paragraph, and the workflow prompt itself - which additionally teaches FANOUT: partition approved tasks by disjoint scope, delegate the full lifecycle per task to a fresh implementer agent, parallelize wherever leases do not overlap. Concrete model names remain only as parenthetical examples.

## P-0016 CD: goreleaser release pipeline (multi-platform binaries on tag)
kind: proposal
state: approved
created: 2026-07-24
targets: .goreleaser.yaml, cmd/spectacle/main.go

Publish the server properly: .goreleaser.yaml (CGO_ENABLED=0 builds for linux/darwin/windows amd64+arm64, ldflags -X injecting mcpserver.Version, archives + checksums, changelog from commits) and .github/workflows/release.yml (on push tags v*, permissions contents:write, run tests first, then goreleaser release). Version const already converted to var by the orchestrator. CI stays as-is; release workflow is separate and tag-gated.

## T-0028 goreleaser config + tag-gated release workflow
kind: task
state: active
created: 2026-07-24
parent: P-0016

Scope ONLY: new .goreleaser.yaml, new .github/workflows/release.yml, Makefile (one new target release-snapshot), docs/release.md (short: how to cut a release = push tag vX.Y.Z, what artifacts appear). goreleaser v2 schema: version: 2; builds: single build id spectacle, main: ./cmd/spectacle, env [CGO_ENABLED=0], goos [linux, darwin, windows], goarch [amd64, arm64], ldflags: -s -w -X github.com/jxsl13/spectacle/internal/mcpserver.Version={{.Version}}; archives: tar.gz (zip for windows) name_template with OS/Arch; checksum; changelog: use github-native or git, filters exclude docs:/spec:/chore:. release.yml: on push tags 'v*'; permissions contents: write; steps checkout (fetch-depth 0), setup-go (go-version-file), go test ./... (fast gate, no -race for speed... NO: keep -race, releases are rare), goreleaser/goreleaser-action@v6 with args 'release --clean', GITHUB_TOKEN env. Makefile: release-snapshot: goreleaser release --snapshot --clean --skip=publish (via go run github.com/goreleaser/goreleaser/v2@latest if binary absent - pick ONE mechanism and document it). Verify locally: go run github.com/goreleaser/goreleaser/v2@latest check MUST pass (config lint); if the module download fails through the proxy, fall back to careful schema-by-docs authoring + yaml parse check via python3 yaml.safe_load and NOTE the skipped check verbatim in the report. Also verify ldflags path correctness: go build -ldflags "-X github.com/jxsl13/spectacle/internal/mcpserver.Version=v9.9.9-test" -o /tmp/spectacle-vtest ./cmd/spectacle && /tmp/spectacle-vtest version must print v9.9.9-test.
