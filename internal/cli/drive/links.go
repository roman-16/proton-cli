package drive

import (
	"strconv"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	drivesvc "github.com/roman-16/proton-cli/internal/service/drive"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

// A public link is a thing, so it is a collection.
//
// It is made, read back, listed and revoked, which is four verbs every other
// collection already has. Drive keeps one link per item, so `create` on an item
// that has one changes that one - which is what keeps a URL already sent from
// going dead the moment somebody adjusts its expiry.
//
// A link is addressed the way everything else in Drive is: by the path of the
// item it opens.

func linksCmd() *cobra.Command {
	c := &cobra.Command{Use: "links", Short: "Links that open a file or folder for anyone"}
	c.AddCommand(linksCreateCmd(), linksGetCmd(), linksListCmd(), linksRevokeCmd())
	return c
}

func linkFields(l *drivesvc.ShareLink) []ui.Field {
	return []ui.Field{
		{Label: "URL", Value: l.URL},
		{Label: "Access", Value: drivesvc.Access(l.CanEdit)},
		{Label: "Expires", Value: expiry(l.ExpireTime), Always: true},
		{Label: "Opened", Value: ui.Quantity(l.NumAccesses, "times")},
		{Label: "Password", Value: l.CustomPassword},
	}
}

func linksCreateCmd() *cobra.Command {
	access := kit.Viewing()
	var expires string
	var t tree
	password := kit.LinkPassword()
	c := &cobra.Command{
		Use:   "create PATH",
		Short: "Make a link that opens a file or folder for anyone",
		Long: "Make a link that opens a file or folder for anyone.\n\n" +
			"An item carries one link, so running it again changes that link rather than\n" +
			"making a second one, and a URL you have already shared keeps working.\n\n" +
			"The password is read from a file, never from a flag value, and may be at most\n" +
			"50 characters. --clear-link-password takes it off again, and --expires never\n" +
			"makes an expiring link permanent.",
		RunE: kit.Run([]kit.Step{password.Supply}, func(c *kit.Invocation) error {
			opts := drivesvc.LinkOptions{}
			if kit.Changed(c.Cmd) {
				edit, err := kit.CanEdit(access)
				if err != nil {
					return err
				}
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
				Action: ui.Created, Kind: "links", Count: 1,
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
			return kit.Show(c, ui.RecordSpec{Object: link, Fields: linkFields(link)})
		}),
	}
	access.Register(c)
	c.Flags().StringVar(&expires, "expires", "",
		"Stop working after DURATION (e.g. 7d, 2w, 6mo), or never")
	password.Declare(c)
	t.register(c, manages)
	return c
}

func linksGetCmd() *cobra.Command {
	var t tree
	c := &cobra.Command{
		Use:   "get PATH",
		Short: "Show the link on a file or folder, URL and all",
		Long: "Show the link on a file or folder, URL and all.\n\n" +
			"Use it to recover a URL you mislaid, rather than revoking the link and making\n" +
			"a new one. The URL appears here and in no listing.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			dc, err := t.context(c)
			if err != nil {
				return err
			}
			st, err := c.App.Drive.ShareStatusOf(c.Ctx, dc, c.Args[0])
			if err != nil {
				return err
			}
			if len(st.Links) == 0 {
				return kit.Fail("%s has no public link.", st.Path).
					Hint(kit.Program + " drive links create " + c.Args[0])
			}
			link := st.Links[0]
			fields := append([]ui.Field{
				{Label: "Path", Value: st.Path},
				{Label: "Type", Value: st.Type},
			}, linkFields(&link)...)
			return kit.Show(c, ui.RecordSpec{Object: link, Fields: fields})
		}),
	}
	t.register(c, manages)
	return c
}

func linksListCmd() *cobra.Command {
	var page kit.Page
	var order kit.Order
	c := &cobra.Command{
		Use:   "list",
		Short: "List the links you have made",
		Long: "List the links you have made.\n\n" +
			"The URLs are not shown: each one opens its item for anybody holding it. To\n" +
			"read a URL, use `links get`.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			links, err := c.App.Drive.LinksMade(c.Ctx)
			if err != nil {
				return err
			}
			if err := kit.Sort(order, links, kit.Comparators[drivesvc.PublicLink]{
				"name":    func(a, b drivesvc.PublicLink) int { return kit.Fold(a.Name, b.Name) },
				"created": func(a, b drivesvc.PublicLink) int { return kit.Ints(a.CreateTime, b.CreateTime) },
				"opened": func(a, b drivesvc.PublicLink) int {
					return kit.Ints(int64(a.NumAccesses), int64(b.NumAccesses))
				},
			}); err != nil {
				return err
			}
			rows, total := kit.Slice(page, links)
			return kit.List(c, ui.TableSpec[drivesvc.PublicLink]{
				Noun: "links", Total: total, Page: page.Number, PageSize: page.Size,
				Columns: []ui.Column[drivesvc.PublicLink]{
					{Header: "ID", ID: true, Cell: drivesvc.PublicLink.Ref},
					{Header: "NAME", Flex: true, Handle: true, Cell: func(l drivesvc.PublicLink) string { return l.Name }},
					{Header: "ACCESS", Cell: func(l drivesvc.PublicLink) string { return drivesvc.Access(l.CanEdit) }},
					{Header: "PASSWORD", Cell: func(l drivesvc.PublicLink) string { return yesNo(l.CustomPassword != "") }},
					{Header: "EXPIRES", Cell: func(l drivesvc.PublicLink) string { return expiry(l.ExpireTime) }},
					{Header: "OPENED", Right: true, Cell: func(l drivesvc.PublicLink) string {
						return strconv.Itoa(l.NumAccesses)
					}},
				},
			}, rows)
		}),
	}
	page.Register(c, "links")
	order.Register(c, "name", "created", "opened")
	return c
}

func linksRevokeCmd() *cobra.Command {
	var t tree
	c := &cobra.Command{
		Use:   "revoke PATH",
		Short: "Stop the link on a file or folder working",
		Long: "Stop the link on a file or folder working.\n\n" +
			"The item is untouched; only the link stops working. This cannot take back\n" +
			"what somebody already read.",
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
				Action: ui.Revoked, Kind: "links", Count: n,
				Detail: "for " + c.Args[0],
			}, func() error {
				_, err := c.App.Drive.RemoveLinks(c.Ctx, dc, c.Args[0])
				return err
			})
		}),
	}
	t.register(c, manages)
	return c
}
