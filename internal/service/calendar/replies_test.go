package calendar

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/internal/ical"
	"github.com/roman-16/proton-cli/internal/proton"
)

const replyUID = "uid-reply"

func organized(sequence int, updated *int64) stored {
	raw := rawEvent{ID: "ev1", CalendarID: "cal1", UID: replyUID, IsOrganizer: 1}
	raw.AttendeesInfo.Attendees = []rawAttendee{
		{ID: "att-j", Token: attendeeToken(replyUID, "jane@example.com"), UpdateTime: updated},
	}
	return stored{raw: raw, model: ical.VEvent{UID: replyUID, Summary: "Design review", Sequence: sequence}}
}

func janesReply(partstat string, sequence int, stamp time.Time) ical.VEvent {
	return ical.VEvent{UID: replyUID, Sequence: sequence, DTStamp: stamp,
		Attendees: []ical.Attendee{{Email: "Jane@Example.com", PartStat: partstat}}}
}

var canonicalJane = map[string]canonicalAddr{"jane@example.com": "jane@example.com", "Jane@Example.com": "jane@example.com"}

func TestJudgeReplyRecordsTheSendersAnswer(t *testing.T) {
	stamp := time.Unix(2000, 0)
	got := judgeReply(janesReply("ACCEPTED", 0, stamp), organized(0, nil), canonicalJane, "jane@example.com", time.Unix(1, 0))
	r := got.Reply
	if r == nil {
		t.Fatalf("nothing to record: %q", got.Note)
	}
	if r.Answer != "accepted" || r.code != 3 || r.attendeeID != "att-j" || r.updateTime != 2000 || r.EventID != "ev1" {
		t.Errorf("reply = %+v", r)
	}
}

func TestJudgeReplyFallsBackToWhenTheMessageWasSent(t *testing.T) {
	got := judgeReply(janesReply("DECLINED", 0, time.Time{}), organized(0, nil), canonicalJane, "jane@example.com", time.Unix(3000, 0))
	if got.Reply == nil || got.Reply.updateTime != 3000 || got.Reply.Answer != "declined" {
		t.Errorf("verdict = %+v", got)
	}
}

func TestJudgeReplyLeavesTheEventAlone(t *testing.T) {
	newer, same := int64(5000), int64(2000)
	stamp := time.Unix(2000, 0)
	for _, c := range []struct {
		name   string
		reply  ical.VEvent
		master stored
		sender string
		note   string
	}{
		{"a stranger", janesReply("ACCEPTED", 0, stamp), organized(0, nil), "mallory@example.com", "is not invited"},
		{"another version", janesReply("ACCEPTED", 1, stamp), organized(2, nil), "jane@example.com", "another version"},
		{"an older answer", janesReply("ACCEPTED", 0, stamp), organized(0, &newer), "jane@example.com", "already holds a newer answer"},
		{"the same answer again", janesReply("ACCEPTED", 0, stamp), organized(0, &same), "jane@example.com", ""},
		{"no answer in it", janesReply("NEEDS-ACTION", 0, stamp), organized(0, nil), "jane@example.com", ""},
	} {
		got := judgeReply(c.reply, c.master, canonicalJane, c.sender, stamp)
		if got.Reply != nil {
			t.Errorf("%s: recorded %+v", c.name, got.Reply)
		}
		if c.note == "" && got.Note != "" || !strings.Contains(got.Note, c.note) {
			t.Errorf("%s: note = %q, want it to say %q", c.name, got.Note, c.note)
		}
	}
}

func TestReadReplyAsksNothingOfWhatIsNoGuestsAnswer(t *testing.T) {
	occurrence := ical.Timed(time.Unix(4000, 0), "UTC")
	one := janesReply("ACCEPTED", 0, time.Unix(2000, 0))
	one.RecurrenceID = &occurrence
	invitation := janesReply("ACCEPTED", 0, time.Unix(2000, 0))
	for _, c := range []struct {
		name, doc, note string
	}{
		{"an invitation", invitation.Document("REQUEST"), ""},
		{"a file", ical.Calendar([]ical.VEvent{invitation}), ""},
		{"a reply sent through Proton", invitation.ReplyDocument("jane@example.com", "ACCEPTED", true), ""},
		{"an answer to one occurrence", one.Document("REPLY"), "one occurrence"},
	} {
		d := &routeDoer{handler: func(proton.Request) ([]byte, error) { return nil, errors.New("nothing should be asked") }}
		got, err := New(d, testKeys(nil)).ReadReply(t.Context(), c.doc, "jane@example.com", time.Unix(1, 0))
		if err != nil || got.Reply != nil || !strings.Contains(got.Note, c.note) || (c.note == "" && got.Note != "") {
			t.Errorf("%s: verdict %+v, %v", c.name, got, err)
		}
		if len(d.reqs) != 0 {
			t.Errorf("%s: asked Proton %d times", c.name, len(d.reqs))
		}
	}
}
