package pass

import (
	"context"
	"fmt"

	"github.com/roman-16/proton-cli/internal/proton"
)

// Writing as an alias rather than only receiving as one.
//
// An alias forwards mail to you, but a reply would leave from your real address
// and undo the point of it. A contact fixes that: Proton mints a second address
// standing for one correspondent, and mail you send there goes on to them as
// though the alias had written it.

// AliasContact is one correspondent an alias can write to.
type AliasContact struct {
	ID   int    `json:"id"`
	Name string `json:"name,omitempty"`
	// Email is who they really are.
	Email string `json:"email"`
	// ReverseAlias is the address to write to so they receive it from the alias.
	ReverseAlias string `json:"reverse_alias"`
	// Blocked stops mail they send to the alias from reaching you.
	Blocked   bool  `json:"blocked"`
	Forwarded int   `json:"forwarded"`
	Replied   int   `json:"replied"`
	BlockedIn int   `json:"blocked_in"`
	Created   int64 `json:"created"`
}

// AliasContacts lists the correspondents an alias can write to.
func (s *Service) AliasContacts(ctx context.Context, shareID, itemID string) ([]AliasContact, error) {
	// Proton pages this by the last ID seen rather than by page number, and says
	// it has finished by answering with a zero.
	since := ""
	return proton.All(ctx, func(ctx context.Context, _ int) ([]AliasContact, bool, error) {
		var r struct {
			Contacts []struct {
				ID                             int
				Name, Email, ReverseAlias      string
				Blocked                        bool
				ForwardedEmails, RepliedEmails int
				BlockedEmails                  int
				CreateTime                     int64
			}
			LastID int
		}
		req := proton.Request{
			Method: "GET", Path: fmt.Sprintf("/pass/v1/share/%s/alias/%s/contact", shareID, itemID),
		}
		if since != "" {
			req.Query = proton.Query("Since", since)
		}
		if err := s.C.Decode(ctx, req, &r); err != nil {
			return nil, false, err
		}
		out := make([]AliasContact, 0, len(r.Contacts))
		for _, c := range r.Contacts {
			out = append(out, AliasContact{
				ID: c.ID, Name: c.Name, Email: c.Email, ReverseAlias: c.ReverseAlias,
				Blocked: c.Blocked, Forwarded: c.ForwardedEmails, Replied: c.RepliedEmails,
				BlockedIn: c.BlockedEmails, Created: c.CreateTime,
			})
		}
		since = fmt.Sprint(r.LastID)
		return out, r.LastID != 0 && len(r.Contacts) > 0, nil
	})
}

// AliasContactCreate mints the address that writes to somebody as the alias.
func (s *Service) AliasContactCreate(ctx context.Context, shareID, itemID, email, name string) (*AliasContact, error) {
	var r struct{ Contact aliasContactBody }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: fmt.Sprintf("/pass/v1/share/%s/alias/%s/contact", shareID, itemID),
		Body: map[string]any{"Email": email, "Name": name},
	}, &r); err != nil {
		return nil, err
	}
	contact := r.Contact.contact()
	return &contact, nil
}

// AliasContactDelete removes one, and with it the address that stood for them.
func (s *Service) AliasContactDelete(ctx context.Context, shareID, itemID string, contactID int) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "DELETE",
		Path:   fmt.Sprintf("/pass/v1/share/%s/alias/%s/contact/%d", shareID, itemID, contactID),
	}, nil)
}

// AliasContactSetBlocked decides whether mail they send to the alias reaches you.
func (s *Service) AliasContactSetBlocked(ctx context.Context, shareID, itemID string, contactID int, blocked bool) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT",
		Path:   fmt.Sprintf("/pass/v1/share/%s/alias/%s/contact/%d/blocked", shareID, itemID, contactID),
		Body:   map[string]any{"Blocked": blocked},
	}, nil)
}

type aliasContactBody struct {
	ID                        int
	Name, Email, ReverseAlias string
	Blocked                   bool
	CreateTime                int64
}

func (b aliasContactBody) contact() AliasContact {
	return AliasContact{
		ID: b.ID, Name: b.Name, Email: b.Email, ReverseAlias: b.ReverseAlias,
		Blocked: b.Blocked, Created: b.CreateTime,
	}
}
