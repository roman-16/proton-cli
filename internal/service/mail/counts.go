package mail

import (
	"context"
	"log/slog"

	"github.com/roman-16/proton-cli/internal/proton"
)

type MailboxCount struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Unread int    `json:"unread"`
	Total  int    `json:"total"`
}

func (s *Service) Counts(ctx context.Context, threads bool, folder string) ([]MailboxCount, error) {
	places, err := s.countedPlaces(ctx, folder)
	if err != nil {
		return nil, err
	}
	q := proton.Query("OnlyInInboxForCategories", "1")
	req := proton.Request{Method: "GET", Path: "/mail/v4/messages/count", Query: q}
	if threads {
		req = proton.Request{Method: "GET", Path: "/mail/v4/conversations/count", Query: q}
	}
	var r struct {
		Counts []struct {
			LabelID       string
			Total, Unread int
		}
	}
	if err := s.C.Decode(ctx, req, &r); err != nil {
		return nil, err
	}
	type count struct{ total, unread int }
	byLabel := make(map[string]count, len(r.Counts))
	for _, c := range r.Counts {
		byLabel[c.LabelID] = count{total: c.Total, unread: c.Unread}
	}
	out := make([]MailboxCount, 0, len(places))
	unreported := 0
	for _, p := range places {
		c, ok := byLabel[p.ID]
		if !ok {
			unreported++
		}
		out = append(out, MailboxCount{ID: p.ID, Name: p.Name, Unread: c.unread, Total: c.total})
	}
	if unreported > 0 {
		// Recorded and not counted: Proton leaves out a place it has nothing to
		// count in, and the web reads that as zero too. The line is what tells a
		// changed answer apart from an empty mailbox.
		slog.DebugContext(ctx, "mail: Proton reported no count for some places, so they count zero",
			"count", unreported)
	}
	return out, nil
}

func (s *Service) countedPlaces(ctx context.Context, folder string) ([]Mailbox, error) {
	if folder == "" {
		return s.Mailboxes(ctx)
	}
	m, err := s.ResolveMailbox(ctx, folder)
	if err != nil {
		return nil, err
	}
	return []Mailbox{m}, nil
}
