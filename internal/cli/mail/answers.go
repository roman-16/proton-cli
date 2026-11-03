package mail

import (
	"cmp"
	"log/slog"
	"slices"
	"time"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/ui"
)

const unreadInvitation = "The invitation attached to a message would not open, so the answer in it is not on the event."

func recordAnswers(c *kit.Invocation, msgs []mailsvc.Full) error {
	ordered := slices.Clone(msgs)
	slices.SortStableFunc(ordered, func(a, b mailsvc.Full) int { return cmp.Compare(a.Time, b.Time) })
	for i := range ordered {
		if err := recordAnswersIn(c, &ordered[i]); err != nil {
			return err
		}
	}
	return nil
}

func recordAnswersIn(c *kit.Invocation, msg *mailsvc.Full) error {
	sender, _ := msg.Sender["Address"].(string)
	for _, att := range mailsvc.CalendarAttachments(msg.Attachments) {
		doc, _, err := c.App.Mail.AttachmentDownload(c.Ctx, msg.ID, att.ID)
		if err != nil {
			slog.WarnContext(c.Ctx, unreadInvitation, "reason", "undownloadable", "error", err)
			continue
		}
		verdict, err := c.App.Calendar.ReadReply(c.Ctx, string(doc), sender, time.Unix(msg.Time, 0))
		if err != nil {
			slog.WarnContext(c.Ctx, unreadInvitation, "reason", "unreadable", "error", err)
			continue
		}
		c.UI().Note(verdict.Note)
		reply := verdict.Reply
		if reply == nil {
			continue
		}
		if err := kit.Mutate(c, ui.ResultSpec{
			Action: ui.Updated, Kind: "events", Count: 1, Name: reply.Title,
			Detail:        "with " + reply.Guest + "'s answer: " + reply.Answer,
			IDs:           []string{kit.JoinPair(reply.CalendarID, reply.EventID)},
			AnswerFollows: true,
		}, func() error {
			return c.App.Calendar.RecordReply(c.Ctx, reply)
		}); err != nil {
			return err
		}
	}
	return nil
}
