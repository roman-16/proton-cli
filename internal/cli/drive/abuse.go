package drive

import (
	"path"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	drivesvc "github.com/roman-16/proton-cli/internal/service/drive"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

// report is what every report of abuse is told on the command line: the kind of
// abuse, where Proton can reach whoever reports it, what they have to add, and
// their word that it is true.
type report struct {
	category  *kit.Enum
	email     string
	message   string
	goodFaith bool
}

// abuseWords is each kind of abuse as a sentence names it.
var abuseWords = map[string]string{
	"child-abuse":             "child sexual abuse material",
	"copyright":               "copyright infringement",
	"malware":                 "malware",
	"non-consensual-intimate": "non-consensual intimate imagery",
	"other":                   "abuse",
	"spam":                    "spam",
	"stolen-data":             "stolen data",
}

func (r *report) register(c *cobra.Command) {
	r.category = &kit.Enum{
		Name: "category", Usage: "What the report is about",
		Values: drivesvc.AbuseCategories,
	}
	r.category.Register(c)
	_ = c.MarkFlagRequired("category")
	c.Flags().StringVar(&r.email, "email", "",
		"Where Proton can reach you about the report; required for copyright and stolen-data")
	c.Flags().StringVar(&r.message, "message", "",
		"What Proton should know; required for copyright and stolen-data")
	c.Flags().BoolVar(&r.goodFaith, "good-faith", false,
		"Confirm, in good faith, that what the report says is correct and complete")
}

// check judges a report from the command line alone.
func (r *report) check(*kit.Invocation) error {
	if !r.goodFaith {
		return kit.Fail("--good-faith is required: it confirms, in good faith, that what the report says is correct and complete.")
	}
	if r.email != "" && !kit.IsAddress(r.email) {
		return kit.Fail("%q is not an email address.", r.email)
	}
	category, err := r.category.Value()
	if err != nil {
		return err
	}
	if category != "copyright" && category != "stolen-data" {
		return nil
	}
	var missing []string
	if r.message == "" {
		missing = append(missing, "--message")
	}
	if r.email == "" {
		missing = append(missing, "--email")
	}
	if len(missing) == 0 {
		return nil
	}
	return kit.Fail("A %s report needs %s.", category, ui.Listing(missing))
}

func (r *report) abuse() drivesvc.Abuse {
	category, _ := r.category.Value()
	return drivesvc.Abuse{Category: category, Email: r.email, Message: r.message}
}

// detail is where the report goes and what it says, for the sentence that
// reports it.
func (r *report) detail() string {
	category, _ := r.category.Value()
	return "to Proton as " + abuseWords[category]
}

// sharedWithYou refuses a report pointed at your own files, which nobody can
// report, before anything is asked of Proton.
func sharedWithYou(t *tree, hint string) kit.Step {
	return func(*kit.Invocation) error {
		if t.named() {
			return nil
		}
		return kit.Fail("Only something shared with you can be reported.").Hint(hint)
	}
}

func itemsAbuseCmd() *cobra.Command {
	var t tree
	var r report
	c := &cobra.Command{
		Use:   "abuse PATH",
		Short: "Report something shared with you to Proton",
		Long: "Report something shared with you to Proton.\n\n" +
			"PATH is inside --shared REF, something somebody shared with you, or inside\n" +
			"--link URL, a public link. Proton receives the key to the whole of what was\n" +
			"shared, not only PATH. Copyright and stolen-data reports need --message and\n" +
			"--email. Nothing withdraws a report, and the item stays where it is.",
		RunE: kit.Run([]kit.Step{sharedWithYou(&t, "--shared REF, or --link URL"), r.check, t.supply},
			func(c *kit.Invocation) error {
				dc, err := t.context(c)
				if err != nil {
					return err
				}
				res, err := c.App.Drive.ResolvePath(c.Ctx, dc, c.Args[0])
				if err != nil {
					return err
				}
				return kit.Mutate(c, ui.ResultSpec{
					Action: ui.Reported, Count: 1, Name: res.Describe(path.Base(c.Args[0])),
					Detail: r.detail(), IDs: []string{res.LinkID},
				}, func() error {
					return c.App.Drive.ReportAbuse(c.Ctx, res, r.abuse())
				})
			}),
	}
	t.registerTheirs(c, reads)
	r.register(c)
	return c
}

func revisionsAbuseCmd() *cobra.Command {
	var t tree
	var r report
	c := &cobra.Command{
		Use:   "abuse PATH REVISION_REF",
		Short: "Report an earlier version of a shared file to Proton",
		Long: "Report an earlier version of a shared file to Proton.\n\n" +
			"PATH is inside --shared REF, something somebody shared with you. Proton\n" +
			"receives the key to the whole of what was shared, not only this version.\n" +
			"Copyright and stolen-data reports need --message and --email. Nothing\n" +
			"withdraws a report.",
		RunE: kit.Run([]kit.Step{sharedWithYou(&t, "--shared REF"), r.check, kit.StepExpand},
			func(c *kit.Invocation) error {
				rev, err := findRevision(c, &t)
				if err != nil {
					return err
				}
				return kit.Mutate(c, ui.ResultSpec{
					Action: ui.Reported, Kind: "revisions", Count: 1,
					Name: units.Time(rev.CreateTime), Detail: "of " + rev.File + " " + r.detail(),
					IDs: []string{rev.ID},
				}, func() error {
					return c.App.Drive.ReportRevisionAbuse(c.Ctx, rev, r.abuse())
				})
			}),
	}
	t.registerTheirs(c, manages)
	r.register(c)
	return c
}

func invitationsAbuseCmd() *cobra.Command {
	var r report
	c := &cobra.Command{
		Use:   "abuse REF",
		Short: "Report what an invitation offers to Proton",
		Long: "Report what an invitation offers to Proton, without accepting it.\n\n" +
			"Proton receives the key to the whole of what is offered. Copyright and\n" +
			"stolen-data reports need --message and --email. Nothing withdraws a report,\n" +
			"and the invitation stays until you accept or decline it.",
		RunE: kit.Run([]kit.Step{r.check, kit.StepExpand}, func(c *kit.Invocation) error {
			inv, err := c.App.Drive.GetInvitation(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			name := inv.Name
			if name == "" {
				name = c.Mention(inv.InvitationID)
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Reported, Count: 1, Name: name,
				Detail: r.detail(), IDs: []string{inv.InvitationID},
			}, func() error {
				return c.App.Drive.ReportInvitationAbuse(c.Ctx, inv.InvitationID, r.abuse())
			})
		}),
	}
	r.register(c)
	return c
}
