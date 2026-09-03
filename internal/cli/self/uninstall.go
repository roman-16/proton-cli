package self

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/roman-16/proton-cli/internal/account/session"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/selfmanage"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

// UninstallCmd removes a manually installed binary.
//
// It is a mutation like any other, so it reports through kit.Mutate and inherits
// what that guarantees: --dry-run describes it without doing it, and being
// unable to take it back is what makes it stop for a yes. There is no local
// --yes here; the global one means "proceed without asking" everywhere,
// including here.
func UninstallCmd() *cobra.Command {
	var purge bool
	cmd := &cobra.Command{
		Use:         "uninstall",
		Annotations: map[string]string{kit.OnThisMachine: "yes"},
		Short:       "Remove a curl/PowerShell-installed " + kit.Program,
		Long: `Remove a proton binary installed with the curl or PowerShell installer,
or downloaded by hand.

A package-managed install (apt, dnf, apk, AUR, Homebrew, winget, npm, Nix)
is refused, with the right command to use instead.

Only the binary goes, under both names it answers to. --purge also deletes
your saved sessions, the ID cache and the diagnostic log.`,
		Args: cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			return runUninstall(c, purge)
		}),
	}
	cmd.Flags().BoolVar(&purge, "purge", false,
		"Also remove local data (saved sessions, ID cache and diagnostic log)")
	return cmd
}

func runUninstall(c *kit.Invocation, purge bool) error {
	exe, err := resolveExe()
	if err != nil {
		return err
	}
	// A package-managed install is refused before anything else, so the question
	// is never asked about a removal that was never going to happen.
	if err := guardManaged(exe, actionUninstall); err != nil {
		return err
	}

	dataDir := ""
	if purge {
		if d, err := session.Dir(); err == nil {
			dataDir = d
		}
	}
	detail := ""
	if dataDir != "" {
		detail = "with its saved sessions, ID cache and diagnostic log"
	}

	if err := kit.Mutate(c, ui.ResultSpec{
		Action: ui.Uninstalled, Count: 1, Name: exe, Detail: detail,
		Extra: map[string]any{"binary": exe, "data": dataDir},
	}, func() error {
		if err := selfmanage.Remove(exe, aliasesBeside(exe), dedicatedDir(exe)); err != nil {
			return selfManageError(err, exe, actionUninstall)
		}
		if dataDir != "" {
			if err := os.RemoveAll(dataDir); err != nil {
				c.Warn("Could not remove %s: %v", dataDir, err)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if c.App.DryRun {
		return nil
	}

	if runtime.GOOS == "windows" {
		c.Note("A temporary copy will be cleaned up after this process exits.")
	}
	// What is left behind is the part worth stopping on: the binary is gone, so
	// nothing here can remove the credential afterwards.
	if dataDir != "" {
		c.Warn("The session this machine held is still valid at Proton.\n" +
			"Sign it out from any Proton app to invalidate the tokens it carried.")
	} else {
		if d, err := session.Dir(); err == nil {
			c.Warn("Your saved session is still on this machine, at %s.\n"+
				"Re-run with --purge to remove it, and sign the session out from any Proton app.", d)
		}
	}
	return nil
}

// dedicatedDir returns the install directory when it belongs solely to this
// install, so uninstall can remove it once empty. Shared bin directories (e.g.
// ~/.local/bin) yield "".
//
// Only Windows has one: the PowerShell installer makes a folder of its own,
// named after the project, and puts the program in it.
func dedicatedDir(exe string) string {
	if runtime.GOOS != "windows" {
		return ""
	}
	dir := filepath.Dir(exe)
	if filepath.Base(dir) == kit.Alias {
		return dir
	}
	return ""
}
