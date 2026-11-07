// Package buildinfo carries the version stamped into a binary at build time.
//
// Release builds set the variables with the linker:
//
//	go build -ldflags "-X github.com/karim-w/slop/pkg/domain/buildinfo.Version=v1.2.3 \
//	  -X github.com/karim-w/slop/pkg/domain/buildinfo.Commit=abc1234"
//
// Builds without those flags fall back to the module and VCS data that the
// Go toolchain embeds on its own.
package buildinfo

import (
	"fmt"
	"runtime/debug"
)

var (
	// Version is the release tag, such as v1.2.3.
	Version = ""
	// Commit is the short git hash the binary was built from.
	Commit = ""
	// Date is the build time in RFC 3339.
	Date = ""
)

// Info is a resolved view of the build metadata.
type Info struct {
	Version, Commit, Date string
	Modified              bool
}

// Get returns the stamped values, filling gaps from the embedded build info.
func Get() Info {
	i := Info{Version: Version, Commit: Commit, Date: Date}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if i.Version == "" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			i.Version = bi.Main.Version
		}
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if i.Commit == "" {
					i.Commit = s.Value
				}
			case "vcs.time":
				if i.Date == "" {
					i.Date = s.Value
				}
			case "vcs.modified":
				i.Modified = s.Value == "true"
			}
		}
	}
	if i.Version == "" {
		i.Version = "dev"
	}
	if len(i.Commit) > 7 {
		i.Commit = i.Commit[:7]
	}
	return i
}

// String renders "v1.2.3 (abc1234)" or "dev (abc1234, modified)".
func (i Info) String() string {
	switch {
	case i.Commit == "":
		return i.Version
	case i.Modified:
		return fmt.Sprintf("%s (%s, modified)", i.Version, i.Commit)
	}
	return fmt.Sprintf("%s (%s)", i.Version, i.Commit)
}
