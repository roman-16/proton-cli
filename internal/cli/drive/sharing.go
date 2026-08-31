package drive

import (
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
	return &cobra.Command{
		Use:   "get PATH",
		Short: "Show how a file or folder is shared",
		Args:  cobra.ExactArgs(1),
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			dc, err := context(c)
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
					ui.Field{Label: "Link Access", Value: access(l.CanEdit)},
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
}

func access(canEdit bool) string {
	if canEdit {
		return "edit"
	}
	return "view"
}

func expiry(at *int64) string {
	if at == nil {
		return "never"
	}
	return units.Time(*at)
}

func shareLinkCmd() *cobra.Command {
	var edit bool
	var expires, password string
	c := &cobra.Command{
		Use:   "link PATH",
		Short: "Create or update the public link for a file or folder",
		Long: "Create or update the public link for a file or folder.\n\n" +
			"Running it again with different options changes the existing link rather than\n" +
			"making a second one, so the URL you have shared keeps working.",
		Args: cobra.ExactArgs(1),
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			opts := drivesvc.LinkOptions{}
			if c.Changed("edit") {
				opts.SetEdit, opts.CanEdit = true, edit
			}
			if c.Changed("expires") {
				d, err := units.ParseDuration(expires)
				if err != nil {
					return kit.Fail("--expires: %v", err)
				}
				opts.SetExpiry, opts.ExpireSeconds = true, int(d.Seconds())
			}
			if c.Changed("password") {
				opts.SetPassword, opts.CustomPassword = true, password
			}
			dc, err := context(c)
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
					{Label: "Access", Value: access(link.CanEdit)},
					{Label: "Expires", Value: expiry(link.ExpireTime), Always: true},
					{Label: "Password", Value: link.CustomPassword},
				},
			})
		}),
	}
	c.Flags().BoolVar(&edit, "edit", false, "Allow editing rather than only viewing")
	c.Flags().StringVar(&expires, "expires", "", "Stop working after DURATION (e.g. 7d, 2w, 6mo)")
	c.Flags().StringVar(&password, "password", "", "Require this password to open the link")
	return c
}

func shareUnlinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unlink PATH",
		Short: "Remove the public links for a file or folder",
		Args:  cobra.ExactArgs(1),
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			dc, err := context(c)
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
}

func shareAddCmd() *cobra.Command {
	var edit bool
	var message string
	c := &cobra.Command{
		Use:   "add PATH EMAIL",
		Short: "Invite someone to a file or folder",
		Args:  cobra.ExactArgs(2),
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			dc, err := context(c)
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
	return c
}

// Changing what somebody may do is `update`, the same verb every other
// collection uses for changing a field. Re-running `add` would read as inviting
// them twice.
func shareUpdateCmd() *cobra.Command {
	var edit bool
	c := &cobra.Command{
		Use:   "update PATH EMAIL",
		Short: "Change what somebody may do with a file or folder",
		Long: "Change what somebody may do with a file or folder.\n\n" +
			"It applies to whoever holds the address, whether they have accepted yet or\n" +
			"not: Proton keeps members and invitations apart, but the question is the\n" +
			"same one either way.",
		Args: cobra.ExactArgs(2),
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if !c.Changed("edit") {
				return kit.Fail("Nothing to change.").
					Hint("--edit to allow editing, or --edit=false to restrict to viewing.")
			}
			dc, err := context(c)
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated.WithConsent(), Count: 1, Name: c.Args[1],
				Detail: "to " + access(edit) + " on " + c.Args[0],
			}, func() error {
				return c.App.Drive.SetMemberRole(c.Ctx, dc, c.Args[0], c.Args[1], edit)
			})
		}),
	}
	c.Flags().BoolVar(&edit, "edit", false, "Allow editing rather than only viewing")
	return c
}

// An invitation nobody answered is usually one nobody saw, so it can be sent
// again rather than cancelled and remade.
func shareResendCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resend PATH EMAIL",
		Short: "Send an unanswered invitation again",
		Args:  cobra.ExactArgs(2),
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			dc, err := context(c)
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
}

func shareRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove PATH EMAIL",
		Short: "Revoke someone's access, or cancel their invitation",
		Args:  cobra.ExactArgs(2),
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			dc, err := context(c)
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Removed.WithConsent(), Count: 1, Name: c.Args[1],
				Detail: "from " + c.Args[0],
			}, func() error {
				return c.App.Drive.RemoveMember(c.Ctx, dc, c.Args[0], c.Args[1])
			})
		}),
	}
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
		Args:  cobra.NoArgs,
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
			}, invitations, func(i drivesvc.Invitation) []string { return []string{i.InvitationID} })
		}),
	}
}

