# Releasing clinote (Homebrew tap distribution)

This document covers how clinote is shipped to users via a Homebrew tap.

## Distribution model

Users will install with:

```sh
brew tap pmuston/tap
brew install pmuston/tap/clinote
```

`pmuston/tap` is a shared Homebrew tap that hosts formulas for several utilities — clinote is just one of them. Homebrew strips the `homebrew-` prefix from tap repo names, so `brew tap pmuston/tap` clones `github.com/pmuston/homebrew-tap`.

Behind the scenes:

1. clinote's source lives in `github.com/pmuston/clinote` (this repo).
2. The shared tap at `github.com/pmuston/homebrew-tap` holds the Homebrew formula (`Formula/clinote.rb`), alongside formulas for your other utilities.
3. Each release publishes pre-built tarballs for 4 platforms as GitHub release assets, plus a `checksums.txt`. The formula references those URLs by SHA256.

This model means **users don't need Go installed** — they get a pre-built static binary.

## Prerequisites (one-time)

### 1. LICENSE at the repo root

Already in place — [LICENSE](../LICENSE), MIT. The generated formula declares `license "MIT"` to match. If you ever switch licenses, update both files together.

### 2. Create the shared tap repository

On GitHub, create an empty public repo called `homebrew-tap` under your user:

```
github.com/pmuston/homebrew-tap
```

The `homebrew-` prefix is mandatory — Homebrew strips it when resolving names, so `brew tap pmuston/tap` looks for `homebrew-tap`. Pick this once for all your utilities; each formula goes under `Formula/` in that single repo.

The tap's layout will end up like:

```
homebrew-tap/
└── Formula/
    ├── clinote.rb
    ├── other-tool.rb
    └── ...
```

Clone the tap locally next to this repo; on each release you'll drop the freshly-generated formula into `Formula/`.

### 3. (Optional but recommended) `gh` CLI

```sh
brew install gh
gh auth login
```

The release script doesn't depend on `gh`, but the suggested commands at the end use it.

## Per-release workflow

### 1. Tag the release

Pick a version following semver:

```sh
git tag v0.1.0
git push origin v0.1.0
```

### 2. Build the artifacts

```sh
./scripts/release.sh v0.1.0
```

The script produces:

```
dist/
  clinote-v0.1.0-darwin-arm64.tar.gz
  clinote-v0.1.0-darwin-amd64.tar.gz
  clinote-v0.1.0-linux-amd64.tar.gz
  clinote-v0.1.0-linux-arm64.tar.gz
  checksums.txt
  clinote.rb            ← Homebrew formula, ready to drop into the tap repo
```

Each tarball contains:

- The `clinote` binary (statically linked, CGO disabled, `-s -w` stripped).
- `LICENSE` and `README.md` if present at the repo root.

The version is embedded in the binary via `-ldflags "-X main.version=v0.1.0"`, so `clinote version` reports the right thing.

### 3. Publish the GitHub release

```sh
gh release create v0.1.0 \
  --title "clinote v0.1.0" \
  --notes "Release notes here." \
  dist/*.tar.gz dist/checksums.txt
```

…or use the web UI: create a release for the `v0.1.0` tag and upload all five files as assets.

### 4. Update the tap

```sh
cd ../homebrew-tap
mkdir -p Formula     # first time only
cp ../clinote/dist/clinote.rb Formula/clinote.rb
git add Formula/clinote.rb
git commit -m "clinote v0.1.0"
git push
```

### 5. Verify

From any machine (or a fresh shell so brew sees the new formula):

```sh
brew update
brew install pmuston/tap/clinote
clinote version           # should print v0.1.0
```

If a previous version is installed, use `brew upgrade pmuston/tap/clinote` instead.

## How the script works

`scripts/release.sh` is a thin wrapper around `go build`. For each platform:

1. `GOOS=<os> GOARCH=<arch> CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=<v>" ./cmd/clinote`
2. Stages the binary + LICENSE + README in a tempdir, tarballs it.
3. Computes the SHA256 (via `shasum -a 256`, which is available on macOS and most Linux).
4. After all four builds, writes `checksums.txt` and a `clinote.rb` formula populated with the URLs and checksums.

CGO is disabled deliberately: clinote has no C dependencies, and a CGO-free binary is fully static and works across the broadest range of Linux distros (no glibc version surprises).

## Verifying a release

After publishing, do a clean install in a sandbox to verify:

```sh
brew uninstall clinote 2>/dev/null || true
brew untap pmuston/tap 2>/dev/null || true
brew tap pmuston/tap
brew install pmuston/tap/clinote
clinote version
clinote --help
```

For deeper testing, run the formula's audit:

```sh
brew audit --strict pmuston/tap/clinote
brew test pmuston/tap/clinote      # runs the test block from the formula
```

## Updating an existing release (rare)

If you need to ship a fix without bumping version (e.g., a bad tarball was uploaded):

1. Delete the GitHub release assets.
2. Re-run `./scripts/release.sh v0.1.0`.
3. Re-upload to the same release.
4. Update the tap formula — checksums will have changed, so users will see a `brew upgrade` even though the version label is the same.

Cleaner: just cut a `v0.1.1`.

## Alternative: GoReleaser

If the manual workflow becomes a chore, [GoReleaser](https://goreleaser.com/) automates everything in this document — cross-compile, tarball, checksum, GitHub release, AND Homebrew formula generation in the tap repo. Triggered by a tag push, usually via GitHub Actions.

For v1 we keep the bash script: it's auditable, has no external dependencies beyond Go itself, and the per-release work is small enough not to need automation yet. Switch to GoReleaser later if release cadence picks up.

## Troubleshooting

**`brew install` says "no available formula"** — make sure `homebrew-clinote` is public, that `Formula/clinote.rb` is committed at the repo root, and that you ran `brew tap pmuston/clinote` before `brew install`.

**`brew audit` complains about `license`** — the LICENSE file at the source repo root and the `license "MIT"` line in `clinote.rb` must match. Update either to fix the mismatch.

**SHA256 mismatch on install** — the tarball on GitHub doesn't match what the formula says. Re-run `./scripts/release.sh` and re-upload the assets so they match the new formula.

**Binary doesn't run on user's Mac** — could be Gatekeeper because the binary is unsigned. Users can clear the quarantine attribute with `xattr -d com.apple.quarantine $(which clinote)`. For broader distribution, set up Apple Developer ID signing + notarization (out of scope for v1).

**Linux user reports "GLIBC not found"** — shouldn't happen with `CGO_ENABLED=0` (the binary doesn't link against libc at all). If it does, double-check that the build didn't accidentally re-enable CGO via an environment override.
