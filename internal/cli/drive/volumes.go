package drive

import (
	stdctx "context"
	"errors"
	"fmt"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/errs"
	drivesvc "github.com/roman-16/proton-cli/internal/service/drive"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

// The volumes files and photos are kept on, and what a password reset does to
// them.
//
// Proton's Drive client shows a banner when a reset has locked one, with two
// ways out: restore the files into the volume it made since, or delete them.
// Neither has a home among items, photos or the trash - the locked volume is a
// thing of its own, with a state and a restore under way - so it is a collection
// of its own, with the two verbs the banner offers and a listing to find it by.

func volumesCmd() *cobra.Command {
	c := &cobra.Command{Use: "volumes", Short: "The volumes your files and photos are kept on"}
	c.AddCommand(volumesListCmd(), volumesRestoreCmd(), volumesDeleteCmd())
	return c
}

func volumeColumns() []ui.Column[drivesvc.Volume] {
	return []ui.Column[drivesvc.Volume]{
		{Header: "ID", ID: true, Cell: func(v drivesvc.Volume) string { return v.ID }},
		{Header: "TYPE", Cell: func(v drivesvc.Volume) string { return v.Type }},
		{
			Header: "STATE",
			Role: func(v drivesvc.Volume) ui.Role {
				if v.Locked() {
					return ui.Caution
				}
				return ui.Success
			},
			Cell: func(v drivesvc.Volume) string { return v.State },
		},
		{Header: "USED", Right: true, Cell: func(v drivesvc.Volume) string { return units.Size(v.UsedSpace) }},
		{Header: "RESTORE", Cell: func(v drivesvc.Volume) string { return v.Restore }},
		{Header: "CREATED", Cell: func(v drivesvc.Volume) string { return units.Time(v.Created) }},
	}
}

// volumeList is the collection a volume REF names.
func volumeList(c *kit.Invocation) *kit.Lookup[drivesvc.Volume] {
	return &kit.Lookup[drivesvc.Volume]{
		Kind: "volume",
		Load: func(ctx stdctx.Context) ([]drivesvc.Volume, error) { return c.App.Drive.Volumes(ctx) },
		ID:   func(v drivesvc.Volume) string { return v.ID },
	}
}

// lockedScope is what --all covers, and what a bare command line is told to
// name instead.
const lockedScope = "every locked volume"

// lockedVolumes is the selection a locked-volume verb acts on: the volumes
// named, or with --all every locked one.
//
// An active volume is refused here rather than at Proton, because the command
// line already says which volume it is: restoring or deleting the one your files
// are in is not something to find out from a failed request.
func lockedVolumes(c *kit.Invocation, all bool, verb string) (kit.Selection[drivesvc.Volume], error) {
	list := volumeList(c)
	selector := kit.Selector[drivesvc.Volume]{
		Noun: "volumes", Columns: volumeColumns(),
		IDOf:  func(v drivesvc.Volume) string { return v.ID },
		ByRef: list.Find,
		Scope: lockedScope,
	}
	if all {
		selector.ByFilter = func(ctx stdctx.Context) ([]drivesvc.Volume, error) {
			rows, err := list.Rows(ctx)
			if err != nil {
				return nil, err
			}
			var locked []drivesvc.Volume
			for _, v := range rows {
				if v.Locked() {
					locked = append(locked, v)
				}
			}
			return locked, nil
		}
	}
	sel, err := kit.Select(c, selector)
	if err != nil {
		return sel, err
	}
	for _, v := range sel.Rows {
		if !v.Locked() {
			return sel, kit.Fail("Volume %s is not locked.", v.ID).
				Hint("only a volume a password reset locked can be " + verb)
		}
	}
	return sel, nil
}

func volumesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the volumes your files and photos are kept on",
		Long: "List the volumes your files and photos are kept on.\n\n" +
			"There is one for files and, on newer accounts, one for photos. A password\n" +
			"reset locks a volume and a fresh one takes its place, so a locked volume holds\n" +
			"what you had before the reset. RESTORE says how far `volumes restore` has got\n" +
			"with one.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			volumes, err := volumeList(c).Rows(c.Ctx)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[drivesvc.Volume]{
				Noun: "volumes", Columns: volumeColumns(),
				Total: ui.Unknown, Page: ui.Unpaged,
			}, volumes)
		}),
	}
}

