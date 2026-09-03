package mail

import (
	"fmt"
	"time"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/ical"
	"github.com/roman-16/proton-cli/internal/mailtext"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

// Conversations are whole threads. They take the same verbs as messages wherever
// the verb means the same thing, which is nearly everywhere - the exception being
// `unschedule`, since a queued send is one message rather than a thread.

func conversationsCmd() *cobra.Command {
	c := &cobra.Command{Use: "conversations", Short: "Whole threads"}
	c.AddCommand(
		convListCmd(), convGetCmd(), conversationExportCmd(),
		convReplyCmd(), convForwardCmd(),
		convMoveCmd(), convLabelCmd(), convUnlabelCmd(),
		convStarCmd(), convUnstarCmd(), convMarkCmd(),
		convTrashCmd(), convDeleteCmd(), convAttachmentsCmd(),
		convSnoozeCmd(), convUnsnoozeCmd(),
	)
	return c
}

func convListCmd() *cobra.Command {
	var f filters
	c := &cobra.Command{
		Use:   "list",
		Short: "List threads in a folder",
		Long: "List threads in a folder.\n\n" +
			"Takes the same filters as the verbs that organise threads, so you can\n" +
			"preview a selection here before acting on it. Text filters go through\n" +
			"Proton's index, which lags a change by a few seconds.\n\n" +
			"Looks in the inbox unless told otherwise. Use --folder all to search\n" +
			"everything.",
		Args: cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			opts, err := f.list()
			if err != nil {
				return err
			}
			convs, total, err := c.App.Mail.ConversationsList(c.Ctx, opts)
			if err != nil {
				return err
			}
			convs = keepStarred(convs, f.starred)
			if len(convs) == 0 {
				addressOnlyHint(c, opts.Keyword, opts.From, opts.To)
			}
			return kit.List(c, ui.TableSpec[mailsvc.Conversation]{
				Noun: "conversations", Columns: conversationColumns(),
				Total: total, Page: opts.Page, PageSize: opts.PageSize,
				Filtered: f.narrowed(),
			}, convs)
		}),
	}
	f.registerNarrowing(c, "inbox")
	registerPaging(c, &f.page, &f.pageSize, "threads")
	return c
}

func convGetCmd() *cobra.Command {
	var bodyOnly, stripQuotes, includeInline, summary bool
	render := bodyRendering()
	c := &cobra.Command{
		Use:   "get REF",
		Short: "Show a whole thread, decrypted",
		Args:  cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			shape, err := render.Value()
			if err != nil {
				return err
			}
			id, err := c.App.Mail.ResolveConversation(c.Ctx, c.Args[0])
			if err != nil {
				return wrongTable(err, "get")
			}
			conv, err := c.App.Mail.ConversationRead(c.Ctx, id)
			if err != nil {
				return wrongTable(err, "get")
			}
			if summary {
				return threadSummary(c, conv)
			}
			return kit.Read(c, threadDocument(conv, shape, bodyOnly, stripQuotes, includeInline))
		}),
	}
	render.Register(c)
	c.Flags().BoolVar(&bodyOnly, "body-only", false, "Emit only the bodies, with no headers or dividers")
	c.Flags().BoolVar(&stripQuotes, "strip-quotes", false, "Drop quoted reply blocks from each body")
	c.Flags().BoolVar(&includeInline, "include-inline", false, "List inline attachments too")
	c.Flags().BoolVar(&summary, "summary", false, "One line per message instead of the full thread")
	return c
}

func threadDocument(conv *mailsvc.ConversationFull, shape string, bodyOnly, stripQuotes, includeInline bool) ui.DocumentSpec {
	n := len(conv.Messages)
	parts := make([]ui.Part, 0, n)
	for i := range conv.Messages {
		part := messagePart(&conv.Messages[i], shape, stripQuotes, includeInline)
		part.Divider = fmt.Sprintf("%d/%d", i+1, n)
		parts = append(parts, part)
	}
	return ui.DocumentSpec{
		Object: conv,
		Header: []ui.Field{
			{Label: "Subject", Value: conv.Conversation.Subject, Handle: true},
			{Label: "Messages", Value: fmt.Sprintf("%d", n)},
			{Label: "ID", Value: conv.Conversation.ID, ID: true},
		},
		Parts:    parts,
		BodyOnly: bodyOnly || shape != "text",
	}
}

