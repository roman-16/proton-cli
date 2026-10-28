package ui

import (
	"fmt"
	"strings"

	"github.com/roman-16/proton-cli/internal/inflect"
)

type Consequence int

const (
	Ordinary Consequence = iota
	OutOfSight
	// Forever cannot be undone by this CLI or by Proton's own clients, so it is
	// always asked about and always says so.
	Forever
)

type Action struct {
	Past string
	Verb string
	Key  string
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
	Cancelled    = Action{"Cancelled", "cancel", "cancelled", Ordinary}
	Resumed      = Action{"Resumed", "resume", "resumed", Ordinary}
	Undone       = Action{"Undone", "undo", "undone", Forever}
	Merged       = Action{"Merged", "merge", "merged", Ordinary}
	Indexed      = Action{"Indexed", "index", "indexed", Ordinary}
	Resent       = Action{"Resent", "resend", "resent", Ordinary}
	Verified     = Action{"Verified", "verify", "verified", Ordinary}
	Blocked      = Action{"Blocked", "block", "blocked", Ordinary}
	Allowed      = Action{"Allowed", "allow", "allowed", Ordinary}
	Filed        = Action{"Filed", "file", "filed", Ordinary}
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

	MarkedLegitimate = Action{"Marked", "mark", "marked_legitimate", Ordinary}
	Reported         = Action{"Reported", "report", "reported", Ordinary}
	Enabled          = Action{"Enabled", "enable", "enabled", Ordinary}
	Disabled         = Action{"Disabled", "disable", "disabled", Ordinary}
	Added            = Action{"Added", "add", "added", Ordinary}
	Removed          = Action{"Removed", "remove", "removed", Ordinary}
	Confirmed        = Action{"Confirmed", "confirm", "confirmed", Ordinary}
	Left             = Action{"Left", "leave", "left", Forever}
	Accepted         = Action{"Accepted", "accept", "accepted", Ordinary}
	Declined         = Action{"Declined", "decline", "declined", Ordinary}
	Favorited        = Action{"Favorited", "favorite", "favorited", Ordinary}
	Unfavorited      = Action{"Unfavorited", "unfavorite", "unfavorited", Ordinary}
	Pinned           = Action{"Pinned", "pin", "pinned", Ordinary}
	Unpinned         = Action{"Unpinned", "unpin", "unpinned", Ordinary}
	Hidden           = Action{"Hid", "hide", "hidden", Ordinary}
	Unhidden         = Action{"Unhid", "unhide", "unhidden", Ordinary}
	Excluded         = Action{"Excluded", "exclude", "excluded", Ordinary}
	Included         = Action{"Included", "include", "included", Ordinary}
	Trusted          = Action{"Trusted", "trust", "trusted", Ordinary}
	Untrusted        = Action{"Untrusted", "untrust", "untrusted", Ordinary}
	Reactivated      = Action{"Reactivated", "reactivate", "reactivated", Ordinary}
	Reopened         = Action{"Reopened", "reopen", "reopened", Ordinary}
	Resolved         = Action{"Resolved", "resolve", "resolved", Ordinary}
	Granted          = Action{"Granted", "grant", "granted", Ordinary}
	Requested        = Action{"Requested", "request", "requested", Ordinary}
	Accessed         = Action{"Signed in as", "access", "accessed", Ordinary}
	Responded        = Action{"Responded", "respond", "responded", Ordinary}
	Set              = Action{"Set", "set", "set", Ordinary}
	Invited          = Action{"Invited", "invite", "invited", Ordinary}
	Revoked          = Action{"Revoked", "revoke", "revoked", Ordinary}
	Rotated          = Action{"Rotated", "rotate", "rotated", Ordinary}
	Transferred      = Action{"Transferred", "transfer", "transferred", Ordinary}
	SignedIn         = Action{"Signed in as", "sign in as", "signed_in", Ordinary}
	SignedOut        = Action{"Signed out", "sign out", "signed_out", Ordinary}
)

