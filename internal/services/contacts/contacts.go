// Package contacts provides Proton Contacts operations.
package contacts

import (
	"context"
	"fmt"
	"strings"

	"github.com/roman-16/proton-cli/internal/api"
	"github.com/roman-16/proton-cli/internal/ical"
	"github.com/roman-16/proton-cli/internal/keys"
	"github.com/roman-16/proton-cli/internal/pgp"
)

// Service is the Contacts domain service.
type Service struct{ C *api.Client }

// New returns a new Contacts service.
func New(c *api.Client) *Service { return &Service{C: c} }

// Contact is a decrypted contact.
type Contact struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Email string   `json:"email,omitempty"`
	Phone string   `json:"phone,omitempty"`
	Org   string   `json:"org,omitempty"`
	Note  string   `json:"note,omitempty"`
	Cards []string `json:"cards,omitempty"`
}

// NewContact describes a contact to create or update.
type NewContact struct {
	Name  string
	Email string
	Phone string
	Note  string
	Org   string
}

// List returns all decrypted contacts on the account.
func (s *Service) List(ctx context.Context, u *keys.Unlocked) ([]Contact, error) {
	var out []Contact
	for page := 0; ; page++ {
		var r struct {
			Contacts []struct {
				ID    string
				Cards []map[string]any
			}
		}
		q := keys.Query("Page", fmt.Sprintf("%d", page), "PageSize", "50")
		if err := s.C.Send(ctx, api.Request{Method: "GET", Path: "/contacts/v4/contacts/export", Query: q}, &r); err != nil {
			return nil, err
		}
		if len(r.Contacts) == 0 {
			break
		}
		for _, c := range r.Contacts {
			cards, err := pgp.DecryptCardsRaw(c.Cards, u.UserKR, u.UserKR, nil)
			if err != nil {
				continue
			}
			out = append(out, contactFromCards(c.ID, cards))
		}
		if len(r.Contacts) < 50 {
			break
		}
	}
	return out, nil
}

// Get returns a single decrypted contact by ID.
func (s *Service) Get(ctx context.Context, u *keys.Unlocked, id string) (*Contact, error) {
	var r struct {
		Contact struct {
			ID    string
			Cards []map[string]any
		}
	}
	if err := s.C.Send(ctx, api.Request{Method: "GET", Path: "/contacts/v4/contacts/" + id}, &r); err != nil {
		return nil, err
	}
	cards, err := pgp.DecryptCardsRaw(r.Contact.Cards, u.UserKR, u.UserKR, nil)
	if err != nil {
		return nil, err
	}
	c := contactFromCards(r.Contact.ID, cards)
	c.Cards = cards
	return &c, nil
}

// Resolve returns a contact ID for either a literal ID or a name/email search.
// On ambiguity, returns the list of candidates in the error.
func (s *Service) Resolve(ctx context.Context, u *keys.Unlocked, ref string) (string, error) {
	if looksLikeID(ref) {
		return ref, nil
	}
	contacts, err := s.List(ctx, u)
	if err != nil {
		return "", err
	}
	needle := strings.ToLower(ref)
	var matches []Contact
	for _, c := range contacts {
		if strings.Contains(strings.ToLower(c.Name), needle) || strings.Contains(strings.ToLower(c.Email), needle) {
			matches = append(matches, c)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no contact matching %q", ref)
	case 1:
		return matches[0].ID, nil
	}
	lines := []string{fmt.Sprintf("ambiguous: %d contacts match %q:", len(matches), ref)}
	for _, m := range matches {
		lines = append(lines, fmt.Sprintf("  %s  %s <%s>", m.ID, m.Name, m.Email))
	}
	return "", fmt.Errorf("%s", strings.Join(lines, "\n"))
}

// Create creates a new contact.
func (s *Service) Create(ctx context.Context, u *keys.Unlocked, nc NewContact) ([]byte, error) {
	if nc.Name == "" && nc.Email == "" {
		return nil, fmt.Errorf("name or email is required")
	}
	name := nc.Name
	if name == "" {
		name = nc.Email
	}
	signed := ical.SignedVCard(name, nc.Email, ical.ContactUID())
	signedCard, err := pgp.SignCard(signed, u.UserKR)
	if err != nil {
		return nil, err
	}
	cards := []any{signedCard}
	if nc.Phone != "" || nc.Note != "" || nc.Org != "" {
		enc := ical.EncryptedVCard(nc.Phone, nc.Note, nc.Org)
		ec, err := pgp.EncryptAndSignCard(enc, u.UserKR, u.UserKR)
		if err != nil {
			return nil, err
		}
		cards = append(cards, ec)
	}
	body := map[string]any{
		"Contacts":  []map[string]any{{"Cards": cards}},
		"Overwrite": 0,
		"Labels":    0,
	}
	resp, err := s.C.Do(ctx, api.Request{Method: "POST", Path: "/contacts/v4/contacts", Body: body})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// Update replaces the cards on an existing contact.
func (s *Service) Update(ctx context.Context, u *keys.Unlocked, id string, patch NewContact) error {
	existing, err := s.Get(ctx, u, id)
	if err != nil {
		return err
	}
	merged := NewContact{
		Name:  firstNonEmpty(patch.Name, existing.Name),
		Email: firstNonEmpty(patch.Email, existing.Email),
		Phone: firstNonEmpty(patch.Phone, existing.Phone),
		Note:  firstNonEmpty(patch.Note, existing.Note),
		Org:   firstNonEmpty(patch.Org, existing.Org),
	}
	uid := ical.Field(strings.Join(existing.Cards, "\n"), "UID")
	if uid == "" {
		uid = ical.ContactUID()
	}
	name := merged.Name
	if name == "" {
		name = merged.Email
	}
	signed := ical.SignedVCard(name, merged.Email, uid)
	signedCard, err := pgp.SignCard(signed, u.UserKR)
	if err != nil {
		return err
	}
	cards := []any{signedCard}
	if merged.Phone != "" || merged.Note != "" || merged.Org != "" {
		enc := ical.EncryptedVCard(merged.Phone, merged.Note, merged.Org)
		ec, err := pgp.EncryptAndSignCard(enc, u.UserKR, u.UserKR)
		if err != nil {
			return err
		}
		cards = append(cards, ec)
	}
	return s.C.Send(ctx, api.Request{
		Method: "PUT", Path: "/contacts/v4/contacts/" + id,
		Body: map[string]any{"Cards": cards},
	}, nil)
}

// Delete permanently deletes the given contacts.
func (s *Service) Delete(ctx context.Context, ids []string) error {
	return s.C.Send(ctx, api.Request{
		Method: "PUT", Path: "/contacts/v4/contacts/delete",
		Body: map[string]any{"IDs": ids},
	}, nil)
}

func contactFromCards(id string, cards []string) Contact {
	joined := strings.Join(cards, "\n")
	return Contact{
		ID:    id,
		Name:  ical.Field(joined, "FN"),
		Email: ical.Field(joined, "EMAIL"),
		Phone: ical.Field(joined, "TEL"),
		Org:   ical.Field(joined, "ORG"),
		Note:  ical.Field(joined, "NOTE"),
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// looksLikeID returns true when s looks like a Proton contact ID.
func looksLikeID(s string) bool {
	return len(s) > 60 && strings.HasSuffix(s, "==")
}
