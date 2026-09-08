package self

import (
	"net/http"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/selfmanage"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

// UpdateCmd replaces this binary in place with a published release.
//
// It is a mutation like any other and reports through kit.Mutate, which is what
// gives it --dry-run. That matters more here than almost anywhere else: this is
// the one command that rewrites the program the user is running, so "show me
// what you would do" has to be answerable without doing it.
func UpdateCmd(version string) *cobra.Command {
	var checkOnly, reinstall bool
	cmd := &cobra.Command{
		Use:         "update [VERSION]",
		Aliases:     []string{"upgrade", "self-update"},
		Annotations: map[string]string{kit.OnThisMachine: "yes"},
		Short:       "Update proton to the latest release",
		Long: `Replace this proton binary in place with the latest GitHub release
(or a specific version), verifying the download against the published
SHA-256 checksums.

Only a curl-script install or a manually downloaded binary can update
itself. If proton was installed with a package manager (apt, dnf,
apk, Homebrew, winget, npm, Nix), update it with that package manager.`,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			target := ""
			if len(c.Args) == 1 {
				target = strings.TrimPrefix(c.Args[0], "v")
			}
			return runUpdate(c, version, target, checkOnly, reinstall)
		}),
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "Only report whether an update is available; don't install")
	cmd.Flags().BoolVar(&reinstall, "reinstall", false, "Install again even if already up to date")
	return cmd
}

type updateStatus struct {
	Current         string `json:"current"`
	Latest          string `json:"latest"`
	UpdateAvailable bool   `json:"update_available"`
}

// releaseNotes is where a reader goes to find out what they just installed.
const releaseNotes = "https://github.com/roman-16/proton-cli/releases/tag/v"

func runUpdate(c *kit.Invocation, version, target string, checkOnly, reinstall bool) error {
	u := c.UI()
	client := &http.Client{Timeout: 60 * time.Second}

	latest := target
	if latest == "" {
		v, err := selfmanage.LatestVersion(c.Ctx, client)
		if err != nil {
			return kit.Fail("Could not reach GitHub to find the latest release: %v", err).
				Hint("check your connection, or name a version: proton update 1.9.11").
				Exit(5)
		}
		latest = v
	}
	newer := selfmanage.IsNewer(latest, version)

	if checkOnly {
		if u.Format.Machine() {
			return kit.Object(c, updateStatus{Current: version, Latest: latest, UpdateAvailable: newer})
		}
		if newer {
			Available(u, version, latest)
		} else {
			c.Note("proton is up to date (%s).", version)
		}
		return nil
	}

	if version == "dev" && target == "" && !reinstall {
		return kit.Fail("This is a development build, so there is no version to compare against.").
			Hint("proton update 1.9.11",
				"curl -fsSL https://raw.githubusercontent.com/roman-16/proton-cli/main/scripts/install.sh | sh")
	}

	if !newer && target == "" && !reinstall {
		if u.Format.Machine() {
			return kit.Object(c, updateStatus{Current: version, Latest: latest, UpdateAvailable: false})
		}
		c.Note("proton is already up to date (%s).", version)
		return nil
	}

	exe, err := resolveExe()
	if err != nil {
		return err
	}
	if err := guardManaged(exe, actionUpdate); err != nil {
		return err
	}

	// A preview reports the version still running and the one it would bring; the
	// change itself reports the version now on disk and nothing left to fetch.
	installed := map[string]any{"current": latest, "latest": latest, "update_available": false}
	if c.App.DryRun {
		installed = map[string]any{"current": version, "latest": latest, "update_available": true}
	}
	if err := kit.Mutate(c, ui.ResultSpec{
		Action: ui.Updated, Count: 1, Name: kit.Program,
		Detail: version + " → " + latest,
		Extra:  installed,
	}, func() error {
		bin, err := selfmanage.Download(c.Ctx, client, latest, ui.NewProgress(u))
		if err != nil {
			return err
		}
		c.Note("%s Checksum verified.", ui.GlyphSuccess)
		if err := selfmanage.Apply(bin, exe); err != nil {
			return selfManageError(err, exe, actionUpdate)
		}
		// An install answers to both names, so writing a release into one is
		// only half of it until the other is there beside it.
		linkAlias(c, exe)
		return nil
	}); err != nil {
		return err
	}
	if !c.App.DryRun {
		c.Note("  Release notes: %s%s", releaseNotes, latest)
	}
	return nil
}