// threadPreview is one message reduced to a line, for --summary.
type threadPreview struct {
	Position    string `json:"position"`
	Date        string `json:"date"`
	From        string `json:"from"`
	Preview     string `json:"preview"`
	Attachments int    `json:"attachments"`
}

// threadSummary renders a thread as a table, which is what a one-line-per-message
// view actually is - so it aligns, and survives jq.
func threadSummary(c *kit.Invocation, conv *mailsvc.ConversationFull) error {
	n := len(conv.Messages)
	rows := make([]threadPreview, 0, n)
	for i := range conv.Messages {
		m := &conv.Messages[i]
		rows = append(rows, threadPreview{
			Position:    fmt.Sprintf("%d/%d", i+1, n),
			Date:        units.Time(m.Time),
			From:        addressLine(m.Sender),
			Preview:     mailtext.MessagePreview(m.Body, m.MIMEType),
			Attachments: len(mailsvc.FilterInline(m.Attachments)),
		})
	}
	return kit.List(c, ui.TableSpec[threadPreview]{
		Noun:  "messages",
		Total: ui.Unknown, Page: ui.Unpaged,
		Columns: []ui.Column[threadPreview]{
			{Header: "#", Cell: func(p threadPreview) string { return p.Position }},
			{Header: "DATE", Cell: func(p threadPreview) string { return p.Date }},
			{Header: "FROM", Flex: true, Cell: func(p threadPreview) string { return p.From }},
			{Header: "PREVIEW", Flex: true, Cell: func(p threadPreview) string { return p.Preview }},
			{Header: "FLAGS", Marks: func(p threadPreview) ui.Marks {
				return flags(false, false, p.Attachments)
			}},
		},
	}, rows)
}

// ── organising ──

func convMoveCmd() *cobra.Command {
	var f filters
	var into string
	c := &cobra.Command{
		Use:   "move [REF...]",
		Short: "Move threads to a folder",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			dest, err := c.App.Mail.ResolveFolderTarget(c.Ctx, into)
			if err != nil {
				return err
			}
			sel, err := selectConversations(c, &f)
			if err != nil {
				return wrongTable(err, "move")
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Moved, Kind: "conversations", Count: sel.Len(), IDs: sel.IDs,
				Detail: "to " + dest.Name, Preview: sel.Preview(),
			}, func() error {
				return c.App.Mail.ConversationsLabel(c.Ctx, sel.IDs, dest.ID)
			})
		}),
	}
	c.Flags().StringVar(&into, "into", "", "Destination folder, by name or ID")
	_ = c.MarkFlagRequired("into")
	registerFolderCompletion(c, "into")
	f.register(c)
	return c
}

func convLabelCmd() *cobra.Command {
	return convLabelVerb("label", "Attach a label to threads", ui.Labelled,
		func(c *kit.Invocation, ids []string, labelID string) error {
			return c.App.Mail.ConversationsLabel(c.Ctx, ids, labelID)
		})
}

func convUnlabelCmd() *cobra.Command {
	return convLabelVerb("unlabel", "Detach a label from threads", ui.Unlabelled,
		func(c *kit.Invocation, ids []string, labelID string) error {
			return c.App.Mail.ConversationsUnlabel(c.Ctx, ids, labelID)
		})
}

func convLabelVerb(use, short string, action ui.Action, apply func(*kit.Invocation, []string, string) error) *cobra.Command {
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
			sel, err := selectConversations(c, &f)
			if err != nil {
				return wrongTable(err, use)
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: action, Kind: "conversations", Count: sel.Len(), IDs: sel.IDs,
				Detail: quoted(target.Name), Preview: sel.Preview(),
			}, func() error { return apply(c, sel.IDs, target.ID) })
		}),
	}
	c.Flags().StringVar(&label, "label", "", "The label to attach or detach, by name or ID")
	_ = c.MarkFlagRequired("label")
	f.register(c)
	return c
}

