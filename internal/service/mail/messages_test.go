package mail

import (
	"context"
	"strings"
	"testing"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
)

// Proton says what it made of a message in the flags of every envelope it sends,
// which is a bit position each. Reading the wrong one would report the wrong
// verdict about the honesty of somebody's mail, so each is pinned to the value
// the web client reads (MESSAGE_FLAGS in WebClients).
func TestVerdictsReadTheFlagsProtonSets(t *testing.T) {
	tests := []struct {
		name  string
		flags int64
		want  verdict
	}{
		{"an ordinary message carries no verdict", 0, verdict{}},
		{"dmarc failure", 1 << 26, verdict{dmarcFailed: true}},
		{"marked legitimate", 1 << 27, verdict{markedLegitimate: true}},
		{"flagged as phishing", 1 << 30, verdict{phishing: true}},
		{"flagged as suspicious", 1 << 33, verdict{suspicious: true}},
		{
			"phishing the reader has overruled",
			1<<30 | 1<<27,
			verdict{phishing: true, markedLegitimate: true},
		},
		{
			"every verdict at once, beside flags that mean other things",
			1<<1 | 1<<9 | 1<<26 | 1<<27 | 1<<30 | 1<<33,
			verdict{dmarcFailed: true, markedLegitimate: true, phishing: true, suspicious: true},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := verdicts(tc.flags); got != tc.want {
				t.Errorf("verdicts(%d) = %+v, want %+v", tc.flags, got, tc.want)
			}
		})
	}
}

// A listing row carries the verdict, which is what lets a listing mark a flagged
// message without asking Proton for anything more.
func TestToMessageCarriesTheVerdict(t *testing.T) {
	m := toMessage(rawListMessage{ID: "1", Flags: 1<<26 | 1<<30})
	if !m.DMARCFailed || !m.Phishing {
		t.Errorf("toMessage lost the verdict: %+v", m)
	}
	if m.MarkedLegitimate || m.Suspicious {
		t.Errorf("toMessage invented a verdict: %+v", m)
	}
}

// Flagged is what a reader is warned about, and the overrule reaches only half
// of it: saying a message is legitimate settles what the filters thought, and
// says nothing about a domain that would not vouch for it.
func TestFlaggedRespectsWhatAnOverruleCanSettle(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		want bool
	}{
		{"nothing against it", Message{}, false},
		{"phishing", Message{Phishing: true}, true},
		{"suspicious", Message{Suspicious: true}, true},
		{"phishing, overruled", Message{Phishing: true, MarkedLegitimate: true}, false},
		{"dmarc failed", Message{DMARCFailed: true}, true},
		{
			"dmarc failed, and the filters overruled",
			Message{DMARCFailed: true, Phishing: true, MarkedLegitimate: true},
			true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.msg.Flagged(); got != tc.want {
				t.Errorf("Flagged() = %v, want %v", got, tc.want)
			}
		})
	}
}

// openBody is the only thing that decrypts a body, so what it does when it
// cannot is what every reader inherits: an error to decide about, never a
// sentence standing where the message should be.
func TestOpenBodyReportsWhyItCouldNotOpenABody(t *testing.T) {
	kr := genMailKeyRing(t)
	other := genMailKeyRing(t)
	enc, err := kr.Encrypt(pgp.NewPlainMessageFromString("secret body"), nil)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	armored, err := enc.GetArmored()
	if err != nil {
		t.Fatalf("GetArmored: %v", err)
	}

	rings := func(kr *pgp.KeyRing) *keys.Unlocked {
		return &keys.Unlocked{
			Addresses: []keys.Address{{ID: "addr-1"}},
			AddrKRs:   map[string]keys.Rings{"addr-1": {Read: kr, Write: kr}},
		}
	}
	s := New(&fakeDoer{}, testKeys(nil))
	msg := rawMessage{ID: "m1", AddressID: "addr-1", Body: armored}

	t.Run("the body it can open", func(t *testing.T) {
		body, _, err := s.openBody(context.Background(), rings(kr), msg, false)
		if err != nil {
			t.Fatalf("openBody: %v", err)
		}
		if body != "secret body" {
			t.Errorf("body = %q", body)
		}
	})

	t.Run("a key that does not open it", func(t *testing.T) {
		_, _, err := s.openBody(context.Background(), rings(other), msg, false)
		if err == nil {
			t.Fatal("a body that would not decrypt came back as text")
		}
		if !strings.Contains(err.Error(), "decrypt message") {
			t.Errorf("err = %v, want it to say what failed", err)
		}
	})

	t.Run("no key at all", func(t *testing.T) {
		_, _, err := s.openBody(context.Background(), &keys.Unlocked{}, msg, false)
		if err == nil {
			t.Fatal("a message with no key to try came back as text")
		}
	})

	t.Run("a body that is not a PGP message", func(t *testing.T) {
		_, _, err := s.openBody(context.Background(), rings(kr), rawMessage{ID: "m2", AddressID: "addr-1", Body: "not armour"}, false)
		if err == nil {
			t.Fatal("a body that is not a message came back as text")
		}
	})
}

// The fallback is the first address that has keys, for a message whose own
// address this account no longer holds.
func TestOpenBodyFallsBackToTheFirstAddress(t *testing.T) {
	kr := genMailKeyRing(t)
	enc, err := kr.Encrypt(pgp.NewPlainMessageFromString("body"), nil)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	armored, err := enc.GetArmored()
	if err != nil {
		t.Fatalf("GetArmored: %v", err)
	}
	u := &keys.Unlocked{
		Addresses: []keys.Address{{ID: "addr-1"}},
		AddrKRs:   map[string]keys.Rings{"addr-1": {Read: kr, Write: kr}},
	}
	s := New(&fakeDoer{}, testKeys(nil))

	body, _, err := s.openBody(context.Background(), u, rawMessage{AddressID: "gone", Body: armored}, false)
	if err != nil {
		t.Fatalf("openBody: %v", err)
	}
	if body != "body" {
		t.Errorf("body = %q, want the message opened under the first address", body)
	}
}

// Reading one message is the one place a body that will not open is a failure:
// this is the message somebody asked for, and there is nothing else in the
// answer. A sentence standing in for it would be quoted, exported and piped as
// though it were the message.
func TestReadFailsOnABodyItCannotOpen(t *testing.T) {
	api := &recordingAPI{message: encryptedMessage(t, genMailKeyRing(t), "secret", mimeTypePlain)}
	s := New(api, testKeys(unlockedRings("addr-1", genMailKeyRing(t))))

	msg, err := s.Read(context.Background(), "m1")
	if err == nil {
		t.Fatalf("Read returned a message whose body never opened: %q", msg.Body)
	}
	if !strings.Contains(err.Error(), "decrypt") {
		t.Errorf("err = %v, want it to name what failed", err)
	}
}

// A decrypted message carries what Proton made of it, so the screen and the JSON
// have something to say it from.
func TestAsFullCarriesTheVerdict(t *testing.T) {
	full := asFull(rawMessage{ID: "m1", Flags: 1<<33 | 1<<27}, "body", "")
	if !full.Suspicious || !full.MarkedLegitimate {
		t.Errorf("asFull lost the verdict: %+v", full)
	}
	if full.Flagged() {
		t.Error("a suspicious message the reader overruled is still reported as flagged")
	}
	if !full.SpamFlagged() {
		t.Error("the filters' own verdict disappeared once it was overruled")
	}
}
