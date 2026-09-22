package mail

import (
	"strconv"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/mailtext"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

func messagesCmd() *cobra.Command {
	c := &cobra.Command{Use: "messages", Short: "Individual messages"}
	c.AddCommand(
		listCmd(), watchCmd(), getCmd(), sendCmd(), replyCmd(), forwardCmd(), exportCmd(),
		emptyCmd(), updateCmd(), unsubscribeCmd(), receiptCmd(),
		moveCmd(), labelCmd(), unlabelCmd(), starCmd(), unstarCmd(), markCmd(),
		trashCmd(), deleteCmd(), unscheduleCmd(), attachmentsCmd(),
	)
	return c
}

// ── reading ──

// list is where a set of messages is worked out.
//
// Paging a folder and asking a question about its contents are one command,
// because the selection either produces is the same one the bulk verbs take.
// Where the question is answered differs: a folder and a page are Proton's to
// answer, and anything about what a message says is answered by the copy on
// this machine when there is one, since Proton cannot read a body.
func listCmd() *cobra.Command {
	var f filters
	c := &cobra.Command{
		Use:   "list",
		Short: "List messages in a folder",
		Long: "List messages in a folder.\n\n" +
			"Takes the same filters as trash, move, label and export, so you can preview\n" +
			"a selection here before acting on it. A text filter goes through Proton's\n" +
			"own index, which lags a change by a few seconds and does not cover bodies,\n" +
			"or through the copy `index create mail` builds, which does.\n\n" +
			"Looks in the inbox unless told otherwise. Use --folder all to search\n" +
			"everything.\n\n" +
			"Newest first, or largest first with --sort size; --desc reverses either.\n" +
			"Ordering by size is Proton's to do, so it is not answered from a local\n" +
			"index and does not search bodies.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			opts, err := f.list(c.Ctx, c)
			if err != nil {
				return err
			}
			msgs, total, cover, err := c.App.Mail.Search(c.Ctx, opts)
			if err != nil {
				return err
			}
			if err := kit.List(c, ui.TableSpec[mailsvc.Message]{
				Noun: "messages", Columns: orderedColumns(opts, messageColumns(),
					func(m mailsvc.Message) int64 { return m.Size }),
				Total: f.total(total, len(msgs)), Page: opts.Page, PageSize: opts.PageSize,
				Filtered: f.narrowed(),
			}, msgs); err != nil {
				return err
			}
			// What the answer did not cover is said after the answer, so the rows
			// and the count come first and the caveat reads as being about them.
			if len(msgs) == 0 {
				addressOnlyHint(c, cover, opts)
			}
			shortIndex(c, cover, opts)
			return nil
		}),
	}
	f.registerNarrowing(c, "inbox")
	f.registerPaging(c, "messages")
	f.registerOrder(c)
	return c
}

func getCmd() *cobra.Command {
	var bodyOnly, stripQuotes, includeInline bool
	render := bodyRendering()
	c := &cobra.Command{
		Use:   "get REF",
		Short: "Show one message, decrypted",
		Long: "Show one message, decrypted.\n\n" +
			"A message Proton flagged carries a Flagged line reading phishing or\n" +
			"suspicious; `mark legitimate` overrules it. DMARC: failed means the\n" +
			"sender's domain did not vouch for the message, so the address it claims\n" +
			"to come from may not be the address it came from.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			shape, err := render.Value()
			if err != nil {
				return err
			}
			if err := protectedElsewhere(c.Args[0]); err != nil {
				return err
			}
			id, err := c.App.Mail.Resolve(c.Ctx, c.Args[0])
			if err != nil {
				return wrongTable(err, "get")
			}
			msg, err := c.App.Mail.Read(c.Ctx, id)
			if err != nil {
				return wrongTable(err, "get")
			}
			if err := kit.Read(c, ui.DocumentSpec{
				Object: msg,
				// Asking for html or raw means asking for the body as it is, so the
				// header block and attachment list would only get in the way.
				BodyOnly: bodyOnly || shape != "text",
				Parts:    []ui.Part{messagePart(msg, shape, stripQuotes, includeInline)},
			}); err != nil {
				return err
			}
			overruleHint(c, msg)
			receiptHint(c, msg)
			return nil
		}),
	}
	render.Register(c)
	c.Flags().BoolVar(&bodyOnly, "body-only", false, "Emit only the body, with no headers or attachment list")
	c.Flags().BoolVar(&stripQuotes, "strip-quotes", false, "Drop quoted reply blocks from the body")
	c.Flags().BoolVar(&includeInline, "include-inline", false, "List inline attachments too, such as signature graphics")
	return c
}

