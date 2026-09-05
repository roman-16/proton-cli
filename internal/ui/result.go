package ui

import (
	"fmt"
	"strings"
)

// Consequence is how much a mutation is worth stopping for.
//
// The CLI prompts for exactly one reason: something is about to be removed. The
// two hazards that makes real are different, so they are named separately - a
// wrong verb (`delete` typed where `trash` was meant) is dangerous however few
// things it touches, while a wrong filter is only dangerous because it selected
// things the user never named.
type Consequence int

const (
	// Ordinary changes are made without asking. Almost everything is ordinary:
	// a move, a label, a star is cheap to look at and cheaper to reverse.
	Ordinary Consequence = iota
	// OutOfSight takes things out of the working set but leaves them
	// recoverable. Worth a question only when a filter chose them, since a
	// reference typed by hand carries no surprise.
	OutOfSight
	// Forever cannot be undone by this CLI or by Proton's own clients, so it is
	// always asked about and always says so.
	Forever
)

// Action names a mutation in the three grammatical forms the CLI needs - the
// past tense for a confirmation, the infinitive for a preview, and a stable key
// for machine output - plus how much it is worth stopping for.
//
// The set below is the complete vocabulary of things this CLI does. A command
// that wants a word not in this list is a command that has invented one.
type Action struct {
	// Past opens the confirmation, and carries a preposition when the sentence
	// needs one to reach its subject: "Signed in as you@proton.me", not "Signed
	// in you@proton.me".
	Past string // "Moved"  → "✓ Moved 3 messages to trash."
	Verb string // "move"   → "Dry run - would move 3 messages to trash:"
	Key  string // "moved"  → {"action": "moved"}
	Cost Consequence
}

var (
	Created      = Action{"Created", "create", "created", Ordinary}
	Updated      = Action{"Updated", "update", "updated", Ordinary}
	Deleted      = Action{"Deleted", "delete", "deleted", Forever}
	Trashed      = Action{"Moved", "move", "trashed", OutOfSight}
	Restored     = Action{"Restored", "restore", "restored", Ordinary}
	Emptied      = Action{"Emptied", "empty", "emptied", Forever}
	Uninstalled  = Action{"Uninstalled", "uninstall", "uninstalled", Forever}
	Moved        = Action{"Moved", "move", "moved", Ordinary}
	Copied       = Action{"Copied", "copy", "copied", Ordinary}
	Uploaded     = Action{"Uploaded", "upload", "uploaded", Ordinary}
	Downloaded   = Action{"Downloaded", "download", "downloaded", Ordinary}
	Exported     = Action{"Exported", "export", "exported", Ordinary}
	Imported     = Action{"Imported", "import", "imported", Ordinary}
	Merged       = Action{"Merged", "merge", "merged", Ordinary}
	Resent       = Action{"Resent", "resend", "resent", Ordinary}
	Verified     = Action{"Verified", "verify", "verified", Ordinary}
	Blocked      = Action{"Blocked", "block", "blocked", Ordinary}
	Allowed      = Action{"Allowed", "allow", "allowed", Ordinary}
	Filed        = Action{"Filed", "file", "filed", Ordinary}
	Forgot       = Action{"Forgot", "forget", "forgot", Ordinary}
	Unsubscribed = Action{"Unsubscribed", "unsubscribe", "unsubscribed", Ordinary}
	Snoozed      = Action{"Snoozed", "snooze", "snoozed", Ordinary}
	Unsnoozed    = Action{"Unsnoozed", "unsnooze", "unsnoozed", Ordinary}
	Applied      = Action{"Applied", "apply", "applied", Ordinary}
	Reordered    = Action{"Reordered", "reorder", "reordered", Ordinary}
	Sent         = Action{"Sent", "send", "sent", Ordinary}
	Scheduled    = Action{"Scheduled", "schedule", "scheduled", Ordinary}
	Unscheduled  = Action{"Unscheduled", "unschedule", "unscheduled", Ordinary}
	Saved        = Action{"Saved", "save", "saved", Ordinary}
	Labelled     = Action{"Labelled", "label", "labelled", Ordinary}
	Unlabelled   = Action{"Unlabelled", "unlabel", "unlabelled", Ordinary}
	Starred      = Action{"Starred", "star", "starred", Ordinary}
	Unstarred    = Action{"Unstarred", "unstar", "unstarred", Ordinary}
	MarkedRead   = Action{"Marked", "mark", "marked_read", Ordinary}
	MarkedUnread = Action{"Marked", "mark", "marked_unread", Ordinary}
	Enabled      = Action{"Enabled", "enable", "enabled", Ordinary}
	Disabled     = Action{"Disabled", "disable", "disabled", Ordinary}
	Linked       = Action{"Created", "create", "linked", Ordinary}
	Unlinked     = Action{"Removed", "remove", "unlinked", Ordinary}
	Added        = Action{"Added", "add", "added", Ordinary}
	Removed      = Action{"Removed", "remove", "removed", Ordinary}
	Accepted     = Action{"Accepted", "accept", "accepted", Ordinary}
	Declined     = Action{"Declined", "decline", "declined", Ordinary}
	Favorited    = Action{"Favorited", "favorite", "favorited", Ordinary}
	Unfavorited  = Action{"Unfavorited", "unfavorite", "unfavorited", Ordinary}
	Pinned       = Action{"Pinned", "pin", "pinned", Ordinary}
	Unpinned     = Action{"Unpinned", "unpin", "unpinned", Ordinary}
	Responded    = Action{"Responded", "respond", "responded", Ordinary}
	Set          = Action{"Set", "set", "set", Ordinary}
	Invited      = Action{"Invited", "invite", "invited", Ordinary}
	Revoked      = Action{"Revoked", "revoke", "revoked", Ordinary}
	Transferred  = Action{"Transferred", "transfer", "transferred", Ordinary}
	SignedIn     = Action{"Signed in as", "sign in as", "signed_in", Ordinary}
	SignedOut    = Action{"Signed out", "sign out", "signed_out", Ordinary}
)

