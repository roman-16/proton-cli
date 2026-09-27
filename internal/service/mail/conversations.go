package mail

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/skip"
)

type rawConversation struct {
	ID                                     string
	Subject                                string
	NumMessages, NumUnread, NumAttachments int
	Size                                   int64
	Time                                   int64
	Senders                                []map[string]any
	Recipients                             []map[string]any
	Labels                                 []rawConversationLabel
	ExpiringByRetention                    bool
}

type rawConversationLabel struct {
	ID                    string
	ContextExpirationTime int64
}

func toConversation(c rawConversation, listed string) Conversation {
	labels := make([]string, 0, len(c.Labels))
	var expires int64
	for _, l := range c.Labels {
		labels = append(labels, l.ID)
		if l.ID == listed {
			expires = l.ContextExpirationTime
		}
	}
	return Conversation{
		ID: c.ID, Subject: c.Subject,
		NumMessages: c.NumMessages, NumUnread: c.NumUnread, NumAttachments: c.NumAttachments,
		Size: c.Size,
		Time: c.Time, Senders: c.Senders, Recipients: c.Recipients, Labels: labels,
		Expires: expires, ExpiryByRetention: c.ExpiringByRetention,
	}
}

func listedIn(folder string) string {
	if folder == "" || isCategory(folder) {
		return labelInbox
	}
	return folder
}

func (s *Service) ConversationsList(ctx context.Context, opts ListOptions) ([]Conversation, int, error) {
	if opts.Starred {
		return starredOnly(ctx, opts, s.ConversationsList, func(c Conversation) []string { return c.Labels })
	}
	q := listQuery(opts, true)
	return proton.Window(ctx, opts.Page, opts.PageSize, pageMax, func(ctx context.Context, page, size int) ([]Conversation, int, error) {
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
			out = append(out, toConversation(c, listedIn(opts.Folder)))
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
			full, err := s.fetchMessageRaw(ctx, m.ID)
			if err != nil {
				skip.Record(ctx, skip.KindMessage, m.ID, skip.Unreadable, err)
				continue
			}
			m = *full
		}
		// One message of a thread that will not open is no reason to refuse the
		// other four, and every reason to say so: the count above the thread is
		// what was shown, and the warning beside it is what was not.
		body, sig, err := s.openBody(ctx, u, m, true)
		if err != nil {
			skip.Record(ctx, skip.KindMessage, m.ID, u.Shut(sealed(m.Body)), err)
			continue
		}
		msgs = append(msgs, asFull(m, body, sig))
	}
	return &ConversationFull{Conversation: toConversation(r.Conversation, labelAllMail), Messages: msgs}, nil
}

func (s *Service) AssertConversationKind(ctx context.Context, id string) error {
	err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/mail/v4/conversations/" + id}, nil)
	if err == nil {
		return nil
	}
	return s.crossTableProbe(ctx, id, err, "conversations")
}

// ConversationMessages lists a thread's messages oldest first, which is the
// order an exported thread reads in.
func (s *Service) ConversationMessages(ctx context.Context, convID string) ([]Message, error) {
	var r struct{ Messages []rawListMessage }
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/mail/v4/conversations/" + convID}, &r); err != nil {
		return nil, s.crossTableProbe(ctx, convID, err, "conversations")
	}
	sort.SliceStable(r.Messages, func(i, j int) bool { return r.Messages[i].Time < r.Messages[j].Time })
	out := make([]Message, 0, len(r.Messages))
	for _, m := range r.Messages {
		out = append(out, toMessage(m))
	}
	return out, nil
}

func (s *Service) ConversationsMessages(ctx context.Context, ids []string) ([][]Message, error) {
	out := make([][]Message, len(ids))
	failures := make([]error, len(ids))
	var wg sync.WaitGroup
	slots := make(chan struct{}, bodiesAtOnce)
	for i, id := range ids {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			out[i], failures[i] = s.ConversationMessages(ctx, id)
		}()
	}
	wg.Wait()
	for _, err := range failures {
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func MessageIDs(msgs []Message) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.ID)
	}
	return out
}
