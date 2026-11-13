package self

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/selfmanage"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

func UpdateCmd(version string) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:         "update [VERSION]",
		Aliases:     []string{"upgrade", "self-update"},
		Annotations: map[string]string{kit.SignedOut: "yes"},
		Short:       "Update proton to the latest release",
		Long: `Replace this proton binary in place with the latest GitHub release
(or a specific version). The release has to carry a signature from the
proton-cli release key, and the download has to match its SHA-256
checksum, or nothing is installed. Releases up to 5.4.0 carry no
signature, so download those from the releases page instead.

--dry-run reports what it would install without installing it.

Only a curl-script install or a manually downloaded binary can update
itself. If proton was installed with a package manager (apt, dnf,
apk, Homebrew, winget, npm, Nix), update it with that package manager.`,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			target := ""
			if len(c.Args) == 1 {
				target = strings.TrimPrefix(c.Args[0], "v")
			}
			return runUpdate(c, version, target, force)
		}),
	}
	cmd.Flags().BoolVar(&force, "force", false, "Install again even if already up to date")
	return cmd
}

type updateStatus struct {
	Current         string `json:"current"`
	Latest          string `json:"latest"`
	UpdateAvailable bool   `json:"update_available"`
}

const releaseNotes = "https://github.com/roman-16/proton-cli/releases/tag/v"

func runUpdate(c *kit.Invocation, version, target string, force bool) error {
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

	if version == "dev" && target == "" && !force {
		return kit.Fail("This is a development build, so there is no version to compare against.").
			Hint("proton update 1.9.11",
				"curl -fsSL https://raw.githubusercontent.com/roman-16/proton-cli/main/scripts/install.sh | sh")
	}

	if !newer && target == "" && !force {
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
			return refusedRelease(c, latest, err)
		}
		c.Note("%s Signature and checksum verified.", ui.GlyphSuccess)
		if err := selfmanage.Apply(bin, exe); err != nil {
			return selfManageError(err, exe, actionUpdate)
		}
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

func refusedRelease(c *kit.Invocation, version string, err error) error {
	switch {
	case errors.Is(err, selfmanage.ErrUnsigned):
		return kit.Fail("Release %s carries no signature, so it was not installed.", version).
			Hint("download it from " + releaseNotes + version)
	case errors.Is(err, selfmanage.ErrForged):
		slog.DebugContext(c.Ctx, "release refused", "error", err.Error())
		return kit.Fail("The signature on release %s does not match the %s release key, so it was not installed.",
			version, kit.Alias)
	}
	return err
}
