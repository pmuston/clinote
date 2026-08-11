// Package version reports which build of clinote is running.
//
// The version is a constant here rather than an -ldflags injection so the
// binary, the build script and the Homebrew formula all read the same source
// and cannot drift apart. The commit comes from Go's own VCS stamping, which
// -trimpath preserves.
package version

import (
	"fmt"
	"runtime/debug"
	"strings"
)

// Version is the released version. Bump it whenever the interface changes so
// a running instance can be told apart from an older one.
const Version = "2.0.0"

// Provenance is the `tool` value written into every result block (§6), in the
// format's name/version shape.
//
// It is derived from Version rather than written out again, so the string in a
// notebook and the string `clinote version` prints cannot drift. Major.minor is
// enough: a result records which tool wrote it, not which patch.
func Provenance() string {
	major, rest, ok := strings.Cut(Version, ".")
	if !ok {
		return "clinote/" + Version
	}
	minor, _, _ := strings.Cut(rest, ".")
	return "clinote/" + major + "." + minor
}

// String returns the version plus the VCS revision Go recorded at build time,
// e.g. "0.1.0 (a1b2c3d4e5f6)".
//
// A ", modified" marker means the working tree was dirty when the binary was
// compiled, so the artifact corresponds to no commit and must not be released.
// An empty revision means the binary was built outside a git checkout (or with
// -buildvcs=false), in which case only the bare version is reported.
func String() string {
	rev, modified := "", false
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.modified":
				modified = s.Value == "true"
			}
		}
	}
	if rev == "" {
		return Version
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if modified {
		return fmt.Sprintf("%s (%s, modified)", Version, rev)
	}
	return fmt.Sprintf("%s (%s)", Version, rev)
}
