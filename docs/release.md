# Cutting a release

Releases are built by [goreleaser](https://goreleaser.com) (config:
`.goreleaser.yaml`) and triggered by pushing a version tag.

## Steps

1. Make sure `main` is green (CI passing).
2. Tag the commit and push the tag:

   ```sh
   git tag v0.3.0
   git push origin v0.3.0
   ```

3. `.github/workflows/release.yml` fires on any `v*` tag push: it runs
   `go test -race ./...` as a gate, then runs `goreleaser release --clean`,
   which builds, packages, and publishes a GitHub Release for the tag.

## Artifacts

For each tag, goreleaser produces:

- `spectackle` binaries for `linux/amd64`, `linux/arm64`, `darwin/amd64`,
  `darwin/arm64`, `windows/amd64`, `windows/arm64` (CGO disabled).
- Archives: `spectackle_<version>_<os>_<arch>.tar.gz` (`.zip` on Windows),
  each containing the binary.
- `checksums.txt` covering all archives.
- A GitHub Release with an auto-generated changelog (commits prefixed
  `docs:`, `spec:`, or `chore:` are excluded).
- A Homebrew cask pushed to `jxsl13/homebrew-tap`, installable with
  `brew install --cask jxsl13/tap/spectackle`.

## The Homebrew tap

Two things must exist before a tag can update the tap, and neither is a
code-signing certificate:

1. The repository `jxsl13/homebrew-tap` (that exact name — `brew` maps the
   short form `jxsl13/tap` onto it).
2. A repository secret `HOMEBREW_TAP_GITHUB_TOKEN` on *this* repository,
   holding a token that may push to the tap repository. The workflow's
   built-in `GITHUB_TOKEN` is scoped to this repository alone and cannot.

If the secret is missing the release still succeeds — only the tap update is
skipped — so a fork can cut releases without owning a tap.

The published binaries are unsigned. macOS quarantines a cask download, so
the cask carries a `postflight` hook that removes the quarantine attribute
(`xattr -dr com.apple.quarantine`), which is the supported alternative to an
Apple Developer ID plus notarization. Prerelease tags (`-rc`, `-beta`, …)
are detected by `skip_upload: auto` and never become the version
`brew install` resolves to.

Homebrew casks are macOS-only; Linux users install from the release tarball.

The version string baked into the binary (`spectackle version`) is stamped at
build time via ldflags into
`github.com/jxsl13/spectackle/internal/mcpserver.Version`.

## Local dry run

To sanity-check the goreleaser config and produce local build artifacts
without publishing anything:

```sh
make release-snapshot
```

This runs `goreleaser release --snapshot --clean --skip=publish` via
`go run github.com/goreleaser/goreleaser/v2@latest`, so no goreleaser
install is required. Output lands in `dist/`.
