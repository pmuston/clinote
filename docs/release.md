# Releasing clinote (Homebrew tap distribution)

How clinote is shipped to users via Homebrew. The workflow follows the
`homebrew-tap-release` skill, with one documented deviation (see
[Where the assets live](#where-the-assets-live)).

## Distribution model

Users install with:

```sh
brew tap pmuston/tap
brew trust pmuston/tap      # required for third-party taps
brew install pmuston/tap/clinote
```

`pmuston/tap` is a shared Homebrew tap hosting formulae for several utilities —
clinote is one of them. Homebrew strips the `homebrew-` prefix when resolving
tap names, so `brew tap pmuston/tap` clones `github.com/pmuston/homebrew-tap`.

Behind the scenes:

1. Source lives in `github.com/pmuston/clinote` (this repo, **public**).
2. Each release publishes pre-built tarballs for four platforms plus a
   `checksums.txt` as GitHub release assets **on this repo**.
3. The shared tap holds only `Formula/clinote.rb`, which references those asset
   URLs by SHA256.

Users don't need Go installed — they get a pre-built static binary.

### Where the assets live

The skill hosts release assets on the *tap* rather than the source repo, because
it assumes a **private** source whose assets would 404 for everyone but the
owner. clinote's source is public, so it hosts its own releases: tags and
release notes then sit next to the code they describe, and the shared tap stays
formula-only.

`pmuston/graphdb` is private and correctly keeps the skill's tap-hosted
arrangement. If clinote ever goes private, move the assets and set
`RELEASE_REPO` in the build script to the tap.

Note that a shared tap only avoids tag collisions because it holds no releases.
If assets ever move there, tags must be namespaced per tool (`clinote-v2.0.0`),
since several tools would otherwise compete for `v2.0.0` in one repo.

## Prerequisites

All one-time setup is already done:

- **[LICENSE](../LICENSE)** — MIT, matching the `license "MIT"` the formula
  declares. Change both together or neither.
- **Version constant** — [internal/version/version.go](../internal/version/version.go).
- **`version` subcommand** — prints `clinote v<version> (<revision>)`. The
  formula's `test` block asserts on this, so it is load-bearing.
- **The tap** — `github.com/pmuston/homebrew-tap`, public, with `Formula/`.
- **`/dist` gitignored** — not cosmetic; see [Why `dist/` must be
  ignored](#why-dist-must-be-ignored).
- **`gh` CLI** — `brew install gh && gh auth login`.

## Per-release workflow

Order matters at every step. Getting it wrong produces a broken install for
anyone who taps in the gap.

### 1. Bump the version constant and commit

```go
// internal/version/version.go
const Version = "2.0.0"
```

The version is single-sourced here so the binary, the build script and the
formula cannot disagree. Bump it whenever the interface changes, not only for
features — a running instance has no other way to identify itself.

Commit this **before** building. A dirty tree stamps the binary `modified`.

### 2. Verify clean, then tag and push

```sh
git status --short        # must be empty
git tag -a v2.0.0 -m "clinote v2.0.0"
git push origin main && git push origin v2.0.0
```

Push **named tags only**. `git push --tags` pushes every local tag, including
scratch tags never meant to be public.

Tag every released version, even one whose binaries ship inside a later release
— a gap means nobody can check out what a given version contained.

### 3. Build

```sh
./scripts/build-release.sh
```

No arguments: the version comes from the constant. Output:

```
dist/
  clinote-v2.0.0-darwin-arm64.tar.gz
  clinote-v2.0.0-darwin-amd64.tar.gz
  clinote-v2.0.0-linux-amd64.tar.gz
  clinote-v2.0.0-linux-arm64.tar.gz
  checksums.txt
  THIRD-PARTY-NOTICES.md   ← also bundled inside every tarball
  clinote.rb               ← formula, ready for the tap
```

Each tarball contains a top-level `clinote-v2.0.0-<os>-<arch>/` directory
holding the binary, `LICENSE`, `README.md` and `THIRD-PARTY-NOTICES.md`.

**Confirm the binary self-identifies before going further:**

```sh
tar xzf dist/clinote-v2.0.0-darwin-arm64.tar.gz -C /tmp
/tmp/clinote-v2.0.0-darwin-arm64/clinote version
# → clinote v2.0.0 (a1b2c3d4e5f6)     ← no ", modified"
```

`modified` means the tree was dirty at build time and the artifact matches no
commit. Rebuild from a clean tree; do not ship it.

### 4. Publish the GitHub release — before the formula

```sh
gh release create v2.0.0 dist/clinote-v2.0.0-*.tar.gz dist/checksums.txt \
  --repo pmuston/clinote --title "clinote v2.0.0" --notes "..."
```

The formula's URLs point at these assets. Publishing the formula first leaves a
window where anyone installing gets a 404. Verify the upload landed:

```sh
gh release view v2.0.0 --repo pmuston/clinote --json assets --jq '.assets[].name'
```

Expect all five.

### 5. Publish the formula

```sh
cd ../homebrew-tap && git pull --ff-only
cp ../clinote/dist/clinote.rb Formula/clinote.rb
git add Formula/clinote.rb && git commit -m "clinote 2.0.0" && git push
```

### 6. Verify the path a user actually takes

```sh
brew update && brew upgrade pmuston/tap/clinote   # or `brew install` first time
clinote version                                    # matches the tag, clean revision
```

Then exercise whatever this release changed, on the **brew-installed binary** —
not your build tree. This is what catches a formula pointing at stale assets,
and it's worth doing even when the code is obviously fine, because it tests the
distribution rather than the code.

## Release notes worth reading

Write for someone deciding whether to upgrade. Lead with what changed for
*them*, not the commit list. Spell out the symptom for any silent-wrong-answer
bug, because anyone affected doesn't know they were. Call out behaviour changes
explicitly, even backward-compatible ones. Always include the upgrade command.

## How the build script works

`scripts/build-release.sh` is adapted from the skill's stock script. For each
platform it runs:

```sh
CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> go build -trimpath -ldflags "-s -w" ./cmd/clinote
```

then stages the binary plus `LICENSE`, `README.md` and `THIRD-PARTY-NOTICES.md`,
tarballs it, and records the SHA256. After all four, it writes `checksums.txt`
and a `clinote.rb` populated with real URLs and checksums.

CGO is off deliberately: clinote has no C dependencies, and a CGO-free binary is
fully static and works across the broadest range of Linux distros with no glibc
surprises. `-trimpath` strips local paths but **preserves** Go's VCS stamps,
which is how the binary reports its own revision via `debug.ReadBuildInfo()`.

Two additions over the stock script:

- Runs `go test ./...` first — never ship a release that fails its own tests.
- Generates `THIRD-PARTY-NOTICES.md` (below).

### Why `dist/` must be ignored

Go reads the working tree's state when it compiles, and the script creates
`dist/` before building. If `dist/` were untracked but not ignored, its mere
existence would dirty the tree at exactly that moment — stamping every binary
`modified` from an otherwise spotless commit. Silent, and it makes the artifact
unattributable. The script refuses to run if `dist/` isn't ignored.

## Third-party notices

MIT and BSD-3-Clause both require reproducing their copyright and permission
notices in redistributions — **including binary form** — and the release binary
statically links those dependencies. Every tarball therefore ships a generated
`THIRD-PARTY-NOTICES.md` with their full license texts.

The module set comes from:

```sh
go list -deps -f '{{if .Module}}...{{end}}' ./cmd/clinote
```

Using `go list -deps` on the binary's package rather than `go list -m all` means
the notices cover **exactly what gets linked**: test-only dependencies such as
`testify` are excluded, and stdlib packages self-exclude because they report no
module. License texts are read from the local module cache via `{{.Module.Dir}}`,
so no external tooling is needed and it works offline after `go mod download`.

Two entries are special-cased:

- **Go standard library** — BSD-3-Clause, compiled into every Go binary. Includes
  `$GOROOT/LICENSE` when the toolchain ships it; some packaged distributions
  (Homebrew among them) omit it, in which case the file records the license and
  links to <https://go.dev/LICENSE> rather than inventing the text.
- **htmx** — the vendored `internal/server/static/htmx.min.js` is 0BSD
  (Zero-Clause BSD), which imposes no attribution requirement at all. Listed
  purely so readers know what's in the binary.

If a linked module has no discoverable license file the script **fails the
build** rather than shipping incomplete attribution. Adding or upgrading a
dependency needs no manual step.

## Licensing

The formula's `license` must match the LICENSE the tarball actually ships. They
are checked by different people at different times, so a mismatch survives
indefinitely — `brew audit --strict` passes either way. It surfaces later in the
SBOM Homebrew writes at install time:

```sh
python3 -c "
import json; d=json.load(open('$(brew --cellar clinote)/2.0.0/sbom.spdx.json'))
print([(p['name'], p.get('licenseConcluded')) for p in d['packages']])"
```

The SBOM is written at **install** time, so correcting a formula doesn't update
an existing install — that machine needs `brew reinstall` before a compliance
scan sees the change.

## Failure modes

**404 on the tarball** — the formula was published before the release, or an
asset upload failed. Check with `gh release view <tag> --repo pmuston/clinote
--json assets --jq '.assets[].name'` and expect all five. Upload what's missing
with `gh release upload`.

**Binary reports `(abc123, modified)`** — the tree was dirty when Go compiled
it, so the artifact matches no commit. Commit or stash, rebuild, re-upload the
assets, update the formula's checksums. The non-obvious cause is `dist/` not
being gitignored. Always check this on an extracted tarball *before* creating
the release — it's unfixable in place afterwards.

**Binary reports no revision at all** — `debug.ReadBuildInfo()` found no VCS
stamps: built outside a git checkout, from a source archive, or with
`-buildvcs=false`. Build from a real clone.

**`brew info` shows the wrong version, or upgrade is a no-op** — Homebrew caches
tap metadata; `brew update` first. If it persists, confirm the formula on the
tap's default branch really has the new version and URLs. Committing the formula
and forgetting to push is easy.

**A running instance reports an older version than expected** — usually not a
release bug. An old process is still running, or a second install is earlier in
`PATH`. Check `which -a clinote`. Trust what the running binary says about
itself over what you believe you deployed; that's what the `version` subcommand
is for.

**SHA256 mismatch on install** — the assets on GitHub don't match what the
formula claims. Rebuild and re-upload so they agree.

**`brew install` fails on an untrusted tap** — recent Homebrew requires
`brew trust pmuston/tap`. Not a formula bug, but the error text doesn't make the
fix obvious, which is why it's in both READMEs.

**`brew style` reports Sorbet/FrozenStringLiteral/Documentation offenses** — an
artifact of linting a formula by path instead of inside a tap. A known-good
formula from an existing tap produces the identical four. Not a defect.

**Binary won't run on a user's Mac** — Gatekeeper, because the binary is
unsigned. Users can clear quarantine with
`xattr -d com.apple.quarantine $(which clinote)`. Apple Developer ID signing and
notarization would fix it properly; out of scope for now.

**Linux "GLIBC not found"** — shouldn't happen with `CGO_ENABLED=0`, since the
binary doesn't link libc at all. Check nothing re-enabled CGO via the
environment.

## Alternative: GoReleaser

[GoReleaser](https://goreleaser.com/) automates everything here — cross-compile,
tarball, checksum, GitHub release, and formula generation in the tap — triggered
by a tag push via GitHub Actions.

The bash script stays for now: it's auditable, needs nothing beyond Go itself,
and the per-release work is small. Switch if the cadence picks up.
