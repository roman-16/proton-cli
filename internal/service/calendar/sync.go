package calendar

import (
	"context"
	"encoding/base64"
	"fmt"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	pgphelper "github.com/roman-16/proton-cli/internal/crypto/pgp"
	"github.com/roman-16/proton-cli/internal/ical"
	"github.com/roman-16/proton-cli/internal/proton"
)

// Every write to a calendar goes through the sync endpoint, which takes a batch
// of creates, updates and deletes and applies them together. That matters for a
// recurring change: truncating a series, removing the overrides past the split
// and starting the remainder are one intention, and half of them landing is a
// broken calendar.

// attendeePermissions is what Proton grants an attendee on an event
// (ATTENDEE_PERMISSIONS.SEE). Granting more, as a convenient-looking all-bits
// value would, hands every participant the right to edit and delete.
const attendeePermissions = 1

// eventBody is everything the API stores about an event: the model that becomes
// its cards, and the fields that live outside them.
type eventBody struct {
	model ical.VEvent

	// notifications is the reminder list. A nil slice is not "no reminders": it is
	// how Proton spells "use the calendar's defaults", so the difference has to
	// survive a round trip or an edit silently resets somebody's reminders.
	notifications []map[string]any
	// color is the event's own colour, nil when it takes the calendar's.
	color *string
	// isOrganizer is 0 on an invitation you received. Asserting 1 unconditionally
	// would make the CLI claim authorship of other people's events.
	isOrganizer int

	// keyPacket reuses an existing session key, which is what makes a write an
	// update to the same encrypted event rather than a re-key.
	keyPacket string
	// attendeeList is the cleartext per-attendee record.
	attendeeList []map[string]any
	// attendeeKeys are the participants Proton can deliver to directly, whose copy
	// of the event needs the session key wrapped to their own key.
	//
	// They are wrapped where the session key is made rather than by the caller,
	// because a caller that has to build the cards once to obtain the key and once
	// more to send them ends up wrapping a key the event was never encrypted with.
	attendeeKeys []protonAttendee
}

// optionalColor is a colour as the API wants it: the value, or null for an event
// that has none of its own. The empty string is how the rest of this package
// spells "no colour", and Proton refuses it as an invalid one, so the two meet
// here rather than at each caller.
func optionalColor(color string) *string {
	if color == "" {
		return nil
	}
	return &color
}

// protonAttendee is a participant with a Proton account, and the key their copy of
// the event has to be readable with.
type protonAttendee struct {
	email string
	kr    *pgp.KeyRing
}

// object renders the API's Event object, returning the session key so a caller
// can wrap it for newly added Proton attendees.
func (ck *calKeys) object(b eventBody) (map[string]any, *pgp.SessionKey, error) {
	signedCard, encCard, keyPacket, sk, err := pgphelper.EncryptAndSignCardSplit(
		b.model.SharedSigned(), b.model.SharedEncrypted(), ck.calKR, ck.addrKR, b.keyPacket)
	if err != nil {
		return nil, nil, err
	}
	event := map[string]any{
		"Permissions":        attendeePermissions,
		"IsOrganizer":        b.isOrganizer,
		"SharedEventContent": []any{signedCard, encCard},
		"Notifications":      b.notifications,
		"Color":              b.color,
	}
	if keyPacket != "" {
		event["SharedKeyPacket"] = keyPacket
	}
	if attendees := b.model.AttendeesEncrypted(); attendees != "" {
		attCard, err := pgphelper.EncryptPartWithSessionKey(attendees, sk, ck.addrKR)
		if err != nil {
			return nil, nil, err
		}
		event["AttendeesEventContent"] = []any{attCard}
		if b.attendeeList != nil {
			event["Attendees"] = b.attendeeList
		}
		added, err := wrapForAttendees(b.attendeeKeys, sk)
		if err != nil {
			return nil, nil, err
		}
		if len(added) > 0 {
			event["AddedProtonAttendees"] = added
		}
	}
	return event, sk, nil
}

