package calendar

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/ical"
	"github.com/roman-16/proton-cli/internal/proton"
)

type answer struct {
	code     int
	partstat string
	reply    string
	sentence string
	word     string
}

var answers = []answer{
	{code: 3, partstat: "ACCEPTED", reply: "accept", sentence: "accepted", word: "accepted"},
	{code: 1, partstat: "TENTATIVE", reply: "tentative", sentence: "tentatively accepted", word: "tentative"},
	{code: 2, partstat: "DECLINED", reply: "decline", sentence: "declined", word: "declined"},
}

var unanswered = answer{code: 0, partstat: "NEEDS-ACTION", sentence: "did not answer", word: "pending"}

func Replies() []string {
	out := make([]string, 0, len(answers))
	for _, a := range answers {
		out = append(out, a.reply)
	}
	return out
}

func answerWhere(match func(answer) bool) (answer, bool) {
	for _, a := range answers {
		if match(a) {
			return a, true
		}
	}
	return unanswered, false
}

func answerOfCode(code int) answer {
	a, _ := answerWhere(func(a answer) bool { return a.code == code })
	return a
}

func answerOfPartstat(partstat string) answer {
	a, _ := answerWhere(func(a answer) bool { return strings.EqualFold(a.partstat, strings.TrimSpace(partstat)) })
	return a
}

func answerOfReply(reply string) (answer, bool) {
	return answerWhere(func(a answer) bool { return a.reply == strings.ToLower(strings.TrimSpace(reply)) })
}

func replyBody(selfEmail string, a answer, title string) string {
	return fmt.Sprintf("%s %s your invitation to %s", selfEmail, a.sentence, title)
}

func replySubject(start ical.DateTime) string {
	when := start.In(time.Local)
	if start.AllDay {
		return "Re: Invitation for an event on " + when.Format("2006-01-02")
	}
	return "Re: Invitation for an event starting on " + when.Format("2006-01-02 15:04")
}

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

func findSelfAttendee(tokenToEmail map[string]string, attendees []rawAttendee) (attendeeID, selfEmail string, ok bool) {
	for _, at := range attendees {
		if email, found := tokenToEmail[at.Token]; found {
			return at.ID, email, true
		}
	}
	return "", "", false
}

type Reply struct {
	ICS        string
	Recipients []string
	Subject    string
	Body       string
}

type RespondResult struct {
	Title  string
	Status string
	Reply  *Reply
}

func (s *Service) EventRespond(ctx context.Context, calendarID, eventID, reply string) (*RespondResult, error) {
	a, ok := answerOfReply(reply)
	if !ok {
		return nil, fmt.Errorf("calendar: %q is not an answer to an invitation", reply)
	}
	ev, err := s.rawEvent(ctx, calendarID, eventID)
	if err != nil {
		return nil, err
	}
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
	if !ok {
		return nil, &errs.NotFound{Kind: "attendee record for you on this event"}
	}

	if err := s.C.Decode(ctx, proton.Request{
		Method: "PUT",
		Path:   fmt.Sprintf("/calendar/v1/%s/events/%s/attendees/%s", calendarID, eventID, attendeeID),
		Body:   map[string]any{"Status": a.code, "UpdateTime": time.Now().Unix()},
	}, nil); err != nil {
		return nil, err
	}

	res := &RespondResult{Status: a.sentence}
	ck, err := s.unlockCalendar(ctx, calendarID)
	if err != nil {
		// Recorded and not counted: the answer is already on the shared event the
		// organizer reads; only the courtesy email that repeats it stays unsent.
		slog.DebugContext(ctx, "calendar: the calendar key would not open to write the reply email",
			"reason", "unlockable", "error", err)
		return res, nil
	}
	e := s.decrypt(ctx, ck, ev)
	if e.readErr != nil {
		// Recorded and not counted, for the same reason: the answer is recorded.
		slog.DebugContext(ctx, "calendar: the event would not decrypt to write the reply email",
			"reason", "undecryptable", "error", e.readErr)
		return res, nil
	}
	res.Title = e.model.Summary
	if organizer := e.model.Organizer; organizer != "" && !strings.EqualFold(organizer, selfEmail) {
		res.Reply = &Reply{
			ICS:        e.model.ReplyDocument(selfEmail, a.partstat, ev.IsProtonProtonInvite == 1),
			Recipients: []string{organizer},
			Subject:    replySubject(e.model.Start),
			Body:       replyBody(selfEmail, a, e.model.Summary),
		}
	}
	return res, nil
}