// messagePart turns a decrypted message into the header block, body and
// attachment table that make up one part of a document. A single message is one
// part; a thread is several.
func messagePart(msg *mailsvc.Full, shape string, stripQuotes, includeInline bool) ui.Part {
	return bodyPart(msg, messageHeader(msg), shape, stripQuotes, includeInline)
}

// bodyPart is a message laid out for reading: the header block it was given,
// the body in the shape that was asked for, and what it carries underneath.
//
// The header is passed in because what belongs above a body depends on where
// the message came from - one in the mailbox has an ID and a signature verdict,
// and one behind a password has neither.
func bodyPart(msg *mailsvc.Full, header []ui.Field, shape string, stripQuotes, includeInline bool) ui.Part {
	body := msg.Body
	if stripQuotes {
		if mailtext.IsHTML(msg.MIMEType) {
			body = mailtext.StripHTMLQuotes(body)
		} else {
			body = mailtext.StripPlaintextQuotes(body)
		}
	}
	if shape == "text" && mailtext.IsHTML(msg.MIMEType) {
		body = mailtext.HTMLToText(body)
	}

	part := ui.Part{Header: header, Body: body}
	visible := msg.Attachments
	if !includeInline {
		visible = mailsvc.FilterInline(visible)
	}
	if len(visible) > 0 {
		part.TrailerTitle = "Attachments"
		part.Trailer = func(u *ui.UI) error {
			return ui.Table(u, attachmentTableSpec(includeInline), visible)
		}
	}
	return part
}

// messageHeader is the block above a message body: the date, the full recipient
// lists, and what Proton made of the message.
func messageHeader(msg *mailsvc.Full) []ui.Field {
	fields := []ui.Field{
		{Label: "Subject", Value: msg.Subject, Handle: true},
		{Label: "From", Value: addressLine(msg.Sender)},
	}
	for _, group := range []struct {
		label string
		list  []map[string]any
	}{{"To", msg.ToList}, {"Cc", msg.CCList}, {"Bcc", msg.BCCList}} {
		for _, a := range group.list {
			fields = append(fields, ui.Field{Label: group.label, Value: addressLine(a)})
		}
	}
	fields = append(fields, ui.Field{Label: "Date", Value: units.Time(msg.Time)})
	if msg.Expires > 0 {
		fields = append(fields, ui.Field{Label: "Expires", Value: units.Time(msg.Expires)})
	}
	if msg.ReceiptRequested {
		fields = append(fields, ui.Field{Label: "Receipt", Value: receiptLine(msg)})
	}
	fields = append(fields, kit.SignatureField(string(msg.Signature)))
	if msg.DMARCFailed {
		fields = append(fields, ui.Field{Label: "DMARC", Value: "failed", Role: ui.Danger})
	}
	if msg.SpamFlagged() {
		fields = append(fields, ui.Field{Label: "Flagged", Value: verdictLine(msg), Role: verdictRole(msg)})
	}
	return append(fields, ui.Field{Label: "ID", Value: msg.ID, ID: true})
}

// verdictLine names what Proton's filters concluded, and says when the reader
// has already overruled it - which is what stops a message somebody has cleared
// from looking exactly like one nobody has looked at.
func verdictLine(msg *mailsvc.Full) string {
	var found []string
	if msg.Phishing {
		found = append(found, "phishing")
	}
	if msg.Suspicious {
		found = append(found, "suspicious")
	}
	line := strings.Join(found, ", ")
	if msg.MarkedLegitimate {
		line += " (marked legitimate)"
	}
	return line
}

func verdictRole(msg *mailsvc.Full) ui.Role {
	if msg.MarkedLegitimate {
		return ui.Plain
	}
	return ui.Danger
}

// receiptLine says where a message stands with read receipts: a request that is
// still open, or one that has been answered.
func receiptLine(msg *mailsvc.Full) string {
	if msg.ReceiptSent {
		return "sent"
	}
	return "requested"
}

