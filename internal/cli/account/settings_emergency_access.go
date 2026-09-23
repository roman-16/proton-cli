package account

import (
	"context"
	"time"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/profile"
	acctsvc "github.com/roman-16/proton-cli/internal/service/account"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

// Emergency access: the people who may get into the whole account if its owner
// cannot. Each is given a copy of the account's keys, sealed to them, and a
// waiting period; a request opens the access once the wait runs out, unless the
// owner grants it sooner or takes it back.

// defaultWait is the wait a new emergency contact gets when none is named, the
// same seven days Proton's own selector defaults to.
const defaultWait = 7 * 24 * time.Hour

func emergencyAccessCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "emergency-access",
		Short: "People who can get into your account if you cannot",
		Long: "People who can get into your account if you cannot.\n\n" +
			"A contact you add is handed a copy of your keys, sealed so only they can\n" +
			"open it, and a waiting period. If they ever request access, it opens once\n" +
			"the wait runs out - unless you grant it sooner or cancel the request. A\n" +
			"contact has to have a Proton account of their own.\n\n" +
			"`list` shows the people you granted; `list --incoming` shows accounts that\n" +
			"granted you.",
	}
	c.AddCommand(emergencyListCmd(), emergencyGetCmd(), emergencyAddCmd(),
		emergencyUpdateCmd(), emergencyRequestCmd(), emergencyAccessInCmd(), emergencyGrantCmd(),
		emergencyCancelCmd(), emergencyRemoveCmd())
	return c
}

func emergencyColumns() []ui.Column[acctsvc.DelegatedAccess] {
	return []ui.Column[acctsvc.DelegatedAccess]{
		{Header: "ID", ID: true, Cell: func(d acctsvc.DelegatedAccess) string { return d.ID }},
		{Header: "CONTACT", Flex: true, Handle: true, Cell: func(d acctsvc.DelegatedAccess) string { return d.Contact }},
		{Header: "WAIT", Cell: func(d acctsvc.DelegatedAccess) string { return waitText(d.Wait) }},
		{Header: "STATUS", Cell: func(d acctsvc.DelegatedAccess) string { return d.Status }},
		{Header: "CREATED", Cell: func(d acctsvc.DelegatedAccess) string { return units.Time(d.Created) }},
	}
}

func emergencyListCmd() *cobra.Command {
	var incoming bool
	var held kit.Held[acctsvc.DelegatedAccess]
	c := &cobra.Command{
		Use:   "list",
		Short: "List the people you let into your account",
		Long: "List the people you let into your account.\n\n" +
			"WAIT is how long after a request the access opens. STATUS is enabled,\n" +
			"requested, or open. --incoming lists accounts that granted you emergency\n" +
			"access instead.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			rows, err := delegatedRows(c, acctsvc.KindEmergency, incoming)
			if err != nil {
				return err
			}
			return held.Answer(c, ui.TableSpec[acctsvc.DelegatedAccess]{
				Noun: "emergency contacts", Columns: emergencyColumns(),
			}, rows)
		}),
	}
	kit.Incoming(c.Flags(), &incoming)
	held.Register(c, "emergency contacts",
		kit.Key[acctsvc.DelegatedAccess]{Name: "created", Less: func(a, b acctsvc.DelegatedAccess) int {
			return kit.Ints(b.Created, a.Created)
		}},
		kit.Key[acctsvc.DelegatedAccess]{Name: "contact", Less: func(a, b acctsvc.DelegatedAccess) int {
			return kit.Fold(a.Contact, b.Contact)
		}},
	)
	return c
}

func emergencyGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: "Show one emergency access",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			da, err := delegatedLookup(c, acctsvc.KindEmergency).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			return kit.Show(c, ui.RecordSpec{
				Object: da,
				Fields: []ui.Field{
					{Label: "Contact", Value: da.Contact, Handle: true},
					{Label: "Direction", Value: string(da.Direction)},
					{Label: "Wait", Value: waitText(da.Wait)},
					{Label: "Status", Value: da.Status, Always: true},
					{Label: "Opens", Value: opensText(da.AccessibleAt)},
					{Label: "Created", Value: units.Time(da.Created)},
					{Label: "ID", Value: da.ID, ID: true},
				},
			})
		}),
	}
}

func emergencyAddCmd() *cobra.Command {
	var wait string
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "add EMAIL",
		Short: "Let somebody into your account in an emergency",
		Long: "Let somebody into your account in an emergency.\n\n" +
			"Your password is asked for. --wait sets how long after a request the\n" +
			"access opens, and defaults to 7d. The contact has to have a Proton account.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			email := c.Args[0]
			delay, err := waitDuration(wait)
			if err != nil {
				return err
			}
			if err := reauth.Supply(c); err != nil {
				return err
			}
			var da acctsvc.DelegatedAccess
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Added, Kind: "emergency contacts", Count: 1, Name: email,
				Detail: "(" + waitText(int(delay/time.Second)) + " wait)",
			}, func() error {
				da, err = c.App.Account.AddDelegatedAccess(c.Ctx, acctsvc.KindEmergency, email, delay)
				if err == nil {
					c.Note("ID %s", da.ID)
				}
				return err
			})
		}),
	}
	c.Flags().StringVar(&wait, "wait", "", "How long after a request the access opens (e.g. 3d); default 7d")
	reauth.Declare(c)
	return c
}

