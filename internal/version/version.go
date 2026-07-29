// Package version carries the build stamp. The values are set with -ldflags at
// release time and keep their defaults in every other build, so a binary can
// always say where it came from.
package version

import (
	"runtime/debug"
	"time"
)

var (
	// Version is the release tag, for example v1.2.0.
	Version = "dev"
	// Commit is the git revision the binary was built from.
	Commit = ""
	// BuildTime is when it was built, as RFC 3339.
	BuildTime = ""
)

type Build struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"buildTime"`
}

// Current fills in whatever -ldflags did not. Go stamps the revision and the
// build time into every binary built from a git checkout, so a developer build
// still reports something useful.
func Current() Build {
	build := Build{Version: Version, Commit: Commit, BuildTime: BuildTime}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return build
	}

	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if build.Commit == "" {
				build.Commit = setting.Value
			}
		case "vcs.time":
			if build.BuildTime == "" {
				build.BuildTime = setting.Value
			}
		case "vcs.modified":
			if setting.Value == "true" && build.Version == "dev" {
				build.Version = "dev (modified)"
			}
		}
	}

	if build.BuildTime == "" {
		build.BuildTime = time.Now().UTC().Format(time.RFC3339)
	}
	return build
}

// Short is the commit trimmed to the length people actually read.
func (b Build) Short() string {
	if len(b.Commit) > 7 {
		return b.Commit[:7]
	}
	return b.Commit
}
