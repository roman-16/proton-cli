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

// Reason is why one thing could not be shown. The set is closed: a reason is
// written to the log under a Keep policy, so it has to be a word chosen here
// rather than a message assembled at the call site out of whatever was to hand.
type Reason string

const (
	// Inactive is a key Proton has marked as no longer in use.
	Inactive Reason = "inactive"
	// Malformed is a blob that did not parse: not base64, not armoured, not the
	// protocol buffer it was supposed to be.
	Malformed Reason = "malformed"
	// NoKey is a thing whose key never arrived, so there was nothing to try.
	NoKey Reason = "no key"
	// PostQuantum is a key Proton wrote with post-quantum algorithms, which the
	// OpenPGP libraries here do not implement.
	PostQuantum Reason = "post-quantum"
	// Undecryptable is a blob that parsed and that the key would not open.
	Undecryptable Reason = "undecryptable"
	// Unreadable is a thing a further request was needed for, and the request
	// failed.
	Unreadable Reason = "unreadable"
	// Unlockable is a key that would not open with the passphrase it should
	// have opened with.
	Unlockable Reason = "unlockable"
	// Untokenized is an address key whose token this account's user key cannot
	// decrypt, which is the shape a key hierarchy in trouble has.
	Untokenized Reason = "token not decryptable by user key"
)

// Kind is the sort of thing that went missing, in the singular, as a reader
// would name it. It is what the warning is phrased from, so a listing of items
// says "item" and a listing of vaults says "vault".
type Kind string

const (
	KindAddress    Kind = "address"
	KindCalendar   Kind = "calendar"
	KindContact    Kind = "contact"
	KindEvent      Kind = "event"
	KindFolder     Kind = "folder"
	KindInvitation Kind = "invitation"
	KindItem       Kind = "item"
	KindKey        Kind = "key"
	KindMember     Kind = "member"
	KindProfile    Kind = "profile"
	KindReminder   Kind = "reminder"
	KindShare      Kind = "share"
	KindVault      Kind = "vault"
)

// Hides marks the kinds whose loss takes more with it than itself.
//
// A skipped item is one row missing from a listing. A skipped folder or vault is
// a container, and everything inside it is missing too - which the count cannot
// express, because what was inside is exactly what could not be read. The
// warning has to say so instead of reporting one.
var Hides = map[Kind]bool{
	KindFolder: true,
	KindShare:  true,
	KindVault:  true,
}

// Tally counts what one invocation could not show.
type Tally struct {
	count atomic.Int64
	kind  atomic.Value
	hides atomic.Bool
}

// Count is how many things went missing.
func (t *Tally) Count() int {
	if t == nil {
		return 0
	}
	return int(t.count.Load())
}

// Kind is what went missing. When an invocation skipped more than one sort of
// thing, the first stands for all of them: the warning exists to say the answer
// is short, and the log is where the breakdown lives.
func (t *Tally) Kind() Kind {
	if t == nil {
		return ""
	}
	k, _ := t.kind.Load().(Kind)
	return k
}

// Hides reports whether anything that went missing took its contents with it.
func (t *Tally) Hides() bool { return t != nil && t.hides.Load() }

type tallyKey struct{}

// With returns a context that counts skips, and the tally they land in. Every
// invocation gets one, so no service has to be told whether anybody is counting.
func With(ctx context.Context) (context.Context, *Tally) {
	t := &Tally{}
	return context.WithValue(ctx, tallyKey{}, t), t
}

// From returns the tally on the context, or nil when nothing is counting -
// which is the ordinary case in a unit test and never the case under a command.
func From(ctx context.Context) *Tally {
	t, _ := ctx.Value(tallyKey{}).(*Tally)
	return t
}

// Record notes that one thing could not be shown, and why.
//
// ref is the thing's own ID, which the log writes as a handle rather than as
// itself, so that a reader can tell two failures about one item from one failure
// about each of two. It is the only argument a caller has to think about: the
// kind and the reason are words from the lists above.
//
// Every attribute is written under a name spelled out here, including the ones
// with nothing to say. A record assembled out of whatever happened to be to
// hand is a record nothing can check the names of, and the names are what decide
// whether a value may be written at all.
func Record(ctx context.Context, kind Kind, ref string, reason Reason, cause error) {
	detail := ""
	if cause != nil {
		detail = cause.Error()
	}
	slog.DebugContext(ctx, "not shown",
		"kind", string(kind), "reason", string(reason), "ref", ref, "error", detail)

	t := From(ctx)
	if t == nil {
		return
	}
	t.count.Add(1)
	t.kind.CompareAndSwap(nil, kind)
	if Hides[kind] {
		t.hides.Store(true)
	}
}
