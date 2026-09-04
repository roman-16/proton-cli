package mail

import (
	"context"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

// Forwarding hands mail arriving at one of your addresses to another Proton
// account, end-to-end encrypted the whole way. This mirrors Proton's "Forward
// emails" settings page, minus accepting one - see the Long text below.

func forwardingCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "forwarding",
		Short: "Mail forwarded to and from your addresses",
	}
	c.AddCommand(
		forwardingListCmd(), forwardingGetCmd(), forwardingCreateCmd(),
		forwardingVerbCmd("delete", "Stop forwardings, in either direction", ui.Deleted,
			func(c *kit.Invocation, id string) error { return c.App.Mail.ForwardingDelete(c.Ctx, id) }),
		forwardingVerbCmd("disable", "Pause forwardings without taking them down", ui.Disabled,
			func(c *kit.Invocation, id string) error { return c.App.Mail.ForwardingPause(c.Ctx, id) }),
		forwardingVerbCmd("enable", "Resume paused forwardings", ui.Enabled,
			func(c *kit.Invocation, id string) error { return c.App.Mail.ForwardingResume(c.Ctx, id) }),
		forwardingVerbCmd("resend", "Ask the forwardee again", ui.Sent,
			func(c *kit.Invocation, id string) error { return c.App.Mail.ForwardingResend(c.Ctx, id) }),
	)
	return c
}

func forwardingColumns() []ui.Column[mailsvc.Forwarding] {
	return []ui.Column[mailsvc.Forwarding]{
		{Header: "ID", ID: true, Cell: func(f mailsvc.Forwarding) string { return f.ID }},
		{Header: "FROM", Flex: true, Handle: true, Cell: func(f mailsvc.Forwarding) string { return f.From }},
		{Header: "TO", Flex: true, Cell: func(f mailsvc.Forwarding) string { return f.To }},
		{Header: "STATE", Cell: func(f mailsvc.Forwarding) string { return f.State }},
		{Header: "DIRECTION", Cell: func(f mailsvc.Forwarding) string { return f.Direction }},
	}
}

func forwardingList(c *kit.Invocation) *kit.Lookup[mailsvc.Forwarding] {
	return &kit.Lookup[mailsvc.Forwarding]{
		Kind: "forwarding",
		Load: func(ctx context.Context) ([]mailsvc.Forwarding, error) {
			return c.App.Mail.ForwardingsList(ctx)
		},
		ID:     func(f mailsvc.Forwarding) string { return f.ID },
		Handle: func(f mailsvc.Forwarding) string { return f.To },
	}
}

func forwardingListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List forwardings in both directions",
		Long: "List forwardings in both directions.\n\n" +
			"Outgoing is mail leaving one of your addresses for somebody else's; incoming\n" +
			"is mail somebody else is sending to you. A forwarding is pending until the\n" +
			"forwardee accepts it, and outdated once the forwarder's key changes.",
		Args: cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			rows, err := forwardingList(c).Rows(c.Ctx)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[mailsvc.Forwarding]{
				Noun: "forwardings", Columns: forwardingColumns(),
				Total: ui.Unknown, Page: ui.Unpaged,
			}, rows)
		}),
	}
}

func forwardingGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: "Show one forwarding",
		Args:  cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			found, err := forwardingList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			return kit.Show(c, ui.RecordSpec{
				Object: found,
				Fields: []ui.Field{
					{Label: "From", Value: found.From},
					{Label: "To", Value: found.To, Handle: true},
					{Label: "Direction", Value: found.Direction, Always: true},
					{Label: "State", Value: found.State, Always: true},
					{Label: "Encrypted", Value: yesNo(found.Encrypted), Always: true},
					{Label: "Created", Value: units.Time(found.Created)},
					{Label: "ID", Value: found.ID, ID: true},
				},
			})
		}),
	}
}

func forwardingCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create REF EMAIL",
		Short: "Forward one of your addresses to another Proton address",
		Long: "Forward one of your addresses to another Proton address.\n\n" +
			"REF is the address of yours mail arrives at, EMAIL is the Proton address it\n" +
			"is handed to. Mail stays end-to-end encrypted: a key is derived from your\n" +
			"address key so Proton can re-wrap each message for them without reading it.\n\n" +
			"Nothing is forwarded until they accept, which they do in a Proton client -\n" +
			"accepting one writes a new address key, and proton changes no key material.\n\n" +
			"Forwarding to an address outside Proton is not built: Proton emails it a link\n" +
			"its owner must follow, which no command can answer.",
		Args: cobra.ExactArgs(2),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			from, err := c.App.Mail.ResolveAddress(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			to := c.Args[1]
			return kit.Create(c, ui.ResultSpec{
				Action: ui.Created, Kind: "forwardings", Name: to,
				Detail: "from " + from.Email + ", once they accept it",
			}, func() (string, error) {
				return c.App.Mail.ForwardingCreate(c.Ctx, from.Email, to)
			})
		}),
	}
}

// forwardingVerbCmd builds every verb that acts on named forwardings. They
// differ only in what they do to each one, so they share how one is found.
func forwardingVerbCmd(use, short string, action ui.Action, apply func(*kit.Invocation, string) error) *cobra.Command {
	return &cobra.Command{
		Use:   use + " REF...",
		Short: short,
		Args:  cobra.MinimumNArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			sel, err := kit.SelectFrom(c, "forwardings", forwardingColumns(), forwardingList(c))
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: action, Kind: "forwardings", Count: sel.Len(), IDs: sel.IDs,
				Name:    kit.Sole(sel.Rows, func(f mailsvc.Forwarding) string { return f.To }),
				Preview: sel.Preview(),
			}, func() error {
				for _, id := range sel.IDs {
					if err := apply(c, id); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
}
