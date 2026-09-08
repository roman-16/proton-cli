package drive

import (
	"github.com/roman-16/proton-cli/internal/cli/kit"
	drivesvc "github.com/roman-16/proton-cli/internal/service/drive"
	"github.com/spf13/cobra"
)

// Drive holds more than one tree of files, and a path is a path in whichever one
// the command was pointed at.
//
// Your own files are where a command works when it is pointed at none. The other
// two are named the way every container is named in this CLI - by REF, on a flag
// called after the collection that lists them - so `/Documents` means the same
// thing in all three and nothing has to learn a second notation for a file that
// happens to be somewhere else.
type tree struct {
	computer string
	shared   string
}

func (t *tree) register(c *cobra.Command) {
	c.Flags().StringVar(&t.computer, "computer", "", "Work inside this computer's files, by name or ID")
	c.Flags().StringVar(&t.shared, "shared", "", "Work inside an item shared with you, by name or ID")
	c.MarkFlagsMutuallyExclusive("computer", "shared")
}

// context opens the tree the flags name, which is your own files when they name
// none.
func (t *tree) context(c *kit.Invocation) (*drivesvc.Context, error) {
	switch {
	case t.computer != "":
		ref, err := kit.Expand(c.App, t.computer)
		if err != nil {
			return nil, err
		}
		computer, err := computerList(c).Find(c.Ctx, ref)
		if err != nil {
			return nil, err
		}
		return c.App.Drive.OpenComputer(c.Ctx, computer)
	case t.shared != "":
		ref, err := kit.Expand(c.App, t.shared)
		if err != nil {
			return nil, err
		}
		item, err := sharedList(c).Find(c.Ctx, ref)
		if err != nil {
			return nil, err
		}
		return c.App.Drive.OpenShared(c.Ctx, item)
	}
	return context(c)
}