func (a Action) Asks(computed bool) bool {
	return a.Cost == Forever || (a.Cost == OutOfSight && computed)
}

var Actions = []Action{
	Created, Updated, Deleted, Trashed, Restored, Emptied, Uninstalled, Moved,
	Copied, Uploaded, Downloaded, Exported, Imported, Cancelled, Resumed, Undone,
	Indexed, Merged, Resent, Verified, Blocked, Allowed, Filed, Unsubscribed, Snoozed, Unsnoozed, Applied, Reordered, Sent, Scheduled, Unscheduled, Saved,
	Labelled, Unlabelled, Starred, Unstarred, MarkedRead, MarkedUnread,
	MarkedLegitimate, Reported,
	Enabled, Disabled, Added, Removed, Confirmed, Left, Accepted, Declined,
	Favorited, Unfavorited, Pinned, Unpinned, Hidden, Unhidden, Excluded, Included, Trusted, Untrusted,
	Reactivated, Reopened, Resolved, Responded, Set, Invited, Revoked, Rotated, Granted,
	Requested, Accessed,
	Transferred, SignedIn, SignedOut,
}

type ResultSpec struct {
	Action        Action
	Kind          string
	Count         int
	IDs           []string
	Name          string
	Detail        string
	EmitIDs       bool
	DryRun        bool
	Preview       func(*UI) error
	Extra         map[string]any
	AnswerFollows bool

	Skipped IncompleteSpec
}

func Result(u *UI, spec ResultSpec) error {
	if u.Format.Machine() {
		if spec.AnswerFollows && !spec.DryRun {
			return nil
		}
		return u.encode(spec.object(), 0)
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

	if spec.EmitIDs {
		for _, id := range spec.IDs {
			if id != "" {
				_, _ = fmt.Fprintln(u.Out, id)
			}
		}
	}
	if !u.Quiet {
		_, _ = fmt.Fprintf(u.Err, "%s %s\n", u.errStyle.Paint(Success, GlyphSuccess), spec.message())
	}
	u.Unread(spec.Skipped)
	return nil
}

func (s ResultSpec) message() string {
	if s.Count == 0 {
		return "Nothing to " + s.untouched()
	}
	var b strings.Builder
	b.WriteString(s.Action.Past)
	b.WriteByte(' ')
	switch {
	case s.Name != "" && s.Count == 1 && s.Kind != "":
		b.WriteString(inflect.Singular(s.Kind))
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
	if s.Count == 0 {
		return "Dry run - nothing to " + s.untouched()
	}
	return "Dry run - " + s.wouldLine("would", s.hasPreview())
}

func (s ResultSpec) untouched() string {
	line := s.Action.Verb
	if s.Detail != "" {
		line += " " + s.Detail
	}
	return line + "."
}

func (s ResultSpec) hasPreview() bool { return s.Preview != nil && s.Count > 0 }

func (s ResultSpec) wouldLine(opening string, withTable bool) string {
	subject := Quantity(s.Count, s.Kind)
	if s.Name != "" && s.Count == 1 {
		subject = s.Name
		if s.Kind != "" {
			subject = inflect.Singular(s.Kind) + ` "` + s.Name + `"`
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

func (s ResultSpec) Refusal() string {
	line := s.wouldLine("Would", false)
	if s.Action.Cost == Forever {
		line += " This cannot be undone."
	}
	return line
}

func Confirm(u *UI, spec ResultSpec) (bool, error) {
	question := "Continue?"
	if spec.Action.Cost == Forever {
		question = "This cannot be undone. " + question
	}
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
		obj["kind"] = inflect.Singular(s.Kind)
	}
	if len(s.IDs) > 0 {
		obj["ids"] = s.IDs
	}
	if s.Name != "" {
		obj["name"] = s.Name
	}
	if s.Skipped.Count > 0 {
		obj["skipped"] = s.Skipped.Count
	}
	for k, v := range s.Extra {
		obj[k] = v
	}
	return obj
}
