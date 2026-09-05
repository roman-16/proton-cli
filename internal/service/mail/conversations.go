package mail

import (
	"context"
	"fmt"
	"sort"

	"github.com/roman-16/proton-cli/internal/proton"
)

type rawConversation struct {
	ID                                     string
	Subject                                string
	NumMessages, NumUnread, NumAttachments int
	Time                                   int64
	Senders                                []map[string]any
	Recipients                             []map[string]any
	Labels                                 []struct{ ID string }
}

func toConversation(c rawConversation) Conversation {
	labels := make([]string, 0, len(c.Labels))
	for _, l := range c.Labels {
		labels = append(labels, l.ID)
	}
	return Conversation{
		ID: c.ID, Subject: c.Subject,
		NumMessages: c.NumMessages, NumUnread: c.NumUnread, NumAttachments: c.NumAttachments,
		Time: c.Time, Senders: c.Senders, Recipients: c.Recipients, Labels: labels,
	}
}

func (s *Service) ConversationsList(ctx context.Context, opts ListOptions) ([]Conversation, int, error) {
	q, err := listQuery(opts, true)
	if err != nil {
		return nil, 0, err
	}
	return window(ctx, opts.Page, opts.PageSize, func(ctx context.Context, page, size int) ([]Conversation, int, error) {
		q.Set("Page", fmt.Sprintf("%d", page))
		q.Set("PageSize", fmt.Sprintf("%d", size))
		var r struct {
			Total         int
			Conversations []rawConversation
		}
		if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/mail/v4/conversations", Query: q}, &r); err != nil {
			return nil, 0, err
		}
		out := make([]Conversation, 0, len(r.Conversations))
		for _, c := range r.Conversations {
			out = append(out, toConversation(c))
		}
		return out, r.Total, nil
	})
}

func (s *Service) ConversationRead(ctx context.Context, id string) (*ConversationFull, error) {
	var r struct {
		Conversation rawConversation
		Messages     []rawMessage
	}
	var fetchErr error
	u, err := s.keys.Alongside(ctx, func(ctx context.Context) error {
		fetchErr = s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/mail/v4/conversations/" + id}, &r)
		return fetchErr
	})
	if fetchErr != nil {
		return nil, s.crossTableProbe(ctx, id, fetchErr, "conversations")
	}
	if err != nil {
		return nil, err
	}
	sort.SliceStable(r.Messages, func(i, j int) bool { return r.Messages[i].Time < r.Messages[j].Time })
	msgs := make([]Full, 0, len(r.Messages))
	for _, m := range r.Messages {
		// Proton returns the full Body only for the most recent message; older
		// ones come back as metadata. Lazy-load each older body so the whole
		// thread decrypts.
		if m.Body == "" {
			if full, err := s.fetchMessageRaw(ctx, m.ID); err == nil {
				m = *full
			}
		}
		msgs = append(msgs, s.decryptMessage(ctx, u, m))
	}
	return &ConversationFull{Conversation: toConversation(r.Conversation), Messages: msgs}, nil
}

func (s *Service) AssertConversationKind(ctx context.Context, id string) error {
	err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/mail/v4/conversations/" + id}, nil)
	if err == nil {
		return nil
	}
	return s.crossTableProbe(ctx, id, err, "conversations")
}

// ConversationMessageIDs lists a thread's message IDs oldest first, which is the
// order an exported thread reads in.
func (s *Service) ConversationMessageIDs(ctx context.Context, convID string) ([]string, error) {
	var r struct{ Messages []rawMessage }
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/mail/v4/conversations/" + convID}, &r); err != nil {
		return nil, s.crossTableProbe(ctx, convID, err, "conversations")
	}
	sort.SliceStable(r.Messages, func(i, j int) bool { return r.Messages[i].Time < r.Messages[j].Time })
	out := make([]string, 0, len(r.Messages))
	for _, m := range r.Messages {
		out = append(out, m.ID)
	}
	return out, nil
}
