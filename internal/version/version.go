package version

import (
	"os"
	"runtime/debug"
	"strconv"
)

// Build-time parameters set via -ldflags.

var (
	// Version is the release version, set via -ldflags.
	Version = "devel"
	// Commit is the git commit hash, set via -ldflags.
	Commit = "unknown"
	// BuildID is a unique build fingerprint derived from the executable's
	// modification time, which changes on every recompilation.
	BuildID = ""
)

func init() {
	// Fallback for go install: only use embedded build info if ldflags weren't set.
	if Version != "devel" {
		return
	}
	info, ok := debug.ReadBuildInfo()
	if ok {
		mainVersion := info.Main.Version
		if mainVersion != "" && mainVersion != "(devel)" {
			Version = mainVersion
		}
	}

	if BuildID == "" {
		BuildID = deriveBuildID()
	}
}

func deriveBuildID() string {
	exe, err := os.Executable()
	if err != nil {
		return "unknown"
	}
	fi, err := os.Stat(exe)
	if err != nil {
		return "unknown"
	}
	return strconv.FormatInt(fi.ModTime().UnixNano(), 36)
}