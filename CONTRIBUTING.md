# Contributing to mailescrow

## Development setup

Prerequisites: Go 1.26+. No CGO or system libraries are required.

```bash
git clone https://github.com/albert/mailescrow
cd mailescrow
go mod download
```

## Running tests

```bash
go test ./...            # all tests
go test -race ./...      # with race detector (matches CI)
go test ./integration/...  # integration tests only
```

## Linting and formatting

golangci-lint is managed as a Go tool dependency and requires no separate installation.

```bash
gofmt -w .                    # format all files
go vet ./...                  # vet
go tool golangci-lint run ./... # full lint
```

CI will fail if any files are not `gofmt`-formatted, so run `gofmt -w .` before pushing.

## Submitting changes

1. Fork the repo and create a branch from `main`.
2. Make your changes. Add or update tests as appropriate.
3. Ensure `go test -race ./...` and `gofmt -l .` (no output) both pass.
4. Open a pull request against `main`. The CI suite runs automatically.

Keep pull requests focused — one logical change per PR makes review easier.

## Release process

Releases are cut by a maintainer. The Docker image is built and published to
`ghcr.io/albert/mailescrow` automatically by CI.

### Steps

1. **Confirm `main` is green.** All CI checks on `main` must pass before tagging.

2. **Choose a version.** mailescrow follows [Semantic Versioning](https://semver.org/):
   - Patch (`v1.2.3` → `v1.2.4`): bug fixes, documentation
   - Minor (`v1.2.3` → `v1.3.0`): backwards-compatible new features
   - Major (`v1.2.3` → `v2.0.0`): breaking changes (config keys, behaviour)

3. **Create and push a tag.**
   ```bash
   git tag v1.2.3
   git push origin v1.2.3
   ```

4. **CI publishes the image.** Pushing the tag triggers the Docker workflow, which
   builds the image and pushes the following tags to the registry:
   - `ghcr.io/albert/mailescrow:1.2.3`
   - `ghcr.io/albert/mailescrow:1.2`
   - `ghcr.io/albert/mailescrow:latest` is **not** updated on a version tag — it
     tracks the `main` branch only.

5. **Create a GitHub Release.** Go to *Releases → Draft a new release*, select
   the tag, and write a short changelog. This makes the release visible to users
   watching the repo.

### Fixing a bad release

If a tagged release is broken, **do not** delete and re-push the tag. Instead:

- For a patch fix, cut a new patch release (e.g. `v1.2.4`).
- If the image itself is the problem, the new tag will produce a new image;
  `latest` will be corrected on the next push to `main`.
