package version

import (
	"runtime/debug"
	"testing"
)

func TestMergeBuildInfoUsesGoModuleVersionForDevBuilds(t *testing.T) {
	info := mergeBuildInfo(Info{Version: "dev"}, &debug.BuildInfo{
		Main: debug.Module{Version: "v1.2.3"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "abcdef0123456789"},
			{Key: "vcs.time", Value: "2026-04-12T10:00:00Z"},
		},
	})

	if info.Version != "v1.2.3" {
		t.Fatalf("got version %q", info.Version)
	}
	if info.Commit != "abcdef0123456789" {
		t.Fatalf("got commit %q", info.Commit)
	}
	if info.BuildDate != "2026-04-12T10:00:00Z" {
		t.Fatalf("got build date %q", info.BuildDate)
	}
}

func TestMergeBuildInfoKeepsInjectedReleaseValues(t *testing.T) {
	info := mergeBuildInfo(Info{
		Version:   "v9.9.9",
		Commit:    "release-commit",
		BuildDate: "release-date",
	}, &debug.BuildInfo{
		Main: debug.Module{Version: "v1.2.3"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "abcdef0123456789"},
			{Key: "vcs.time", Value: "2026-04-12T10:00:00Z"},
		},
	})

	if info.Version != "v9.9.9" {
		t.Fatalf("got version %q", info.Version)
	}
	if info.Commit != "release-commit" {
		t.Fatalf("got commit %q", info.Commit)
	}
	if info.BuildDate != "release-date" {
		t.Fatalf("got build date %q", info.BuildDate)
	}
}

func TestNormalizeFallsBackToDev(t *testing.T) {
	info := normalize(Info{})
	if info.Version != "dev" {
		t.Fatalf("got version %q", info.Version)
	}
}
