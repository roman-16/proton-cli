package pass

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/roman-16/proton-cli/internal/errs"

	"github.com/roman-16/proton-cli/internal/proton"
)

// Where aliases arrive.
//
// A mailbox is a real address an alias forwards to. It is the account's rather
// than a vault's, which is why it takes no share.

// Mailbox is an address aliases forward to.
type Mailbox struct {
	ID int `json:"id"`
	// Email is where mail arrives today. Changing it needs the new address to
	// confirm, so until it does the old one is still the one being used.
	Email string `json:"email"`
	// Pending is an address that has been asked for and not yet confirmed.
	Pending string `json:"pending,omitempty"`
	Default bool   `json:"default"`
	// Verified is false until the address answers the mail Proton sends it. An
	// unverified mailbox receives nothing.
	Verified bool `json:"verified"`
	Aliases  int  `json:"aliases"`
}

// Mailboxes lists the addresses aliases forward to.
func (s *Service) Mailboxes(ctx context.Context) ([]Mailbox, error) {
	var r struct {
		Mailboxes []struct {
			MailboxID     int
			Email         string
			PendingEmail  *string
			IsDefault     bool
			Verified      bool
			AliasCount    int
			CanBeEdited   bool
			MissingScopes []string
		}
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/pass/v1/user/alias/mailbox",
	}, &r); err != nil {
		return nil, err
	}
	out := make([]Mailbox, 0, len(r.Mailboxes))
	for _, m := range r.Mailboxes {
		box := Mailbox{
			ID: m.MailboxID, Email: m.Email, Default: m.IsDefault,
			Verified: m.Verified, Aliases: m.AliasCount,
		}
		if m.PendingEmail != nil {
			box.Pending = *m.PendingEmail
		}
		out = append(out, box)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out, nil
}

// MailboxAdd asks for a new address to forward aliases to.
//
// It arrives unverified and receives nothing: Proton emails it a code, and
// MailboxVerify is where that code is handed back.
func (s *Service) MailboxAdd(ctx context.Context, email string) (*Mailbox, error) {
	var r struct {
		Mailbox struct {
			MailboxID  int
			Email      string
			Verified   bool
			IsDefault  bool
			AliasCount int
		}
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/pass/v1/user/alias/mailbox",
		Body: map[string]any{"Email": email},
	}, &r); err != nil {
		return nil, err
	}
	return &Mailbox{
		ID: r.Mailbox.MailboxID, Email: r.Mailbox.Email,
		Verified: r.Mailbox.Verified, Default: r.Mailbox.IsDefault, Aliases: r.Mailbox.AliasCount,
	}, nil
}

// MailboxVerify hands back the code Proton emailed the address.
func (s *Service) MailboxVerify(ctx context.Context, id int, code string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: fmt.Sprintf("/pass/v1/user/alias/mailbox/%d/verify", id),
		Body: map[string]any{"Code": code},
	}, nil)
}

// MailboxResend sends the verification mail again.
func (s *Service) MailboxResend(ctx context.Context, id int) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: fmt.Sprintf("/pass/v1/user/alias/mailbox/%d/verify", id),
	}, nil)
}

// MailboxDelete removes a mailbox, moving the aliases that arrive in it to
// another one.
//
// Where those aliases go has to be decided here: an alias whose only mailbox
// went away receives nothing, and Proton will not choose on anybody's behalf.
func (s *Service) MailboxDelete(ctx context.Context, id, transferTo int) error {
	body := map[string]any{}
	if transferTo != 0 {
		body["TransferMailboxID"] = transferTo
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "DELETE", Path: fmt.Sprintf("/pass/v1/user/alias/mailbox/%d", id),
		Body: body,
	}, nil)
}

// MailboxSetDefault picks the mailbox a new alias arrives in.
func (s *Service) MailboxSetDefault(ctx context.Context, id int) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/pass/v1/user/alias/settings/default_mailbox_id",
		Body: map[string]any{"DefaultMailboxID": id},
	}, nil)
}

// MailboxByEmail finds a mailbox by the address it is, since that is what a
// person knows it by rather than the number Proton files it under.
func (s *Service) MailboxByEmail(ctx context.Context, ref string) (*Mailbox, error) {
	boxes, err := s.Mailboxes(ctx)
	if err != nil {
		return nil, err
	}
	var found []Mailbox
	for _, b := range boxes {
		if b.Email == ref || strconv.Itoa(b.ID) == ref {
			return &b, nil
		}
		if strings.Contains(b.Email, ref) {
			found = append(found, b)
		}
	}
	switch len(found) {
	case 1:
		return &found[0], nil
	case 0:
		return nil, &errs.NotFound{Kind: "mailbox", Ref: ref}
	}
	candidates := make([]errs.Candidate, 0, len(found))
	for _, b := range found {
		candidates = append(candidates, errs.Candidate{ID: strconv.Itoa(b.ID), Label: b.Email})
	}
	return nil, &errs.Ambiguous{Kind: "mailbox", Ref: ref, Candidates: candidates}
}