func emergencyUpdateCmd() *cobra.Command {
	var wait string
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "update REF",
		Short: "Change how long an emergency contact waits",
		Long: "Change how long an emergency contact waits.\n\n" +
			"Your password is asked for. --wait is required.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			if wait == "" {
				return kit.Fail("Nothing to change.").Hint("--wait 3d")
			}
			delay, err := waitDuration(wait)
			if err != nil {
				return err
			}
			da, err := delegatedLookup(c, acctsvc.KindEmergency).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			if err := refuseIncoming(da); err != nil {
				return err
			}
			if err := reauth.Supply(c); err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: "emergency contacts", Count: 1, Name: da.Contact,
				IDs: []string{da.ID}, Detail: "- wait is now " + waitText(int(delay/time.Second)),
			}, func() error {
				_, err := c.App.Account.UpdateDelegatedAccess(c.Ctx, da.ID, acctsvc.KindEmergency, da.Contact, delay)
				return err
			})
		}),
	}
	c.Flags().StringVar(&wait, "wait", "", "How long after a request the access opens (e.g. 3d)")
	reauth.Declare(c)
	return c
}

func emergencyRequestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "request REF",
		Short: "Start the wait to get into an account that granted you access",
		Long: "Start the wait to get into an account that granted you access.\n\n" +
			"The access opens once the account's wait runs out, unless it cancels the\n" +
			"request first. `access` is what signs in once it is open.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			da, err := delegatedLookup(c, acctsvc.KindEmergency).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			if err := refuseOutgoing(da); err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Requested, Kind: "emergency access", Count: 1, Name: da.Contact,
				IDs: []string{da.ID}, Detail: "- opens after " + waitText(da.Wait),
			}, func() error {
				return c.App.Account.RequestDelegatedAccess(c.Ctx, da.ID)
			})
		}),
	}
}

func emergencyAccessInCmd() *cobra.Command {
	var as string
	c := &cobra.Command{
		Use:   "access REF",
		Short: "Sign in to an account whose emergency access is open",
		Long: "Sign in to an account whose emergency access is open.\n\n" +
			"--as names the profile to save the account under, which is then an ordinary\n" +
			"profile: `" + kit.Program + " --profile NAME mail list` reads its mail. The\n" +
			"access has to be open first - request it and wait, or have the account grant\n" +
			"it.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			if as == "" {
				return kit.Fail("Name the profile to save the account under.").Hint("--as dads-account")
			}
			target, err := profile.Parse(as)
			if err != nil {
				return err
			}
			if target == c.App.Profile {
				return kit.Fail("--as names the profile you are already acting as; choose another.")
			}
			da, err := delegatedLookup(c, acctsvc.KindEmergency).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			if err := refuseOutgoing(da); err != nil {
				return err
			}
			if !da.Accessible() {
				return kit.Fail("This access is not open yet.").
					Hint(kit.Program + " account settings emergency-access request " + da.ID)
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Accessed, Count: 1, Name: da.Contact,
				Detail: "(profile " + target.String() + ")",
			}, func() error {
				sess, err := c.App.Account.AccessDelegatedAccess(c.Ctx, da)
				if err != nil {
					return err
				}
				if _, err := c.App.AccessInto(c.Ctx, target, sess); err != nil {
					return err
				}
				c.Note("%s account get reads it", kit.Program+" --profile "+target.String())
				return nil
			})
		}),
	}
	c.Flags().StringVar(&as, "as", "", "Profile to save the accessed account under")
	return c
}

func emergencyGrantCmd() *cobra.Command {
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "grant REF",
		Short: "Let a pending request in now, without the wait",
		Long: "Let a pending request in now, without the wait.\n\n" +
			"Your password is asked for. The contact gets access at once, so this is a\n" +
			"decision to make only when you meant them to have it.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			da, err := delegatedLookup(c, acctsvc.KindEmergency).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			if err := refuseIncoming(da); err != nil {
				return err
			}
			if !da.Pending() {
				return kit.Fail("There is no request to grant on this access.")
			}
			if err := reauth.Supply(c); err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Granted, Kind: "emergency access", Count: 1, Name: da.Contact,
				IDs: []string{da.ID}, Detail: "- they can sign in now",
			}, func() error {
				return c.App.Account.GrantDelegatedAccess(c.Ctx, da.ID, da.Contact)
			})
		}),
	}
	reauth.Declare(c)
	return c
}