// receiptHint offers the answer beside the message that asked for it.
//
// A message this account sent carries the request it made rather than one to
// answer, so it gets the line above and nothing to act on.
func receiptHint(c *kit.Invocation, msg *mailsvc.Full) {
	if !msg.ReceiptDue {
		return
	}
	c.UI().Hint("The sender asked to be told when you read this: " +
		kit.Program + " mail messages receipt " + ui.Short(msg.ID, c.UI().ShortIDs()))
}

// overruleHint offers the way out of a wrong verdict, beside the message it is
// about.
//
// A reader who has just read the thing and can see it is their accountant is the
// one person able to settle it, and the line is on the commentary stream, so a
// body being piped somewhere is still only the body.
func overruleHint(c *kit.Invocation, msg *mailsvc.Full) {
	if !msg.SpamFlagged() || msg.MarkedLegitimate {
		return
	}
	c.UI().Hint("Proton flagged this message. If it is legitimate: " +
		kit.Program + " mail messages mark legitimate " + ui.Short(msg.ID, c.UI().ShortIDs()))
}

// addressLine renders one recipient the way mail does: a display name with the
// address in angle brackets, or the bare address when there is no name.
func addressLine(a map[string]any) string {
	addr, _ := a["Address"].(string)
	name, _ := a["Name"].(string)
	if name == "" || name == addr {
		return addr
	}
	return name + " <" + addr + ">"
}

// ── organising ──

func moveCmd() *cobra.Command {
	var f filters
	var into string
	c := &cobra.Command{
		Use:   "move [REF...]",
		Short: "Move messages to a folder",
		Long: "Move messages to a folder.\n\n" +
			"A message is in exactly one folder, so this takes it out of the one it was\n" +
			"in. To tag it while leaving it where it is, use `label` instead.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			dest, err := c.App.Mail.ResolveFolderTarget(c.Ctx, into)
			if err != nil {
				return err
			}
			sel, err := selectMessages(c, &f)
			if err != nil {
				return wrongTable(err, "move")
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Moved, Kind: "messages", Count: sel.Len(), IDs: sel.IDs,
				Detail: "to " + dest.Name, Preview: sel.Preview(),
			}, func() error {
				return c.App.Mail.Label(c.Ctx, sel.IDs, dest.ID)
			})
		}),
	}
	c.Flags().StringVar(&into, "into", "", "Destination folder, by name or ID")
	_ = c.MarkFlagRequired("into")
	registerFolderCompletion(c, "into")
	f.register(c)
	return c
}

func labelCmd() *cobra.Command {
	return labelVerb("label", "Attach a label to messages", ui.Labelled,
		func(c *kit.Invocation, ids []string, labelID string) error {
			return c.App.Mail.Label(c.Ctx, ids, labelID)
		})
}

func unlabelCmd() *cobra.Command {
	return labelVerb("unlabel", "Detach a label from messages", ui.Unlabelled,
		func(c *kit.Invocation, ids []string, labelID string) error {
			return c.App.Mail.Unlabel(c.Ctx, ids, labelID)
		})
}

// labelVerb builds `label` and `unlabel`.
//
// Attaching a label and moving a message are separate actions, as they are in the
// web client and in the API's own /label and /unlabel endpoints. Folding them
// together would mean `move` could take a label and report a move that never
// happened.
func labelVerb(use, short string, action ui.Action, apply func(*kit.Invocation, []string, string) error) *cobra.Command {
	var f filters
	var label string
	c := &cobra.Command{
		Use:   use + " [REF...]",
		Short: short,
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			target, err := c.App.Mail.ResolveLabelTarget(c.Ctx, label)
			if err != nil {
				return err
			}
			sel, err := selectMessages(c, &f)
			if err != nil {
				return wrongTable(err, use)
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: action, Kind: "messages", Count: sel.Len(), IDs: sel.IDs,
				Detail: quoted(target.Name), Preview: sel.Preview(),
			}, func() error {
				return apply(c, sel.IDs, target.ID)
			})
		}),
	}
	c.Flags().StringVar(&label, "label", "", "The label to attach or detach, by name or ID")
	_ = c.MarkFlagRequired("label")
	f.register(c)
	return c
}

func starCmd() *cobra.Command {
	return starVerb("star", "Star messages", ui.Starred,
		func(c *kit.Invocation, ids []string) error {
			return c.App.Mail.Label(c.Ctx, ids, mailsvc.StarredLabelID)
		})
}

