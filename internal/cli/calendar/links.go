package calendar

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	calsvc "github.com/roman-16/proton-cli/internal/service/calendar"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
)

// A link that opens a calendar for somebody who has no Proton account.
//
// A calendar carries several at once, each showing as much or as little as it
// was made to show, so a link is a thing in its own right rather than a state
// of the calendar: it is named, listed and revoked on its own, and only making
// one names the calendar it is for.
//
// The URL is the secret, and the two levels give away different amounts of it.
// That is worth saying wherever one is handed over, which is why the commands
// that put a URL on the screen warn beside it.

func linksCmd() *cobra.Command {
	c := &cobra.Command{Use: "links", Short: "Links that open a calendar for anyone"}
	c.AddCommand(linksCreateCmd(), linksGetCmd(), linksListCmd(),
		linksRevokeCmd(), linksUpdateCmd())
	return c
}

func linkList(c *kit.Invocation) *kit.Lookup[calsvc.Link] {
	return &kit.Lookup[calsvc.Link]{
		Kind: "link",
		Load: func(ctx context.Context) ([]calsvc.Link, error) {
			return c.App.Calendar.LinksAll(ctx)
		},
		ID: func(l calsvc.Link) string { return l.ID },
		// A link is addressed by its ID or by the name given to it. The URL is
		// not a handle: pasting one into a command line is the thing this avoids.
		Handle: func(l calsvc.Link) string { return l.Name },
	}
}

func linkColumns() []ui.Column[calsvc.Link] {
	return []ui.Column[calsvc.Link]{
		{Header: "ID", ID: true, Cell: func(l calsvc.Link) string { return l.ID }},
		{Header: "CALENDAR", Flex: true, Cell: func(l calsvc.Link) string { return l.Calendar }},
		{Header: "ACCESS", Cell: func(l calsvc.Link) string { return l.Access }},
		{Header: "NAME", Flex: true, Handle: true, Cell: func(l calsvc.Link) string { return l.Name }},
		{Header: "CREATED", Cell: func(l calsvc.Link) string { return units.Time(l.Created) }},
	}
}

func linkFields(l *calsvc.LinkURL) []ui.Field {
	return []ui.Field{
		{Label: "URL", Value: l.URL},
		{Label: "Calendar", Value: l.Calendar},
		{Label: "Access", Value: l.Access},
		{Label: "Name", Value: l.Name, Handle: true},
		{Label: "Created", Value: units.Time(l.Created)},
		{Label: "ID", Value: l.ID, ID: true},
	}
}

// warning is what somebody holding the URL can do with it, which differs by how
// much the link was made to show.
func warning(access string) string {
	if access == calsvc.AccessFull {
		return "Anyone with this link can see every detail of these events, including " +
			"title, location and who is invited, and Proton can read them to serve it."
	}
	return "Anyone with this link can see when you are busy, until it is revoked."
}

func linksCreateCmd() *cobra.Command {
	access := kit.Watching()
	var name string
	c := &cobra.Command{
		Use:         "create REF",
		Short:       "Publish a calendar as a link anyone can follow",
		Annotations: map[string]string{kit.Addresses: "calendar settings calendars"},
		Long: "Publish a calendar as a link anyone can follow.\n\n" +
			"The link is an .ics feed, so any calendar app can follow it and stays up to\n" +
			"date. A limited link shows only whether you are busy; a full one shows every\n" +
			"detail of every event, and Proton reads them to serve it.\n\n" +
			"--name is yours alone and is never shown to anyone following the link. A\n" +
			"calendar carries at most five links.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			level, err := access.Value()
			if err != nil {
				return err
			}
			name = strings.TrimSpace(name)
			if err := nameWithin(name); err != nil {
				return err
			}
			cal, err := calendarList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			if err := cal.Shareable(); err != nil {
				return err
			}
			var link *calsvc.LinkURL
			if err := kit.Mutate(c, ui.ResultSpec{
				Action: ui.Created, Kind: "links", Count: 1, Name: name,
				Detail: "for " + cal.Name, AnswerFollows: true,
			}, func() error {
				link, err = c.App.Calendar.LinkCreate(c.Ctx, cal,
					calsvc.NewLink{Full: level == kit.Full, Name: name})
				return err
			}); err != nil {
				return err
			}
			if link == nil || c.App.DryRun {
				return nil
			}
			// The URL is the answer, so it goes to stdout: the point of this
			// command is to be able to capture it. The warning goes to stderr,
			// where it does not end up in whatever captured the link.
			c.Warn("%s", warning(link.Access))
			return kit.Show(c, ui.RecordSpec{Object: link, Fields: linkFields(link)})
		}),
	}
	access.Register(c)
	c.Flags().StringVar(&name, "name", "", "Name for the new link, which only you see")
	return c
}

func linksGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: "Show one link, URL and all",
		Long: "Show one link, URL and all.\n\n" +
			"Use this to recover a URL you mislaid, rather than revoking the link and\n" +
			"making a new one. The URL appears here and in no listing.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			found, err := linkList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			link, err := c.App.Calendar.LinkOpen(c.Ctx, found)
			if err != nil {
				return err
			}
			c.Warn("%s", warning(link.Access))
			return kit.Show(c, ui.RecordSpec{Object: link, Fields: linkFields(link)})
		}),
	}
}

func linksListCmd() *cobra.Command {
	var held kit.Held[calsvc.Link]
	var calendar string
	c := &cobra.Command{
		Use:   "list",
		Short: "List the links you have published",
		Long: "List the links you have published.\n\n" +
			"The URLs are not shown: each one opens its calendar for anybody holding it.\n" +
			"To read a URL, use `links get`.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			rows, err := publishedLinks(c, calendar)
			if err != nil {
				return err
			}
			return held.Answer(c, ui.TableSpec[calsvc.Link]{
				Noun: "links", Columns: linkColumns(),
			}, rows)
		}),
	}
	c.Flags().StringVar(&calendar, "calendar", "", "Which calendar, by name or ID (default: all of them)")
	held.Register(c, "links",
		kit.Key[calsvc.Link]{Name: "calendar", Less: func(a, b calsvc.Link) int {
			return kit.Fold(a.Calendar, b.Calendar)
		}},
		kit.Key[calsvc.Link]{Name: "created", Less: func(a, b calsvc.Link) int {
			return kit.Ints(a.Created, b.Created)
		}},
		kit.Key[calsvc.Link]{Name: "name", Less: func(a, b calsvc.Link) int {
			return kit.Fold(a.Name, b.Name)
		}},
	)
	return c
}

func linksRevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke REF...",
		Short: "Stop a link working",
		Long: "Stop a link working.\n\n" +
			"The calendar is untouched; only the link stops opening it. Whatever somebody\n" +
			"already copied out of it stays copied.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			sel, err := kit.SelectFrom(c, "links", linkColumns(), linkList(c))
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Revoked, Kind: "links", Count: sel.Len(), IDs: sel.IDs,
				Name:    kit.Sole(sel.Rows, func(l calsvc.Link) string { return l.Name }),
				Preview: sel.Preview(),
			}, func() error {
				for _, l := range sel.Rows {
					if err := c.App.Calendar.LinkRevoke(c.Ctx, l); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
}

func linksUpdateCmd() *cobra.Command {
	var name string
	var clear bool
	c := &cobra.Command{
		Use:   "update REF",
		Short: "Rename a link",
		Long: "Rename a link.\n\n" +
			"The name is yours alone and is never shown to anyone following the link.\n" +
			"--clear-name takes it off again.\n\n" +
			"What a link shows, and the URL it is opened at, cannot be changed: make\n" +
			"another link for that.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			name = strings.TrimSpace(name)
			switch {
			case clear && name != "":
				return kit.Fail("--name and --clear-name ask for opposite things.")
			case !clear && name == "":
				return kit.Fail("Nothing to change.").Hint("--name 'Team feed'", "--clear-name")
			}
			if err := nameWithin(name); err != nil {
				return err
			}
			found, err := linkList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: "links", Count: 1, Name: name,
				IDs: []string{found.ID},
			}, func() error {
				return c.App.Calendar.LinkRename(c.Ctx, found, name)
			})
		}),
	}
	c.Flags().StringVar(&name, "name", "", "New name, which only you see")
	c.Flags().BoolVar(&clear, "clear-name", false, "Remove the link's name")
	return c
}

// publishedLinks is every link, or the ones on the calendar that was named.
func publishedLinks(c *kit.Invocation, calendar string) ([]calsvc.Link, error) {
	if calendar == "" {
		return c.App.Calendar.LinksAll(c.Ctx)
	}
	cal, err := calendarList(c).Find(c.Ctx, calendar)
	if err != nil {
		return nil, err
	}
	if err := cal.Shareable(); err != nil {
		return nil, err
	}
	return c.App.Calendar.Links(c.Ctx, cal)
}

// nameWithin refuses a label longer than what Proton stores, before anything is
// sent: how long it is, is a fact about the command line.
func nameWithin(name string) error {
	if len([]rune(name)) > calsvc.LabelLimit {
		return kit.Fail("A link's name may be at most %d characters.", calsvc.LabelLimit)
	}
	return nil
}
