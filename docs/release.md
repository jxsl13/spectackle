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

- `spectacle` binaries for `linux/amd64`, `linux/arm64`, `darwin/amd64`,
  `darwin/arm64`, `windows/amd64`, `windows/arm64` (CGO disabled).
- Archives: `spectacle_<version>_<os>_<arch>.tar.gz` (`.zip` on Windows),
  each containing the binary.
- `checksums.txt` covering all archives.
- A GitHub Release with an auto-generated changelog (commits prefixed
  `docs:`, `spec:`, or `chore:` are excluded).

The version string baked into the binary (`spectacle version`) is stamped at
build time via ldflags into
`github.com/jxsl13/spectacle/internal/mcpserver.Version`.

## Local dry run

To sanity-check the goreleaser config and produce local build artifacts
without publishing anything:

```sh
make release-snapshot
```

This runs `goreleaser release --snapshot --clean --skip=publish` via
`go run github.com/goreleaser/goreleaser/v2@latest`, so no goreleaser
install is required. Output lands in `dist/`.
