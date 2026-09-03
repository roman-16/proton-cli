package mail

import (
	"context"
	"strings"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

// A draft is a message, so `mail messages get` and the organising verbs already
// work on one. This tree holds only what is specific to a message that has not
// gone out - writing it, changing it, sending it - plus a list, because a
// reference here resolves within Drafts only, so updating "Report" can never reach
// something already sent.

func draftsCmd() *cobra.Command {
	c := &cobra.Command{Use: "drafts", Short: "Messages not yet sent"}
	c.AddCommand(draftsListCmd(), draftsCreateCmd(), draftsUpdateCmd(), draftsSendCmd(), draftsDeleteCmd())
	return c
}

func draftsListCmd() *cobra.Command {
	var page, pageSize int
	c := &cobra.Command{
		Use:   "list",
		Short: "List drafts",
		Args:  cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			msgs, total, err := c.App.Mail.DraftsList(c.Ctx, page, pageSize)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[mailsvc.Message]{
				Noun: "drafts", Columns: draftColumns(),
				Total: total, Page: page, PageSize: pageSize,
			}, msgs)
		}),
	}
	c.Flags().IntVar(&page, "page", 0, "Which page of results, counting from zero")
	c.Flags().IntVar(&pageSize, "page-size", 25, "How many drafts per page")
	return c
}

func draftsCreateCmd() *cobra.Command {
	var f composeFlags
	c := &cobra.Command{
		Use:   "create",
		Short: "Save a draft without sending it",
		Args:  cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			content, err := f.content(c)
			if err != nil {
				return err
			}
			return kit.Create(c, ui.ResultSpec{
				Action: ui.Created, Kind: "drafts", Name: content.Subject,
			}, func() (string, error) {
				d, err := c.App.Mail.DraftCreate(c.Ctx, content)
				if err != nil {
					return "", err
				}
				return d.ID, nil
			})
		}),
	}
	f.registerRecipients(c)
	f.registerBody(c)
	f.registerAttachments(c)
	f.registerIdentity(c)
	f.registerEML(c)
	return c
}

func draftsUpdateCmd() *cobra.Command {
	var f composeFlags
	c := &cobra.Command{
		Use:   "update REF",
		Short: "Change a draft's recipients, subject, body or attachments",
		Long: "Change a draft. Only what you pass is replaced; everything else is kept.\n\n" +
			"--to, --cc and --bcc replace the whole list rather than adding to it. --attach\n" +
			"adds files and --detach removes one by name or ID.",
		Args: cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			id, err := c.App.Mail.ResolveDraft(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			draft, err := c.App.Mail.DraftLoad(c.Ctx, id)
			if err != nil {
				return err
			}
			content, err := f.applyTo(c, draft)
			if err != nil {
				return err
			}
			// Resolve every --detach before changing anything, so a typo cannot
			// leave the draft half-edited.
			detach := make([]string, 0, len(f.detach))
			for _, spec := range f.detach {
				attID, err := matchDraftAttachment(draft, spec)
				if err != nil {
					return err
				}
				detach = append(detach, attID)
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: "drafts", Count: 1,
				Name: content.Subject, IDs: []string{id},
			}, func() error {
				for _, attID := range detach {
					if err := c.App.Mail.DraftDetach(c.Ctx, id, attID); err != nil {
						return err
					}
				}
				_, err := c.App.Mail.DraftUpdate(c.Ctx, id, content)
				return err
			})
		}),
	}
	f.registerRecipients(c)
	f.registerBody(c)
	c.Flags().Lookup("html").Usage = "Switch the draft to text/html"
	c.Flags().BoolVar(&f.plain, "plain", false, "Switch the draft to text/plain")
	f.registerAttachments(c)
	c.Flags().StringArrayVar(&f.detach, "detach", nil, "Remove an attachment by name or ID (repeatable)")
	f.registerIdentity(c)
	return c
}

// matchDraftAttachment resolves a --detach value against the draft's own
// attachments, naming what it does have when nothing matches.
func matchDraftAttachment(draft *mailsvc.Draft, spec string) (string, error) {
	var names []string
	for _, a := range draft.AttachmentList() {
		if a.ID == spec || strings.EqualFold(a.Name, spec) {
			return a.ID, nil
		}
		names = append(names, a.Name)
	}
	if len(names) == 0 {
		return "", kit.Fail("That draft has no attachments.").Exit(3)
	}
	return "", kit.Fail("That draft has no attachment called %q.", spec).
		Hint("it has: " + strings.Join(names, ", ")).Exit(3)
}

func draftsSendCmd() *cobra.Command {
	var d deliveryFlags
	c := &cobra.Command{
		Use:   "send REF",
		Short: "Send a draft as it stands",
		Long: "Send a draft as it stands.\n\n" +
			"No signature is appended: the draft already holds whatever signature it was\n" +
			"created with.",
		Args: cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{d.supply, kit.StepExpand}, func(c *kit.Invocation) error {
			del, at, err := d.delivery()
			if err != nil {
				return err
			}
			id, err := c.App.Mail.ResolveDraft(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			draft, err := c.App.Mail.DraftLoad(c.Ctx, id)
			if err != nil {
				return err
			}
			if !draft.Content.HasRecipients() {
				return kit.Fail("That draft has no recipients.").
					Hint("proton mail drafts update " + c.Args[0] + " --to someone@example.com")
			}
			action, detail := ui.Sent, ""
			if !at.IsZero() {
				action = ui.Scheduled
				detail = "for " + at.Format("2006-01-02 15:04 -07:00")
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: action, Kind: "messages", Count: 1,
				Name: draft.Content.Subject, Detail: detail,
				IDs: []string{id}, EmitID: true,
			}, func() error {
				if err := withPinnedKeys(c, &del, draft.Content); err != nil {
					return err
				}
				return c.App.Mail.SendDraft(c.Ctx, draft, del)
			})
		}),
	}
	d.register(c)
	return c
}

func draftsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete REF...",
		Short: "Delete drafts",
		Args:  cobra.MinimumNArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			sel, err := kit.Select(c, kit.Selector[mailsvc.Message]{
				Noun:    "drafts",
				Columns: draftColumns(),
				IDOf:    func(m mailsvc.Message) string { return m.ID },
				ByRef: func(ctx context.Context, ref string) (mailsvc.Message, error) {
					id, err := c.App.Mail.ResolveDraft(ctx, ref)
					if err != nil {
						return mailsvc.Message{}, err
					}
					m, err := c.App.Mail.FindMessage(ctx, id)
					return m, err
				},
			})
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Deleted, Kind: "drafts", Count: sel.Len(), IDs: sel.IDs,
				Preview: sel.Preview(),
			}, func() error { return c.App.Mail.Delete(c.Ctx, sel.IDs) })
		}),
	}
}
