package drive

import (
	stdctx "context"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	drivesvc "github.com/roman-16/proton-cli/internal/service/drive"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

// Sharing has two mechanisms and therefore two verb pairs: link and unlink for a
// public URL, add and remove for named people. `get` reports both at once, which
// is the question a user actually has about a file.

func shareCmd() *cobra.Command {
	c := &cobra.Command{Use: "share", Short: "Public links and the people you share with"}
	c.AddCommand(shareGetCmd(), shareLinkCmd(), shareUnlinkCmd(), shareAddCmd(),
		shareUpdateCmd(), shareResendCmd(), shareRemoveCmd())
	return c
}

func shareGetCmd() *cobra.Command {
	var t tree
	c := &cobra.Command{
		Use:   "get PATH",
		Short: "Show how a file or folder is shared",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			dc, err := t.context(c)
			if err != nil {
				return err
			}
			st, err := c.App.Drive.ShareStatusOf(c.Ctx, dc, c.Args[0])
			if err != nil {
				return err
			}
			fields := []ui.Field{
				{Label: "Path", Value: st.Path},
				{Label: "Type", Value: st.Type},
			}
			for _, l := range st.Links {
				fields = append(fields, ui.Field{Label: "Public Link", Value: l.URL})
				fields = append(fields,
					ui.Field{Label: "Link Access", Value: drivesvc.Access(l.CanEdit)},
					ui.Field{Label: "Link Expires", Value: expiry(l.ExpireTime), Always: true},
					ui.Field{Label: "Link Opened", Value: ui.Quantity(l.NumAccesses, "times")},
				)
				if l.CustomPassword != "" {
					fields = append(fields, ui.Field{Label: "Link Password", Value: l.CustomPassword})
				}
			}
			for _, m := range st.Members {
				fields = append(fields, ui.Field{Label: "Member", Value: m.Email + " (" + m.Role + ")"})
			}
			for _, p := range st.Invitees {
				fields = append(fields, ui.Field{
					Label: "Invited", Value: p.Email + " (" + p.Role + ", not yet accepted)",
				})
			}
			if len(st.Links) == 0 && len(st.Members) == 0 && len(st.Invitees) == 0 {
				fields = append(fields, ui.Field{Label: "Shared", Value: "no", Always: true})
			}
			return kit.Show(c, ui.RecordSpec{Object: st, Fields: fields})
		}),
	}
	t.register(c, changes)
	return c
}

func expiry(at *int64) string {
	if at == nil {
		return "never"
	}
	return units.Time(*at)
}

func shareLinkCmd() *cobra.Command {
	var edit bool
	var expires string
	var t tree
	password := kit.LinkPassword()
	c := &cobra.Command{
		Use:   "link PATH",
		Short: "Create or update the public link for a file or folder",
		Long: "Create or update the public link for a file or folder.\n\n" +
			"Running it again changes the existing link rather than making a second one,\n" +
			"so a URL you have already shared keeps working.\n\n" +
			"The password is read from a file or from stdin, never from a flag value, and\n" +
			"may be at most 50 characters. --clear-link-password takes it off again, and\n" +
			"--expires never makes an expiring link permanent.",
		RunE: kit.Run([]kit.Step{password.Supply}, func(c *kit.Invocation) error {
			opts := drivesvc.LinkOptions{}
			if c.Changed("edit") {
				opts.SetEdit, opts.CanEdit = true, edit
			}
			if c.Changed("expires") {
				d, err := kit.Expires(expires)
				if err != nil {
					return err
				}
				opts.SetExpiry, opts.ExpireSeconds = true, int(d.Seconds())
			}
			if password.Wanted() {
				custom, err := password.Value()
				if err != nil {
					return err
				}
				opts.SetPassword, opts.CustomPassword = true, custom
			}
			dc, err := t.context(c)
			if err != nil {
				return err
			}
			var link *drivesvc.ShareLink
			if err := kit.Mutate(c, ui.ResultSpec{
				Action: ui.Linked, Kind: "links", Count: 1,
				Detail: "for " + c.Args[0], AnswerFollows: true,
			}, func() error {
				var err error
				link, err = c.App.Drive.EnsureLink(c.Ctx, dc, c.Args[0], opts)
				return err
			}); err != nil {
				return err
			}
			if link == nil || c.App.DryRun {
				return nil
			}
			// The URL is the answer, so it goes to stdout on its own line: the
			// point of this command is to be able to capture it.
			return kit.Show(c, ui.RecordSpec{
				Object: link,
				Fields: []ui.Field{
					{Label: "URL", Value: link.URL},
					{Label: "Access", Value: drivesvc.Access(link.CanEdit)},
					{Label: "Expires", Value: expiry(link.ExpireTime), Always: true},
					{Label: "Password", Value: link.CustomPassword},
				},
			})
		}),
	}
	c.Flags().BoolVar(&edit, "edit", false, "Allow editing rather than only viewing")
	c.Flags().StringVar(&expires, "expires", "",
		"Stop working after DURATION (e.g. 7d, 2w, 6mo), or never")
	password.Declare(c)
	t.register(c, changes)
	return c
}

