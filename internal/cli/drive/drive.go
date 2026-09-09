// Package drive is the `proton drive` tree.
//
// Drive addresses things two ways, and both are real. A file or folder that
// exists in a tree is named by its PATH, because that is how a person thinks
// about it and how Proton's own API resolves it. Something with no place in a
// tree - a trashed item, a photo, an album - is named by REF, its link ID, which
// shortens on a terminal like every other reference.
//
// There is more than one tree: your own files, each computer syncing to Drive,
// each item somebody shared with you, and each public link somebody sent you. A
// path means the same thing in all of them, and which one a command works in is
// said by --computer, --shared or --link.
package drive

import (
	"github.com/roman-16/proton-cli/internal/cli/kit"
	drivesvc "github.com/roman-16/proton-cli/internal/service/drive"
	"github.com/spf13/cobra"
)

func New() *cobra.Command {
	c := &cobra.Command{
		Use:   "drive",
		Short: "Files and folders in Drive",
	}
	c.AddCommand(computersCmd(), itemsCmd(), trashCmd(), invitationsCmd(), sharedCmd(),
		sharingCmd(), photosCmd(), settingsCmd())
	return c
}

// context opens your own files, which is the tree a command works in when it is
// not pointed at another.
func context(c *kit.Invocation) (*drivesvc.Context, error) {
	return c.App.Drive.Resolve(c.Ctx)
}

// photosContext opens the photo volume, which Proton keeps separate from the
// file tree.
func photosContext(c *kit.Invocation) (*drivesvc.Context, error) {
	return c.App.Drive.ResolvePhotos(c.Ctx)
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
