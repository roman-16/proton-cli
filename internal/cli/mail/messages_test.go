package mail

import (
	"testing"

	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/ui"
)

// What Proton made of a message is shown where the message is read, as a field
// like any other: painted for a reader, named for a pipe, and absent from a
// message nobody doubts.

func field(fields []ui.Field, label string) (ui.Field, bool) {
	for _, f := range fields {
		if f.Label == label {
			return f, true
		}
	}
	return ui.Field{}, false
}

func TestMessageHeaderStatesWhatProtonConcluded(t *testing.T) {
	tests := []struct {
		name string
		msg  mailsvc.Full
		// flagged is the Flagged line, or "" when there should not be one.
		flagged string
		role    ui.Role
		dmarc   bool
	}{
		{name: "a message nothing is wrong with"},
		{
			name:    "phishing",
			msg:     mailsvc.Full{Phishing: true},
			flagged: "phishing", role: ui.Danger,
		},
		{
			name:    "suspicious",
			msg:     mailsvc.Full{Suspicious: true},
			flagged: "suspicious", role: ui.Danger,
		},
		{
			name:    "both verdicts at once",
			msg:     mailsvc.Full{Phishing: true, Suspicious: true},
			flagged: "phishing, suspicious", role: ui.Danger,
		},
		{
			name:    "a verdict the reader has overruled",
			msg:     mailsvc.Full{Phishing: true, MarkedLegitimate: true},
			flagged: "phishing (marked legitimate)", role: ui.Plain,
		},
		{
			name:  "a domain that would not vouch for it",
			msg:   mailsvc.Full{DMARCFailed: true},
			dmarc: true,
		},
		{
			name:    "a domain that would not vouch, and a verdict overruled",
			msg:     mailsvc.Full{DMARCFailed: true, Suspicious: true, MarkedLegitimate: true},
			flagged: "suspicious (marked legitimate)", role: ui.Plain,
			dmarc: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fields := messageHeader(&tc.msg)

			got, ok := field(fields, "Flagged")
			if ok != (tc.flagged != "") {
				t.Fatalf("Flagged present = %v, want %v", ok, tc.flagged != "")
			}
			if ok {
				if got.Value != tc.flagged {
					t.Errorf("Flagged = %q, want %q", got.Value, tc.flagged)
				}
				if got.Role != tc.role {
					t.Errorf("Flagged role = %v, want %v", got.Role, tc.role)
				}
			}

			dmarc, ok := field(fields, "DMARC")
			if ok != tc.dmarc {
				t.Fatalf("DMARC present = %v, want %v", ok, tc.dmarc)
			}
			// A reader can overrule the filters and not the domain, so the DMARC
			// line reads the same either way.
			if ok && (dmarc.Value != "failed" || dmarc.Role != ui.Danger) {
				t.Errorf("DMARC = %q/%v, want failed in the danger colour", dmarc.Value, dmarc.Role)
			}
		})
	}
}

// A read receipt is stated where the message is read: whether one was asked for,
// and whether it has been answered.
func TestMessageHeaderStatesWhereAReceiptStands(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  mailsvc.Full
		want string
	}{
		{name: "a message that asked for nothing"},
		{
			name: "a request nobody has answered",
			msg:  mailsvc.Full{ReceiptRequested: true, ReceiptDue: true},
			want: "requested",
		},
		{
			name: "a request already answered",
			msg:  mailsvc.Full{ReceiptRequested: true, ReceiptSent: true},
			want: "sent",
		},
		{
			name: "a request of your own, on a message you sent",
			msg:  mailsvc.Full{ReceiptRequested: true},
			want: "requested",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := field(messageHeader(&tc.msg), "Receipt")
			if ok != (tc.want != "") {
				t.Fatalf("Receipt present = %v, want %v", ok, tc.want != "")
			}
			if ok && got.Value != tc.want {
				t.Errorf("Receipt = %q, want %q", got.Value, tc.want)
			}
		})
	}
}

// The ID closes the block wherever a verdict lands, because that is the field a
// reader copies to act on what they have just read.
func TestMessageHeaderKeepsTheIDLast(t *testing.T) {
	fields := messageHeader(&mailsvc.Full{ID: "m1", DMARCFailed: true, Phishing: true})
	if last := fields[len(fields)-1]; last.Label != "ID" || !last.ID {
		t.Errorf("last field = %+v, want the ID", last)
	}
}

// The FLAGS column marks a message Proton distrusts, and stops as soon as the
// reader says otherwise.
func TestListFlagsMarkAMessageProtonFlagged(t *testing.T) {
	marks := flags(false, false, true, 0).String()
	if marks != ui.GlyphFlagged {
		t.Errorf("marks = %q, want %q", marks, ui.GlyphFlagged)
	}
	if got := flags(true, true, true, 2).String(); got != ui.GlyphUnread+ui.GlyphStarred+ui.GlyphFlagged+"2" {
		t.Errorf("marks = %q, want the four in their fixed order", got)
	}
	if got := flags(false, false, false, 0).String(); got != "" {
		t.Errorf("marks = %q, want nothing for an ordinary message", got)
	}
}
