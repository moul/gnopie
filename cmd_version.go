package main

import (
	"context"
	"runtime/debug"

	"github.com/gnolang/gno/tm2/pkg/commands"
)

// devVersion is what a build reports when the VCS stamp says nothing, which is
// the case for `go run .` and for a build from a tree with no tags.
const devVersion = "(devel)"

// buildVersion reads the version out of the binary rather than out of a constant.
//
// Since Go 1.24 `go build` stamps Main.Version from the VCS tag on its own, so a
// release binary reports its tag with no -X ldflags anywhere. A hardcoded
// constant is a second source of truth that disagrees with the first the moment
// somebody tags without editing it, and the only person who would notice is
// whoever is trying to report a bug against a version that does not exist.
func buildVersion() (version, revision string) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return devVersion, ""
	}
	version = info.Main.Version
	if version == "" {
		version = devVersion
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if len(s.Value) > 12 {
				revision = s.Value[:12]
			} else {
				revision = s.Value
			}
		case "vcs.modified":
			if s.Value == "true" {
				revision += "-dirty"
			}
		}
	}
	return version, revision
}

// gnoVersion reports which gno the binary links, which is the number that
// actually explains most surprises: gnopie links gno as a library and pins it,
// so a chain that has moved past the pin can make it wrong rather than broken.
func gnoVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, dep := range info.Deps {
		if dep.Path == "github.com/gnolang/gno" {
			if dep.Replace != nil {
				return dep.Replace.Version + " (replaced)"
			}
			return dep.Version
		}
	}
	return "unknown"
}

func newVersionCmd(io commands.IO) *commands.Command {
	return commands.NewCommand(
		commands.Metadata{
			Name:       "version",
			ShortUsage: "gnopie version",
			ShortHelp:  "Print the version, and the gno it is pinned to.",
		},
		nil,
		func(_ context.Context, _ []string) error {
			version, revision := buildVersion()
			if revision != "" {
				io.Printfln("gnopie %s (%s)", version, revision)
			} else {
				io.Printfln("gnopie %s", version)
			}
			io.Printfln("gno    %s", gnoVersion())
			return nil
		},
	)
}