func shareUnlinkCmd() *cobra.Command {
	var t tree
	c := &cobra.Command{
		Use:   "unlink PATH",
		Short: "Remove the public links for a file or folder",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			dc, err := t.context(c)
			if err != nil {
				return err
			}
			n, err := c.App.Drive.CountLinks(c.Ctx, dc, c.Args[0])
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Unlinked, Kind: "links", Count: n,
				Detail: "from " + c.Args[0],
			}, func() error {
				_, err := c.App.Drive.RemoveLinks(c.Ctx, dc, c.Args[0])
				return err
			})
		}),
	}
	t.register(c, changes)
	return c
}

func shareAddCmd() *cobra.Command {
	var edit bool
	var message string
	var t tree
	c := &cobra.Command{
		Use:   "add PATH EMAIL",
		Short: "Invite someone to a file or folder",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			dc, err := t.context(c)
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Invited, Count: 1, Name: c.Args[1],
				Detail: "to " + c.Args[0],
			}, func() error {
				return c.App.Drive.InviteMember(c.Ctx, dc, c.Args[0], c.Args[1], edit, message)
			})
		}),
	}
	c.Flags().BoolVar(&edit, "edit", false, "Allow editing rather than only viewing")
	c.Flags().StringVar(&message, "message", "", "Note to include in the invitation email")
	t.register(c, changes)
	return c
}

// Changing what somebody may do is `update`, the same verb every other
// collection uses for changing a field. Re-running `add` would read as inviting
// them twice.
func shareUpdateCmd() *cobra.Command {
	var edit bool
	var t tree
	c := &cobra.Command{
		Use:   "update PATH EMAIL",
		Short: "Change what somebody may do with a file or folder",
		Long: "Change what somebody may do with a file or folder.\n\n" +
			"Name them by address. It works whether they have accepted the share or\n" +
			"still have it pending.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if !c.Changed("edit") {
				return kit.Fail("Nothing to change.").
					Hint("--edit to allow editing, or --edit=false to restrict to viewing.")
			}
			dc, err := t.context(c)
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Count: 1, Name: c.Args[1],
				Detail: "to " + drivesvc.Access(edit) + " on " + c.Args[0],
			}, func() error {
				return c.App.Drive.SetMemberRole(c.Ctx, dc, c.Args[0], c.Args[1], edit)
			})
		}),
	}
	c.Flags().BoolVar(&edit, "edit", false, "Allow editing rather than only viewing")
	t.register(c, changes)
	return c
}