func volumesRestoreCmd() *cobra.Command {
	var all bool
	c := &cobra.Command{
		Use:   "restore [REF...]",
		Short: "Put the files of a locked volume back",
		Long: "Put the files of a locked volume back.\n\n" +
			"The keys the volume was locked with have to be active first: reactivate them\n" +
			"with `" + kit.Program + " account keys reactivate`. Files come back as a folder named\n" +
			"`Restored files` and the date; computers and the photo library stay where they\n" +
			"are. Proton moves the files in its own time, so they appear as it finishes.\n\n" +
			"A locked photo volume is skipped until a Proton client has made a photo\n" +
			"library for it to come back into.",
		RunE: kit.Run([]kit.Step{kit.StepExpand, kit.StepSelection(func() bool { return all }, "", lockedScope)},
			func(c *kit.Invocation) error {
				sel, err := lockedVolumes(c, all, "restored")
				if err != nil {
					return err
				}
				var folders []string
				if err := kit.Attempt(c, ui.ResultSpec{
					Action: ui.Restored, Kind: "volumes", Count: sel.Len(), IDs: sel.IDs,
					Preview: sel.Preview(),
				}, func() ([]notRestored, error) {
					var skipped []notRestored
					for _, v := range sel.Rows {
						into, err := restoreTarget(c, v)
						var missing *errs.NotFound
						if errors.As(err, &missing) {
							skipped = append(skipped, notRestored{v, "has no photo library to come back into; a Proton client makes one"})
							continue
						}
						if err != nil {
							return nil, err
						}
						restored, err := c.App.Drive.Restore(c.Ctx, into, v.ID)
						if err != nil {
							return nil, err
						}
						if restored.Folder != "" {
							folders = append(folders, "/"+restored.Folder)
						}
					}
					return skipped, nil
				}); err != nil {
					return err
				}
				if len(folders) > 0 {
					c.Note("Proton is moving the files into %s; they appear as it finishes.", ui.Listing(folders))
				}
				return nil
			}),
	}
	kit.All(c.Flags(), &all)
	return c
}

// notRestored is a locked volume a restore left as it was, phrased for the
// warning that names it.
type notRestored struct {
	volume drivesvc.Volume
	why    string
}

func (n notRestored) String() string {
	return fmt.Sprintf("Volume %s %s.", n.volume.ID, n.why)
}

// restoreTarget is the tree a locked volume's files come back into: the photo
// library for a photo volume, and your own files for anything else.
func restoreTarget(c *kit.Invocation, v drivesvc.Volume) (*drivesvc.Context, error) {
	if v.Photos() {
		return photosContext(c)
	}
	return context(c)
}

func volumesDeleteCmd() *cobra.Command {
	var all bool
	c := &cobra.Command{
		Use:   "delete [REF...]",
		Short: "Delete a locked volume and everything on it",
		Long: "Delete a locked volume and everything on it.\n\n" +
			"Only a locked volume can be deleted. Proton removes its files within 72 hours\n" +
			"and nothing brings them back afterwards.",
		RunE: kit.Run([]kit.Step{kit.StepExpand, kit.StepSelection(func() bool { return all }, "", lockedScope)},
			func(c *kit.Invocation) error {
				sel, err := lockedVolumes(c, all, "deleted")
				if err != nil {
					return err
				}
				if err := kit.Mutate(c, ui.ResultSpec{
					Action: ui.Deleted, Kind: "volumes", Count: sel.Len(), IDs: sel.IDs,
					Preview: sel.Preview(),
				}, func() error {
					for _, v := range sel.Rows {
						if err := c.App.Drive.DeleteLocked(c.Ctx, v.ID); err != nil {
							return err
						}
					}
					return nil
				}); err != nil {
					return err
				}
				if sel.Len() > 0 && !c.App.DryRun {
					c.Note("Proton removes the files within 72 hours.")
				}
				return nil
			}),
	}
	kit.All(c.Flags(), &all)
	return c
}