func unstarCmd() *cobra.Command {
	return starVerb("unstar", "Remove the star from messages", ui.Unstarred,
		func(c *kit.Invocation, ids []string) error {
			return c.App.Mail.Unlabel(c.Ctx, ids, mailsvc.StarredLabelID)
		})
}

// starVerb builds `star` and `unstar`, which are `label` and `unlabel` with the
// one label Proton gives a button of its own.
func starVerb(use, short string, action ui.Action, apply func(*kit.Invocation, []string) error) *cobra.Command {
	var f filters
	c := &cobra.Command{
		Use:   use + " [REF...]",
		Short: short,
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			sel, err := selectMessages(c, &f)
			if err != nil {
				return wrongTable(err, use)
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: action, Kind: "messages", Count: sel.Len(), IDs: sel.IDs,
				Preview: sel.Preview(),
			}, func() error { return apply(c, sel.IDs) })
		}),
	}
	f.register(c)
	return c
}

// markCmd is a group with real subcommands rather than a verb taking a verb as
// an argument. `mark read` therefore has its own help and completion, and no
// hand-written check that the word was one of a few.
func markCmd() *cobra.Command {
	c := &cobra.Command{Use: "mark", Short: "Set what messages count as"}
	c.AddCommand(
		legitimateCmd(), phishingCmd(),
		markVerb("read", "Mark messages as read", ui.MarkedRead,
			func(c *kit.Invocation, ids []string) error { return c.App.Mail.MarkRead(c.Ctx, ids) }),
		markVerb("unread", "Mark messages as unread", ui.MarkedUnread,
			func(c *kit.Invocation, ids []string) error { return c.App.Mail.MarkUnread(c.Ctx, ids) }),
	)
	return c
}

func legitimateCmd() *cobra.Command {
	return verdictVerb("legitimate", "Mark a message Proton flagged as legitimate",
		"Mark a message Proton flagged as legitimate.\n\n"+
			"This overrules the phishing or suspicious verdict on that message alone.\n"+
			"To let a sender through from now on, use `settings senders allow`.",
		ui.MarkedLegitimate, "as legitimate",
		func(c *kit.Invocation, ids []string) error {
			return c.App.Mail.MarkLegitimate(c.Ctx, ids)
		})
}

func phishingCmd() *cobra.Command {
	return verdictVerb("phishing", "Report a message to Proton as phishing",
		"Report a message to Proton as phishing.\n\n"+
			"Proton receives the message decrypted, body included, and the message\n"+
			"moves to spam. To keep a sender out without reporting anything, use\n"+
			"`settings senders block`.",
		ui.Reported, "to Proton as phishing",
		func(c *kit.Invocation, ids []string) error {
			for _, id := range ids {
				if err := c.App.Mail.ReportPhishing(c.Ctx, id); err != nil {
					return err
				}
			}
			return nil
		})
}

// verdictVerb builds the two verdicts a reader can give a message, against
// Proton's own.
//
// Both name their messages and take no filters: each is a judgement on something
// that was read, rather than on everything matching a pattern. The standing
// decision about a sender is `settings senders`, which is where a rule belongs.
func verdictVerb(use, short, long string, action ui.Action, detail string,
	apply func(*kit.Invocation, []string) error) *cobra.Command {
	return &cobra.Command{
		Use:   use + " REF...",
		Short: short,
		Long:  long,
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			sel, err := selectMessages(c, &filters{})
			if err != nil {
				return wrongTable(err, "mark "+use)
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: action, Kind: "messages", Count: sel.Len(), IDs: sel.IDs,
				Name:    kit.Sole(sel.Rows, func(m mailsvc.Message) string { return m.Subject }),
				Detail:  detail,
				Preview: sel.Preview(),
			}, func() error { return apply(c, sel.IDs) })
		}),
	}
}

func markVerb(use, short string, action ui.Action, apply func(*kit.Invocation, []string) error) *cobra.Command {
	var f filters
	c := &cobra.Command{
		Use:   use + " [REF...]",
		Short: short,
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			sel, err := selectMessages(c, &f)
			if err != nil {
				return wrongTable(err, "mark "+use)
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: action, Kind: "messages", Count: sel.Len(), IDs: sel.IDs,
				Detail: "as " + use, Preview: sel.Preview(),
			}, func() error { return apply(c, sel.IDs) })
		}),
	}
	f.register(c)
	return c
}

