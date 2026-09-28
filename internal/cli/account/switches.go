package account

import (
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

type feature struct {
	name      string
	eligible  bool
	on        bool
	emails    bool
	enforced  bool
	set       func(on bool) error
	setEmails func(on bool) error
}

func (f feature) refuseForPlan() error {
	return kit.Fail("Your plan does not include %s.", f.name)
}

func (f feature) refuseForOrganization() error {
	return kit.Fail("Your organization turns %s on for this account.", f.name)
}

func switchCmd(use, short string, on bool, read func(*kit.Invocation) (feature, error)) *cobra.Command {
	var emails bool
	long := short + ".\n\n--emails turns on the emails it sends about what it finds as well, and\n" +
		"turns it on first if it was off."
	usage := "Also the emails it sends"
	if !on {
		long = short + ".\n\n--emails stops only the emails it sends, and leaves it on."
		usage = "Only the emails it sends"
	}
	c := &cobra.Command{
		Use:   use,
		Short: short,
		Long:  long,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			f, err := read(c)
			if err != nil {
				return err
			}
			if on {
				return switchOn(c, f, emails)
			}
			return switchOff(c, f, emails)
		}),
	}
	c.Flags().BoolVar(&emails, "emails", false, usage)
	return c
}

func switchOn(c *kit.Invocation, f feature, emails bool) error {
	switch {
	case !f.eligible:
		return f.refuseForPlan()
	case emails && f.enforced:
		return f.refuseForOrganization()
	case emails && f.on && f.emails:
		return kit.Fail("%s and its emails are already on.", f.name)
	case !emails && f.on:
		return kit.Fail("%s is already on.", f.name)
	}
	spec := ui.ResultSpec{Action: ui.Enabled, Count: 1, Name: f.name}
	switch {
	case emails && f.on:
		spec.Name = f.name + "'s emails"
	case emails && !f.emails:
		spec.Detail = "and its emails"
	}
	return kit.Mutate(c, spec, func() error {
		if !f.on {
			if err := f.set(true); err != nil {
				return err
			}
		}
		if emails && !f.emails {
			return f.setEmails(true)
		}
		return nil
	})
}

func switchOff(c *kit.Invocation, f feature, emails bool) error {
	switch {
	case f.enforced:
		return f.refuseForOrganization()
	case emails && !f.eligible:
		return f.refuseForPlan()
	case emails && !f.emails:
		return kit.Fail("%s's emails are already off.", f.name)
	case !emails && !f.on:
		return kit.Fail("%s is already off.", f.name)
	}
	if emails {
		return kit.Mutate(c, ui.ResultSpec{Action: ui.Disabled, Count: 1, Name: f.name + "'s emails"},
			func() error { return f.setEmails(false) })
	}
	return kit.Mutate(c, ui.ResultSpec{Action: ui.Disabled, Count: 1, Name: f.name},
		func() error { return f.set(false) })
}

func darkWebMonitoring(c *kit.Invocation) (feature, error) {
	security, err := c.App.Account.Security(c.Ctx)
	if err != nil {
		return feature{}, err
	}
	d := security.DarkWebMonitoring
	return feature{
		name: "Dark Web Monitoring", eligible: d.Eligible, on: d.On, emails: d.Emails,
		set:       func(on bool) error { return c.App.Account.SetDarkWebMonitoring(c.Ctx, on) },
		setEmails: func(on bool) error { return c.App.Account.SetBreachEmails(c.Ctx, on) },
	}, nil
}

func sentinel(c *kit.Invocation) (feature, error) {
	security, err := c.App.Account.Security(c.Ctx)
	if err != nil {
		return feature{}, err
	}
	s := security.Sentinel
	return feature{
		name: "Proton Sentinel", eligible: s.Eligible, on: s.On, emails: s.Emails, enforced: s.Enforced,
		set:       func(on bool) error { return c.App.Account.SetSentinel(c.Ctx, on) },
		setEmails: func(on bool) error { return c.App.Account.SetSentinelEmails(c.Ctx, on) },
	}, nil
}
