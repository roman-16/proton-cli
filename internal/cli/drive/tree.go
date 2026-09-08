package drive

import (
	"github.com/roman-16/proton-cli/internal/cli/kit"
	drivesvc "github.com/roman-16/proton-cli/internal/service/drive"
	"github.com/spf13/cobra"
)

// Drive holds more than one tree of files, and a path is a path in whichever one
// the command was pointed at.
//
// Your own files are where a command works when it is pointed at none. The
// others are named the way every container is named in this CLI - by REF, on a
// flag called after the collection that lists them, and a public link by the URL
// that is the only name it has - so `/Documents` means the same thing in all of
// them and nothing has to learn a second notation for a file that happens to be
// somewhere else.
//
// A link is the one tree that can be read and not written, so it is offered only
// by the commands that read.
type tree struct {
	computer string
	shared   string
	link     string
	password *kit.Password
	readOnly bool
}

func (t *tree) register(c *cobra.Command) {
	t.declare(c)
	c.MarkFlagsMutuallyExclusive("computer", "shared")
}

// registerReadOnly adds the tree flags to a command that only reads, which is
// every command a public link can answer.
func (t *tree) registerReadOnly(c *cobra.Command) {
	t.declare(c)
	t.readOnly = true
	t.password = kit.LinkPasswordToOpen()
	c.Flags().StringVar(&t.link, "link", "", "Work inside a public link somebody sent you, by URL")
	c.MarkFlagsMutuallyExclusive("computer", "shared", "link")
	t.password.Declare(c)
}

func (t *tree) declare(c *cobra.Command) {
	c.Flags().StringVar(&t.computer, "computer", "", "Work inside this computer's files, by name or ID")
	c.Flags().StringVar(&t.shared, "shared", "", "Work inside an item shared with you, by name or ID")
}

// supply claims standard input for the link password before anything else can
// drain it. A command that cannot open a link has none to claim.
func (t *tree) supply(c *kit.Invocation) error {
	if t.password == nil {
		return nil
	}
	return t.password.Supply(c)
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
		if item.IsLink() && !t.readOnly {
			return nil, kit.Fail("%q is a public link somebody sent you, which can only be listed and downloaded.",
				sharedName(item))
		}
		return c.App.Drive.OpenShared(c.Ctx, item)
	case t.link != "":
		password, err := t.linkPassword()
		if err != nil {
			return nil, err
		}
		return c.App.Drive.OpenLink(c.Ctx, t.link, password)
	}
	return context(c)
}

// linkPassword is what the link's owner set on it, which is nothing at all for
// most links.
func (t *tree) linkPassword() (string, error) {
	if t.password == nil || !t.password.Wanted() {
		return "", nil
	}
	return t.password.Value()
}