// An invitation nobody answered is usually one nobody saw, so it can be sent
// again rather than cancelled and remade.
func shareResendCmd() *cobra.Command {
	var t tree
	c := &cobra.Command{
		Use:   "resend PATH EMAIL",
		Short: "Send an unanswered invitation again",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			dc, err := t.context(c)
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Resent, Kind: "invitations", Count: 1, Name: c.Args[1],
				Detail: "for " + c.Args[0],
			}, func() error {
				return c.App.Drive.ResendInvite(c.Ctx, dc, c.Args[0], c.Args[1])
			})
		}),
	}
	t.register(c, changes)
	return c
}

func shareRemoveCmd() *cobra.Command {
	var t tree
	c := &cobra.Command{
		Use:   "remove PATH EMAIL",
		Short: "Revoke someone's access, or cancel their invitation",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			dc, err := t.context(c)
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Removed, Count: 1, Name: c.Args[1],
				Detail: "from " + c.Args[0],
			}, func() error {
				return c.App.Drive.RemoveMember(c.Ctx, dc, c.Args[0], c.Args[1])
			})
		}),
	}
	t.register(c, changes)
	return c
}

// ── invitations sent to you ──

func invitationsCmd() *cobra.Command {
	c := &cobra.Command{Use: "invitations", Short: "Shares other people have offered you"}
	c.AddCommand(
		invitationsListCmd(),
		invitationVerb("accept", "Accept invitations", ui.Accepted),
		// Proton's own word is Decline, so that is the word here.
		invitationVerb("decline", "Decline invitations", ui.Declined),
	)
	return c
}

func invitationsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List invitations waiting for an answer",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			invitations, err := c.App.Drive.ListInvitations(c.Ctx)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[drivesvc.Invitation]{
				Noun:  "invitations",
				Total: ui.Unknown, Page: ui.Unpaged,
				Columns: []ui.Column[drivesvc.Invitation]{
					{Header: "ID", ID: true, Cell: func(i drivesvc.Invitation) string { return i.InvitationID }},
					{Header: "FROM", Flex: true, Cell: func(i drivesvc.Invitation) string { return i.InviterEmail }},
					{Header: "ROLE", Cell: func(i drivesvc.Invitation) string { return i.Role }},
					{Header: "CREATED", Cell: func(i drivesvc.Invitation) string { return units.Time(i.CreateTime) }},
				},
			}, invitations)
		}),
	}
}