func trashCmd() *cobra.Command {
	var f filters
	c := &cobra.Command{
		Use:   "trash [REF...]",
		Short: "Move messages to the trash",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			sel, err := selectMessages(c, &f)
			if err != nil {
				return wrongTable(err, "trash")
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Trashed, Kind: "messages", Count: sel.Len(), IDs: sel.IDs,
				Detail: "to trash", Preview: sel.Preview(),
			}, func() error { return c.App.Mail.Trash(c.Ctx, sel.IDs) })
		}),
	}
	f.register(c)
	return c
}

func deleteCmd() *cobra.Command {
	var f filters
	c := &cobra.Command{
		Use:   "delete [REF...]",
		Short: "Delete messages permanently",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			sel, err := selectMessages(c, &f)
			if err != nil {
				return wrongTable(err, "delete")
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Deleted, Kind: "messages", Count: sel.Len(), IDs: sel.IDs,
				Preview: sel.Preview(),
			}, func() error { return c.App.Mail.Delete(c.Ctx, sel.IDs) })
		}),
	}
	f.register(c)
	return c
}

func unscheduleCmd() *cobra.Command {
	var all bool
	c := &cobra.Command{
		Use:   "unschedule [REF...]",
		Short: "Cancel a scheduled send, returning the message to drafts",
		Long: "Cancel a scheduled send.\n\n" +
			"The message leaves the queue and returns to Drafts, keeping its ID. To change\n" +
			"the time, cancel it and send again with --send-at.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			ids, rows, err := scheduled(c, all)
			if err != nil {
				return err
			}
			preview := func(u *ui.UI) error {
				return ui.Table(u, ui.TableSpec[mailsvc.Message]{
					Noun: "messages", Columns: messageColumns(),
					Total: ui.Unknown, Page: ui.Unpaged,
				}, rows)
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Unscheduled, Kind: "messages", Count: len(ids), IDs: ids,
				Detail: "and returned them to drafts", Preview: preview,
			}, func() error { return c.App.Mail.Unschedule(c.Ctx, ids) })
		}),
	}
	kit.All(c.Flags(), &all)
	return c
}

// scheduled resolves what to unschedule. References resolve within the Scheduled
// folder only, so a subject can never reach something already sent.
func scheduled(c *kit.Invocation, all bool) ([]string, []mailsvc.Message, error) {
	if len(c.Args) == 0 && !all {
		return nil, nil, kit.Fail("Nothing selected.").
			Hint("pass a REF, or --all to cancel every scheduled send.")
	}
	var rows []mailsvc.Message
	for _, refArg := range c.Args {
		id, err := c.App.Mail.ResolveScheduled(c.Ctx, refArg)
		if err != nil {
			return nil, nil, err
		}
		rows = append(rows, mailsvc.Message{ID: id})
	}
	if all {
		msgs, _, err := c.App.Mail.List(c.Ctx, mailsvc.ListOptions{Folder: "scheduled"})
		if err != nil {
			return nil, nil, err
		}
		rows = append(rows, msgs...)
	}
	ids := make([]string, 0, len(rows))
	for _, m := range rows {
		ids = append(ids, m.ID)
	}
	return kit.Dedupe(ids), rows, nil
}

// ── shared flag registration ──

// bodyFormat is how a message body should be rendered. Declaring it as an enum
// gives it one error wording and shell completion, which a hand-checked string
// never had.
// The word is "render", not "format", because export already uses --format for
// the file layout it writes - eml or mbox - and a container on disk is not the
// same question as which representation of a body to print. One flag name, one
// question.
func bodyRendering() *kit.Enum {
	return &kit.Enum{
		Name: "render", Usage: "Which representation of the body to print", Default: "text",
		Values: []string{"text", "html", "raw"},
	}
}

// registerFolder adds --folder. The flag's own default is empty so that a
// command can tell a folder nobody named from one that was; shown names the
// folder used in that case, which is what the help has to say.
func registerFolder(c *cobra.Command, target *string, def, shown string) {
	usage := "Folder or label to look in"
	if shown != "" {
		usage += " (default: " + shown + ")"
	}
	c.Flags().StringVar(target, "folder", def, usage)
	registerFolderCompletion(c, "folder")
}

