package mail

import (
	"errors"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/errs"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

func trackersCmd() *cobra.Command {
	c := &cobra.Command{Use: "trackers", Short: "Trackers blocked in a message"}
	c.AddCommand(trackersListCmd())
	return c
}

func trackerTableSpec() ui.TableSpec[mailsvc.Tracker] {
	return ui.TableSpec[mailsvc.Tracker]{
		Noun: "trackers",
		Columns: []ui.Column[mailsvc.Tracker]{
			{Header: "KIND", Cell: func(t mailsvc.Tracker) string { return t.Kind }},
			{Header: "TRACKER", Cell: func(t mailsvc.Tracker) string { return t.Name }},
			{Header: "URL", Flex: true, Cell: func(t mailsvc.Tracker) string { return t.URL }},
		},
		Total: ui.Unknown, Page: ui.Unpaged,
	}
}

func trackersListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list REF",
		Short: "List the trackers blocked in a message",
		Long: "List the trackers blocked in a message.\n\n" +
			"An image row names who serves it; nothing is loaded to find out. A link\n" +
			"row names the tracking taken out of it, or redirect for a link that went\n" +
			"through a click-tracking address, and `--output json` adds the cleaned link.\n" +
			"Needs `mail settings set image-proxy on`.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			id, err := c.App.Mail.Resolve(c.Ctx, c.Args[0])
			if err != nil {
				return wrongTable(err, "trackers list")
			}
			trackers, err := c.App.Mail.Trackers(c.Ctx, id)
			if errors.Is(err, mailsvc.ErrTrackingNotBlocked) {
				return errs.Problemf("Block email tracking is off, so nothing in this message was blocked or cleaned.").
					Hint(kit.Program + " mail settings set image-proxy on")
			}
			if err != nil {
				return wrongTable(err, "trackers list")
			}
			return kit.List(c, trackerTableSpec(), trackers)
		}),
	}
}
