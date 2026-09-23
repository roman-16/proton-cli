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

type builtIn struct {
	name, id  string
	tab       bool
	label     bool
	movable   bool
	emptiable bool
}

var builtIns = []builtIn{
	{name: "inbox", id: labelInbox, movable: true},
	{name: "primary", id: labelPrimary, tab: true, movable: true},
	{name: "social", id: labelSocial, tab: true, movable: true},
	{name: "promotions", id: labelPromotions, tab: true, movable: true},
	{name: "newsletters", id: labelNewsletters, tab: true, movable: true},
	{name: "transactions", id: labelTransactions, tab: true, movable: true},
	{name: "updates", id: labelUpdates, tab: true, movable: true},
	{name: "drafts", id: labelDrafts},
	{name: "scheduled", id: labelScheduled},
	{name: "snoozed", id: labelSnoozed, emptiable: true},
	{name: "sent", id: labelSent},
	{name: "starred", id: labelStarred, label: true},
	{name: "archive", id: labelArchive, movable: true},
	{name: "spam", id: labelSpam, movable: true, emptiable: true},
	{name: "trash", id: labelTrash, movable: true, emptiable: true},
	{name: "all", id: labelAllMail},
}

func builtInNamed(name string) (builtIn, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, b := range builtIns {
		if b.name == name {
			return b, true
		}
	}
	return builtIn{}, false
}

func builtInWithID(id string) (builtIn, bool) {
	for _, b := range builtIns {
		if b.id == id {
			return b, true
		}
	}
	return builtIn{}, false
}

func isCategory(id string) bool {
	b, ok := builtInWithID(id)
	return ok && b.tab
}

func builtInNames(keep func(builtIn) bool) []string {
	var out []string
	for _, b := range builtIns {
		if keep(b) {
			out = append(out, b.name)
		}
	}
	return out
}

// SystemFolderNames lists the built-in folder names in the order the web's
// sidebar shows them, for help text and completion.
func SystemFolderNames() []string {
	return builtInNames(func(builtIn) bool { return true })
}

func MoveTargetNames() []string {
	return builtInNames(func(b builtIn) bool { return b.movable })
}

func EmptiableFolderNames() []string {
	return builtInNames(func(b builtIn) bool { return b.emptiable })
}

func Emptiable(ref string) bool {
	b, ok := builtInNamed(ref)
	return !ok || b.emptiable
}

// ResolveMailbox finds what a name or ID refers to, so a command can tell the
// user that the thing they named is a label when a folder was needed.
//
// Built-in folders resolve without a request, except `all`, which is All mail as
// the almost-all-mail setting shapes it. Anything else is looked up among the
// account's own folders and labels.
func (s *Service) ResolveMailbox(ctx context.Context, ref string) (Mailbox, error) {
	if ref == "" {
		return Mailbox{}, errs.Problemf("No mailbox given.")
	}
	if b, ok := builtInNamed(ref); ok {
		return s.builtInMailbox(ctx, b)
	}

	all, err := s.ownMailboxes(ctx)
	if err != nil {
		return Mailbox{}, err
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

func (s *Service) builtInMailbox(ctx context.Context, b builtIn) (Mailbox, error) {
	id := b.id
	if id == labelAllMail {
		set, err := s.settings(ctx)
		if err != nil {
			return Mailbox{}, err
		}
		if set.almostAllMail() {
			id = labelAlmostAllMail
		}
	}
	return Mailbox{ID: id, Name: b.name, Folder: !b.label, System: true}, nil
}

func (s *Service) ownMailboxes(ctx context.Context) ([]Mailbox, error) {
	labels, folders, err := s.LabelsList(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Mailbox, 0, len(labels)+len(folders))
	for _, f := range folders {
		out = append(out, Mailbox{ID: f.ID, Name: f.Name, Folder: true})
	}
	for _, l := range labels {
		out = append(out, Mailbox{ID: l.ID, Name: l.Name})
	}
	return out, nil
}

func (s *Service) Mailboxes(ctx context.Context) ([]Mailbox, error) {
	tabs, err := s.shownTabs(ctx)
	if err != nil {
		return nil, err
	}
	var out []Mailbox
	for _, b := range builtIns {
		if b.tab && !tabs[b.id] {
			continue
		}
		m, err := s.builtInMailbox(ctx, b)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	own, err := s.ownMailboxes(ctx)
	if err != nil {
		return nil, err
	}
	return append(out, own...), nil
}

func (s *Service) shownTabs(ctx context.Context) (map[string]bool, error) {
	on, err := s.CategoryViewOn(ctx)
	if err != nil || !on {
		return nil, err
	}
	categories, err := s.Categories(ctx)
	if err != nil {
		return nil, err
	}
	shown := make(map[string]bool, len(categories))
	for _, c := range categories {
		if c.Shown {
			shown[c.ID] = true
		}
	}
	return shown, nil
}

func inPlace(labels []string, place string) bool {
	switch {
	case place == labelAllMail:
		return true
	case !hasLabel(labels, place):
		return false
	case isCategory(place):
		return hasLabel(labels, labelInbox)
	}
	return true
}

// MailboxNames is what each destination ID is called, so a stored ID can be
// shown as the word somebody chose for it.
//
// It is the reverse of ResolveMailbox and exists for the same reason: a rule
// Proton hands back names its destination by ID, and a listing that printed the
// ID would be reporting something nobody can read.
func (s *Service) MailboxNames(ctx context.Context) (map[string]string, error) {
	names := make(map[string]string, len(builtIns))
	for _, b := range builtIns {
		names[b.id] = b.name
	}
	own, err := s.ownMailboxes(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range own {
		names[m.ID] = m.Name
	}
	return names, nil
}

// ResolveFolderTarget resolves a move destination, refusing a label and a
// built-in folder mail cannot be moved into.
//
// Refusing is the point: applying a label where a move was asked for is a
// different operation with a different outcome, and the CLI has a verb for it.
func (s *Service) ResolveFolderTarget(ctx context.Context, ref string) (Mailbox, error) {
	if b, ok := builtInNamed(ref); ok && !b.label && !b.movable {
		return Mailbox{}, errs.Problemf("%q cannot be moved into - only %s, a tab and your own folders can.",
			ref, strings.Join(builtInNames(func(b builtIn) bool { return b.movable && !b.tab }), ", "))
	}
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

const ScheduledLabelID = labelScheduled