func emergencyCancelCmd() *cobra.Command {
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "cancel REF",
		Short: "Take back a request in flight",
		Long: "Take back a request in flight.\n\n" +
			"Your password is asked for. On one somebody made against your account, this\n" +
			"denies it. The access itself stays; only the pending request is cancelled.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			da, err := delegatedLookup(c, acctsvc.KindEmergency).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			if !da.Pending() {
				return kit.Fail("There is no request in flight on this access.")
			}
			if err := reauth.Supply(c); err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Cancelled, Kind: "requests", Count: 1, Name: da.Contact,
				IDs: []string{da.ID},
			}, func() error {
				return c.App.Account.CancelDelegatedAccess(c.Ctx, da.ID)
			})
		}),
	}
	reauth.Declare(c)
	return c
}

func emergencyRemoveCmd() *cobra.Command {
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "remove REF...",
		Short: "Remove an emergency contact",
		Long: "Remove an emergency contact.\n\n" +
			"Your password is asked for. Their copy of your keys stops working. A contact\n" +
			"who is also a recovery contact keeps that.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			sel, err := kit.SelectFrom(c, "emergency contacts", emergencyColumns(), delegatedLookup(c, acctsvc.KindEmergency))
			if err != nil {
				return err
			}
			if err := reauth.Supply(c); err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Removed, Kind: "emergency contacts", Count: sel.Len(), IDs: sel.IDs,
			}, func() error {
				for _, id := range sel.IDs {
					if err := c.App.Account.DeleteDelegatedAccess(c.Ctx, id, acctsvc.KindEmergency); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
	reauth.Declare(c)
	return c
}

// ── shared across both trusted-contact collections ──

// delegatedLookup resolves a reference to one delegated access of a kind, across
// both directions.
func delegatedLookup(c *kit.Invocation, kind acctsvc.Kind) *kit.Lookup[acctsvc.DelegatedAccess] {
	return &kit.Lookup[acctsvc.DelegatedAccess]{
		Kind: string(kind) + " access",
		Load: func(ctx context.Context) ([]acctsvc.DelegatedAccess, error) {
			return c.App.Account.DelegatedAccesses(ctx, kind)
		},
		ID:     func(d acctsvc.DelegatedAccess) string { return d.ID },
		Handle: func(d acctsvc.DelegatedAccess) string { return d.Contact },
	}
}

// delegatedRows lists one kind in one direction, which is what a `list` answers.
func delegatedRows(c *kit.Invocation, kind acctsvc.Kind, incoming bool) ([]acctsvc.DelegatedAccess, error) {
	all, err := c.App.Account.DelegatedAccesses(c.Ctx, kind)
	if err != nil {
		return nil, err
	}
	want := acctsvc.Outgoing
	if incoming {
		want = acctsvc.Incoming
	}
	rows := all[:0:0]
	for _, d := range all {
		if d.Direction == want {
			rows = append(rows, d)
		}
	}
	return rows, nil
}

// refuseIncoming stops an owner-only verb acting on an access somebody else
// granted this account.
func refuseIncoming(da acctsvc.DelegatedAccess) error {
	if da.Direction == acctsvc.Incoming {
		return kit.Fail("That access was granted to you, so you cannot change it.").
			Hint("proton account settings " + collectionFor(da.Kind) + " list shows the ones you granted")
	}
	return nil
}

// refuseOutgoing stops a contact-only verb acting on an access this account
// granted somebody else.
func refuseOutgoing(da acctsvc.DelegatedAccess) error {
	if da.Direction == acctsvc.Outgoing {
		return kit.Fail("That is an access you granted, so it is not yours to use.").
			Hint("proton account settings " + collectionFor(da.Kind) + " list --incoming shows the ones granted to you")
	}
	return nil
}

func collectionFor(kind acctsvc.Kind) string {
	if kind == acctsvc.KindRecovery {
		return "recovery-contacts"
	}
	return "emergency-access"
}

// waitText is a wait as a person reads it, and the dash a recovery contact's
// missing wait shows as.
func waitText(seconds int) string {
	if seconds <= 0 {
		return "-"
	}
	return units.Duration(time.Duration(seconds) * time.Second)
}

// opensText is when a pending access opens, or nothing when none is.
func opensText(at int64) string {
	if at == 0 {
		return ""
	}
	return units.Time(at)
}

// waitDuration reads --wait, defaulting to a week and refusing anything that is
// not a positive span.
func waitDuration(wait string) (time.Duration, error) {
	if wait == "" {
		return defaultWait, nil
	}
	d, err := units.ParseDuration(wait)
	if err != nil {
		return 0, kit.Fail("--wait: %v", err).Hint("--wait 3d", "--wait 72h")
	}
	if d <= 0 {
		return 0, kit.Fail("--wait has to be a positive span of time.").Hint("--wait 7d")
	}
	return d, nil
}
