package calendar

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/ical"
	"github.com/roman-16/proton-cli/internal/proton"
)

// Attendee participation statuses (ATTENDEE_STATUS_API in WebClients).
const (
	partstatNeedsAction = 0
	partstatTentative   = 1
	partstatDeclined    = 2
	partstatAccepted    = 3
)

// StatusFromFlag maps the CLI --status word to an ATTENDEE_STATUS_API value.
func StatusFromFlag(s string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "accept":
		return partstatAccepted, nil
	case "tentative":
		return partstatTentative, nil
	case "decline":
		return partstatDeclined, nil
	}
	return 0, errs.Problemf("invalid --status %q (use: accept, tentative, decline)", s)
}

// partstatICS maps an ATTENDEE_STATUS_API value to its iCal PARTSTAT verb.
func partstatICS(status int) string {
	switch status {
	case partstatAccepted:
		return "ACCEPTED"
	case partstatTentative:
		return "TENTATIVE"
	case partstatDeclined:
		return "DECLINED"
	}
	return "NEEDS-ACTION"
}

// statusWord is the human phrasing used in the reply email and success line,
// mirroring WebClients' response wording.
func statusWord(status int) string {
	switch status {
	case partstatAccepted:
		return "accepted"
	case partstatTentative:
		return "tentatively accepted"
	case partstatDeclined:
		return "declined"
	}
	return "did not answer"
}

func replyBody(selfEmail string, status int, title string) string {
	return fmt.Sprintf("%s %s your invitation to %s", selfEmail, statusWord(status), title)
}

func replySubject(start ical.DateTime) string {
	when := start.In(time.Local)
	if start.AllDay {
		return "Re: Invitation for an event on " + when.Format("2006-01-02")
	}
	return "Re: Invitation for an event starting on " + when.Format("2006-01-02 15:04")
}

// selfTokens maps each of the account's addresses to the deterministic X-PM
// token that names it on an event, keyed by token.
func (s *Service) selfTokens(ctx context.Context, uid string, addrs []keys.Address) (map[string]string, error) {
	emails := make([]string, 0, len(addrs))
	for _, a := range addrs {
		emails = append(emails, a.Email)
	}
	canonical, err := s.canonicalEmails(ctx, emails)
	if err != nil {
		return nil, err
	}
	tokenToEmail := make(map[string]string, len(addrs))
	for _, a := range addrs {
		tokenToEmail[attendeeToken(uid, canonical[a.Email])] = a.Email
	}
	return tokenToEmail, nil
}

// findSelfAttendee returns the attendee ID and matched email for the account's
// own attendee record, matching its token against the event's attendee list.
func findSelfAttendee(tokenToEmail map[string]string, attendees []rawAttendee) (attendeeID, selfEmail string, ok bool) {
	for _, at := range attendees {
		if email, found := tokenToEmail[at.Token]; found {
			return at.ID, email, true
		}
	}
	return "", "", false
}

// findSelfAttendeePaged walks the paginated attendees endpoint (page 0 is
// already in the event's AttendeesInfo) when an event has more attendees than
// the inline list carried.
func (s *Service) findSelfAttendeePaged(ctx context.Context, calendarID, eventID string, tokenToEmail map[string]string) (attendeeID, selfEmail string, ok bool, err error) {
	err = proton.Pages(ctx, func(ctx context.Context, page int) (bool, error) {
		var r struct {
			Attendees     []rawAttendee
			MoreAttendees int
		}
		q := url.Values{}
		// Page zero arrived with the event itself, so the walk starts after it.
		q.Set("Page", fmt.Sprintf("%d", page+1))
		if err := s.C.Decode(ctx, proton.Request{
			Method: "GET", Path: fmt.Sprintf("/calendar/v1/%s/events/%s/attendees", calendarID, eventID), Query: q,
		}, &r); err != nil {
			return false, err
		}
		if id, email, found := findSelfAttendee(tokenToEmail, r.Attendees); found {
			attendeeID, selfEmail, ok = id, email, true
			return false, nil
		}
		return r.MoreAttendees == 1 && len(r.Attendees) > 0, nil
	})
	if err != nil {
		return "", "", false, err
	}
	return attendeeID, selfEmail, ok, nil
}

// Reply is a ready-to-send METHOD:REPLY invitation answer for the organizer.
type Reply struct {
	ICS        string
	Recipients []string
	Subject    string
	Body       string
}

// RespondResult is the outcome of EventRespond. Reply is non-nil when the
// organizer should be emailed the answer.
type RespondResult struct {
	Title  string
	Status string
	Reply  *Reply
}

// EventRespond replies to an invitation by updating the account's attendee
// participation status (accept/tentative/decline) and, matching WebClients,
// preparing a METHOD:REPLY notification for the organizer.
func (s *Service) EventRespond(ctx context.Context, calendarID, eventID string, status int) (*RespondResult, error) {
	var r struct{ Event rawEvent }
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/calendar/v1/" + calendarID + "/events/" + eventID}, &r); err != nil {
		return nil, err
	}
	ev := r.Event
	if ev.IsOrganizer == 1 {
		return nil, errs.WithExit(1, fmt.Errorf("you are the organizer of this event; RSVP is for attendees"))
	}

	u, err := s.keys(ctx)
	if err != nil {
		return nil, err
	}
	tokenToEmail, err := s.selfTokens(ctx, ev.UID, u.Addresses)
	if err != nil {
		return nil, err
	}
	attendeeID, selfEmail, ok := findSelfAttendee(tokenToEmail, ev.AttendeesInfo.Attendees)
	if !ok && ev.AttendeesInfo.MoreAttendees == 1 {
		if attendeeID, selfEmail, ok, err = s.findSelfAttendeePaged(ctx, calendarID, eventID, tokenToEmail); err != nil {
			return nil, err
		}
	}
	if !ok {
		return nil, &errs.NotFound{Kind: "attendee record for you on this event"}
	}

	// Recording the answer needs no calendar keys: the UID is cleartext, the
	// attendee token is deterministic, and the attendee ID comes from the
	// cleartext AttendeesInfo. So update the partstat before any unlock.
	if err := s.C.Decode(ctx, proton.Request{
		Method: "PUT",
		Path:   fmt.Sprintf("/calendar/v1/%s/events/%s/attendees/%s", calendarID, eventID, attendeeID),
		Body:   map[string]any{"Status": status, "UpdateTime": time.Now().Unix()},
	}, nil); err != nil {
		return nil, err
	}

	res := &RespondResult{Status: statusWord(status)}
	// The organizer reply needs the decrypted title and organizer, so it is
	// best-effort: the answer is already recorded even if unlocking fails.
	if ck, err := s.unlockCalendar(ctx, calendarID); err == nil {
		e := s.decrypt(ctx, ck, ev)
		if e.readErr != nil {
			return res, nil
		}
		res.Title = e.model.Summary
		if organizer := e.model.Organizer; organizer != "" && !strings.EqualFold(organizer, selfEmail) {
			res.Reply = &Reply{
				ICS:        e.model.ReplyDocument(selfEmail, partstatICS(status), ev.IsProtonProtonInvite == 1),
				Recipients: []string{organizer},
				Subject:    replySubject(e.model.Start),
				Body:       replyBody(selfEmail, status, e.model.Summary),
			}
		}
	}
	return res, nil
}
