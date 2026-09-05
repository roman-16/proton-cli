package contacts

import (
	"context"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/vcard"
)

// Group is a contact group (a Type-2 label).
type Group struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

func (s *Service) GroupsList(ctx context.Context) ([]Group, error) {
	var r struct {
		Labels []struct{ ID, Name, Color string }
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/core/v4/labels", Query: proton.Query("Type", "2")}, &r); err != nil {
		return nil, err
	}
	out := make([]Group, 0, len(r.Labels))
	for _, l := range r.Labels {
		out = append(out, Group{ID: l.ID, Name: l.Name, Color: l.Color})
	}
	return out, nil
}

func (s *Service) GroupCreate(ctx context.Context, name, color string) (string, error) {
	var r struct{ Label struct{ ID string } }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/core/v4/labels",
		Body: map[string]any{"Name": name, "Color": color, "Type": 2},
	}, &r); err != nil {
		return "", err
	}
	return r.Label.ID, nil
}

// GroupUpdate renames or recolours a group.
//
// A group is a label to Proton, and updating one replaces the whole record
// rather than patching it - a body without a Name is refused - so the current
// one is read and the change is laid over it.
func (s *Service) GroupUpdate(ctx context.Context, id, name, color string) error {
	cur, err := s.groupByID(ctx, id)
	if err != nil {
		return err
	}
	if name != "" {
		cur.Name = name
	}
	if color != "" {
		cur.Color = color
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/core/v4/labels/" + id,
		Body: map[string]any{
			"Name": cur.Name, "Color": cur.Color,
			"Notify": cur.Notify, "Sticky": cur.Sticky,
			"Expanded": cur.Expanded, "Display": cur.Display,
		},
	}, nil)
}

// rawGroup is a contact group as Proton keeps it, with the fields its update
// replaces.
type rawGroup struct {
	ID, Name, Color                   string
	Notify, Sticky, Expanded, Display int
}

func (s *Service) groupByID(ctx context.Context, id string) (rawGroup, error) {
	var r struct{ Labels []rawGroup }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/core/v4/labels", Query: proton.Query("Type", "2"),
	}, &r); err != nil {
		return rawGroup{}, err
	}
	for _, g := range r.Labels {
		if g.ID == id {
			return g, nil
		}
	}
	return rawGroup{}, &errs.NotFound{Kind: "contact group", Ref: id}
}

func (s *Service) GroupDelete(ctx context.Context, id string) error {
	return s.C.Decode(ctx, proton.Request{Method: "DELETE", Path: "/core/v4/labels/" + id}, nil)
}

// Membership reads which groups every address in the book is in, by contact.
//
// Proton keeps membership on the address, as label IDs, and the names on the
// labels, so the book is read once and the groups once and the two are joined
// here. An account with no groups is answered without reading the book, which is
// the common case and the cheap one.
func (s *Service) Membership(ctx context.Context) (map[string]vcard.Membership, error) {
	groups, err := s.GroupsList(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]vcard.Membership{}
	if len(groups) == 0 {
		return out, nil
	}
	names := make(map[string]string, len(groups))
	for _, g := range groups {
		names[g.ID] = g.Name
	}
	all, err := s.contactEmails(ctx, url.Values{})
	if err != nil {
		return nil, err
	}
	for _, e := range all {
		if len(e.Groups) == 0 {
			continue
		}
		m := out[e.ContactID]
		if m == nil {
			m = vcard.Membership{}
			out[e.ContactID] = m
		}
		addr := canonicalEmail(e.Email)
		for _, id := range e.Groups {
			if name, ok := names[id]; ok {
				m[addr] = append(m[addr], name)
			}
		}
	}
	return out, nil
}

// groupsNamed resolves group names to IDs, creating the ones the account does
// not have.
//
// A name is matched exactly, which is what Proton's own importer does before it
// offers to create the rest. Which were created is reported, because a group
// that did not exist a moment ago is the one thing about an import worth a
// second look.
func (s *Service) groupsNamed(ctx context.Context, names []string, color string) (ids map[string]string, created []string, err error) {
	existing, err := s.GroupsList(ctx)
	if err != nil {
		return nil, nil, err
	}
	ids = make(map[string]string, len(names))
	for _, g := range existing {
		ids[g.Name] = g.ID
	}
	for _, name := range names {
		if _, ok := ids[name]; ok {
			continue
		}
		id, err := s.GroupCreate(ctx, name, color)
		if err != nil {
			return nil, created, err
		}
		ids[name] = id
		created = append(created, name)
	}
	return ids, created, nil
}

// AddressesOf are every address the named contacts hold.
//
// Naming a contact means all of their addresses, which is what the commands
// promise. Proton has an endpoint that takes contact IDs, but it labels one
// address per contact rather than all of them - so the addresses are resolved
// here and grouped the only way Proton actually groups anything.
func (s *Service) AddressesOf(ctx context.Context, contactIDs []string) ([]string, error) {
	wanted := make(map[string]bool, len(contactIDs))
	for _, id := range contactIDs {
		wanted[id] = true
	}
	all, err := s.contactEmails(ctx, url.Values{})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(contactIDs))
	for _, e := range all {
		if wanted[e.ContactID] {
			out = append(out, e.ID)
		}
	}
	if len(out) == 0 {
		return nil, errs.Problemf("those contacts have no addresses to put in a group.")
	}
	return out, nil
}

