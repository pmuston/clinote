#!/usr/bin/env bash
#
# build-release.sh — cross-compile release archives, checksums, third-party
# notices and a Homebrew formula for distribution via a public tap.
#
# Adapted from the homebrew-tap-release skill. Two deliberate differences from
# the stock script, both documented inline below:
#   1. Release assets live on the SOURCE repo, not the tap. The skill hosts them
#      on the tap because it assumes a private source repo whose assets would
#      404; clinote's source is public, so the source repo is the simpler and
#      more conventional home. (pmuston/graphdb, whose source IS private, keeps
#      the skill's tap-hosted arrangement.)
#   2. Generates THIRD-PARTY-NOTICES.md and runs the test suite before building.
#
# Usage:
#   scripts/build-release.sh
#
# Environment overrides:
#   CLINOTE_VERSION       version string (default: read from the version constant)
#   CLINOTE_OWNER         GitHub owner for release URLs
#   CLINOTE_RELEASE_REPO  repo hosting the release assets (default: the tap)
#   CLINOTE_PLATFORMS     space-separated os/arch list
#
# Output (in ./dist):
#   clinote-v<version>-<os>-<arch>.tar.gz   one per platform
#   checksums.txt                           SHA256 of every archive
#   THIRD-PARTY-NOTICES.md                  also bundled inside every archive
#   clinote.rb                              formula with real SHAs + URLs

set -euo pipefail

# ---------------------------------------------------------------- CONFIG ----
# The binary/formula name, as typed by the user: `brew install <BINARY>`.
BINARY="clinote"

# GitHub owner, and the repo hosting the release assets. clinote's source repo
# is public, so it hosts its own releases — tags and release notes then live
# next to the code they describe. The tap (pmuston/tap) carries only the
# formula, and is shared across several tools.
#
# If this source repo is ever made private, move assets to the tap: private
# release assets 404 for everyone but the owner.
OWNER="pmuston"
RELEASE_REPO="clinote"
TAP_REPO="homebrew-tap"

# One-line formula description. Homebrew style: no trailing period, and don't
# begin with the tool's own name (brew audit checks this).
DESC="Personal lab notebook for shell commands; runs in your browser"

# SPDX identifier matching the LICENSE actually shipped in the tarball.
LICENSE_ID="MIT"

# Go file holding `const Version = "x.y.z"`, and the main package to build.
VERSION_FILE="internal/version/version.go"
MAIN_PKG="./cmd/clinote"
# -------------------------------------------------------------- /CONFIG ----

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

PREFIX="$(echo "$BINARY" | tr '[:lower:]-' '[:upper:]_')"
lookup() { local v="${PREFIX}_$1"; echo "${!v:-$2}"; }

OWNER="$(lookup OWNER "$OWNER")"
RELEASE_REPO="$(lookup RELEASE_REPO "$RELEASE_REPO")"

# Version is single-sourced from Go so the binary and formula cannot disagree.
version_from_source() {
  sed -n 's/^const Version = "\(.*\)"/\1/p' "$VERSION_FILE" | head -1
}
VERSION="$(lookup VERSION "$(version_from_source)")"
if [[ -z "$VERSION" ]]; then
  echo "error: no version found in $VERSION_FILE (expected: const Version = \"x.y.z\")" >&2
  echo "       or set ${PREFIX}_VERSION" >&2
  exit 1
fi

# One tag, on clinote's own repo. Per-tool namespacing would only be needed if
# several tools published releases into one shared repo; the shared tap holds
# formulae (distinct filenames), not releases, so nothing can collide.
TAG="v${VERSION}"

DIST="dist"

# The output directory must be gitignored. Go stamps vcs.modified from the state
# of the tree at compile time, and this script creates $DIST before building —
# so if $DIST is untracked-but-not-ignored, every binary is stamped "modified"
# even from a spotless commit. Silent, and it makes the artifact unattributable.
if git rev-parse --git-dir >/dev/null 2>&1; then
  if ! git check-ignore -q "$DIST/" 2>/dev/null; then
    echo "error: '$DIST/' is not gitignored." >&2
    echo "       Go reads the tree state when it compiles, and this script creates" >&2
    echo "       '$DIST/' first — so the binaries would be stamped 'modified' and" >&2
    echo "       match no commit. Add this to .gitignore and re-run:" >&2
    echo >&2
    echo "         /$DIST/" >&2
    exit 1
  fi
fi

# A dirty tree has the same effect for the same reason. Warn rather than fail:
# a local test build is a legitimate reason to ignore it.
if [[ -n "$(git status --porcelain 2>/dev/null)" ]]; then
  echo "WARNING: working tree is dirty — binaries will be stamped 'modified'." >&2
  echo "         Commit or stash before building a real release." >&2
  echo >&2
fi