// Asks reports whether a change with this action stops for a yes.
//
// There are two reasons to stop, and no others: the change cannot be taken
// back, or it removes things a filter chose rather than things the user named.
// computed says which of those a selection was.
func (a Action) Asks(computed bool) bool {
	return a.Cost == Forever || (a.Cost == OutOfSight && computed)
}

// Actions is the vocabulary, for the conformance test to check against.
var Actions = []Action{
	Created, Updated, Deleted, Trashed, Restored, Emptied, Uninstalled, Moved,
	Copied, Uploaded, Downloaded, Exported, Imported, Merged, Resent, Verified, Blocked, Allowed, Filed, Forgot, Unsubscribed, Snoozed, Unsnoozed, Applied, Reordered, Sent, Scheduled, Unscheduled, Saved,
	Labelled, Unlabelled, Starred, Unstarred, MarkedRead, MarkedUnread,
	Enabled, Disabled, Linked, Unlinked, Added, Removed, Accepted, Declined,
	Favorited, Unfavorited, Pinned, Unpinned, Responded, Set, Invited, Revoked,
	Transferred, SignedIn, SignedOut,
}

// ResultSpec describes what a mutation did.
type ResultSpec struct {
	Action Action
	// Kind is the affected collection's plural noun ("messages"), matching
	// TableSpec.Noun so both halves of a command speak the same word.
	Kind  string
	Count int
	IDs   []string
	// Name is the affected thing's own name, used instead of a count when
	// exactly one thing was touched and naming it is more useful.
	Name string
	// Detail is a trailing clause: "to trash", "to /Documents", "as read".
	Detail string
	// EmitID prints the first ID on Out in text mode, so a script can capture
	// what was just created with a plain assignment.
	EmitID bool
	// DryRun switches to the preview form. Preview, when set, draws the
	// selection that would have been affected.
	DryRun  bool
	Preview func(*UI) error
	// Extra adds fields to the machine-format object.
	Extra map[string]any
	// AnswerFollows says the command writes a record of its own after this
	// result. The confirmation still goes to Err in text mode, but a machine
	// format stays silent here, so the record is the one document the command
	// produces rather than the second of two. A dry run writes no record, so it
	// reports as usual.
	AnswerFollows bool

	// Skipped is what the run could not read on the way to this change; see
	// IncompleteSpec. It is filled in by kit from the invocation's tally rather
	// than by the command, exactly as TableSpec.Skipped is, so that a change made
	// on a short reading says so at every moment it is described - the preview,
	// the question, the confirmation - instead of wherever somebody remembered.
	Skipped IncompleteSpec
}

// Result reports a mutation. In text mode the new ID (if any) goes to Out and
// the confirmation to Err, so a redirect captures the ID alone. In a machine
// format the whole result goes to Out, so --output json always means JSON.
func Result(u *UI, spec ResultSpec) error {
	if u.Format.Machine() {
		if spec.AnswerFollows && !spec.DryRun {
			return nil
		}
		return u.encode(spec.object())
	}

	if spec.DryRun {
		_, _ = fmt.Fprintf(u.Err, "%s\n", spec.dryRunLine())
		if spec.Preview != nil {
			_, _ = fmt.Fprintln(u.Err)
			if err := spec.Preview(u.preview()); err != nil {
				return err
			}
			if spec.Skipped.Count > 0 {
				_, _ = fmt.Fprintln(u.Err)
			}
		}
		u.Unread(spec.Skipped)
		return nil
	}

	if spec.EmitID && len(spec.IDs) > 0 && spec.IDs[0] != "" {
		_, _ = fmt.Fprintln(u.Out, spec.IDs[0])
	}
	if !u.Quiet {
		_, _ = fmt.Fprintf(u.Err, "%s %s\n", u.errStyle.Paint(Success, GlyphSuccess), spec.message())
	}
	u.Unread(spec.Skipped)
	return nil
}

