// Package skip is how the CLI admits that an answer is incomplete.
//
// Decryption happens on this machine, over data Proton hands back in bulk, and
// any single item in a listing can fail to open: a key that will not unlock, a
// blob that will not parse, a token this account cannot decrypt. The loop has to
// carry on - one damaged item is no reason to refuse the other forty-one - but
// carrying on quietly is how a listing comes to under-report and exit zero,
// which is a wrong answer dressed as a right one.
//
// So a skip is recorded rather than swallowed. It is written to the diagnostic
// log with the reason, and counted on the invocation so that whatever renders
// the answer can say a thing is missing. Both halves matter: the log is what
// makes it fixable, and the count is what stops the person at the terminal
// trusting a listing that is short.
package skip

import (
	"context"
	"log/slog"
	"sync/atomic"
)

type Reason string

const (
	// Inactive is a key Proton has marked as no longer in use.
	Inactive      Reason = "inactive"
	Locked        Reason = "sealed to a locked key"
	Malformed     Reason = "malformed"
	NoKey         Reason = "no key"
	Undecryptable Reason = "undecryptable"
	Unreadable    Reason = "unreadable"
	Unlockable    Reason = "unlockable"
	Untokenized   Reason = "token not decryptable by user key"
)

type Kind string

const (
	KindAddress     Kind = "address"
	KindAlbum       Kind = "album"
	KindAttachment  Kind = "attachment"
	KindBookingPage Kind = "booking page"
	KindCalendar    Kind = "calendar"
	KindContact     Kind = "contact"
	KindEvent       Kind = "event"
	KindFolder      Kind = "folder"
	KindInvitation  Kind = "invitation"
	KindItem        Kind = "item"
	KindKey         Kind = "key"
	KindMeeting     Kind = "meeting"
	KindMember      Kind = "member"
	KindMessage     Kind = "message"
	KindProfile     Kind = "profile"
	KindReminder    Kind = "reminder"
	KindShare       Kind = "share"
	KindVault       Kind = "vault"
	KindVolume      Kind = "volume"
)

var Hides = map[Kind]bool{
	KindAlbum:    true,
	KindCalendar: true,
	KindFolder:   true,
	KindShare:    true,
	KindVault:    true,
}

type Tally struct {
	count  atomic.Int64
	locked atomic.Int64
	kind   atomic.Value
	hides  atomic.Bool
}

func (t *Tally) Count() int {
	if t == nil {
		return 0
	}
	return int(t.count.Load())
}

func (t *Tally) Locked() int {
	if t == nil {
		return 0
	}
	return int(t.locked.Load())
}

func (t *Tally) Kind() Kind {
	if t == nil {
		return ""
	}
	k, _ := t.kind.Load().(Kind)
	return k
}

func (t *Tally) Hides() bool { return t != nil && t.hides.Load() }

type tallyKey struct{}

func With(ctx context.Context) (context.Context, *Tally) {
	t := &Tally{}
	return context.WithValue(ctx, tallyKey{}, t), t
}

func From(ctx context.Context) *Tally {
	t, _ := ctx.Value(tallyKey{}).(*Tally)
	return t
}

func Record(ctx context.Context, kind Kind, ref string, reason Reason, cause error) {
	slog.DebugContext(ctx, "not shown",
		"kind", string(kind), "reason", string(reason), "ref", ref, "error", cause)

	t := From(ctx)
	if t == nil {
		return
	}
	t.count.Add(1)
	if reason == Locked {
		t.locked.Add(1)
	}
	t.kind.CompareAndSwap(nil, kind)
	if Hides[kind] {
		t.hides.Store(true)
	}
}
