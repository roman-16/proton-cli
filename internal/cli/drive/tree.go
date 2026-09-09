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
type tree struct {
	computer string
	shared   string
	link     string
	password *kit.Password
	relation relation
}

// relation is what a command needs of the tree it is pointed at: to read it, to
// put something new in it, or to reach what is already in it - changing an item,
// or the history and the sharing of one.
//
// A public link answers the first always, the second when it allows editing, and
// the third never: Proton serves a link one version of a file and no way to act
// on what is under it. So this is what decides which trees a command offers and
// which it refuses, rather than each command knowing.
type relation int

const (
	reads relation = iota
	adds
	changes
)

// askForALinkThatEdits is what to do about a link that allows viewing only:
// nobody but its owner can widen it.
const askForALinkThatEdits = "Ask whoever sent it for a link that allows editing."

// register adds the tree flags to a command, by what the command does to a tree.
func (t *tree) register(c *cobra.Command, r relation) {
	t.relation = r
	c.Flags().StringVar(&t.computer, "computer", "", "Work inside this computer's files, by name or ID")
	c.Flags().StringVar(&t.shared, "shared", "", "Work inside an item shared with you, by name or ID")
	if r == changes {
		c.MarkFlagsMutuallyExclusive("computer", "shared")
		return
	}
	t.password = kit.LinkPasswordToOpen()
	c.Flags().StringVar(&t.link, "link", "", "Work inside a public link somebody sent you, by URL")
	c.MarkFlagsMutuallyExclusive("computer", "shared", "link")
	t.password.Declare(c)
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
		if item.IsLink() && t.relation == changes {
			return nil, kit.Fail(
				"%q is a public link somebody sent you, which can only be listed, downloaded and uploaded into.",
				sharedName(item))
		}
		dc, err := c.App.Drive.OpenShared(c.Ctx, item)
		if err != nil {
			return nil, err
		}
		if t.refused(dc) {
			if item.IsLink() {
				return nil, kit.Fail("The link to %q allows viewing only.", sharedName(item)).
					Hint(askForALinkThatEdits)
			}
			return nil, kit.Fail("%q was shared with you for viewing only.", sharedName(item)).
				Hint("Ask whoever shared it to allow editing.")
		}
		return dc, nil
	case t.link != "":
		password, err := t.linkPassword()
		if err != nil {
			return nil, err
		}
		dc, err := c.App.Drive.OpenLink(c.Ctx, t.link, password)
		if err != nil {
			return nil, err
		}
		if t.refused(dc) {
			return nil, kit.Fail("This link allows viewing only.").Hint(askForALinkThatEdits)
		}
		if dc.Anonymous {
			c.AuthorisedByALink()
		}
		return dc, nil
	}
	return context(c)
}

// refused reports whether the tree will not take what the command is about to
// put in it, which is judged as it opens rather than when Proton refuses the
// first request: what the person who sent the link would see is no way to upload
// at all.
func (t *tree) refused(dc *drivesvc.Context) bool {
	return t.relation != reads && !dc.CanEdit()
}

// linkPassword is what the link's owner set on it, which is nothing at all for
// most links.
func (t *tree) linkPassword() (string, error) {
	if t.password == nil || !t.password.Wanted() {
		return "", nil
	}
	return t.password.Value()
}