func convStarCmd() *cobra.Command {
	return convSimpleVerb("star", "Star threads", ui.Starred, "",
		func(c *kit.Invocation, ids []string, _ string) error {
			return c.App.Mail.ConversationsLabel(c.Ctx, ids, mailsvc.StarredLabelID)
		})
}

func convUnstarCmd() *cobra.Command {
	return convSimpleVerb("unstar", "Remove the star from threads", ui.Unstarred, "",
		func(c *kit.Invocation, ids []string, _ string) error {
			return c.App.Mail.ConversationsUnlabel(c.Ctx, ids, mailsvc.StarredLabelID)
		})
}

func convTrashCmd() *cobra.Command {
	return convSimpleVerb("trash", "Move threads to the trash", ui.Trashed, "to trash",
		func(c *kit.Invocation, ids []string, _ string) error {
			return c.App.Mail.ConversationsTrash(c.Ctx, ids)
		})
}

func convDeleteCmd() *cobra.Command {
	return convSimpleVerb("delete", "Delete threads permanently", ui.Deleted, "",
		func(c *kit.Invocation, ids []string, scope string) error {
			return c.App.Mail.ConversationsDelete(c.Ctx, ids, scope)
		})
}

func convMarkCmd() *cobra.Command {
	c := &cobra.Command{Use: "mark", Short: "Set whether threads count as read"}
	c.AddCommand(
		convSimpleVerb("read", "Mark threads as read", ui.MarkedRead, "as read",
			func(c *kit.Invocation, ids []string, _ string) error {
				return c.App.Mail.ConversationsMarkRead(c.Ctx, ids)
			}),
		convSimpleVerb("unread", "Mark threads as unread", ui.MarkedUnread, "as unread",
			func(c *kit.Invocation, ids []string, scope string) error {
				return c.App.Mail.ConversationsMarkUnread(c.Ctx, ids, scope)
			}),
	)
	return c
}

// convSimpleVerb builds a thread verb that needs no argument beyond the selection.
//
// The apply function receives the scope label, because Proton applies two of these
// - marking unread, and deleting - within a mailbox rather than globally: a thread
// can have messages in several places at once.
func convSimpleVerb(use, short string, action ui.Action, detail string,
	apply func(*kit.Invocation, []string, string) error) *cobra.Command {
	var f filters
	c := &cobra.Command{
		Use:   use + " [REF...]",
		Short: short,
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			sel, err := selectConversations(c, &f)
			if err != nil {
				return wrongTable(err, use)
			}
			scope := ""
			if f.folder != "" {
				mailbox, err := c.App.Mail.ResolveMailbox(c.Ctx, f.folder)
				if err != nil {
					return err
				}
				scope = mailbox.ID
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: action, Kind: "conversations", Count: sel.Len(), IDs: sel.IDs,
				Detail: detail, Preview: sel.Preview(),
			}, func() error { return apply(c, sel.IDs, scope) })
		}),
	}
	f.register(c)
	return c
}

// ── replying to a thread ──

func convReplyCmd() *cobra.Command {
	return convAnswerCmd("reply", "Reply to the newest message in a thread", false)
}

func convForwardCmd() *cobra.Command {
	return convAnswerCmd("forward", "Forward the newest message in a thread", true)
}