// wrapForAttendees encrypts the event's session key to each Proton attendee, which
// is how their own calendar can read the copy Proton puts there.
func wrapForAttendees(attendees []protonAttendee, sk *pgp.SessionKey) ([]map[string]any, error) {
	if len(attendees) == 0 {
		return nil, nil
	}
	out := make([]map[string]any, 0, len(attendees))
	for _, a := range attendees {
		kp, err := a.kr.EncryptSessionKey(sk)
		if err != nil {
			return nil, fmt.Errorf("wrap the event key for %s: %w", a.email, err)
		}
		out = append(out, map[string]any{
			"Email":            a.email,
			"AddressKeyPacket": base64.StdEncoding.EncodeToString(kp),
		})
	}
	return out, nil
}

// syncOp is one entry in a sync batch.
type syncOp map[string]any

func deleteOp(eventID string) syncOp { return syncOp{"ID": eventID} }

// sync applies a batch and returns the IDs of the events it created, in order.
func (s *Service) sync(ctx context.Context, calendarID, memberID string, ops []syncOp) ([]string, error) {
	return s.syncBatch(ctx, calendarID, memberID, ops, false)
}

// syncImported applies a batch of events read out of a file.
//
// Proton is told these came from an import, which is what lets each one take the
// place of the event already carrying its UID instead of being refused as a
// clash. That is the difference between an export you can restore and a file the
// calendar will not read back.
func (s *Service) syncImported(ctx context.Context, calendarID, memberID string, ops []syncOp) ([]string, error) {
	return s.syncBatch(ctx, calendarID, memberID, ops, true)
}

func (s *Service) syncBatch(ctx context.Context, calendarID, memberID string, ops []syncOp, isImport bool) ([]string, error) {
	if len(ops) == 0 {
		return nil, nil
	}
	var r struct {
		Responses []struct {
			Index    int
			Response struct {
				Code  int
				Error string
				Event struct{ ID string }
			}
		}
	}
	body := map[string]any{"MemberID": memberID, "Events": ops}
	if isImport {
		body["IsImport"] = 1
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/calendar/v1/" + calendarID + "/events/sync",
		Body: body,
	}, &r); err != nil {
		return nil, err
	}
	// Proton answers a batch inside a 200, so whether an event was written is in
	// the entry rather than in the status. A refusal read as an empty ID would be
	// reported as a success that changed nothing.
	written := make([]string, 0, len(r.Responses))
	for _, resp := range r.Responses {
		if resp.Response.Error != "" {
			return nil, fmt.Errorf("%s", resp.Response.Error)
		}
		if resp.Response.Event.ID == "" {
			return nil, fmt.Errorf("nothing came back about event %d", resp.Index)
		}
		written = append(written, resp.Response.Event.ID)
	}
	return written, nil
}

// createOp builds the entry that stores a brand-new event.
func (ck *calKeys) createOp(b eventBody) (syncOp, *pgp.SessionKey, error) {
	b.keyPacket = ""
	event, sk, err := ck.object(b)
	if err != nil {
		return nil, nil, err
	}
	return syncOp{"Overwrite": 0, "Event": event}, sk, nil
}

// importOp builds the entry that stores an event read out of a file, which takes
// the place of whatever already carries its UID.
func (ck *calKeys) importOp(b eventBody) (syncOp, error) {
	op, _, err := ck.createOp(b)
	if err != nil {
		return nil, err
	}
	op["Overwrite"] = 1
	return op, nil
}

// updateOp builds the entry that rewrites an existing event in place, reusing its
// session key.
func (ck *calKeys) updateOp(eventID string, b eventBody) (syncOp, error) {
	if b.keyPacket == "" {
		return nil, fmt.Errorf("event %s has no shared key packet to update against", eventID)
	}
	event, _, err := ck.object(b)
	if err != nil {
		return nil, err
	}
	return syncOp{"ID": eventID, "Event": event}, nil
}
