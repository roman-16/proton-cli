package mail

import (
	"context"
	"net/url"

	"github.com/roman-16/proton-cli/internal/mailtext"
	"github.com/roman-16/proton-cli/internal/proton"
)

// Every organising action Proton offers on mail reduces to four primitives:
// attach a label, detach a label, mark read, mark unread. Moving to a folder is
// attaching a folder label. Starring is attaching the Starred label. Trashing is
// attaching Trash.
//
// Naming the primitives and building the verbs from them means the CLI's words
// and Proton's model line up, instead of a single `Mark(read, unread, starred,
// unstar bool)` doing four unrelated things depending on which flag is set.

// Label attaches a label to messages.
func (s *Service) Label(ctx context.Context, ids []string, labelID string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/mail/v4/messages/label",
		Body: map[string]any{"LabelID": labelID, "IDs": ids},
	}, nil)
}

// Unlabel detaches a label from messages.
func (s *Service) Unlabel(ctx context.Context, ids []string, labelID string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/mail/v4/messages/unlabel",
		Body: map[string]any{"LabelID": labelID, "IDs": ids},
	}, nil)
}

// Trash moves messages to the Trash folder, from where they can be restored.
func (s *Service) Trash(ctx context.Context, ids []string) error {
	return s.Label(ctx, ids, labelTrash)
}

// Delete removes messages irrecoverably.
func (s *Service) Delete(ctx context.Context, ids []string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/mail/v4/messages/delete",
		Body: map[string]any{"IDs": ids},
	}, nil)
}

// MarkRead clears the unread flag on messages.
func (s *Service) MarkRead(ctx context.Context, ids []string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/mail/v4/messages/read",
		Body: map[string]any{"IDs": ids},
	}, nil)
}

// MarkUnread sets the unread flag on messages.
func (s *Service) MarkUnread(ctx context.Context, ids []string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/mail/v4/messages/unread",
		Body: map[string]any{"IDs": ids},
	}, nil)
}

// MarkLegitimate tells Proton a message it flagged is what it claims to be.
//
// It is one message at a time because that is what Proton takes: the verdict is
// about this message, not about its sender - `settings senders allow` is the
// standing decision.
func (s *Service) MarkLegitimate(ctx context.Context, ids []string) error {
	for _, id := range ids {
		if err := s.C.Decode(ctx, proton.Request{
			Method: "PUT", Path: "/mail/v4/messages/" + id + "/mark/ham",
		}, nil); err != nil {
			return err
		}
	}
	return nil
}

// ReportPhishing hands a message to Proton's anti-abuse team and files it as
// spam, which is what the web client's "Report phishing" does.
//
// The report carries the body decrypted, because a report nobody can read is not
// a report: Proton holds no key to the message, so the only way for anyone there
// to see what was sent is for this machine to open it first. A body that will not
// open is refused rather than reported as the armour it still is.
func (s *Service) ReportPhishing(ctx context.Context, id string) error {
	raw, u, err := s.messageAndKeys(ctx, id)
	if err != nil {
		return err
	}
	body, _, err := s.openBody(ctx, u, *raw, false)
	if err != nil {
		return err
	}
	mimeType := mimeTypeHTML
	if !mailtext.IsHTML(raw.MIMEType) {
		mimeType = mimeTypePlain
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/core/v4/reports/phishing",
		Body: map[string]any{"MessageID": id, "MIMEType": mimeType, "Body": body},
	}, nil); err != nil {
		return err
	}
	return s.Label(ctx, []string{id}, labelSpam)
}

// Unschedule cancels a scheduled send, pulling the message out of the Scheduled
// queue and back into Drafts - the web client's "Edit and reschedule". The
// message keeps its ID.
func (s *Service) Unschedule(ctx context.Context, ids []string) error {
	for _, id := range ids {
		if err := s.C.Decode(ctx, proton.Request{
			Method: "POST", Path: "/mail/v4/messages/" + id + "/cancel_send",
		}, nil); err != nil {
			return err
		}
	}
	return nil
}

// ── conversations ──
//
// The same four primitives, with one asymmetry Proton imposes: marking a thread
// unread, and deleting one, apply within a mailbox rather than globally, because
// a thread can have messages in several. An empty scope means All Mail.

func (s *Service) ConversationsLabel(ctx context.Context, ids []string, labelID string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/mail/v4/conversations/label",
		Body: map[string]any{"LabelID": labelID, "IDs": ids},
	}, nil)
}

func (s *Service) ConversationsUnlabel(ctx context.Context, ids []string, labelID string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/mail/v4/conversations/unlabel",
		Body: map[string]any{"LabelID": labelID, "IDs": ids},
	}, nil)
}

func (s *Service) ConversationsTrash(ctx context.Context, ids []string) error {
	return s.ConversationsLabel(ctx, ids, labelTrash)
}

func (s *Service) ConversationsDelete(ctx context.Context, ids []string, scopeLabelID string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/mail/v4/conversations/delete",
		Body: map[string]any{"IDs": ids, "LabelID": orAllMail(scopeLabelID)},
	}, nil)
}

func (s *Service) ConversationsMarkRead(ctx context.Context, ids []string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/mail/v4/conversations/read",
		Body: map[string]any{"IDs": ids},
	}, nil)
}

func (s *Service) ConversationsMarkUnread(ctx context.Context, ids []string, scopeLabelID string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/mail/v4/conversations/unread",
		Body: map[string]any{"IDs": ids, "LabelID": orAllMail(scopeLabelID)},
	}, nil)
}

func orAllMail(labelID string) string {
	if labelID == "" {
		return labelAllMail
	}
	return labelID
}

// ── emptying, expiring, unsubscribing, snoozing ──

// EmptyFolder removes everything in a folder, permanently.
//
// This is what the web calls "Empty trash" and "Delete all". It is a different
// act from deleting a selection: nothing is enumerated first, so it does not
// page and cannot be narrowed - which is exactly why the CLI stops for a yes
// before it.
func (s *Service) EmptyFolder(ctx context.Context, folder string) error {
	q := url.Values{}
	q.Set("LabelID", ResolveFolder(folder))
	return s.C.Decode(ctx, proton.Request{
		Method: "DELETE", Path: "/mail/v4/messages/empty", Query: q,
	}, nil)
}

// SetExpiration makes messages delete themselves at a moment, or stops them.
//
// A zero time clears it, which is how a message that was going to disappear is
// kept. Proton stores the moment, not the duration, so a message already counting
// down reports when rather than how long.
func (s *Service) SetExpiration(ctx context.Context, ids []string, at int64) error {
	body := map[string]any{"IDs": ids, "ExpirationTime": nil}
	if at > 0 {
		body["ExpirationTime"] = at
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/mail/v4/messages/expire", Body: body,
	}, nil)
}

// Unsubscribe asks a mailing list to stop, using whatever the message itself
// offered - a List-Unsubscribe header, or the one-click form behind it.
//
// Proton does the asking, because it is the party the list already knows; this
// only says which message.
func (s *Service) Unsubscribe(ctx context.Context, id string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/mail/v4/messages/" + id + "/unsubscribe",
	}, nil)
}

// Snooze takes threads out of the inbox until a moment, and Unsnooze brings them
// back early.
func (s *Service) Snooze(ctx context.Context, ids []string, until int64) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/mail/v4/conversations/snooze",
		Body: map[string]any{"IDs": ids, "SnoozeTime": until},
	}, nil)
}

func (s *Service) Unsnooze(ctx context.Context, ids []string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/mail/v4/conversations/unsnooze",
		Body: map[string]any{"IDs": ids},
	}, nil)
}