// convAnswerCmd answers the newest message in a thread, which is what the web
// client's Reply and Forward buttons do from the thread view.
func convAnswerCmd(use, short string, forward bool) *cobra.Command {
	var f composeFlags
	var d deliveryFlags
	var replyAll, noQuote, noAttachments, asDraft bool
	c := &cobra.Command{
		Use:   use + " REF",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{d.supply, kit.StepExpand}, func(c *kit.Invocation) error {
			del, at, err := d.delivery()
			if err != nil {
				return err
			}
			convID, err := c.App.Mail.ResolveConversation(c.Ctx, c.Args[0])
			if err != nil {
				return wrongTable(err, use)
			}
			ids, err := c.App.Mail.ConversationMessageIDs(c.Ctx, convID)
			if err != nil {
				return wrongTable(err, use)
			}
			if len(ids) == 0 {
				return kit.Fail("That thread has no messages.")
			}
			newest := ids[len(ids)-1]
			content, err := buildAnswer(c, newest, &f, answerAction(forward, replyAll), noQuote, noAttachments)
			if err != nil {
				return err
			}
			if asDraft {
				return saveDraft(c, content)
			}
			return deliver(c, content, del, at)
		}),
	}
	f.registerRecipients(c)
	c.Flags().StringVar(&f.body, "body", "", "Your text, placed above the quoted original (- reads stdin)")
	c.Flags().BoolVar(&f.html, "html", false, "Compose in HTML (default: match the original)")
	f.registerAttachments(c)
	f.registerIdentity(c)
	d.register(c)
	c.Flags().BoolVar(&noQuote, "no-quote", false, "Do not quote the original message")
	c.Flags().BoolVar(&asDraft, "draft", false, "Save as a draft instead of sending")
	if forward {
		c.Flags().BoolVar(&noAttachments, "no-attachments", false, "Leave the original's attachments behind")
	} else {
		c.Flags().BoolVar(&replyAll, "everyone", false, "Reply to everyone who was on the message, not just the sender")
	}
	return c
}

// ── attachments across a thread ──

func convAttachmentsCmd() *cobra.Command {
	c := &cobra.Command{Use: "attachments", Short: "Files attached anywhere in a thread"}
	c.AddCommand(convAttachmentsListCmd(), convAttachmentsDownloadCmd())
	return c
}

func convAttachmentTableSpec(includeInline bool) ui.TableSpec[mailsvc.ConversationAttachment] {
	cols := []ui.Column[mailsvc.ConversationAttachment]{
		{Header: "ID", ID: true, Cell: func(a mailsvc.ConversationAttachment) string { return a.ID }},
		{Header: "NAME", Flex: true, Cell: func(a mailsvc.ConversationAttachment) string { return a.Name }},
		{Header: "SIZE", Right: true, Cell: func(a mailsvc.ConversationAttachment) string { return units.Size(a.Size) }},
		{Header: "TYPE", Flex: true, Cell: func(a mailsvc.ConversationAttachment) string { return a.MIMEType }},
		{Header: "MESSAGE_ID", Ref: "mail messages", Cell: func(a mailsvc.ConversationAttachment) string { return a.MessageID }},
	}
	if includeInline {
		cols = append(cols, ui.Column[mailsvc.ConversationAttachment]{
			Header: "DISPOSITION", Cell: func(a mailsvc.ConversationAttachment) string {
				if a.Disposition == "" {
					return "attachment"
				}
				return a.Disposition
			},
		})
	}
	return ui.TableSpec[mailsvc.ConversationAttachment]{
		Noun: "attachments", Columns: cols, Total: ui.Unknown, Page: ui.Unpaged,
	}
}

func convAttachmentsListCmd() *cobra.Command {
	var includeInline bool
	c := &cobra.Command{
		Use:   "list REF",
		Short: "List every attachment in a thread",
		Args:  cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			id, err := c.App.Mail.ResolveConversation(c.Ctx, c.Args[0])
			if err != nil {
				return wrongTable(err, "attachments list")
			}
			atts, err := c.App.Mail.ConversationAttachmentsList(c.Ctx, id, includeInline)
			if err != nil {
				return wrongTable(err, "attachments list")
			}
			return kit.List(c, convAttachmentTableSpec(includeInline), atts)
		}),
	}
	c.Flags().BoolVar(&includeInline, "include-inline", false, "Include inline attachments")
	return c
}

