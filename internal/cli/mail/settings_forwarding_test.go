package mail

import (
	"strings"
	"testing"

	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
)

// What each verb takes is judged here, from the row alone, so a forwarding in a
// state a verb has nothing to do with is refused before anything is sent and
// before a dry run says what it would do.

func forwarding(direction, state string, encrypted bool) mailsvc.Forwarding {
	f := mailsvc.Forwarding{
		ID: "id", Direction: direction, State: state, Encrypted: encrypted,
		From: "jane@proton.me", To: "me@proton.me",
	}
	if direction == mailsvc.DirectionOutgoing {
		f.From, f.To = "me@proton.me", "jane@proton.me"
	}
	return f
}

func TestForwardingVerbsTakeOnlyWhatTheyCanAct(t *testing.T) {
	accept := answerable("accept", "accepted")
	decline := answerable("decline", "declined")

	tests := []struct {
		name  string
		takes func(mailsvc.Forwarding) error
		f     mailsvc.Forwarding
		// refusal is a fragment of the sentence, or "" when the verb takes it.
		refusal string
	}{
		{"accept one sent to you", accept,
			forwarding(mailsvc.DirectionIncoming, mailsvc.StatePending, true), ""},
		{"accept one already running", accept,
			forwarding(mailsvc.DirectionIncoming, mailsvc.StateActive, true), "is active, not pending"},
		{"accept one you set up", accept,
			forwarding(mailsvc.DirectionOutgoing, mailsvc.StatePending, true), "is one you set up"},
		{"decline one sent to you", decline,
			forwarding(mailsvc.DirectionIncoming, mailsvc.StatePending, true), ""},
		{"decline one you already declined", decline,
			forwarding(mailsvc.DirectionIncoming, mailsvc.StateRejected, true), "is rejected, not pending"},
		{"decline one you set up", decline,
			forwarding(mailsvc.DirectionOutgoing, mailsvc.StatePending, true), "only one sent to you can be declined"},

		{"disable a running one", pausable,
			forwarding(mailsvc.DirectionOutgoing, mailsvc.StateActive, true), ""},
		{"disable one nobody has accepted", pausable,
			forwarding(mailsvc.DirectionOutgoing, mailsvc.StatePending, true), "only an active one can be disabled"},
		{"disable one sent to you", pausable,
			forwarding(mailsvc.DirectionIncoming, mailsvc.StateActive, true), "was sent to you"},

		{"enable a paused one", resumable,
			forwarding(mailsvc.DirectionOutgoing, mailsvc.StatePaused, true), ""},
		{"enable a running one", resumable,
			forwarding(mailsvc.DirectionOutgoing, mailsvc.StateActive, true), "only a paused one can be enabled"},
		{"enable one sent to you", resumable,
			forwarding(mailsvc.DirectionIncoming, mailsvc.StatePaused, true), "was sent to you"},

		{"resend one the forwardee declined", resendable,
			forwarding(mailsvc.DirectionOutgoing, mailsvc.StateRejected, true), ""},
		{"resend one a key change left outdated", resendable,
			forwarding(mailsvc.DirectionOutgoing, mailsvc.StateOutdated, true), ""},
		{"resend one still waiting for an answer", resendable,
			forwarding(mailsvc.DirectionOutgoing, mailsvc.StatePending, true), "still waiting for their answer"},
		{"resend one already running", resendable,
			forwarding(mailsvc.DirectionOutgoing, mailsvc.StateActive, true), "nothing to send again"},
		{"resend the email to an address outside Proton", resendable,
			forwarding(mailsvc.DirectionOutgoing, mailsvc.StatePending, false), ""},
		{"resend one outside Proton that was declined", resendable,
			forwarding(mailsvc.DirectionOutgoing, mailsvc.StateRejected, false), ""},
		{"resend one sent to you", resendable,
			forwarding(mailsvc.DirectionIncoming, mailsvc.StateRejected, true), "was sent to you"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.takes(tc.f)
			switch {
			case tc.refusal == "" && err != nil:
				t.Errorf("refused a forwarding the verb takes: %v", err)
			case tc.refusal != "" && err == nil:
				t.Errorf("took a forwarding it cannot act on")
			case tc.refusal != "" && !strings.Contains(err.Error(), tc.refusal):
				t.Errorf("the refusal reads %q, want it to say %q", err, tc.refusal)
			}
		})
	}
}

// A forwarding goes by the other account's address, whichever way it runs. Your
// own address is on every row of the collection, so naming one by it would be
// naming all of them.
func TestForwardingIsNamedByTheOtherParty(t *testing.T) {
	incoming := forwarding(mailsvc.DirectionIncoming, mailsvc.StatePending, true)
	if got := counterparty(incoming); got != incoming.From {
		t.Errorf("an incoming forwarding goes by %q, want the forwarder %q", got, incoming.From)
	}
	outgoing := forwarding(mailsvc.DirectionOutgoing, mailsvc.StateActive, true)
	if got := counterparty(outgoing); got != outgoing.To {
		t.Errorf("an outgoing forwarding goes by %q, want the forwardee %q", got, outgoing.To)
	}
}