read -r -a PLATFORMS <<<"$(lookup PLATFORMS "darwin/amd64 darwin/arm64 linux/amd64 linux/arm64")"

# Local addition: never ship a release that fails its own tests.
echo "Running tests"
go test ./...
echo

rm -rf "$DIST"
mkdir -p "$DIST"

# --- third-party notices ---------------------------------------------------
#
# Local addition. MIT and BSD-3-Clause both require reproducing their notices in
# redistributions, INCLUDING binary form, and the release binary statically
# links those dependencies.
#
# `go list -deps` on the binary's package covers exactly what gets linked:
# test-only dependencies are excluded, and stdlib packages self-exclude because
# they report no module. License texts come from the local module cache, so no
# external tooling is needed.

NOTICES="$DIST/THIRD-PARTY-NOTICES.md"

echo "Generating third-party notices"
{
  echo "# Third-party notices"
  echo
  echo "${BINARY} ${TAG} is distributed under the ${LICENSE_ID} License (see LICENSE)."
  echo
  echo "The distributed binary statically links the Go standard library and the"
  echo "third-party Go modules listed below. Their license texts are reproduced"
  echo "here to satisfy the attribution terms of the MIT and BSD licenses."
  echo
  echo "---"
  echo
  echo "## Go standard library"
  echo
  echo "The Go standard library and runtime are compiled into this binary."
  echo "Licensed under BSD-3-Clause, Copyright (c) 2009 The Go Authors."
  echo
} > "$NOTICES"

# Ship the Go LICENSE text when the toolchain provides it; some packaged Go
# distributions (Homebrew among them) omit it from GOROOT, so fall back to a
# pointer rather than inventing the text.
GOROOT_LICENSE="$(go env GOROOT)/LICENSE"
if [[ -f "$GOROOT_LICENSE" ]]; then
  { echo '```'; cat "$GOROOT_LICENSE"; echo '```'; echo; } >> "$NOTICES"
else
  { echo "Full text: <https://go.dev/LICENSE>"; echo; } >> "$NOTICES"
fi

# htmx is 0BSD (Zero-Clause BSD), which imposes no attribution requirement at
# all — listed purely for transparency about what the binary contains.
{
  echo "---"
  echo
  echo "## htmx (vendored)"
  echo
  echo "\`internal/server/static/htmx.min.js\` is embedded in the binary."
  echo "Licensed under 0BSD (Zero-Clause BSD), which imposes no attribution"
  echo "requirement. Listed here for transparency."
  echo "Upstream: <https://github.com/bigskysoftware/htmx>"
  echo
} >> "$NOTICES"

missing_licenses=0
while IFS='|' read -r mod_path mod_version mod_dir; do
  [[ -z "$mod_path" ]] && continue
  license_file=$(ls "$mod_dir" 2>/dev/null | grep -iE '^(LICENSE|COPYING|LICENCE)' | head -1 || true)
  if [[ -z "$license_file" ]]; then
    echo "  WARNING: no license file for ${mod_path}@${mod_version}" >&2
    missing_licenses=$((missing_licenses + 1))
    continue
  fi
  {
    echo "---"
    echo
    echo "## ${mod_path} ${mod_version}"
    echo
    echo '```'
    cat "$mod_dir/$license_file"
    echo '```'
    echo
  } >> "$NOTICES"
done < <(
  go list -deps -f '{{if .Module}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}{{end}}' "$MAIN_PKG" \
    | sort -u \
    | grep -v "^github.com/${OWNER}/${BINARY}|"
)

# Incomplete notices are an attribution gap — fail rather than release quietly.
if [[ "$missing_licenses" -gt 0 ]]; then
  echo "error: ${missing_licenses} module(s) had no license file; refusing to build" >&2
  exit 1
fi
echo

sha256_of() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    sha256sum "$1" | awk '{print $1}'
  fi
}

echo "Building ${BINARY} ${TAG} for: ${PLATFORMS[*]}"
echo

EXTRAS=()
for f in README.md LICENSE; do
  [[ -f "$f" ]] && EXTRAS+=("$f")
done
EXTRAS+=("$NOTICES")

# Man page: if docs/<binary>.1.md exists, render it to troff with go-md2man
# (pure Go, run via `go run …@latest` so it never touches this project's go.mod)
# and bundle <binary>.1 into every tarball for the formula to install.
MANPAGE=""
if [[ -f "docs/${BINARY}.1.md" ]]; then
  MANPAGE="$DIST/${BINARY}.1"
  echo "Rendering man page from docs/${BINARY}.1.md"
  go run github.com/cpuguy83/go-md2man/v2@latest -in "docs/${BINARY}.1.md" -out "$MANPAGE"
  EXTRAS+=("$MANPAGE")
  echo
fi

declare -A SHA