// ── one address at a time ──

// ContactEmail is one of a contact's addresses, as Proton addresses it. Group
// membership is per address, not per contact, which is why it has an ID of its
// own.
type ContactEmail struct {
	ID        string `json:"id"`
	ContactID string `json:"contact_id"`
	Email     string `json:"email"`
	Name      string `json:"name,omitempty"`
	// Groups are the groups this address is in. Membership lives here rather
	// than on the group, which is why asking what a group holds means reading
	// the addresses.
	Groups []string `json:"groups"`
}

// ContactEmails lists a contact's addresses with the IDs a group is applied to.
//
// Proton groups **addresses**, not people: a colleague's work address can be in
// the team group while their personal one is not. Labelling a whole contact is
// the shorthand for labelling all of them, and it is what `groups add` does with
// no --email.
func (s *Service) ContactEmails(ctx context.Context, contactID string) ([]ContactEmail, error) {
	// Proton's listing takes Page, PageSize, Email and LabelID and nothing else:
	// there is no way to ask it for one contact's addresses, so the whole book is
	// read and narrowed here. Sending a ContactID it ignores would return every
	// address in the account as though they were this contact's - which is how
	// an address belonging to somebody else ends up in a group.
	all, err := s.contactEmails(ctx, url.Values{})
	if err != nil {
		return nil, err
	}
	out := make([]ContactEmail, 0, 4)
	for _, e := range all {
		if e.ContactID == contactID {
			out = append(out, e)
		}
	}
	return out, nil
}

// GroupMembers lists the addresses in one group.
//
// Proton keeps membership on the address rather than on the group, so this asks
// the addresses which groups they are in - there is no endpoint that answers it
// the other way round.
func (s *Service) GroupMembers(ctx context.Context, groupID string) ([]ContactEmail, error) {
	all, err := s.contactEmails(ctx, proton.Query("LabelID", groupID))
	if err != nil {
		return nil, err
	}
	// Proton is asked to narrow it, and the answer is checked rather than
	// trusted: a filter it ignored would otherwise report the whole address book
	// as belonging to one group.
	out := make([]ContactEmail, 0, len(all))
	for _, e := range all {
		if slices.Contains(e.Groups, groupID) {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out, nil
}

// contactEmails reads the whole listing, a page at a time.
//
// It is paged because an address book outgrows one page long before anybody
// notices: reading only the first would quietly leave later contacts out of
// every answer built on this.
func (s *Service) contactEmails(ctx context.Context, q url.Values) ([]ContactEmail, error) {
	const pageSize = 150
	return proton.All(ctx, func(ctx context.Context, page int) ([]ContactEmail, bool, error) {
		q.Set("Page", strconv.Itoa(page))
		q.Set("PageSize", strconv.Itoa(pageSize))
		var r struct {
			ContactEmails []struct {
				ID, ContactID, Email, Name string
				LabelIDs                   []string
			}
		}
		if err := s.C.Decode(ctx, proton.Request{
			Method: "GET", Path: "/contacts/v4/contacts/emails", Query: q,
		}, &r); err != nil {
			return nil, false, err
		}
		out := make([]ContactEmail, 0, len(r.ContactEmails))
		for _, e := range r.ContactEmails {
			out = append(out, ContactEmail{
				ID: e.ID, ContactID: e.ContactID, Email: e.Email, Name: e.Name, Groups: e.LabelIDs,
			})
		}
		return out, proton.Full(out, pageSize), nil
	})
}

// ResolveContactEmails maps the addresses a user named onto the IDs a group is
// applied to, refusing an address the contact does not have rather than silently
// grouping nothing.
func (s *Service) ResolveContactEmails(ctx context.Context, contactID string, addresses []string) ([]string, error) {
	have, err := s.ContactEmails(ctx, contactID)
	if err != nil {
		return nil, err
	}
	byAddr := make(map[string]string, len(have))
	known := make([]string, 0, len(have))
	for _, e := range have {
		byAddr[strings.ToLower(e.Email)] = e.ID
		known = append(known, e.Email)
	}
	out := make([]string, 0, len(addresses))
	for _, addr := range addresses {
		id, ok := byAddr[strings.ToLower(strings.TrimSpace(addr))]
		if !ok {
			return nil, errs.Problemf("this contact has no address %q.", addr).
				Hint("it has: " + strings.Join(known, ", ")).Exit(3)
		}
		out = append(out, id)
	}
	return out, nil
}

// GroupAddEmails puts individual addresses into a group; GroupRemoveEmails takes
// them out.
func (s *Service) GroupAddEmails(ctx context.Context, groupID string, emailIDs []string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/contacts/v4/contacts/emails/label",
		Body: map[string]any{"LabelID": groupID, "ContactEmailIDs": emailIDs},
	}, nil)
}

func (s *Service) GroupRemoveEmails(ctx context.Context, groupID string, emailIDs []string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/contacts/v4/contacts/emails/unlabel",
		Body: map[string]any{"LabelID": groupID, "ContactEmailIDs": emailIDs},
	}, nil)
}
