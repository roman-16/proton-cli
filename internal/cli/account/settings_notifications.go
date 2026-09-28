package account

import (
	"context"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/errs"
	acctsvc "github.com/roman-16/proton-cli/internal/service/account"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

func notificationsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "notifications",
		Short: "The emails and in-app notices Proton sends you",
		Long: "The emails and in-app notices Proton sends you.\n\n" +
			"Critical account notifications are sent whatever is off here. The daily\n" +
			"email about new mail is a Mail setting:\n" +
			"`" + kit.Program + " mail settings set daily-notifications`.",
	}
	c.AddCommand(notificationsListCmd(),
		notificationsSwitchCmd("enable", "Have Proton send you emails or notices", ui.Enabled, true),
		notificationsSwitchCmd("disable", "Stop emails or notices Proton sends you", ui.Disabled, false))
	return c
}

func notificationColumns() []ui.Column[acctsvc.Notification] {
	return []ui.Column[acctsvc.Notification]{
		{Header: "NAME", Handle: true, Cell: func(n acctsvc.Notification) string { return n.Name }},
		{Header: "DESCRIPTION", Cell: func(n acctsvc.Notification) string { return n.Description }},
		{Header: "DETAIL", Flex: true, Cell: func(n acctsvc.Notification) string { return n.Detail }},
		{Header: "ON", Cell: func(n acctsvc.Notification) string { return yesNo(n.On) }},
	}
}

type notificationLookup struct {
	*kit.Lookup[acctsvc.Notification]
	news acctsvc.News
}

func notificationList(c *kit.Invocation) *notificationLookup {
	l := &notificationLookup{}
	l.Lookup = &kit.Lookup[acctsvc.Notification]{
		Kind: "notification",
		Load: func(ctx context.Context) ([]acctsvc.Notification, error) {
			news, err := c.App.Account.News(ctx)
			if err != nil {
				return nil, err
			}
			l.news = news
			return news.Notifications(), nil
		},
		ID:     func(n acctsvc.Notification) string { return n.Name },
		Handle: func(n acctsvc.Notification) string { return n.Name },
	}
	return l
}

func notificationsListCmd() *cobra.Command {
	var held kit.Held[acctsvc.Notification]
	c := &cobra.Command{
		Use:   "list",
		Short: "List what Proton sends you, and what is on",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			rows, err := notificationList(c).Rows(c.Ctx)
			if err != nil {
				return err
			}
			return held.Answer(c, ui.TableSpec[acctsvc.Notification]{
				Noun: "notifications", Columns: notificationColumns(),
			}, rows)
		}),
	}
	held.Register(c, "notifications")
	return c
}

func notificationsSwitchCmd(use, short string, action ui.Action, on bool) *cobra.Command {
	return &cobra.Command{
		Use:   use + " REF...",
		Short: short,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			lookup := notificationList(c)
			sel, err := kit.SelectFrom(c, "notifications", notificationColumns(), lookup.Lookup)
			if err != nil {
				return err
			}
			for _, n := range sel.Rows {
				if n.On == on {
					return errs.Naming(n.Name, kit.Fail("%s is already %s.", n.Name, kit.OnOffText(boolInt(on))))
				}
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: action, Kind: "notifications", Count: sel.Len(), IDs: sel.IDs,
				Name:    kit.Sole(sel.Rows, func(n acctsvc.Notification) string { return n.Name }),
				Preview: sel.Preview(),
			}, func() error {
				return c.App.Account.SetNotifications(c.Ctx, lookup.news, sel.Rows, on)
			})
		}),
	}
}
