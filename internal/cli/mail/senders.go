package mail

import (
	"github.com/roman-16/proton-cli/internal/cli/kit"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

// Who reaches the inbox, and who does not.
//
// Proton's settings page calls this "Spam, block and allow lists" and shows
// three of them, but they are one record with a destination on it. So this is one
// collection with a verb per destination, and `list` shows all three at once -
// which is the question a person actually has: what have I decided about whom.
//
// It sits under `settings` because that is where Proton's own clients keep the
// list, even though the message context menu can add to it.

func sendersCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "senders",
		Short: "Who always reaches the inbox, and who never does",
	}
	c.AddCommand(
		sendersListCmd(),
		senderVerb("block", "Send someone's mail straight to blocked", "blocked", ui.Blocked),
		senderVerb("spam", "Send someone's mail straight to spam", "spam", ui.Filed),
		senderVerb("allow", "Always let someone reach the inbox", "inbox", ui.Allowed),
		sendersRemoveCmd(),
	)
	return c
}

func senderColumns() []ui.Column[mailsvc.SenderRule] {
	return []ui.Column[mailsvc.SenderRule]{
		{Header: "ID", ID: true, Cell: func(r mailsvc.SenderRule) string { return r.ID }},
		{Header: "SENDER", Flex: true, Cell: func(r mailsvc.SenderRule) string {
			if r.Domain != "" {
				return "@" + r.Domain
			}
			return r.Email
		}},
		{Header: "GOES TO", Cell: func(r mailsvc.SenderRule) string { return r.Goes }},
		{Header: "SINCE", Cell: func(r mailsvc.SenderRule) string {
			if r.Time == 0 {
				return ""
			}
			return units.Time(r.Time)
		}},
	}
}

func sendersListCmd() *cobra.Command {
	var held kit.Held[mailsvc.SenderRule]
	c := &cobra.Command{
		Use:   "list",
		Short: "List every standing decision about a sender",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			rules, err := c.App.Mail.SendersList(c.Ctx)
			if err != nil {
				return err
			}
			return held.Answer(c, ui.TableSpec[mailsvc.SenderRule]{
				Noun: "rules", Columns: senderColumns(),
			}, rules)
		}),
	}
	held.Register(c, "rules",
		kit.Key[mailsvc.SenderRule]{Name: "sender", Less: func(a, b mailsvc.SenderRule) int { return kit.Fold(a.Email+a.Domain, b.Email+b.Domain) }},
		kit.Key[mailsvc.SenderRule]{Name: "since", Less: func(a, b mailsvc.SenderRule) int { return kit.Ints(a.Time, b.Time) }},
	)
	return c
}
func senderVerb(use, short, destination string, action ui.Action) *cobra.Command {
	return &cobra.Command{
		Use:   use + " SENDER...",
		Short: short,
		Long: short + ".\n\n" +
			"A whole domain works too, written with the @: `@example.com`.\n\n" +
			"Deciding again about the same sender replaces the earlier decision rather\n" +
			"than colliding with it.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			targets := kit.Dedupe(c.Args)
			return kit.Mutate(c, ui.ResultSpec{
				Action: action, Kind: "senders", Count: len(targets),
				Name:   kit.Sole(targets, func(s string) string { return s }),
				Detail: "- their mail goes to " + destination,
			}, func() error {
				for _, t := range targets {
					if err := c.App.Mail.SenderSet(c.Ctx, t, mailsvc.SenderDestinations[destination]); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
}

func sendersRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove SENDER...",
		Short: "Drop a standing decision, letting the spam filter decide again",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			targets := kit.Dedupe(c.Args)
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Removed, Kind: "senders", Count: len(targets),
				Name: kit.Sole(targets, func(s string) string { return s }),
			}, func() error {
				return c.App.Mail.SenderForget(c.Ctx, targets)
			})
		}),
	}
}
