package pass

import (
	"context"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	passsvc "github.com/roman-16/proton-cli/internal/service/pass"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
)

// A link that shows one item to somebody with no Proton account.
//
// The key that opens it is in the URL, after the '#', so a browser never sends
// it to Proton - and so anyone who sees the whole URL can read the item until
// the link expires or is revoked. That is worth saying where somebody makes one.

func linksCmd() *cobra.Command {
	c := &cobra.Command{Use: "links", Short: "Links that show an item to somebody without an account"}
	c.AddCommand(linksListCmd(), linksGetCmd(), linksCreateCmd(), linksRevokeCmd())
	return c
}

func linksGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: "Show one link, URL and all",
		Long: "Show one link, URL and all.\n\n" +
			"Proton stores the key sealed under the item's own, so a link you mislaid is\n" +
			"read back here rather than revoked and made again. The URL is the secret,\n" +
			"which is why it takes a command that says so rather than appearing in a\n" +
			"listing.",
		Args: cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			found, err := secureLinkList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			link, err := c.App.Pass.SecureLinkGet(c.Ctx, found.LinkID)
			if err != nil {
				return err
			}
			c.Warn("Anyone with this link can read the item until it expires.")
			return kit.Show(c, ui.RecordSpec{
				Object: link,
				Fields: []ui.Field{
					{Label: "URL", Value: link.URL},
					{Label: "Item", Value: kit.JoinPair(link.ShareID, link.ItemID), ID: true},
					{Label: "Expires", Value: units.Time(link.Expires)},
					{Label: "Views", Value: reads(*link)},
					{Label: "Active", Value: yesNo(link.Active)},
					{Label: "ID", Value: link.LinkID, ID: true},
				},
			})
		}),
	}
}

func secureLinkList(c *kit.Invocation) *kit.Lookup[passsvc.SecureLink] {
	return &kit.Lookup[passsvc.SecureLink]{
		Kind: "link",
		Load: func(ctx context.Context) ([]passsvc.SecureLink, error) {
			return c.App.Pass.SecureLinksList(ctx)
		},
		ID: func(l passsvc.SecureLink) string { return l.LinkID },
		// A link is addressed by its ID or by the item it opens. The URL is not a
		// handle: pasting one into a command line is the thing this avoids.
		Handle: func(l passsvc.SecureLink) string { return kit.JoinPair(l.ShareID, l.ItemID) },
	}
}

func linkColumns() []ui.Column[passsvc.SecureLink] {
	return []ui.Column[passsvc.SecureLink]{
		{Header: "ID", ID: true, Cell: func(l passsvc.SecureLink) string { return l.LinkID }},
		{Header: "ITEM", ID: true, Cell: func(l passsvc.SecureLink) string {
			return kit.JoinPair(l.ShareID, l.ItemID)
		}},
		{Header: "EXPIRES", Cell: func(l passsvc.SecureLink) string { return units.Time(l.Expires) }},
		{Header: "READS", Right: true, Cell: func(l passsvc.SecureLink) string { return reads(l) }},
		{Header: "ACTIVE", Cell: func(l passsvc.SecureLink) string { return yesNo(l.Active) }},
	}
}

// reads is how often a link has been opened against how often it may be.
func reads(l passsvc.SecureLink) string {
	if l.MaxReads > 0 {
		return strconv.Itoa(l.Reads) + "/" + strconv.Itoa(l.MaxReads)
	}
	return strconv.Itoa(l.Reads)
}

func linksListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the links you have made",
		Long: "List the links you have made.\n\n" +
			"The URL is not among them: it carries the key that opens the item, and a\n" +
			"listing is no place for a secret. `links get` reads one back whole, and\n" +
			"`items share get` the ones an item has.",
		Args: cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			rows, err := c.App.Pass.SecureLinksList(c.Ctx)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[passsvc.SecureLink]{
				Noun: "links", Columns: linkColumns(), Total: len(rows), Page: ui.Unpaged,
			}, rows, func(l passsvc.SecureLink) []string { return []string{l.LinkID} })
		}),
	}
}

func linksCreateCmd() *cobra.Command {
	var expires string
	var maxReads int
	c := &cobra.Command{
		Use:   "create REF",
		Short: "Make a link that shows one item",
		Long: "Make a link that shows one item to somebody with no Proton account.\n\n" +
			"The key that opens it travels in the URL after the '#', which a browser\n" +
			"never sends to Proton. So the URL is the secret: anyone holding the whole\n" +
			"of it can read the item until the link expires or is revoked.\n\n" +
			"--expires is required.",
		Args: cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand, func(*kit.Invocation) error {
			if expires == "" {
				return kit.Fail("How long should the link last?").
					Hint("--expires 7d", "--expires 1h")
			}
			return nil
		}}, func(c *kit.Invocation) error {
			d, err := units.ParseDuration(expires)
			if err != nil {
				return kit.Fail("--expires: %v", err)
			}
			shareID, itemID, err := resolveItem(c, c.Args[0])
			if err != nil {
				return err
			}
			var link *passsvc.SecureLink
			if err := kit.Mutate(c, ui.ResultSpec{
				Action: ui.Linked, Kind: "links", Count: 1,
				Detail: "lasting " + units.Duration(d), AnswerFollows: true,
			}, func() error {
				link, err = c.App.Pass.SecureLinkCreate(c.Ctx, shareID, itemID,
					passsvc.NewSecureLink{Expires: d, MaxReads: maxReads})
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
			c.Warn("Anyone with this link can read the item until it expires.")
			return kit.Show(c, ui.RecordSpec{
				Object: link,
				Fields: []ui.Field{
					{Label: "URL", Value: link.URL},
					{Label: "Expires", Value: units.Time(link.Expires)},
					{Label: "Views", Value: reads(*link)},
					{Label: "ID", Value: link.LinkID, ID: true},
				},
			})
		}),
	}
	c.Flags().StringVar(&expires, "expires", "", "How long the link lasts (e.g. 7d, 24h)")
	c.Flags().IntVar(&maxReads, "views", 0, "Stop working after this many openings")
	return c
}

func linksRevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke REF...",
		Short: "Stop a link working",
		Long: "Stop a link working.\n\n" +
			"The item is untouched; only the link is withdrawn. Anyone who already\n" +
			"read it has already read it.",
		Args: cobra.MinimumNArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			sel, err := kit.SelectFrom(c, "links", linkColumns(), secureLinkList(c))
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Revoked, Kind: "links", Count: sel.Len(), IDs: sel.IDs,
			}, func() error {
				for _, id := range sel.IDs {
					if err := c.App.Pass.SecureLinkRevoke(c.Ctx, id); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
}
