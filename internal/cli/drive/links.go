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
// A link is addressed the way the thing it opens is: a file or folder by its
// path, a photo by the ID its listing showed. `links list` is every link the
// account has open, whichever of the two it opens.

func linksCmd() *cobra.Command {
	c := &cobra.Command{Use: "links", Short: "Links that open a file or folder for anyone"}
	c.AddCommand(linksCreateCmd(filesLinked()), linksGetCmd(filesLinked()),
		linksListCmd(), linksRevokeCmd(filesLinked()))
	return c
}

// photosLinksCmd is the same collection for the photo library, where a link
// opens one photo.
//
// There is no such command for an album: Proton shares an album with named
// people and publishes no URL for one, so `photos albums share` is the whole of
// what an album can be handed out by.
func photosLinksCmd() *cobra.Command {
	c := &cobra.Command{Use: "links", Short: "Links that open a photo for anyone"}
	c.AddCommand(linksCreateCmd(photosLinked()), linksGetCmd(photosLinked()),
		linksRevokeCmd(photosLinked()))
	return c
}

// linked is a collection whose things a public link can be made for: what one
// is called, how it is named on the command line, and whether a link into it
// can be widened to allow uploads.
type linked struct {
	noun string
	arg  string
	// label is what `get` calls the reference it shows back.
	label string
	// note is what a reader of this collection's `create` needs telling and a
	// reader of the other one does not.
	note string
	// takes says a link into it can be widened to accept uploads.
	takes   bool
	address func() addressing
}

func filesLinked() linked {
	return linked{noun: "a file or folder", arg: "PATH", label: "Path", takes: true,
		address: func() addressing { return &inTree{} }}
}

// A photo is one file and nothing can be put into it, so a link to one has no
// access to choose: it shows the photo.
func photosLinked() linked {
	return linked{noun: "a photo", arg: "REF", label: "ID",
		note: "An album has no public link. Share one with the people you want to see it,\n" +
			"with `photos albums share add`.",
		address: func() addressing {
			return aPhoto{notAPhoto: "%q is an album, and Proton has no public link for an album."}
		}}
}

// long is a command's help, with whatever this collection needs said after it.
func (l linked) long(body string) string {
	if l.note == "" {
		return body
	}
	return body + "\n\n" + l.note
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

func linksCreateCmd(l linked) *cobra.Command {
	access := kit.Viewing()
	var expires string
	a := l.address()
	password := kit.LinkPassword()
	c := &cobra.Command{
		Use:   "create " + l.arg,
		Short: "Make a link that opens " + l.noun + " for anyone",
		Long: l.long("Make a link that opens " + l.noun + " for anyone.\n\n" +
			"An item carries one link, so running it again changes that link rather than\n" +
			"making a second one, and a URL you have already shared keeps working.\n\n" +
			"The password is read from a file, never from a flag value, and may be at most\n" +
			"50 characters. --clear-link-password takes it off again, and --expires never\n" +
			"makes an expiring link permanent."),
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
			target, err := addressed(c, a)
			if err != nil {
				return err
			}
			var link *drivesvc.ShareLink
			if err := kit.Mutate(c, ui.ResultSpec{
				Action: ui.Created, Kind: "links", Count: 1,
				Detail: "for " + target.Ref, AnswerFollows: true,
			}, func() error {
				var err error
				link, err = c.App.Drive.EnsureLink(c.Ctx, target, opts)
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
	if l.takes {
		access.Register(c)
	}
	c.Flags().StringVar(&expires, "expires", "",
		"Stop working after DURATION (e.g. 7d, 2w, 6mo), or never")
	password.Declare(c)
	a.register(c)
	return c
}

func linksGetCmd(l linked) *cobra.Command {
	a := l.address()
	c := &cobra.Command{
		Use:   "get " + l.arg,
		Short: "Show the link on " + l.noun + ", URL and all",
		Long: "Show the link on " + l.noun + ", URL and all.\n\n" +
			"Use it to recover a URL you mislaid, rather than revoking the link and making\n" +
			"a new one. The URL appears here and in no listing.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			target, err := addressed(c, a)
			if err != nil {
				return err
			}
			st, err := c.App.Drive.ShareStatusOf(c.Ctx, target)
			if err != nil {
				return err
			}
			if len(st.Links) == 0 {
				return kit.Fail("%s has no public link.", st.Ref).
					Hint(kit.Program + " " + target.Linking + " create " + c.Args[0])
			}
			link := st.Links[0]
			fields := append([]ui.Field{
				{Label: l.label, Value: st.Ref},
				{Label: "Type", Value: st.Type},
			}, linkFields(&link)...)
			return kit.Show(c, ui.RecordSpec{Object: link, Fields: fields})
		}),
	}
	a.register(c)
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
			"read a URL, use `links get`, or `photos links get` for a link on a photo.",
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

func linksRevokeCmd(l linked) *cobra.Command {
	a := l.address()
	c := &cobra.Command{
		Use:   "revoke " + l.arg,
		Short: "Stop the link on " + l.noun + " working",
		Long: "Stop the link on " + l.noun + " working.\n\n" +
			"The item is untouched; only the link stops working. This cannot take back\n" +
			"what somebody already read.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			target, err := addressed(c, a)
			if err != nil {
				return err
			}
			n, err := c.App.Drive.CountLinks(c.Ctx, target)
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Revoked, Kind: "links", Count: n,
				Detail: "for " + target.Ref,
			}, func() error {
				_, err := c.App.Drive.RemoveLinks(c.Ctx, target)
				return err
			})
		}),
	}
	a.register(c)
	return c
}
