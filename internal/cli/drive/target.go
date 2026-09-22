package drive

import (
	"strings"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/errs"
	drivesvc "github.com/roman-16/proton-cli/internal/service/drive"
	"github.com/spf13/cobra"
)

// Drive shares a file, a photo and an album by the same mechanisms, and names
// each of them differently: a file or folder by its path in a tree, a photo and
// an album by the reference their listing showed.
//
// That difference lives here, so the sharing commands themselves are written
// once and registered under each collection that has something to share.

// addressing turns what somebody typed into the node a share goes on.
//
// One is made per command, because the flags that say which tree a path is in
// belong to the command that reads them.
type addressing interface {
	register(*cobra.Command)
	resolve(*kit.Invocation, string) (drivesvc.Target, error)
}

// inTree addresses a file or folder by its path, in whichever tree the flags
// name: your own files, a computer's, or something somebody shared with you.
type inTree struct{ t tree }

func (a *inTree) register(c *cobra.Command) { a.t.register(c, manages) }

func (a *inTree) resolve(c *kit.Invocation, ref string) (drivesvc.Target, error) {
	dc, err := a.t.context(c)
	if err != nil {
		return drivesvc.Target{}, err
	}
	node, err := c.App.Drive.ResolvePath(c.Ctx, dc, ref)
	if err != nil {
		return drivesvc.Target{}, err
	}
	path := "/" + strings.Trim(ref, "/")
	return drivesvc.Target{
		Node: node, Ref: path,
		Sharing: "drive items share", Linking: "drive links",
	}, nil
}

// anAlbum addresses a photo album by its name or its ID.
//
// An album has no public link: Proton shares one with people and offers no URL
// for it, so nothing here names a command that would make one.
type anAlbum struct{}

func (anAlbum) register(*cobra.Command) {}

func (anAlbum) resolve(c *kit.Invocation, ref string) (drivesvc.Target, error) {
	dc, err := photosContext(c)
	if err != nil {
		return drivesvc.Target{}, err
	}
	expanded, err := kit.Expand(c.App, ref)
	if err != nil {
		return drivesvc.Target{}, err
	}
	album, err := albumList(c, dc).Find(c.Ctx, expanded)
	if err != nil {
		return drivesvc.Target{}, err
	}
	node, err := c.App.Drive.ResolveRef(c.Ctx, dc, album.LinkID)
	if err != nil {
		return drivesvc.Target{}, err
	}
	name := album.Name
	if name == "" {
		name = album.LinkID
	}
	return drivesvc.Target{Node: node, Ref: name, Sharing: "drive photos albums share"}, nil
}

// aPhoto addresses a photo by the ID its listing showed.
//
// An album is refused rather than acted on as though it were a photo, because
// albums are a collection of their own. What an album is not depends on what
// was asked of it, so the sentence comes from the command.
type aPhoto struct{ notAPhoto string }

func (aPhoto) register(*cobra.Command) {}

func (a aPhoto) resolve(c *kit.Invocation, ref string) (drivesvc.Target, error) {
	dc, err := photosContext(c)
	if err != nil {
		return drivesvc.Target{}, err
	}
	expanded, err := kit.Expand(c.App, ref)
	if err != nil {
		return drivesvc.Target{}, err
	}
	node, err := c.App.Drive.ResolveRef(c.Ctx, dc, expanded)
	if err != nil {
		return drivesvc.Target{}, err
	}
	if node.IsAlbum() {
		name := node.Name
		if name == "" {
			name = ref
		}
		return drivesvc.Target{}, errs.Naming(name, kit.Fail(a.notAPhoto, name).
			Hint(kit.Program+" drive photos albums share add "+name+" EMAIL"))
	}
	return drivesvc.Target{
		Node: node, Ref: ref,
		Sharing: "drive photos share", Linking: "drive photos links",
	}, nil
}
