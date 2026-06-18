#!/usr/bin/env bash
#
# Build release artifacts for a Homebrew tap.
#
# Usage:  ./scripts/release.sh <version>     e.g. ./scripts/release.sh v0.1.0
#
# Produces under dist/:
#   - 4 platform tarballs (darwin-arm64, darwin-amd64, linux-amd64, linux-arm64)
#   - checksums.txt
#   - clinote.rb   (Homebrew formula, ready to commit to the tap repo)
#
# See docs/release.md for the full release workflow.

set -euo pipefail

# ---------- args & paths ----------

if [[ $# -lt 1 ]]; then
  echo "usage: $0 <version>   (e.g. $0 v0.1.0)" >&2
  exit 1
fi

VERSION="$1"
if [[ ! "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.-]+)?$ ]]; then
  echo "error: version must look like v0.1.0 (got: $VERSION)" >&2
  exit 1
fi
VERSION_NO_V="${VERSION#v}"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="$ROOT/dist"
BIN="clinote"
CMD_PKG="./cmd/clinote"

# GitHub repo for download URLs in the generated formula.
REPO_HOST="github.com"
REPO_OWNER="pmuston"
REPO_NAME="clinote"
REPO_URL="https://${REPO_HOST}/${REPO_OWNER}/${REPO_NAME}"
RELEASE_URL_BASE="${REPO_URL}/releases/download/${VERSION}"

# Homebrew tap repo holding the formula. Homebrew strips the "homebrew-"
# prefix when resolving names, so `brew tap pmuston/tap` clones
# `github.com/pmuston/homebrew-tap` and users install with
# `brew install pmuston/tap/clinote`.
TAP_REPO_NAME="homebrew-tap"
TAP_REF="${REPO_OWNER}/tap"

# Platforms to build for. Each line: "<GOOS> <GOARCH>".
PLATFORMS=(
  "darwin arm64"
  "darwin amd64"
  "linux amd64"
  "linux arm64"
)

# ---------- preflight ----------

cd "$ROOT"

if ! command -v go >/dev/null 2>&1; then
  echo "error: 'go' not on PATH" >&2
  exit 1
fi
if ! command -v shasum >/dev/null 2>&1; then
  echo "error: 'shasum' not on PATH" >&2
  exit 1
fi

echo "==> running tests before release"
go test ./...

echo "==> cleaning dist/"
rm -rf "$DIST"
mkdir -p "$DIST"

# ---------- build each platform ----------

for plat in "${PLATFORMS[@]}"; do
  read -r os arch <<<"$plat"
  echo "==> building $os/$arch"

  staging="$(mktemp -d)"
  trap 'rm -rf "$staging"' EXIT

  GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 \
    go build \
      -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o "$staging/$BIN" \
      "$CMD_PKG"

  # Bundle LICENSE and README into the tarball when present.
  [[ -f "$ROOT/LICENSE"   ]] && cp "$ROOT/LICENSE"   "$staging/"
  [[ -f "$ROOT/README.md" ]] && cp "$ROOT/README.md" "$staging/"

  tarball="$DIST/clinote-${VERSION}-${os}-${arch}.tar.gz"
  (cd "$staging" && tar -czf "$tarball" .)

  rm -rf "$staging"
  trap - EXIT

  echo "    $(basename "$tarball")"
done

# ---------- checksums ----------

echo "==> generating checksums"
(cd "$DIST" && shasum -a 256 *.tar.gz > checksums.txt)
cat "$DIST/checksums.txt"

sha_for() {
  local pattern="$1"
  local sha
  sha=$(grep "$pattern" "$DIST/checksums.txt" | awk '{print $1}')
  if [[ -z "$sha" ]]; then
    echo "error: no checksum for $pattern" >&2
    exit 1
  fi
  echo "$sha"
}

DARWIN_ARM_SHA=$(sha_for "darwin-arm64")
DARWIN_AMD_SHA=$(sha_for "darwin-amd64")
LINUX_AMD_SHA=$(sha_for  "linux-amd64")
LINUX_ARM_SHA=$(sha_for  "linux-arm64")

# ---------- Homebrew formula ----------

echo "==> writing Homebrew formula → $DIST/clinote.rb"
cat > "$DIST/clinote.rb" <<EOF
class Clinote < Formula
  desc "Personal lab notebook for shell commands; runs in your browser"
  homepage "${REPO_URL}"
  version "${VERSION_NO_V}"
  license "MIT"

  on_macos do
    on_arm do
      url "${RELEASE_URL_BASE}/clinote-${VERSION}-darwin-arm64.tar.gz"
      sha256 "${DARWIN_ARM_SHA}"
    end
    on_intel do
      url "${RELEASE_URL_BASE}/clinote-${VERSION}-darwin-amd64.tar.gz"
      sha256 "${DARWIN_AMD_SHA}"
    end
  end

  on_linux do
    on_arm do
      url "${RELEASE_URL_BASE}/clinote-${VERSION}-linux-arm64.tar.gz"
      sha256 "${LINUX_ARM_SHA}"
    end
    on_intel do
      url "${RELEASE_URL_BASE}/clinote-${VERSION}-linux-amd64.tar.gz"
      sha256 "${LINUX_AMD_SHA}"
    end
  end

  def install
    bin.install "clinote"
  end

  test do
    assert_match "${VERSION_NO_V}", shell_output("#{bin}/clinote version")
  end
end
EOF

# ---------- summary ----------

echo
echo "Done. Artifacts in $DIST:"
ls -1 "$DIST"
echo
echo "Next steps:"
echo "  1. Create the GitHub release:"
echo "       gh release create ${VERSION} --title \"clinote ${VERSION}\" \\"
echo "         dist/clinote-${VERSION}-*.tar.gz dist/checksums.txt"
echo
echo "  2. Update the tap repo (github.com/${REPO_OWNER}/${TAP_REPO_NAME}):"
echo "       cp dist/clinote.rb ../${TAP_REPO_NAME}/Formula/clinote.rb"
echo "       cd ../${TAP_REPO_NAME} && git add Formula/clinote.rb \\"
echo "         && git commit -m \"clinote ${VERSION}\" && git push"
echo
echo "  3. Verify:"
echo "       brew update && brew install ${TAP_REF}/clinote"
echo "       clinote version"
