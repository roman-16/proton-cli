package mail

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/skip"
)

// A thread is read message by message, and one of them that will not open is no
// reason to refuse the other two - as long as the reader is told, which is what
// the tally is for. A thread quietly one message short is a wrong answer with a
// count that agrees with itself.
func TestConversationReadDropsWhatItCannotOpenAndSaysSo(t *testing.T) {
	mine, theirs := genMailKeyRing(t), genMailKeyRing(t)
	api := &threadAPI{messages: []map[string]any{
		encryptedMessage(t, mine, "the first", mimeTypePlain),
		encryptedMessage(t, theirs, "sealed to a key this account has not got", mimeTypePlain),
		encryptedMessage(t, mine, "the last", mimeTypePlain),
	}}
	ctx, tally := skip.With(context.Background())

	conv, err := New(api, testKeys(unlockedRings("addr-1", mine))).ConversationRead(ctx, "c1")
	if err != nil {
		t.Fatalf("ConversationRead: %v", err)
	}

	if len(conv.Messages) != 2 {
		t.Fatalf("thread has %d messages, want the two that opened", len(conv.Messages))
	}
	if conv.Messages[0].Body != "the first" || conv.Messages[1].Body != "the last" {
		t.Errorf("bodies = %q, %q", conv.Messages[0].Body, conv.Messages[1].Body)
	}
	if tally.Count() != 1 {
		t.Errorf("tally = %d, want 1: a thread short of a message has to say so", tally.Count())
	}
	if tally.Kind() != skip.KindMessage {
		t.Errorf("tally kind = %q, want %q", tally.Kind(), skip.KindMessage)
	}
}

// Proton sends the newest body and leaves the older ones to be asked for. A
// request for one that fails takes the message with it, which is a message
// missing for a different reason and counted the same way.
func TestConversationReadCountsABodyItCouldNotFetch(t *testing.T) {
	mine := genMailKeyRing(t)
	newest := encryptedMessage(t, mine, "the newest", mimeTypePlain)
	older := map[string]any{"ID": "m0", "AddressID": "addr-1", "Body": "", "Time": float64(1)}
	newest["Time"] = float64(2)

	api := &threadAPI{messages: []map[string]any{older, newest}, refuseReads: true}
	ctx, tally := skip.With(context.Background())

	conv, err := New(api, testKeys(unlockedRings("addr-1", mine))).ConversationRead(ctx, "c1")
	if err != nil {
		t.Fatalf("ConversationRead: %v", err)
	}
	if len(conv.Messages) != 1 || conv.Messages[0].Body != "the newest" {
		t.Fatalf("thread = %+v, want only the message whose body arrived", conv.Messages)
	}
	if tally.Count() != 1 {
		t.Errorf("tally = %d, want 1", tally.Count())
	}
}

// threadAPI answers a conversation read, and optionally refuses the follow-up
// request for an older message's body.
type threadAPI struct {
	messages    []map[string]any
	refuseReads bool
}

func (a *threadAPI) Do(_ context.Context, _ proton.Request) (*proton.Response, error) {
	return &proton.Response{Status: 200, Body: []byte(`{"Code":1000}`)}, nil
}

func (a *threadAPI) Decode(_ context.Context, req proton.Request, out any) error {
	if out == nil {
		return nil
	}
	answer := map[string]any{"Code": 1000}
	switch {
	case req.Path == "/mail/v4/conversations/c1":
		answer["Conversation"] = map[string]any{"ID": "c1", "Subject": "Thread"}
		answer["Messages"] = a.messages
	case a.refuseReads:
		return &proton.APIError{HTTPStatus: 500}
	}
	b, err := json.Marshal(answer)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