func invitationVerb(use, short string, action ui.Action) *cobra.Command {
	return &cobra.Command{
		Use:   use + " REF...",
		Short: short,
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			return kit.Mutate(c, ui.ResultSpec{
				Action: action, Kind: "invitations", Count: len(c.Args), IDs: c.Args,
			}, func() error {
				for _, id := range c.Args {
					var err error
					if use == "accept" {
						err = c.App.Drive.AcceptInvitation(c.Ctx, id)
					} else {
						err = c.App.Drive.RejectInvitation(c.Ctx, id)
					}
					if err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
}

// ── trash ──

func trashCmd() *cobra.Command {
	c := &cobra.Command{Use: "trash", Short: "Items you have removed but not yet deleted"}
	c.AddCommand(trashListCmd(), trashRestoreCmd(), trashEmptyCmd())
	return c
}

func trashColumns() []ui.Column[drivesvc.TrashEntry] {
	return []ui.Column[drivesvc.TrashEntry]{
		{Header: "ID", ID: true, Cell: func(e drivesvc.TrashEntry) string { return e.LinkID }},
		{Header: "TYPE", Cell: func(e drivesvc.TrashEntry) string { return e.Type }},
		{Header: "SIZE", Right: true, Cell: func(e drivesvc.TrashEntry) string {
			if e.Type == drivesvc.TypeFolder {
				return ""
			}
			return units.Size(e.Size)
		}},
		{Header: "TRASHED", Cell: func(e drivesvc.TrashEntry) string { return units.Time(e.Trashed) }},
		{Header: "NAME", Flex: true, Cell: func(e drivesvc.TrashEntry) string { return e.Name }},
	}
}

// trashOrder is how the trash may be ordered. The whole of it is held here to be
// counted before anything is shown, so ordering it is this process's to do, the
// way it is for a folder's children.
func trashOrder() kit.Comparators[drivesvc.TrashEntry] {
	return kit.Comparators[drivesvc.TrashEntry]{
		"name":    func(a, b drivesvc.TrashEntry) int { return kit.Fold(a.Name, b.Name) },
		"size":    func(a, b drivesvc.TrashEntry) int { return kit.Ints(a.Size, b.Size) },
		"trashed": func(a, b drivesvc.TrashEntry) int { return kit.Ints(a.Trashed, b.Trashed) },
	}
}

func trashListCmd() *cobra.Command {
	var page kit.Page
	var order kit.Order
	c := &cobra.Command{
		Use:   "list",
		Short: "List what is in the trash",
		Long: "List what is in the trash.\n\n" +
			"This covers everything the account has trashed, photos included. `trash\n" +
			"empty` deletes all of it.\n\n" +
			"A trashed item has no path, so address it by the ID shown here. An item\n" +
			"whose name cannot be decrypted is still listed, so you can still act on it.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			dc, err := context(c)
			if err != nil {
				return err
			}
			refs, err := c.App.Drive.TrashRefs(c.Ctx, dc)
			if err != nil {
				return err
			}
			entries, err := c.App.Drive.TrashDescribe(c.Ctx, dc, refs)
			if err != nil {
				return err
			}
			if err := kit.Sort(order, entries, trashOrder()); err != nil {
				return err
			}
			rows, total := kit.Slice(page, entries)
			return kit.List(c, ui.TableSpec[drivesvc.TrashEntry]{
				Noun: "items", Columns: trashColumns(),
				Total: total, Page: page.Number, PageSize: page.Size,
			}, rows)
		}),
	}
	order.Register(c, "name", "size", "trashed")
	page.Register(c, "items")
	return c
}

func trashRestoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restore REF...",
		Short: "Put items back where they came from",
		Long: "Put items back where they came from.\n\n" +
			"A trashed item has no path. Name it by the ID that `trash list` shows.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			dc, err := context(c)
			if err != nil {
				return err
			}
			return kit.Attempt(c, ui.ResultSpec{
				Action: ui.Restored, Kind: "items", Count: len(c.Args), IDs: c.Args,
			}, func() ([]drivesvc.Refused, error) {
				return c.App.Drive.TrashRestore(c.Ctx, dc, c.Args)
			})
		}),
	}
}

func trashEmptyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "empty",
		Short: "Delete everything in the trash, permanently",
		Long: "Delete everything in the trash, permanently.\n\n" +
			"That is everything `trash list` shows, trashed photos included.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			dc, err := context(c)
			if err != nil {
				return err
			}
			// Counting is the whole of what this needs to know, and identity is the
			// cheap half of a listing: emptying is all or nothing, so a table of what
			// is in there answers a question only `trash list` is asked.
			refs, err := c.App.Drive.TrashRefs(c.Ctx, dc)
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Emptied, Kind: "items", Count: len(refs),
				Detail: "from the trash",
			}, func() error { return c.App.Drive.TrashEmpty(c.Ctx, dc) })
		}),
	}
}

// ── shares, in both directions ──

// Sharing has two directions and this CLI now names both. `items share` is what
// you do to something of yours; `shared` is what other people have done to
// something of theirs, and `sharing` is the standing answer to "what have I left
// open".

func sharedItemColumns() []ui.Column[drivesvc.SharedItem] {
	return []ui.Column[drivesvc.SharedItem]{
		{Header: "ID", ID: true, Cell: drivesvc.SharedItem.Ref},
		{Header: "TYPE", Cell: func(i drivesvc.SharedItem) string { return i.Type }},
		{Header: "NAME", Flex: true, Handle: true, Cell: func(i drivesvc.SharedItem) string { return i.Name }},
		{Header: "SIZE", Right: true, Cell: func(i drivesvc.SharedItem) string {
			if i.Size == 0 {
				return ""
			}
			return units.Size(i.Size)
		}},
	}
}

