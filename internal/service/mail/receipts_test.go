package mail

import (
	"testing"

	"github.com/roman-16/proton-cli/internal/account/keys"
)

func TestReceiptOfReadsWhatAMessageAsksFor(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  rawMessage
		want Receipt
	}{{
		name: "a message that asks nothing",
		raw:  rawMessage{Subject: "Hi"},
		want: Receipt{Subject: "Hi"},
	}, {
		// What arrives at the other end is the header, so that is what a reader
		// answers.
		name: "a request that arrived",
		raw: rawMessage{Subject: "Contract", ParsedHeaders: map[string]any{
			"Disposition-Notification-To": "Jane Roe <jane@proton.me>",
		}},
		want: Receipt{Subject: "Contract", Requested: true, To: "jane@proton.me"},
	}, {
		name: "a request already answered",
		raw: rawMessage{Subject: "Contract", Flags: flagReceiptSent, ParsedHeaders: map[string]any{
			"Disposition-Notification-To": "jane@proton.me",
		}},
		want: Receipt{Subject: "Contract", Requested: true, Sent: true, To: "jane@proton.me"},
	}, {
		// A draft carries the asking before there is a header to carry it.
		name: "a draft that will ask",
		raw:  rawMessage{Subject: "Contract", Flags: flagReceiptRequest},
		want: Receipt{Subject: "Contract", Requested: true},
	}, {
		name: "the copy you sent, which carries the request you made",
		raw: rawMessage{Subject: "Contract", Flags: flagSent | flagReceiptRequest,
			ParsedHeaders: map[string]any{"Disposition-Notification-To": "me@proton.me"}},
		want: Receipt{Subject: "Contract", Requested: true, To: "me@proton.me", Outgoing: true},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			if got := receiptOf(tc.raw); got != tc.want {
				t.Errorf("receiptOf = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// Only a request somebody else made, and only until it has been answered.
func TestOnlyAnUnansweredRequestFromSomebodyElseIsAnswerable(t *testing.T) {
	for _, tc := range []struct {
		name string
		r    Receipt
		want bool
	}{
		{name: "nothing asked", r: Receipt{}},
		{name: "asked", r: Receipt{Requested: true}, want: true},
		{name: "asked and answered", r: Receipt{Requested: true, Sent: true}},
		{name: "asked by you, on your own copy", r: Receipt{Requested: true, Outgoing: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.r.Answerable(); got != tc.want {
				t.Errorf("Answerable = %v, want %v", got, tc.want)
			}
		})
	}
}

// A draft is stored with the flag word Proton already holds, so asking for a
// receipt and taking the request off again leave the rest of it alone.
func TestDraftPayloadCarriesTheReceiptRequestAndNothingElse(t *testing.T) {
	const other = flagSent
	c := Content{
		From:  &Sender{Address: keys.Address{Email: "jane@proton.me"}},
		flags: other,
	}
	if got := draftPayload(c, "")["Flags"]; got != int64(other) {
		t.Errorf("Flags = %v with no request, want the flags the draft came with", got)
	}

	c.Receipt = true
	if got := draftPayload(c, "")["Flags"]; got != int64(other|flagReceiptRequest) {
		t.Errorf("Flags = %v with a request, want it added to what the draft came with", got)
	}

	c.flags, c.Receipt = other|flagReceiptRequest, false
	if got := draftPayload(c, "")["Flags"]; got != int64(other) {
		t.Errorf("Flags = %v with the request taken off, want only that bit gone", got)
	}
}
