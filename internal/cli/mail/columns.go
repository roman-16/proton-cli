package mail

import (
	"strconv"

	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
)

// The mail tables.
//
// Three rules hold across all of them, and across every other collection in the
// CLI: the ID is the first column and is called ID; dates go through one
// formatter; and status is a FLAGS column of glyphs rather than an unpronounceable
// header.

// flags renders the compact status markers: unread, starred, and how many files
// came with the message.
//
// The count is the attachment marker. A paperclip would have to be an emoji -
// Unicode has none below the pictographic planes - and no monospace font carries
// one, so every terminal drew it from a colour emoji font two cells wide and
// nudged whatever followed. A digit costs one cell, needs no font, and says how
// many rather than merely whether.
func flags(unread, starred bool, attachments int) ui.Marks {
	var m ui.Marks
	if unread {
		m = append(m, ui.Mark{Glyph: ui.GlyphUnread, Role: ui.Accent})
	}
	if starred {
		// Proton draws the star in its warning orange (favorite-icon-color),
		// which is the same token a caveat uses. Both mean "look here".
		m = append(m, ui.Mark{Glyph: ui.GlyphStarred, Role: ui.Caution})
	}
	if attachments > 0 {
		m = append(m, ui.Mark{Glyph: strconv.Itoa(attachments), Role: ui.Muted})
	}
	return m
}

func messageColumns() []ui.Column[mailsvc.Message] {
	return []ui.Column[mailsvc.Message]{
		{Header: "ID", ID: true, Cell: func(m mailsvc.Message) string { return m.ID }},
		{Header: "FROM", Flex: true, Cell: func(m mailsvc.Message) string {
			if m.FromName != "" {
				return m.FromName
			}
			return m.FromAddress
		}},
		{Header: "SUBJECT", Flex: true, Handle: true, Cell: func(m mailsvc.Message) string { return m.Subject }},
		{Header: "DATE", Cell: func(m mailsvc.Message) string { return units.Time(m.Time) }},
		{Header: "FLAGS", Marks: func(m mailsvc.Message) ui.Marks {
			return flags(m.Unread == 1, m.Starred(), m.NumAttachments)
		}},
	}
}

func conversationColumns() []ui.Column[mailsvc.Conversation] {
	return []ui.Column[mailsvc.Conversation]{
		{Header: "ID", ID: true, Cell: func(c mailsvc.Conversation) string { return c.ID }},
		{Header: "FROM", Flex: true, Cell: func(c mailsvc.Conversation) string { return firstSender(c) }},
		{Header: "SUBJECT", Flex: true, Handle: true, Cell: func(c mailsvc.Conversation) string { return c.Subject }},
		{Header: "MESSAGES", Right: true, Cell: func(c mailsvc.Conversation) string {
			return strconv.Itoa(c.NumMessages)
		}},
		{Header: "DATE", Cell: func(c mailsvc.Conversation) string { return units.Time(c.Time) }},
		{Header: "FLAGS", Marks: func(c mailsvc.Conversation) ui.Marks {
			return flags(c.NumUnread > 0, c.Starred(), c.NumAttachments)
		}},
	}
}

// draftColumns show recipients rather than senders: every draft is from you, so a
// FROM column would repeat one address down the page.
func draftColumns() []ui.Column[mailsvc.Message] {
	return []ui.Column[mailsvc.Message]{
		{Header: "ID", ID: true, Cell: func(m mailsvc.Message) string { return m.ID }},
		{Header: "SUBJECT", Flex: true, Handle: true, Cell: func(m mailsvc.Message) string { return m.Subject }},
		{Header: "SAVED", Cell: func(m mailsvc.Message) string { return units.Time(m.Time) }},
		{Header: "FLAGS", Marks: func(m mailsvc.Message) ui.Marks {
			return flags(false, m.Starred(), m.NumAttachments)
		}},
	}
}

func firstSender(c mailsvc.Conversation) string {
	if len(c.Senders) == 0 {
		return ""
	}
	if n, ok := c.Senders[0]["Name"].(string); ok && n != "" {
		return n
	}
	if a, ok := c.Senders[0]["Address"].(string); ok {
		return a
	}
	return ""
}