// sharedList is the collection --shared names: what other people have granted
// you, each of which is the top of a tree of its own.
func sharedList(c *kit.Invocation) *kit.Lookup[drivesvc.SharedItem] {
	return &kit.Lookup[drivesvc.SharedItem]{
		Kind:   "shared item",
		Load:   func(ctx stdctx.Context) ([]drivesvc.SharedItem, error) { return c.App.Drive.SharedWithMe(ctx) },
		ID:     drivesvc.SharedItem.Ref,
		Handle: func(i drivesvc.SharedItem) string { return i.Name },
	}
}

// sharedName is what to call a shared item in a sentence: its own name, or the
// ID it is addressable by when the name would not decrypt.
func sharedName(i drivesvc.SharedItem) string {
	if i.Name != "" {
		return i.Name
	}
	return i.Ref()
}

func sharedCmd() *cobra.Command {
	c := &cobra.Command{Use: "shared", Short: "Files and folders other people have shared with you"}
	c.AddCommand(sharedListCmd(), sharedAddCmd(), sharedRemoveCmd(), sharedLeaveCmd())
	return c
}

func sharedListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List what other people have shared with you",
		Long: "List what other people have shared with you.\n\n" +
			"Items shared with you directly and public links you saved with `shared add`\n" +
			"are listed together. A saved link shows `public link` under SHARED BY, and\n" +
			"ROLE is what you may do: a viewer lists and downloads, an editor uploads as\n" +
			"well.\n\n" +
			"These are not in your tree and have no path of their own. To open one, pass\n" +
			"`--shared REF` to any `items` command: / is then the item itself, and\n" +
			"anything below it is a path inside it. Nothing in a saved link can be renamed,\n" +
			"moved or removed from here.\n\n" +
			"An item whose name cannot be decrypted is still listed and can be acted on\n" +
			"by ID.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			items, err := sharedList(c).Rows(c.Ctx)
			if err != nil {
				return err
			}
			cols := append(sharedItemColumns(),
				ui.Column[drivesvc.SharedItem]{Header: "SHARED BY", Flex: true, Cell: sharedBy},
				ui.Column[drivesvc.SharedItem]{
					Header: "ROLE",
					Cell:   func(i drivesvc.SharedItem) string { return i.Role },
				})
			return kit.List(c, ui.TableSpec[drivesvc.SharedItem]{
				Noun: "items", Columns: cols,
				Total: ui.Unknown, Page: ui.Unpaged,
			}, items)
		}),
	}
}

// sharedBy says where an item came from. A link came from a URL and says so:
// nobody's address is attached to one, and the person who sent it is not
// something Proton knows.
func sharedBy(i drivesvc.SharedItem) string {
	if i.IsLink() {
		return "public link"
	}
	return i.SharedBy
}

func sharedAddCmd() *cobra.Command {
	password := kit.LinkPasswordToOpen()
	c := &cobra.Command{
		Use:   "add URL",
		Short: "Add a public link to what is shared with you",
		Long: "Add a public link to what is shared with you.\n\n" +
			"URL is the link as it was sent to you, including everything after the #.\n" +
			"Once added it appears in `shared list` and opens with `--shared REF`, with\n" +
			"nothing to pass again. A link with a password takes it from\n" +
			"--link-password-file or --link-password-stdin and keeps it.",
		RunE: kit.Run([]kit.Step{password.Supply}, func(c *kit.Invocation) error {
			custom := ""
			if password.Wanted() {
				var err error
				if custom, err = password.Value(); err != nil {
					return err
				}
			}
			// The link is opened before it is saved, which is what makes the answer
			// name what was added rather than repeat the URL back, and what keeps a
			// link nobody can open out of the listing.
			dc, err := c.App.Drive.OpenLink(c.Ctx, c.Args[0], custom)
			if err != nil {
				return err
			}
			return kit.Create(c, ui.ResultSpec{
				Action: ui.Added, Kind: "shared items", Name: dc.RootName,
			}, func() (string, error) {
				return c.App.Drive.SaveLink(c.Ctx, dc)
			})
		}),
	}
	password.Declare(c)
	return c
}

func sharedRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove REF...",
		Short: "Forget a public link you saved",
		Long: "Forget a public link you saved.\n\n" +
			"The link itself keeps working, and `shared add` brings it back. To give up\n" +
			"an item somebody shared with you directly, use `shared leave`.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			items, err := findShared(c, drivesvc.SharedItem.IsLink,
				"%q was shared with you directly, so there is no link to forget.",
				"`"+kit.Program+" drive shared leave` gives up an item somebody shared with you.")
			if err != nil {
				return err
			}
			return kit.Mutate(c, sharedSpec(ui.Removed, items), func() error {
				for _, item := range items {
					if err := c.App.Drive.ForgetLink(c.Ctx, item); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
}

func sharedLeaveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "leave REF...",
		Short: "Give up an item somebody shared with you",
		Long: "Give up an item somebody shared with you.\n\n" +
			"Only a new invitation from whoever shared it brings it back, so this asks\n" +
			"first. To take a public link you saved out of the listing, use\n" +
			"`shared remove`.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			items, err := findShared(c, func(i drivesvc.SharedItem) bool { return !i.IsLink() },
				"%q is a public link somebody sent you, so there is no share to leave.",
				"`"+kit.Program+" drive shared remove` forgets a link you saved.")
			if err != nil {
				return err
			}
			return kit.Mutate(c, sharedSpec(ui.Left, items), func() error {
				for _, item := range items {
					if err := c.App.Drive.LeaveShare(c.Ctx, item); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
}

// findShared resolves every REF to something in the shared listing, and refuses
// the kind this command is not about.
//
// The two kinds sit in one listing and are removed by different verbs, so the
// refusal names the other one: a reference that matched is not a reference that
// was wrong.
func findShared(c *kit.Invocation, wanted func(drivesvc.SharedItem) bool, refusal, hint string) ([]drivesvc.SharedItem, error) {
	lookup := sharedList(c)
	items := make([]drivesvc.SharedItem, 0, len(c.Args))
	for _, ref := range c.Args {
		item, err := lookup.Find(c.Ctx, ref)
		if err != nil {
			return nil, err
		}
		if !wanted(item) {
			return nil, kit.Fail(refusal, sharedName(item)).Hint(hint)
		}
		items = append(items, item)
	}
	return items, nil
}

// sharedSpec reports what a removal from the shared listing is about to touch.
func sharedSpec(action ui.Action, items []drivesvc.SharedItem) ui.ResultSpec {
	spec := ui.ResultSpec{Action: action, Kind: "shared items", Count: len(items)}
	for _, item := range items {
		spec.IDs = append(spec.IDs, item.Ref())
	}
	if len(items) == 1 {
		spec.Name = sharedName(items[0])
	}
	return spec
}

func sharingCmd() *cobra.Command {
	c := &cobra.Command{Use: "sharing", Short: "What you have shared with other people"}
	c.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List what you have shared",
		Long: "List everything you have shared, by public link or with named people.\n\n" +
			"To check a single item instead, run `items share get PATH`.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			items, err := c.App.Drive.SharedByMe(c.Ctx)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[drivesvc.SharedItem]{
				Noun: "items", Columns: sharedItemColumns(),
				Total: ui.Unknown, Page: ui.Unpaged,
			}, items)
		}),
	})
	return c
}
