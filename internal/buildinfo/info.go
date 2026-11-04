// Package buildinfo reads metadata embedded by the Go toolchain.
package buildinfo

import (
	"runtime"
	"runtime/debug"
)

// Info describes the running Node binary. Revision may be empty when VCS
// metadata is unavailable; Version is devel when Go supplies no module version.
type Info struct {
	Version   string
	Revision  string
	Modified  bool
	GoVersion string
	OS        string
	Arch      string
}

// Read returns native build metadata without invoking Git or accessing the network.
func Read() Info {
	info := Info{
		Version:   "devel",
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
	if metadata, ok := debug.ReadBuildInfo(); ok {
		if metadata.Main.Version != "" && metadata.Main.Version != "(devel)" {
			info.Version = metadata.Main.Version
		}
		for _, setting := range metadata.Settings {
			switch setting.Key {
			case "vcs.revision":
				info.Revision = setting.Value
			case "vcs.modified":
				info.Modified = setting.Value == "true"
			}
		}
	}
	return info
}