// message composes the confirmation. Three shapes, chosen by what the caller
// actually knows:
//
//	Created label "Work".            one thing, and its name is worth saying
//	Uploaded report.pdf to /Docs.    one thing named, with no useful kind word
//	Moved 3 messages to trash.       a count
func (s ResultSpec) message() string {
	var b strings.Builder
	b.WriteString(s.Action.Past)
	b.WriteByte(' ')
	switch {
	case s.Count == 0:
		b.Reset()
		b.WriteString("Nothing to ")
		b.WriteString(s.Action.Verb)
	case s.Name != "" && s.Count == 1 && s.Kind != "":
		b.WriteString(Singular(s.Kind))
		b.WriteString(` "`)
		b.WriteString(s.Name)
		b.WriteString(`"`)
	case s.Name != "" && s.Count == 1:
		b.WriteString(s.Name)
	default:
		b.WriteString(Quantity(s.Count, s.Kind))
	}
	if s.Detail != "" {
		b.WriteByte(' ')
		b.WriteString(s.Detail)
	}
	return b.String() + "."
}

func (s ResultSpec) dryRunLine() string {
	return "Dry run - " + s.wouldLine("would", s.hasPreview())
}

// hasPreview reports whether there is a table of affected things to draw.
func (s ResultSpec) hasPreview() bool { return s.Preview != nil && s.Count > 0 }

// wouldLine describes the change in the conditional, which is the one sentence
// a preview and a confirmation both need. It ends in a colon when a table is
// about to follow and a full stop when nothing is.
func (s ResultSpec) wouldLine(opening string, withTable bool) string {
	subject := Quantity(s.Count, s.Kind)
	if s.Name != "" && s.Count == 1 {
		subject = s.Name
		if s.Kind != "" {
			subject = Singular(s.Kind) + ` "` + s.Name + `"`
		}
	}
	line := fmt.Sprintf("%s %s %s", opening, s.Action.Verb, subject)
	if s.Detail != "" {
		line += " " + s.Detail
	}
	if withTable {
		return line + ":"
	}
	return line + "."
}

// Refusal is what a confirmation becomes when there is nobody to ask: the same
// account of the change, stated rather than put as a question, so an unattended
// run's log says what it declined to do.
func (s ResultSpec) Refusal() string {
	// There is nobody here to show a table to, so the sentence simply closes.
	line := s.wouldLine("Would", false)
	if s.Action.Cost == Forever {
		line += " This cannot be undone."
	}
	return line
}

// Confirm describes what the change would do, shows the things it would touch,
// and asks. It is the same account of the change a dry run gives, put as a
// question instead of a report, so approving one means having read the other.
//
// Only Forever says the change cannot be undone, because only Forever is true:
// a confirmation that overstated the stakes would be one people learn to skip.
func Confirm(u *UI, spec ResultSpec) (bool, error) {
	question := "Continue?"
	if spec.Action.Cost == Forever {
		question = "This cannot be undone. " + question
	}
	// A machine format has no table to offer: a dry run does not render one
	// either, and a JSON document on stderr in front of a question would be for
	// nobody. The question is asked all the same - what is worth stopping for is
	// a property of the change, not of how its answer is printed.
	withTable := !u.Format.Machine() && spec.hasPreview()
	line := spec.wouldLine("Would", withTable)
	if withTable {
		_, _ = fmt.Fprintf(u.Err, "%s\n\n", line)
		if err := spec.Preview(u.preview()); err != nil {
			return false, err
		}
		_, _ = fmt.Fprintln(u.Err)
	} else {
		question = line + " " + question
	}
	// What the run could not read is said before the question, because it is
	// part of what is being approved: a selection short of a folder is not the
	// selection the table shows.
	if spec.Skipped.Count > 0 {
		u.Unread(spec.Skipped)
		_, _ = fmt.Fprintln(u.Err)
	}
	return u.Confirm(question)
}

func (s ResultSpec) object() map[string]any {
	obj := map[string]any{
		"action":  s.Action.Key,
		"count":   s.Count,
		"dry_run": s.DryRun,
	}
	if s.Kind != "" {
		obj["kind"] = Singular(s.Kind)
	}
	if len(s.IDs) > 0 {
		obj["ids"] = s.IDs
	}
	if s.Name != "" {
		obj["name"] = s.Name
	}
	// Omitted when nothing was skipped, for the same reason the table envelope
	// omits it: a consumer that never sees the key acted on a whole reading.
	if s.Skipped.Count > 0 {
		obj["skipped"] = s.Skipped.Count
	}
	for k, v := range s.Extra {
		obj[k] = v
	}
	return obj
}