func convAttachmentsDownloadCmd() *cobra.Command {
	var dest kit.Destination
	var includeInline bool
	c := &cobra.Command{
		Use:   "download REF [ATTACHMENT_REF]",
		Short: "Download and decrypt attachments from a thread",
		Args:  cobra.RangeArgs(1, 2),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			one := len(c.Args) == 2
			if err := dest.Validate(one); err != nil {
				return err
			}
			convID, err := c.App.Mail.ResolveConversation(c.Ctx, c.Args[0])
			if err != nil {
				return wrongTable(err, "attachments download")
			}
			list, err := c.App.Mail.ConversationAttachmentsList(c.Ctx, convID, true)
			if err != nil {
				return wrongTable(err, "attachments download")
			}
			if one {
				for _, at := range list {
					if at.ID == c.Args[1] {
						return downloadOne(c, at.MessageID, at.ID, &dest)
					}
				}
				return kit.Fail("No attachment %s in that thread.", c.Args[1]).Exit(3)
			}
			if !includeInline {
				kept := list[:0]
				for _, at := range list {
					if at.Disposition != "inline" {
						kept = append(kept, at)
					}
				}
				list = kept
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Downloaded, Kind: "attachments", Count: len(list),
				Detail: "to " + dest.Describe(),
			}, func() error {
				for _, at := range list {
					data, _, err := c.App.Mail.AttachmentDownload(c.Ctx, at.MessageID, at.ID)
					if err != nil {
						return kit.Fail("could not download %s: %v", at.Name, err)
					}
					if _, err := dest.Write(c, at.Name, data); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
	dest.Register(c)
	c.Flags().BoolVar(&includeInline, "include-inline", false, "Include inline attachments")
	return c
}

// ── snooze ──

// Snooze takes threads out of the inbox until a moment and brings them back
// then, which is what the web's snooze does.
//
// It is on threads and not on messages because that is what Proton snoozes: a
// conversation leaves the inbox as a whole and returns as a whole.
func convSnoozeCmd() *cobra.Command {
	var f filters
	var until string
	c := &cobra.Command{
		Use:   "snooze [REF...]",
		Short: "Take threads out of the inbox until later",
		Long: "Take threads out of the inbox until later.\n\n" +
			"--until takes a duration from now, such as 3d, or a moment written out\n" +
			"in full, such as 2026-04-17T09:00. The thread returns to the inbox then,\n" +
			"unread.",
		Args: cobra.ArbitraryArgs,
		RunE: kit.Run([]kit.Step{
			kit.StepSelection(f.set, filterHint, "a whole folder"), kit.StepExpand,
		}, func(c *kit.Invocation) error {
			at, err := snoozeMoment(until)
			if err != nil {
				return err
			}
			sel, err := selectConversations(c, &f)
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Snoozed, Kind: "conversations", Count: sel.Len(), IDs: sel.IDs,
				Detail: "until " + units.Time(at), Preview: sel.Preview(),
			}, func() error {
				return c.App.Mail.Snooze(c.Ctx, sel.IDs, at)
			})
		}),
	}
	c.Flags().StringVar(&until, "until", "", "When they come back (e.g. 3d, or 2026-04-17T09:00)")
	f.register(c)
	return c
}

// snoozeMoment reads either spelling of "when", judged before the network.
func snoozeMoment(until string) (int64, error) {
	if until == "" {
		return 0, kit.Fail("Until when?").
			Hint("--until 3d, or --until 2026-04-17T09:00")
	}
	if d, err := units.ParseDuration(until); err == nil {
		return time.Now().Add(d).Unix(), nil
	}
	at, err := ical.ParseTime(until, time.Local)
	if err != nil {
		return 0, kit.Fail("--until: %v", err).
			Hint("a duration such as 3d, or a moment such as 2026-04-17T09:00")
	}
	if !at.After(time.Now()) {
		return 0, kit.Fail("--until is in the past.")
	}
	return at.Unix(), nil
}

func convUnsnoozeCmd() *cobra.Command {
	var f filters
	c := &cobra.Command{
		Use:   "unsnooze [REF...]",
		Short: "Bring snoozed threads back to the inbox now",
		Args:  cobra.ArbitraryArgs,
		RunE: kit.Run([]kit.Step{
			kit.StepSelection(f.set, filterHint, "a whole folder"), kit.StepExpand,
		}, func(c *kit.Invocation) error {
			sel, err := selectConversations(c, &f)
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Unsnoozed, Kind: "conversations", Count: sel.Len(), IDs: sel.IDs,
				Preview: sel.Preview(),
			}, func() error {
				return c.App.Mail.Unsnooze(c.Ctx, sel.IDs)
			})
		}),
	}
	f.register(c)
	return c
}