// registerFolderCompletion offers the built-in folder names. A custom folder is
// not offered because listing them would need a request, and completion that
// pauses to authenticate is worse than completion that is incomplete.
func registerFolderCompletion(c *cobra.Command, flag string) {
	_ = c.RegisterFlagCompletionFunc(flag,
		func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			return mailsvc.SystemFolderNames(), cobra.ShellCompDirectiveNoFileComp
		})
}

// addressOnlyHint explains an empty result that a different flag would have
// found, since Proton's --from matches the address alone.
//
// An index answers the same question over display names as well, so the hint is
// for the server's answer only: offered where it does not apply, it would send
// somebody to a flag that searches exactly what they just searched.
func addressOnlyHint(c *kit.Invocation, cover mailsvc.Coverage, opts mailsvc.ListOptions) {
	if cover.Indexed || opts.Keyword != "" {
		return
	}
	term := opts.From
	flag := "--from"
	if term == "" {
		term, flag = opts.To, "--to"
	}
	if term == "" {
		return
	}
	c.UI().Hint(flag + " matches the address only. To search display names too, " +
		"use --keyword " + term + ".")
}

func quoted(s string) string { return strconv.Quote(s) }

// ── emptying a folder ──

// Empty removes everything in a folder, which is what the web calls "Empty
// trash" and "Delete all".
//
// It is not `delete --all`, and the difference matters: a filtered delete
// enumerates what it will touch and shows you, while this asks Proton to clear a
// folder without ever naming its contents. So the folder is required and there is
// no filter to narrow it - saying "everything in trash" is the whole command.
func emptyCmd() *cobra.Command {
	var folder string
	c := &cobra.Command{
		Use:   "empty",
		Short: "Delete everything in a folder, permanently",
		Long: "Delete everything in a folder, permanently.\n\n" +
			"Proton clears the folder without reporting what was in it, so nothing is\n" +
			"listed first. This takes no filters and always asks for confirmation.",
		// Which folder to clear is on the command line or it is nowhere, so it is
		// settled before the sign-in rather than after it.
		RunE: kit.Run([]kit.Step{func(*kit.Invocation) error {
			if folder == "" {
				return kit.Fail("Which folder?").
					Hint("--folder trash", "--folder spam")
			}
			return nil
		}}, func(c *kit.Invocation) error {
			box, err := c.App.Mail.ResolveMailbox(c.Ctx, folder)
			if err != nil {
				return err
			}
			// What was emptied is the folder, which is the only thing this command
			// knows: nothing was enumerated, so a number of messages would be invented.
			kind := "folders"
			if !box.Folder {
				kind = "labels"
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Emptied, Kind: kind, Count: 1, Name: box.Name,
			}, func() error {
				return c.App.Mail.EmptyFolder(c.Ctx, box.ID)
			})
		}),
	}
	registerFolder(c, &folder, "", "")
	return c
}

// ── self-destructing messages ──

// Update changes a field of messages that already exist, which for a message is
// when it deletes itself and nothing else: the rest of what a message is was
// settled by whoever sent it.
//
// Proton stores the moment rather than the duration, so this takes a duration
// and works out the moment - which is what a person means by "in a week".
func updateCmd() *cobra.Command {
	var f filters
	var expires string
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "update [REF...]",
		Short: "Change when messages delete themselves",
		Long: "Change when messages delete themselves.\n\n" +
			"--expires takes a duration, or never to stop them expiring. A message already\n" +
			"counting down reports the moment it expires rather than how long is left.",
		RunE: kit.Run([]kit.Step{
			kit.StepSelection(f.set, filterHint, "a whole folder"), kit.StepExpand,
			reauth.Supply,
		}, func(c *kit.Invocation) error {
			if expires == "" {
				return kit.Fail("Nothing to change.").
					Hint("--expires 7d, or --expires never to stop them expiring")
			}
			d, err := kit.Expires(expires)
			if err != nil {
				return err
			}
			var at int64
			detail := "- they will not expire"
			if d > 0 {
				at, detail = time.Now().Add(d).Unix(), "in "+expires
			}
			sel, err := selectMessages(c, &f)
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: "messages", Count: sel.Len(), IDs: sel.IDs,
				Detail: detail, Preview: sel.Preview(),
			}, func() error {
				return c.App.Mail.SetExpiration(c.Ctx, sel.IDs, at)
			})
		}),
	}
	c.Flags().StringVar(&expires, "expires", "",
		"Delete them after DURATION (e.g. 7d, 24h), or never")
	f.register(c)
	// Proton guards this endpoint behind an elevated session and grants that only
	// for another SRP exchange, so the command carries what it can answer with.
	reauth.Declare(c)
	return c
}

