package buildinfo

import "runtime"

var (
	// Version, Commit, and Date are populated by release builds.
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// Info is the immutable build identity exposed by the CLI and failure capsules.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	GoVersion string `json:"goVersion"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

// Current returns the process build identity.
func Current() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		Date:      Date,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
}
