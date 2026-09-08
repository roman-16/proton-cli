// Package self holds the commands that act on proton itself rather than on a
// Proton account.
package self

import (
	"runtime"
	"runtime/debug"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

// VersionCmd reports the build.
//
// It exists alongside --version because a version is a thing you ask for, and
// asking with a subcommand is what every other question in this CLI looks like.
// It also carries more than a bare number: which Go built it, and for what.
func VersionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version and build information",
		// No authentication: this is about the binary, not the account.
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			build := struct {
				Version  string `json:"version"`
				Go       string `json:"go"`
				Platform string `json:"platform"`
				Revision string `json:"revision,omitempty"`
			}{
				Version:  version,
				Go:       runtime.Version(),
				Platform: runtime.GOOS + "/" + runtime.GOARCH,
				Revision: revision(),
			}
			return kit.Show(c, ui.RecordSpec{
				Object: build,
				Fields: []ui.Field{
					{Label: "Version", Value: build.Version},
					{Label: "Go", Value: build.Go},
					{Label: "Platform", Value: build.Platform},
					{Label: "Revision", Value: build.Revision},
				},
			})
		}),
	}
}

// revision reads the VCS revision the toolchain stamped in, when there is one.
func revision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			if len(s.Value) > 12 {
				return s.Value[:12]
			}
			return s.Value
		}
	}
	return ""
}
