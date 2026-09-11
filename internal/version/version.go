// Package version exposes build metadata injected at link time.
package version

import "runtime/debug"

// These are overridden with -ldflags "-X ..." by scripts/build.sh.
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

func init() {
	if Commit != "" {
		return
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			Commit = s.Value
		case "vcs.time":
			if Date == "" {
				Date = s.Value
			}
		}
	}
}
