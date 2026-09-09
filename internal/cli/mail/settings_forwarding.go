package mail

import (
	"context"

	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/errs"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

// Forwarding hands mail arriving at one of your addresses to another Proton
// account, end-to-end encrypted the whole way, and takes what somebody else's
// account is handing to yours. This mirrors Proton's "Forward emails" settings
// page.

func forwardingCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "forwarding",
		Short: "Mail forwarded to and from your addresses",
		Long: "Mail forwarded to and from your addresses.\n\n" +
			"Outgoing is mail leaving one of your addresses for somebody else's; incoming\n" +
			"is mail somebody else sends to you. A forwarding is named by the other\n" +
			"party's address.",
	}
	c.AddCommand(
		forwardingListCmd(), forwardingGetCmd(), forwardingCreateCmd(),
		forwardingVerbCmd(forwardingVerb{
			use:   "accept",
			short: "Accept forwardings sent to you",
			long: "REF is the forwarder's address, or the forwarding's ID. Only a pending\n" +
				"forwarding to one of your addresses can be accepted.\n\n" +
				"Asks for your password even when you are signed in. With no terminal to ask,\n" +
				"pass --password-file or --password-stdin.",
			action: ui.Accepted,
			reason: "accept a forwarding",
			takes:  answerable("accept", "accepted"),
			apply: func(ctx context.Context, m *mailsvc.Service, f mailsvc.Forwarding) error {
				return m.ForwardingAccept(ctx, f)
			},
		}),
		forwardingVerbCmd(forwardingVerb{
			use:    "decline",
			short:  "Decline forwardings sent to you",
			long:   "REF is the forwarder's address, or the forwarding's ID. Only a pending\nforwarding to one of your addresses can be declined; the forwarder sees it as\nrejected.",
			action: ui.Declined,
			takes:  answerable("decline", "declined"),
			apply: func(ctx context.Context, m *mailsvc.Service, f mailsvc.Forwarding) error {
				return m.ForwardingDelete(ctx, f.ID)
			},
		}),
		forwardingVerbCmd(forwardingVerb{
			use:    "delete",
			short:  "Stop forwardings, in either direction",
			action: ui.Deleted,
			apply: func(ctx context.Context, m *mailsvc.Service, f mailsvc.Forwarding) error {
				return m.ForwardingDelete(ctx, f.ID)
			},
		}),
		forwardingVerbCmd(forwardingVerb{
			use:    "disable",
			short:  "Pause forwardings without taking them down",
			long:   "REF is a forwarding you set up that is active. Mail stops being forwarded, and\nresuming it needs nothing from the forwardee.",
			action: ui.Disabled,
			takes:  pausable,
			apply: func(ctx context.Context, m *mailsvc.Service, f mailsvc.Forwarding) error {
				return m.ForwardingPause(ctx, f.ID)
			},
		}),
		forwardingVerbCmd(forwardingVerb{
			use:    "enable",
			short:  "Resume paused forwardings",
			long:   "REF is a forwarding you set up that is paused.",
			action: ui.Enabled,
			takes:  resumable,
			apply: func(ctx context.Context, m *mailsvc.Service, f mailsvc.Forwarding) error {
				return m.ForwardingResume(ctx, f.ID)
			},
		}),
		forwardingVerbCmd(forwardingVerb{
			use:   "resend",
			short: "Ask the forwardee again",
			long: "REF is a forwarding you set up that the forwardee declined, or that is\n" +
				"outdated. They are offered it again, and it is pending until they accept.\n" +
				"A forwarding to an address outside Proton gets its confirmation email sent\n" +
				"again instead.",
			action: ui.Resent,
			takes:  resendable,
			apply: func(ctx context.Context, m *mailsvc.Service, f mailsvc.Forwarding) error {
				return m.ForwardingResend(ctx, f)
			},
		}),
	)
	return c
}

func forwardingColumns() []ui.Column[mailsvc.Forwarding] {
	return []ui.Column[mailsvc.Forwarding]{
		{Header: "ID", ID: true, Cell: func(f mailsvc.Forwarding) string { return f.ID }},
		{Header: "FROM", Flex: true, Cell: func(f mailsvc.Forwarding) string { return f.From }},
		{Header: "TO", Flex: true, Cell: func(f mailsvc.Forwarding) string { return f.To }},
		{Header: "STATE", Cell: func(f mailsvc.Forwarding) string { return f.State }},
		{Header: "DIRECTION", Cell: func(f mailsvc.Forwarding) string { return f.Direction }},
	}
}

// counterparty is the address a forwarding goes by: the other account's, in
// whichever direction it runs.
//
// It is the one name a person has for a forwarding. Their own address names
// every forwarding at once - it is on both sides of the collection - so naming
// one by it would be naming nothing.
func counterparty(f mailsvc.Forwarding) string {
	if f.Direction == mailsvc.DirectionIncoming {
		return f.From
	}
	return f.To
}

func forwardingList(c *kit.Invocation) *kit.Lookup[mailsvc.Forwarding] {
	return &kit.Lookup[mailsvc.Forwarding]{
		Kind: "forwarding",
		Load: func(ctx context.Context) ([]mailsvc.Forwarding, error) {
			return c.App.Mail.ForwardingsList(ctx)
		},
		ID:     func(f mailsvc.Forwarding) string { return f.ID },
		Handle: counterparty,
	}
}

func forwardingListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List forwardings in both directions",
		Long: "List forwardings in both directions.\n\n" +
			"A forwarding is pending until the forwardee accepts it, paused while the\n" +
			"forwarder has it disabled, outdated once the forwarder's key changes, and\n" +
			"rejected once the forwardee declines it.",
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
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			found, err := forwardingList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			incoming := found.Direction == mailsvc.DirectionIncoming
			return kit.Show(c, ui.RecordSpec{
				Object: found,
				Fields: []ui.Field{
					{Label: "From", Value: found.From, Handle: incoming},
					{Label: "To", Value: found.To, Handle: !incoming},
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
			"is handed to. Mail stays end-to-end encrypted, and nothing is forwarded until\n" +
			"they accept it.\n\n" +
			"Proton refuses this request from " + kit.Program + ": add the forwarding at\n" +
			"account.proton.me instead. Accepting one sent to you works here.\n\n" +
			"Forwarding to an address outside Proton is not built.",
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

// forwardingVerb is one of the things that can be done to forwardings already
// there. They differ in what they do to each one and in which ones they take;
// everything else about them is the same, which is why they are built from one
// declaration.
type forwardingVerb struct {
	use   string
	short string
	// long says which forwardings the verb takes, so what takes refuses is on
	// the help screen rather than only in the refusal.
	long   string
	action ui.Action
	// takes admits one forwarding or says why the verb has nothing to do with
	// it. Nil takes all of them, which only deleting does.
	takes func(mailsvc.Forwarding) error
	apply func(context.Context, *mailsvc.Service, mailsvc.Forwarding) error
	// reason completes "Your password is required to ..." for the verb that
	// publishes a key, which Proton guards behind an elevated session. Empty for
	// the ones it does not.
	reason string
}

// forwardingVerbCmd builds every verb that acts on named forwardings.
//
// Which forwardings a verb takes is judged on the rows the references resolved
// to, before anything is changed and before a dry run says what it would change:
// a state Proton would refuse is a state this can see for itself, and a preview
// that promised to accept an active forwarding would be a preview of nothing.
func forwardingVerbCmd(v forwardingVerb) *cobra.Command {
	long := ""
	if v.long != "" {
		long = v.short + ".\n\n" + v.long
	}
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   v.use + " REF...",
		Short: v.short,
		Long:  long,
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			if v.reason != "" {
				if err := reauth.Supply(c); err != nil {
					return err
				}
			}
			sel, err := kit.SelectFrom(c, "forwardings", forwardingColumns(), forwardingList(c))
			if err != nil {
				return err
			}
			if v.takes != nil {
				for _, f := range sel.Rows {
					if err := v.takes(f); err != nil {
						return err
					}
				}
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: v.action, Kind: "forwardings", Count: sel.Len(), IDs: sel.IDs,
				Name:    kit.Sole(sel.Rows, counterparty),
				Preview: sel.Preview(),
			}, func() error {
				// Nothing here arranges the elevation: the client does it when the
				// server asks, and drops the scope again afterwards. All this owes
				// the user is a reason for the prompt.
				ctx := c.Ctx
				if v.reason != "" {
					ctx = app.WithScopeReason(ctx, v.reason)
				}
				for _, f := range sel.Rows {
					if err := v.apply(ctx, c.App.Mail, f); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
	if v.reason != "" {
		reauth.Declare(c)
	}
	return c
}

// answerable admits a forwarding somebody sent you and is waiting on you.
func answerable(verb, past string) func(mailsvc.Forwarding) error {
	return func(f mailsvc.Forwarding) error {
		if f.Direction != mailsvc.DirectionIncoming {
			return errs.Problemf("The forwarding to %s is one you set up; only one sent to you can be %s.", f.To, past)
		}
		if f.State != mailsvc.StatePending {
			return errs.Problemf("The forwarding from %s is %s, not pending, so there is nothing to %s.",
				f.From, f.State, verb)
		}
		return nil
	}
}

// yours admits a forwarding you set up, for the verbs only its forwarder can
// use: how a forwarding to you runs is the other account's to change, and all
// you can do with one is answer it.
func yours(f mailsvc.Forwarding, past string) error {
	if f.Direction != mailsvc.DirectionOutgoing {
		return errs.Problemf("The forwarding from %s was sent to you; only one you set up can be %s.", f.From, past)
	}
	return nil
}

func pausable(f mailsvc.Forwarding) error {
	if err := yours(f, "disabled"); err != nil {
		return err
	}
	if f.State != mailsvc.StateActive {
		return errs.Problemf("The forwarding to %s is %s; only an active one can be disabled.", f.To, f.State)
	}
	return nil
}

func resumable(f mailsvc.Forwarding) error {
	if err := yours(f, "enabled"); err != nil {
		return err
	}
	if f.State != mailsvc.StatePaused {
		return errs.Problemf("The forwarding to %s is %s; only a paused one can be enabled.", f.To, f.State)
	}
	return nil
}

// resendable admits a forwarding whose forwardee has something to answer again.
//
// What that is depends on which kind of forwardee it is. Another Proton account
// declined it, or holds material a key change has left outdated; an address
// outside Proton has an email to answer, which it still has while the forwarding
// is pending.
func resendable(f mailsvc.Forwarding) error {
	if err := yours(f, "sent again"); err != nil {
		return err
	}
	switch {
	case f.Encrypted && f.State == mailsvc.StatePending:
		return errs.Problemf("The forwarding to %s is still waiting for their answer.", f.To)
	case f.Encrypted && (f.State == mailsvc.StateRejected || f.State == mailsvc.StateOutdated):
		return nil
	case !f.Encrypted && (f.State == mailsvc.StatePending || f.State == mailsvc.StateRejected):
		return nil
	}
	return errs.Problemf("The forwarding to %s is %s; there is nothing to send again.", f.To, f.State)
}
