package calendar

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/ical"
	"github.com/roman-16/proton-cli/internal/proton"
)

type GuestReply struct {
	Answer     string
	CalendarID string
	EventID    string
	Guest      string
	Title      string

	attendeeID string
	code       int
	updateTime int64
}

type ReplyVerdict struct {
	Reply *GuestReply
	Note  string
}

func (s *Service) ReadReply(ctx context.Context, doc, sender string, sent time.Time) (ReplyVerdict, error) {
	if ical.Method(doc) != "REPLY" {
		return ReplyVerdict{}, nil
	}
	events, err := ical.ParseCalendar(doc)
	if err != nil || len(events) == 0 {
		return ReplyVerdict{}, err
	}
	reply := events[0]
	if reply.ProtonReply() {
		return ReplyVerdict{}, nil
	}
	if reply.IsOverride() {
		return ReplyVerdict{Note: fmt.Sprintf("%s answered one occurrence of an event; an answer to one occurrence is not recorded.", sender)}, nil
	}
	master, err := s.organizedEvent(ctx, reply.UID)
	if err != nil || master == nil {
		return ReplyVerdict{Note: "The event this message answers is not one you organize."}, err
	}
	canonical, err := s.canonicalEmails(ctx, append(emailsOf(reply), sender))
	if err != nil {
		return ReplyVerdict{}, err
	}
	return judgeReply(reply, *master, canonical, sender, sent), nil
}

func judgeReply(reply ical.VEvent, master stored, canonical map[string]canonicalAddr, sender string, sent time.Time) ReplyVerdict {
	title := master.model.Summary
	guest, invited := rawAttendeeByToken(master.raw.AttendeesInfo.Attendees, attendeeToken(reply.UID, canonical[sender]))
	if !invited {
		return ReplyVerdict{Note: fmt.Sprintf("%s is not invited to %q, so their answer is not recorded.", sender, title)}
	}
	a, answeredIt := senderAnswer(reply, canonical, canonical[sender])
	if !answeredIt {
		return ReplyVerdict{}
	}
	if reply.Sequence != master.model.Sequence {
		return ReplyVerdict{Note: fmt.Sprintf("%s answered another version of %q, so the event is left as it is.", sender, title)}
	}
	stamp := reply.DTStamp
	if stamp.IsZero() {
		stamp = sent
	}
	at := stamp.Unix()
	if guest.UpdateTime != nil && at <= *guest.UpdateTime {
		if at < *guest.UpdateTime {
			return ReplyVerdict{Note: fmt.Sprintf("%q already holds a newer answer from %s.", title, sender)}
		}
		return ReplyVerdict{}
	}
	return ReplyVerdict{Reply: &GuestReply{
		Answer: a.word, CalendarID: master.raw.CalendarID, EventID: master.raw.ID, Guest: sender, Title: title,
		attendeeID: guest.ID, code: a.code, updateTime: at,
	}}
}

func (s *Service) RecordReply(ctx context.Context, r *GuestReply) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT",
		Path:   fmt.Sprintf("/calendar/v1/%s/events/%s/attendees/%s", r.CalendarID, r.EventID, r.attendeeID),
		Body:   map[string]any{"Status": r.code, "UpdateTime": r.updateTime},
	}, nil)
}

func (s *Service) organizedEvent(ctx context.Context, uid string) (*stored, error) {
	raws, err := s.eventsByUID(ctx, uid)
	if err != nil {
		return nil, err
	}
	for _, raw := range raws {
		if raw.IsOrganizer != 1 {
			continue
		}
		ck, err := s.unlockCalendar(ctx, raw.CalendarID)
		if err != nil {
			return nil, err
		}
		e := s.decrypt(ctx, ck, raw)
		if e.readErr != nil {
			return nil, e.readErr
		}
		if !e.model.IsOverride() {
			return &e, nil
		}
	}
	return nil, nil
}

func (s *Service) eventsByUID(ctx context.Context, uid string) ([]rawEvent, error) {
	q := url.Values{}
	q.Set("UID", uid)
	q.Set("PageSize", fmt.Sprintf("%d", eventsPageSize))
	raws, err := proton.All(ctx, func(ctx context.Context, page int) ([]rawEvent, bool, error) {
		q.Set("Page", fmt.Sprintf("%d", page))
		var r struct{ Events []rawEvent }
		if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/calendar/v1/events", Query: q}, &r); err != nil {
			return nil, false, err
		}
		return r.Events, proton.Full(r.Events, eventsPageSize), nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.withEveryAttendee(ctx, raws); err != nil {
		return nil, err
	}
	return raws, nil
}

func rawAttendeeByToken(attendees []rawAttendee, token string) (rawAttendee, bool) {
	for _, a := range attendees {
		if a.Token == token {
			return a, true
		}
	}
	return rawAttendee{}, false
}

func senderAnswer(reply ical.VEvent, canonical map[string]canonicalAddr, sender canonicalAddr) (answer, bool) {
	for _, a := range reply.Attendees {
		if canonical[a.Email] != sender && !strings.EqualFold(a.Email, string(sender)) {
			continue
		}
		return answerWhere(func(known answer) bool { return strings.EqualFold(known.partstat, a.PartStat) })
	}
	return unanswered, false
}
