// Package version exposes build identity.
//
// Values are injected at build time with -ldflags. They are read-only at
// runtime and appear in logs, the /healthz payload, telemetry resource
// attributes, and the schema_migrations ledger — so that "which build applied
// this migration?" is always answerable.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

var (
	// Version is the semantic release, e.g. "1.0.0". Injected at build time.
	Version = "0.0.0-dev"

	// Commit is the git SHA. Injected at build time.
	Commit = "unknown"

	// BuildTime is RFC-3339. Injected at build time.
	BuildTime = "unknown"
)

// Info is the structured build identity.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
	GoVersion string `json:"go_version"`
}

func Get() Info {
	commit := Commit
	if commit == "unknown" {
		// Fall back to the VCS stamp the Go toolchain embeds automatically,
		// so a plain `go build` still produces an identifiable binary.
		if bi, ok := debug.ReadBuildInfo(); ok {
			for _, s := range bi.Settings {
				if s.Key == "vcs.revision" {
					commit = s.Value
				}
			}
		}
	}
	return Info{
		Version:   Version,
		Commit:    commit,
		BuildTime: BuildTime,
		GoVersion: runtime.Version(),
	}
}

func (i Info) String() string {
	short := i.Commit
	if len(short) > 7 {
		short = short[:7]
	}
	return fmt.Sprintf("%s (%s, built %s, %s)", i.Version, short, i.BuildTime, i.GoVersion)
}