func invitationVerb(use, short string, action ui.Action) *cobra.Command {
	return &cobra.Command{
		Use:   use + " REF...",
		Short: short,
		Args:  cobra.MinimumNArgs(1),
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
		{Header: "SIZE", Right: true, Cell: func(e drivesvc.TrashEntry) string { return units.Size(e.Size) }},
		{Header: "TRASHED", Cell: func(e drivesvc.TrashEntry) string { return units.Time(e.Trashed) }},
	}
}

func trashListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List what is in the trash",
		Args:  cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			dc, err := context(c)
			if err != nil {
				return err
			}
			entries, err := c.App.Drive.TrashList(c.Ctx, dc)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[drivesvc.TrashEntry]{
				Noun: "items", Columns: trashColumns(),
				Total: ui.Unknown, Page: ui.Unpaged,
			}, entries, func(e drivesvc.TrashEntry) []string { return []string{e.LinkID} })
		}),
	}
}

func trashRestoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restore REF...",
		Short: "Put items back where they came from",
		Long: "Put items back where they came from.\n\n" +
			"A trashed item has no path any more, so it is named by the ID that\n" +
			"`trash list` shows.",
		Args: cobra.MinimumNArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			dc, err := context(c)
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Restored, Kind: "items", Count: len(c.Args), IDs: c.Args,
			}, func() error {
				return c.App.Drive.TrashRestore(c.Ctx, dc, c.Args)
			})
		}),
	}
}

func trashEmptyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "empty",
		Short: "Delete everything in the trash, permanently",
		Args:  cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			dc, err := context(c)
			if err != nil {
				return err
			}
			entries, err := c.App.Drive.TrashList(c.Ctx, dc)
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Emptied, Kind: "items", Count: len(entries),
				Detail:  "from the trash",
				Preview: kit.Preview("items", trashColumns(), entries),
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
		{Header: "ID", ID: true, Cell: func(i drivesvc.SharedItem) string { return i.LinkID }},
		{Header: "TYPE", Cell: func(i drivesvc.SharedItem) string { return i.Type }},
		{Header: "NAME", Flex: true, Cell: func(i drivesvc.SharedItem) string { return i.Name }},
		{Header: "SIZE", Right: true, Cell: func(i drivesvc.SharedItem) string {
			if i.Size == 0 {
				return ""
			}
			return units.Size(i.Size)
		}},
	}
}

func sharedCmd() *cobra.Command {
	c := &cobra.Command{Use: "shared", Short: "Files and folders other people have shared with you"}
	c.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List what other people have shared with you",
		Long: "List what other people have shared with you.\n\n" +
			"These do not live in your tree, so they have no path: they are addressed by\n" +
			"the ID this listing shows, the way trashed items and photos are.\n\n" +
			"An item whose name cannot be read is still listed, because knowing it is\n" +
			"there is what lets you act on it.",
		Args: cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			items, err := c.App.Drive.SharedWithMe(c.Ctx)
			if err != nil {
				return err
			}
			cols := append(sharedItemColumns(), ui.Column[drivesvc.SharedItem]{
				Header: "SHARED BY", Flex: true,
				Cell: func(i drivesvc.SharedItem) string { return i.SharedBy },
			})
			return kit.List(c, ui.TableSpec[drivesvc.SharedItem]{
				Noun: "items", Columns: cols,
				Total: ui.Unknown, Page: ui.Unpaged,
			}, items, func(i drivesvc.SharedItem) []string { return []string{i.LinkID, i.ShareID} })
		}),
	})
	return c
}

func sharingCmd() *cobra.Command {
	c := &cobra.Command{Use: "sharing", Short: "What you have shared with other people"}
	c.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List what you have shared",
		Long: "List what you have shared, by link or with named people.\n\n" +
			"`items share get PATH` answers the question for one item; this answers the\n" +
			"one you actually have, which is what have I left open.",
		Args: cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			items, err := c.App.Drive.SharedByMe(c.Ctx)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[drivesvc.SharedItem]{
				Noun: "items", Columns: sharedItemColumns(),
				Total: ui.Unknown, Page: ui.Unpaged,
			}, items, func(i drivesvc.SharedItem) []string { return []string{i.LinkID, i.ShareID} })
		}),
	})
	return c
}