for platform in "${PLATFORMS[@]}"; do
  os="${platform%/*}"
  arch="${platform#*/}"
  stage="$DIST/${BINARY}-${TAG}-${os}-${arch}"
  archive="${stage}.tar.gz"

  mkdir -p "$stage"
  echo "  → ${os}/${arch}"
  # CGO off so cross-compilation needs no C toolchain and the binary is static.
  # -trimpath strips local paths but preserves Go's VCS stamps, which is how the
  # binary reports its own revision at runtime (debug.ReadBuildInfo).
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -ldflags "-s -w" -o "$stage/${BINARY}" "$MAIN_PKG"

  for extra in "${EXTRAS[@]}"; do
    cp "$extra" "$stage/"
  done

  tar -czf "$archive" -C "$DIST" "$(basename "$stage")"
  rm -rf "$stage"

  SHA["${os}-${arch}"]="$(sha256_of "$archive")"
done

( cd "$DIST" && for a in *.tar.gz; do echo "$(sha256_of "$a")  $a"; done ) >"$DIST/checksums.txt"

echo
echo "Archives + checksums written to $DIST/:"
( cd "$DIST" && ls -1 ./*.tar.gz checksums.txt )

# --- formula ---------------------------------------------------------------
url_for() { echo "https://github.com/${OWNER}/${RELEASE_REPO}/releases/download/${TAG}/${BINARY}-${TAG}-$1.tar.gz"; }

# Formula class name is the CamelCase of the binary: my-tool -> MyTool.
CLASS="$(echo "$BINARY" | awk -F- '{for(i=1;i<=NF;i++) printf toupper(substr($i,1,1)) substr($i,2)}')"

# Install the man page too when one was rendered. Empty otherwise, so the
# formula is unchanged for a tool with no man page.
MAN_INSTALL=""
if [[ -n "$MANPAGE" ]]; then
  MAN_INSTALL=$'\n    man1.install "'"${BINARY}"$'.1"'
fi

need_all=(darwin-amd64 darwin-arm64 linux-amd64 linux-arm64)
have_all=true
for k in "${need_all[@]}"; do
  [[ -n "${SHA[$k]:-}" ]] || have_all=false
done

if $have_all; then
  # homepage points at the public source repo — that's where the README, docs
  # and issue tracker are. (The skill points it at the tap because it assumes a
  # private source that would 404.)
  cat >"$DIST/${BINARY}.rb" <<EOF
class ${CLASS} < Formula
  desc "${DESC}"
  homepage "https://github.com/${OWNER}/${RELEASE_REPO}"
  version "${VERSION}"
  license "${LICENSE_ID}"

  on_macos do
    on_arm do
      url "$(url_for darwin-arm64)"
      sha256 "${SHA[darwin-arm64]}"
    end
    on_intel do
      url "$(url_for darwin-amd64)"
      sha256 "${SHA[darwin-amd64]}"
    end
  end

  on_linux do
    on_arm do
      url "$(url_for linux-arm64)"
      sha256 "${SHA[linux-arm64]}"
    end
    on_intel do
      url "$(url_for linux-amd64)"
      sha256 "${SHA[linux-amd64]}"
    end
  end

  def install
    bin.install "${BINARY}"${MAN_INSTALL}
    # Homebrew's install_metafiles picks up LICENSE and README automatically
    # but not this, so it must be installed explicitly — otherwise the
    # attribution notices ship in the tarball and are discarded on install.
    doc.install "THIRD-PARTY-NOTICES.md"
  end

  test do
    assert_match "${BINARY} v", shell_output("#{bin}/${BINARY} version")
  end
end
EOF
  echo
  echo "Homebrew formula written to $DIST/${BINARY}.rb"
else
  echo
  echo "note: non-standard platform set — skipping formula generation." >&2
fi

cat <<EOF

Done. Create the release BEFORE the formula — the formula points at these assets,
so publishing it first leaves a window where installs 404.

  1. Verify the binary self-identifies with no ", modified":
       tar xzf dist/${BINARY}-${TAG}-darwin-arm64.tar.gz -C /tmp
       /tmp/${BINARY}-${TAG}-darwin-arm64/${BINARY} version
  2. gh release create ${TAG} dist/${BINARY}-${TAG}-*.tar.gz dist/checksums.txt \\
       --repo ${OWNER}/${RELEASE_REPO} --title "${BINARY} ${TAG}"
  3. gh release view ${TAG} --repo ${OWNER}/${RELEASE_REPO} \\
       --json assets --jq '.assets[].name'      # confirm all 5 uploaded
  4. cp dist/${BINARY}.rb <${TAP_REPO}>/Formula/${BINARY}.rb && commit + push the tap
  5. brew update && brew upgrade ${BINARY} && ${BINARY} version
EOF