// ── read receipts ──

// Receipt answers a message that asked to have its reading confirmed.
//
// It names its messages and takes no filters, for the reason the two verdicts
// do: it is a decision about something that was read, and here one that cannot
// be taken back once the sender has been told.
func receiptCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "receipt REF...",
		Short: "Tell the sender you read their message",
		Long: "Tell the sender you read their message.\n\n" +
			"Only a message that asked for a read receipt can be answered, and only\n" +
			"once. `get` shows such a message as Receipt: requested.\n\n" +
			"To ask for one yourself, send with `" + kit.Program +
			" mail messages send --request-receipt`.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			ids := make([]string, 0, len(c.Args))
			for _, refArg := range c.Args {
				id, err := c.App.Mail.Resolve(c.Ctx, refArg)
				if err != nil {
					return wrongTable(err, "receipt")
				}
				ids = append(ids, id)
			}
			ids = kit.Dedupe(ids)
			due := make([]mailsvc.Receipt, 0, len(ids))
			for _, id := range ids {
				r, err := c.App.Mail.ReceiptOf(c.Ctx, id)
				if err != nil {
					return err
				}
				if !r.Answerable() {
					return noReceiptToSend(r)
				}
				due = append(due, r)
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Sent, Kind: "receipts", Count: len(ids), IDs: ids,
				Detail: receiptDetail(due),
			}, func() error {
				for _, id := range ids {
					if err := c.App.Mail.SendReceipt(c.Ctx, id); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
}

// receiptDetail names who is about to be told, for the one message where there
// is a single answer to that.
func receiptDetail(due []mailsvc.Receipt) string {
	if len(due) != 1 || due[0].To == "" {
		return ""
	}
	return "to " + due[0].To
}

// noReceiptToSend says which of the three ways a message has nothing to answer.
// Whether anything was asked for comes first, because a message that asked for
// nothing is answered by nobody, whichever end of it this account is on.
func noReceiptToSend(r mailsvc.Receipt) error {
	switch {
	case !r.Requested:
		return errs.Naming(r.Subject, kit.Fail(
			"%q did not ask for a read receipt.", r.Subject).Exit(3))
	case r.Outgoing:
		return errs.Naming(r.Subject, kit.Fail(
			"%q is a message you sent, so its request is one you made.", r.Subject).Exit(3))
	}
	return errs.Naming(r.Subject, kit.Fail(
		"A read receipt has already been sent for %q.", r.Subject).Exit(3))
}

// ── mailing lists ──

// Unsubscribe asks the list a message came from to stop, the same three ways
// `mailing-lists unsubscribe` does - this one addressed by the mail that
// arrived rather than by the sender behind it.
func unsubscribeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unsubscribe REF...",
		Short: "Ask the mailing list a message came from to stop",
		Long: "Ask the mailing list a message came from to stop.\n\n" +
			"A list offers one of three ways, and the answer says which was used.\n" +
			"Proton submits a one-click form on your behalf; an unsubscribe address is\n" +
			"a message sent from the address the list writes to; a link is a page,\n" +
			"which is opened in your browser and printed either way.\n\n" +
			"To go by sender instead of by message, see `" + kit.Program +
			" mail mailing-lists`.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			ids := make([]string, 0, len(c.Args))
			for _, ref := range c.Args {
				id, err := c.App.Mail.Resolve(c.Ctx, ref)
				if err != nil {
					return wrongTable(err, "unsubscribe")
				}
				ids = append(ids, id)
			}
			ids = kit.Dedupe(ids)
			lists := make([]mailsvc.UnsubscribeTarget, 0, len(ids))
			for _, id := range ids {
				l, err := c.App.Mail.MailingListOf(c.Ctx, id)
				if err != nil {
					return err
				}
				if l.Offer().Way == "" {
					return noWayToLeave(l)
				}
				lists = append(lists, l)
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Unsubscribed, Kind: "messages", Count: len(ids), IDs: ids,
				Detail: unsubscribeDetail(lists),
			}, func() error {
				for _, l := range lists {
					if err := askToStop(c, l); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
}
