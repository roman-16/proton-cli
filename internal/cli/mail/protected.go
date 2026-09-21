package mail

import (
	"fmt"
	"path/filepath"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/errs"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

// A message sent to an address outside Proton is the one thing under `mail`
// that is in no mailbox. It sits behind a link, opens to the password its
// sender chose, and is reached with nobody signed in - so it is named by that
// link rather than by a reference, and it is its own collection rather than a
// message of yours that happens to be somewhere else.

func protectedCmd() *cobra.Command {
	c := &cobra.Command{Use: "protected", Short: "Messages somebody sent you behind a password"}
	c.AddCommand(protectedAttachmentsCmd(), protectedGetCmd(), protectedReplyCmd())
	return c
}

// protectedPassword is the flag group every command here shares: the password
// is what opens the message, so none of them works without it.
func protectedPassword(c *cobra.Command) *kit.Password {
	p := kit.EOPasswordToOpen()
	p.Declare(c)
	return p
}

// openProtected opens the message the command addresses, which is the first
// thing each of them does.
//
// What follows is authorised by the link and by the password, so a preview of
// it is as true signed out as signed in - which is what the invocation is told
// here, rather than each command remembering to.
func openProtected(c *kit.Invocation, password *kit.Password) (*mailsvc.EOMessage, error) {
	secret, err := password.Value()
	if err != nil {
		return nil, err
	}
	m, err := c.App.Mail.OpenEO(c.Ctx, c.Args[0], secret)
	if err != nil {
		return nil, err
	}
	c.AuthorisedByALink()
	return m, nil
}

// protectedElsewhere turns such a link, typed at the mailbox, into a pointer at
// the command that opens it.
//
// It answers for a whole link only. A bare id is the shape every reference in
// the mailbox has, so reading one as a link would refuse to act on messages of
// your own.
func protectedElsewhere(reference string) error {
	if !mailsvc.IsEOLink(reference) {
		return nil
	}
	return kit.Fail("That is a password-protected message somebody sent you, not one in your mailbox.").
		Hint(fmt.Sprintf("%s mail protected get '%s' --eo-password-file FILE", kit.Program, reference)).
		Exit(3)
}

func protectedGetCmd() *cobra.Command {
	var bodyOnly, stripQuotes, includeInline bool
	render := bodyRendering()
	c := &cobra.Command{
		Use:   "get LINK",
		Short: "Show a password-protected message, decrypted",
		Long: "Show a password-protected message, decrypted.\n\n" +
			"LINK is the address you were sent or the id in it, and the password is the\n" +
			"one whoever sent it gave you rather than a Proton password. Neither this nor\n" +
			"anything else under `protected` needs an account.\n\n" +
			"Such a message is gone 28 days after it was sent, and the Expires line says\n" +
			"when. A Replies line counts the answers already sent from behind the link.",
	}
	password := protectedPassword(c)
	c.RunE = kit.Run([]kit.Step{password.Supply}, func(c *kit.Invocation) error {
		shape, err := render.Value()
		if err != nil {
			return err
		}
		m, err := openProtected(c, password)
		if err != nil {
			return err
		}
		return kit.Read(c, ui.DocumentSpec{
			Object: m,
			// Asking for html or raw means asking for the body as it is, so the
			// header block and attachment list would only get in the way.
			BodyOnly: bodyOnly || shape != "text",
			Parts: []ui.Part{
				bodyPart(&m.Full, protectedHeader(m), shape, stripQuotes, includeInline),
			},
		})
	})
	render.Register(c)
	c.Flags().BoolVar(&bodyOnly, "body-only", false, "Emit only the body, with no headers or attachment list")
	c.Flags().BoolVar(&stripQuotes, "strip-quotes", false, "Drop quoted reply blocks from the body")
	c.Flags().BoolVar(&includeInline, "include-inline", false, "List inline attachments too, such as signature graphics")
	return c
}

// protectedHeader is the block above the body. There is no ID, because the link
// is the only name the message has, and no signature verdict, because Proton
// seals such a message to a password rather than signing it.
func protectedHeader(m *mailsvc.EOMessage) []ui.Field {
	fields := []ui.Field{
		{Label: "Subject", Value: m.Subject, Handle: true},
		{Label: "From", Value: addressLine(m.Sender)},
	}
	for _, group := range []struct {
		label string
		list  []map[string]any
	}{{"To", m.ToList}, {"Cc", m.CCList}} {
		for _, a := range group.list {
			fields = append(fields, ui.Field{Label: group.label, Value: addressLine(a)})
		}
	}
	fields = append(fields, ui.Field{Label: "Date", Value: units.Time(m.Time)})
	if m.Expires > 0 {
		fields = append(fields, ui.Field{Label: "Expires", Value: units.Time(m.Expires)})
	}
	if m.Replies > 0 {
		fields = append(fields, ui.Field{
			Label: "Replies", Value: fmt.Sprintf("%d of %d", m.Replies, mailsvc.EOMaxReplies),
		})
	}
	return fields
}

func protectedReplyCmd() *cobra.Command {
	var f composeFlags
	var noQuote bool
	c := &cobra.Command{
		Use:   "reply LINK",
		Short: "Reply to a password-protected message",
		Long: "Reply to a password-protected message.\n\n" +
			"The answer goes to whoever sent it and nobody else - the link knows no other\n" +
			"address - sealed to their key, with the original quoted below your text.\n\n" +
			"Proton takes five answers from behind one link. --eo-password-file - claims\n" +
			"standard input, so it cannot be combined with --body -.",
	}
	password := protectedPassword(c)
	c.RunE = kit.Run([]kit.Step{password.Supply}, func(c *kit.Invocation) error {
		body, err := f.resolvedBody(c)
		if err != nil {
			return err
		}
		atts, err := f.localAttachments()
		if err != nil {
			return err
		}
		m, err := openProtected(c, password)
		if err != nil {
			return err
		}
		spec := mailsvc.EOReplySpec{Body: body, Attach: atts, NoQuote: noQuote}
		if c.Changed("html") {
			spec.HTML = &f.html
		}
		return kit.Mutate(c, ui.ResultSpec{
			Action: ui.Sent, Kind: "messages", Count: 1, Name: m.EOSubject(),
		}, func() error { return c.App.Mail.EOReply(c.Ctx, m, spec) })
	})
	c.Flags().StringVar(&f.body, "body", "", "Your text, placed above the quoted original (- reads stdin)")
	c.Flags().BoolVar(&f.html, "html", false, "Compose in HTML (default: match the original)")
	c.Flags().StringArrayVar(&f.attach, "attach", nil, "File to attach (repeatable)")
	c.Flags().BoolVar(&noQuote, "no-quote", false, "Do not quote the original message")
	return c
}

func protectedAttachmentsCmd() *cobra.Command {
	c := &cobra.Command{Use: "attachments", Short: "Files attached to a password-protected message"}
	c.AddCommand(protectedAttachmentsDownloadCmd(), protectedAttachmentsListCmd())
	return c
}

func protectedAttachmentsListCmd() *cobra.Command {
	var includeInline bool
	c := &cobra.Command{
		Use:   "list LINK",
		Short: "List a password-protected message's attachments",
	}
	password := protectedPassword(c)
	c.RunE = kit.Run([]kit.Step{password.Supply}, func(c *kit.Invocation) error {
		m, err := openProtected(c, password)
		if err != nil {
			return err
		}
		return kit.List(c, attachmentTableSpec(includeInline), protectedAttachments(m, includeInline))
	})
	c.Flags().BoolVar(&includeInline, "include-inline", false, "Include inline attachments, such as signature graphics")
	return c
}

func protectedAttachmentsDownloadCmd() *cobra.Command {
	var dest kit.Destination
	var includeInline bool
	c := &cobra.Command{
		Use:   "download LINK [ATTACHMENT_REF]",
		Short: "Download and decrypt attachments",
		Long: "Download and decrypt attachments.\n\n" +
			"Naming an attachment downloads that one; naming none downloads them all.\n" +
			"Existing files are never overwritten silently: a collision becomes\n" +
			"\"file (2).pdf\" unless --force says otherwise.",
	}
	password := protectedPassword(c)
	c.RunE = kit.Run([]kit.Step{password.Supply}, func(c *kit.Invocation) error {
		one := len(c.Args) == 2
		if err := dest.Validate(one); err != nil {
			return err
		}
		m, err := openProtected(c, password)
		if err != nil {
			return err
		}
		if one {
			return downloadProtectedOne(c, m, c.Args[1], &dest)
		}
		return downloadProtectedAll(c, m, &dest, includeInline)
	})
	dest.Register(c)
	c.Flags().BoolVar(&includeInline, "include-inline", false, "Include inline attachments when downloading them all")
	return c
}

func downloadProtectedOne(c *kit.Invocation, m *mailsvc.EOMessage, reference string, dest *kit.Destination) error {
	data, name, err := c.App.Mail.EOAttachment(c.Ctx, m, reference)
	if err != nil {
		return err
	}
	written, err := dest.Write(c, name, data)
	if err != nil {
		return err
	}
	if written == "" {
		return nil // streamed to stdout
	}
	return kit.Mutate(c, ui.ResultSpec{
		Action: ui.Downloaded, Count: 1, Name: filepath.Base(written),
		Detail: "to " + written,
	}, func() error { return nil })
}

func downloadProtectedAll(c *kit.Invocation, m *mailsvc.EOMessage, dest *kit.Destination, includeInline bool) error {
	atts := protectedAttachments(m, includeInline)
	if len(atts) == 0 {
		return kit.Mutate(c, ui.ResultSpec{Action: ui.Downloaded, Kind: "attachments", Count: 0},
			func() error { return nil })
	}
	return kit.Mutate(c, ui.ResultSpec{
		Action: ui.Downloaded, Kind: "attachments", Count: len(atts),
		Detail: "to " + dest.Describe(),
	}, func() error {
		for _, at := range atts {
			data, _, err := c.App.Mail.EOAttachment(c.Ctx, m, at.ID)
			if err != nil {
				return errs.Naming(at.Name, err)
			}
			if _, err := dest.Write(c, at.Name, data); err != nil {
				return err
			}
		}
		return nil
	})
}

// protectedAttachments is what the message carries, less the inline images the
// body embeds unless those were asked for.
func protectedAttachments(m *mailsvc.EOMessage, includeInline bool) []mailsvc.Attachment {
	if includeInline {
		return m.Attachments
	}
	return mailsvc.FilterInline(m.Attachments)
}
