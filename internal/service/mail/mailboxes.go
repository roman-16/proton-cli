package mail

import (
	"context"
	"strings"

	"github.com/roman-16/proton-cli/internal/errs"
)

// Mailboxes: the one ID space behind three ideas.
//
// Proton stores system folders, custom folders and custom labels as labels, and
// applies all of them with the same endpoint. What differs is the meaning: a
// message lives in exactly one folder, and carries any number of labels. The web
// client keeps that distinction visible - "Move to" and "Label as" are separate
// actions - and so does this CLI. Collapsing them into one flag would mean a
// custom label silently labelled instead of moving, and reported success.

// Mailbox is somewhere a message can be filed.
type Mailbox struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Folder is true for a place a message lives in, false for a tag it carries.
	Folder bool `json:"folder"`
	// System marks one of Proton's built-in folders, which cannot be renamed or
	// removed.
	System bool `json:"system"`
}

// Kind names what the mailbox is, for wording an error.
func (m Mailbox) Kind() string {
	if m.Folder {
		return "folder"
	}
	return "label"
}

// systemFolders are the built-in destinations, keyed by the names the CLI accepts.
var systemFolders = map[string]string{
	"inbox":     labelInbox,
	"drafts":    labelDrafts,
	"sent":      labelSent,
	"trash":     labelTrash,
	"spam":      labelSpam,
	"archive":   labelArchive,
	"starred":   labelStarred,
	"scheduled": labelScheduled,
	"snoozed":   labelSnoozed,
	"all":       labelAllMail,
	// The inbox categories. Proton shows them as tabs, and a tab is a place mail
	// is, so they are folders here like any other.
	"social":       labelSocial,
	"promotions":   labelPromotions,
	"updates":      labelUpdates,
	"newsletters":  labelNewsletters,
	"transactions": labelTransactions,
}

// SystemFolderNames lists the built-in folder names, for help text and
// completion.
func SystemFolderNames() []string {
	return []string{
		"inbox", "drafts", "sent", "trash", "spam", "archive", "starred",
		"scheduled", "snoozed", "all",
		"social", "promotions", "updates", "newsletters", "transactions",
	}
}

// ResolveFolder maps a folder alias to its label ID, passing anything unknown
// through so a raw ID works wherever a name does.
func ResolveFolder(name string) string {
	if id, ok := systemFolders[strings.ToLower(name)]; ok {
		return id
	}
	return name
}

// ResolveMailbox finds what a name or ID refers to, so a command can tell the
// user that the thing they named is a label when a folder was needed.
//
// System folders resolve without a request; anything else is looked up among the
// account's own folders and labels.
func (s *Service) ResolveMailbox(ctx context.Context, ref string) (Mailbox, error) {
	if ref == "" {
		return Mailbox{}, errs.Problemf("No mailbox given.")
	}
	lower := strings.ToLower(ref)
	if id, ok := systemFolders[lower]; ok {
		return Mailbox{ID: id, Name: lower, Folder: true, System: true}, nil
	}

	labels, folders, err := s.LabelsList(ctx)
	if err != nil {
		return Mailbox{}, err
	}
	all := make([]Mailbox, 0, len(labels)+len(folders))
	for _, f := range folders {
		all = append(all, Mailbox{ID: f.ID, Name: f.Name, Folder: true})
	}
	for _, l := range labels {
		all = append(all, Mailbox{ID: l.ID, Name: l.Name})
	}

	// An exact ID wins over a name, so an ID is never mistaken for a label whose
	// name happens to match it.
	for _, m := range all {
		if m.ID == ref {
			return m, nil
		}
	}
	var matches []Mailbox
	for _, m := range all {
		if strings.EqualFold(m.Name, ref) {
			matches = append(matches, m)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return Mailbox{}, &errs.NotFound{Kind: "folder or label", Ref: ref}
	}
	cands := make([]errs.Candidate, 0, len(matches))
	for _, m := range matches {
		cands = append(cands, errs.Candidate{ID: m.ID, Label: m.Kind()})
	}
	return Mailbox{}, &errs.Ambiguous{Kind: "folder or label", Ref: ref, Candidates: cands}
}

// MailboxNames is what each destination ID is called, so a stored ID can be
// shown as the word somebody chose for it.
//
// It is the reverse of ResolveMailbox and exists for the same reason: a rule
// Proton hands back names its destination by ID, and a listing that printed the
// ID would be reporting something nobody can read.
func (s *Service) MailboxNames(ctx context.Context) (map[string]string, error) {
	names := make(map[string]string, len(systemFolders))
	for name, id := range systemFolders {
		names[id] = name
	}
	labels, folders, err := s.LabelsList(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range append(append([]Label{}, folders...), labels...) {
		names[m.ID] = m.Name
	}
	return names, nil
}

// ResolveFolderTarget resolves a move destination, refusing a label.
//
// Refusing is the point: applying a label where a move was asked for is a
// different operation with a different outcome, and the CLI has a verb for it.
func (s *Service) ResolveFolderTarget(ctx context.Context, ref string) (Mailbox, error) {
	m, err := s.ResolveMailbox(ctx, ref)
	if err != nil {
		return m, err
	}
	if !m.Folder {
		return m, errs.Problemf("%q is a label, not a folder - moving needs a folder.", ref).
			Hint("to attach the label instead, use `label --label "+ref+"`.",
				"To see the folders, run `proton mail settings folders list`.").Exit(3)
	}
	return m, nil
}

// ResolveLabelTarget resolves a label to attach or detach, refusing a folder for
// the same reason in reverse.
func (s *Service) ResolveLabelTarget(ctx context.Context, ref string) (Mailbox, error) {
	m, err := s.ResolveMailbox(ctx, ref)
	if err != nil {
		return m, err
	}
	if m.Folder {
		return m, errs.Problemf("%q is a folder, not a label.", ref).
			Hint("to move there instead, use `move --into "+ref+"`.",
				"To see the labels, run `proton mail settings labels list`.").Exit(3)
	}
	return m, nil
}

// StarredLabelID is the label a star is. Exposing it keeps `star` honest about
// being `label` with one particular label.
const StarredLabelID = labelStarred
