package version

import "runtime/debug"

var (
	Version   = "dev"
	Commit    = ""
	BuildDate = ""
)

type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	BuildDate string `json:"build_date,omitempty"`
}

// Current returns release metadata for the running binary.
//
// Release builds should inject these fields with linker flags. Local builds fall
// back to Go build metadata when it is available so `lazyagent version` stays
// useful outside tagged releases too.
func Current() Info {
	info := Info{
		Version:   Version,
		Commit:    Commit,
		BuildDate: BuildDate,
	}

	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		return normalize(info)
	}

	return normalize(mergeBuildInfo(info, buildInfo))
}

func mergeBuildInfo(info Info, buildInfo *debug.BuildInfo) Info {
	if buildInfo == nil {
		return info
	}

	if shouldUseBuildVersion(info.Version, buildInfo.Main.Version) {
		info.Version = buildInfo.Main.Version
	}

	for _, setting := range buildInfo.Settings {
		switch setting.Key {
		case "vcs.revision":
			if info.Commit == "" {
				info.Commit = setting.Value
			}
		case "vcs.time":
			if info.BuildDate == "" {
				info.BuildDate = setting.Value
			}
		}
	}

	return info
}

func normalize(info Info) Info {
	if info.Version == "" {
		info.Version = "dev"
	}

	return info
}

func shouldUseBuildVersion(currentVersion, buildVersion string) bool {
	if buildVersion == "" || buildVersion == "(devel)" {
		return false
	}

	return currentVersion == "" || currentVersion == "dev"
}
