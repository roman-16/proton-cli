package calendar

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"

	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

// Sharing a calendar with another Proton account.
//
// A calendar is opened by a passphrase, and each member holds that passphrase
// encrypted to their own key. Sharing is therefore not a permission Proton
// grants: it is handing somebody the session key that opens the passphrase,
// encrypted so only they can read it, and signing it so they can tell it came
// from you.
//
// That signature carries a context - Proton's own notation, marked critical - so
// a signature made for something else cannot be replayed as an invitation.

// shareInviteContext is what the signature over the session key is for. Proton's
// clients require it, and marking it critical means a client that does not
// understand the notation refuses the signature rather than accepting it blind.
const shareInviteContext = "calendar.sharing.invite"

// The bits Proton uses for what a member of a calendar may do. They are flags,
// not levels: what a person is given is a combination of them.
const (
	permSuperOwner     = 1
	permOwner          = 2
	permAdmin          = 4
	permReadMemberList = 8
	permWrite          = 16
	permRead           = 32
	permAvailability   = 64
)

// The combinations Proton's own client offers, which are the only ones the
// server accepts.
const (
	// permViewer is seeing what is on the calendar, and being visible in
	// somebody's availability.
	permViewer = permRead | permAvailability
	// permEditor is that, plus changing it.
	permEditor = permWrite | permRead | permAvailability
)

// CalendarMember is somebody who has been given a calendar, whether or not they
// have answered yet.
type CalendarMember struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	// Access is what they may do: see, or edit.
	Access string `json:"access"`
	// Status is where the invitation stands - pending until they accept it.
	Status string `json:"status"`
	// Owner marks the membership the calendar was made under.
	Owner bool `json:"owner"`
}

// memberStatuses are Proton's own numbering of where an invitation stands.
var memberStatuses = map[int]string{0: "pending", 1: "active", 2: "declined"}

// accessWord names a permission bundle the way --edit reads. It tests the bit
// rather than comparing the number, because these are flags: a combination this
// version has not seen still says whether it can write.
func accessWord(p int) string {
	if p&permWrite != 0 {
		return "editor"
	}
	return "viewer"
}

// CalendarShare gives a calendar to somebody, and returns the membership it made.
//
// The address has to be a Proton one: what is handed over is a key encrypted to
// theirs, and an address Proton does not hold keys for has nothing to encrypt to.
func (s *Service) CalendarShare(ctx context.Context, calendarID, email string, canEdit bool) error {
	ck, err := s.unlockCalendar(ctx, calendarID)
	if err != nil {
		return err
	}
	if ck.passphraseKey == nil {
		return errs.Problemf("That calendar's passphrase could not be opened, so it cannot be shared.")
	}

	inviteeKR, err := s.publicKeyRing(ctx, email)
	if err != nil {
		return err
	}
	// The key packet is the session key encrypted to them; the signature is over
	// the session key itself, so they can tell who handed it over.
	keyPacket, err := inviteeKR.EncryptSessionKey(ck.passphraseKey)
	if err != nil {
		return fmt.Errorf("encrypt the passphrase key for %s: %w", email, err)
	}
	signature, err := ck.addr.Write.SignDetachedWithContext(
		pgp.NewPlainMessage(ck.passphraseKey.Key),
		pgp.NewSigningContext(shareInviteContext, true),
	)
	if err != nil {
		return fmt.Errorf("sign the invitation: %w", err)
	}
	armored, err := signature.GetArmored()
	if err != nil {
		return err
	}

	permissions := permViewer
	if canEdit {
		permissions = permEditor
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/calendar/v1/" + calendarID + "/members",
		Body: map[string]any{
			"AddressID": ck.addressID,
			"Signature": armored,
			"Members": []map[string]any{{
				"Email":               email,
				"PassphraseKeyPacket": base64.StdEncoding.EncodeToString(keyPacket),
				"Permissions":         permissions,
			}},
		},
	}, nil)
}

// publicKeyRing is the key Proton publishes for an address, refused with a
// sentence rather than an empty ring when there is none.
func (s *Service) publicKeyRing(ctx context.Context, email string) (*pgp.KeyRing, error) {
	kr, err := keys.Published(ctx, s.C, email)
	if err != nil {
		return nil, err
	}
	if kr == nil {
		return nil, errs.Problemf("%s is not a Proton address, so there is no key to share with.", email).
			Hint("a calendar can only be shared with another Proton account")
	}
	return kr, nil
}

// CalendarMembers lists who has been given a calendar, answered or not.
//
// Proton keeps the two apart: somebody who accepted is a member, and somebody
// who has not answered is an invitation, at an endpoint of its own. A person
// asking who has their calendar means both.
func (s *Service) CalendarMembers(ctx context.Context, calendarID string) ([]CalendarMember, error) {
	var members struct {
		Members []struct {
			ID          string
			Email       string
			Permissions int
		}
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/calendar/v1/" + calendarID + "/members/all",
	}, &members); err != nil {
		return nil, err
	}
	var invites struct {
		Invitations []struct {
			CalendarInvitationID string
			Email                string
			Permissions          int
			Status               int
		}
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/calendar/v1/" + calendarID + "/invitations",
	}, &invites); err != nil {
		return nil, err
	}

	out := make([]CalendarMember, 0, len(members.Members)+len(invites.Invitations))
	for _, m := range members.Members {
		out = append(out, CalendarMember{
			ID: m.ID, Email: m.Email, Access: accessWord(m.Permissions),
			Status: "active", Owner: m.Permissions&permOwner != 0,
		})
	}
	for _, i := range invites.Invitations {
		status, ok := memberStatuses[i.Status]
		if !ok {
			status = fmt.Sprintf("status %d", i.Status)
		}
		out = append(out, CalendarMember{
			ID: i.CalendarInvitationID, Email: i.Email,
			Access: accessWord(i.Permissions), Status: status,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out, nil
}

// CalendarUnshare takes somebody's access away.
//
// An invitation nobody answered and a membership somebody is using are withdrawn
// at different paths, so which it is has to be known: CalendarMembers says.
func (s *Service) CalendarUnshare(ctx context.Context, calendarID string, m CalendarMember) error {
	req := proton.Request{
		Method: "DELETE", Path: "/calendar/v1/" + calendarID + "/members/" + m.ID,
	}
	if m.Status == "pending" {
		req = proton.Request{
			Method: "DELETE", Path: "/calendar/v1/" + calendarID + "/invitations/" + m.ID,
		}
	}
	return s.C.Decode(ctx, req, nil)
}

// ── the other side: a calendar somebody gave you ──

// CalendarInvitation is a calendar somebody has offered you.
type CalendarInvitation struct {
	ID         string `json:"id"`
	CalendarID string `json:"calendar_id"`
	// Name and Color are what the sender calls it. They are readable before the
	// invitation is taken, which is how somebody decides whether to take it.
	Name   string `json:"name"`
	Color  string `json:"color,omitempty"`
	Sender string `json:"sender"`
	// Email is the address of yours it was sent to.
	Email string `json:"email"`
	// Access is what you would be able to do with it.
	Access string `json:"access"`
	Status string `json:"status"`
	// Expires is when the offer lapses, as a Unix time.
	Expires int64 `json:"expires,omitempty"`
}

// CalendarInvitations lists the calendars other people have offered you.
func (s *Service) CalendarInvitations(ctx context.Context) ([]CalendarInvitation, error) {
	var r struct {
		Invitations []rawInvitation
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/calendar/v1/invitations",
	}, &r); err != nil {
		return nil, err
	}
	out := make([]CalendarInvitation, 0, len(r.Invitations))
	for _, i := range r.Invitations {
		status, ok := memberStatuses[i.Status]
		if !ok {
			status = fmt.Sprintf("status %d", i.Status)
		}
		out = append(out, CalendarInvitation{
			ID: i.CalendarInvitationID, CalendarID: i.CalendarID,
			Name: i.Calendar.Name, Color: i.Calendar.Color, Sender: i.Calendar.SenderEmail,
			Email: i.Email, Access: accessWord(i.Permissions),
			Status: status, Expires: i.ExpirationTime,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// rawInvitation is one entry as Proton sends it. The passphrase and the
// signature are only needed to accept, so they are read here and nowhere else.
type rawInvitation struct {
	CalendarInvitationID string
	CalendarID           string
	Email                string
	Permissions          int
	Status               int
	ExpirationTime       int64
	Passphrase           string
	Signature            string
	Calendar             struct {
		Name        string
		Color       string
		SenderEmail string
	}
}

// CalendarInvitationAccept takes a calendar somebody offered.
//
// The invitation carries the calendar's passphrase, encrypted to the address it
// was sent to. Accepting means opening it and signing it back with that
// address's own key, which is how Proton knows the offer reached somebody who
// could actually read it.
func (s *Service) CalendarInvitationAccept(ctx context.Context, invitationID string) error {
	invite, addr, addressID, err := s.findInvitation(ctx, invitationID)
	if err != nil {
		return err
	}

	msg, err := pgp.NewPGPMessageFromArmored(invite.Passphrase)
	if err != nil {
		return err
	}
	passphrase, err := addr.Read.Decrypt(msg, nil, pgp.GetUnixTime())
	if err != nil {
		return fmt.Errorf("open the calendar's passphrase: %w", err)
	}
	signature, err := addr.Write.SignDetached(pgp.NewPlainMessageFromString(passphrase.GetString()))
	if err != nil {
		return err
	}
	armored, err := signature.GetArmored()
	if err != nil {
		return err
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT",
		Path:   "/calendar/v1/" + invite.CalendarID + "/invitations/" + addressID + "/accept",
		Body:   map[string]any{"Signature": armored},
	}, nil)
}

// CalendarInvitationDecline turns one down. Nothing is decrypted: declining is
// saying no to the offer, not opening it first.
func (s *Service) CalendarInvitationDecline(ctx context.Context, invitationID string) error {
	invite, _, addressID, err := s.findInvitation(ctx, invitationID)
	if err != nil {
		return err
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT",
		Path:   "/calendar/v1/" + invite.CalendarID + "/invitations/" + addressID + "/reject",
	}, nil)
}

// findInvitation reads one invitation whole, with the keys of the address it was
// sent to. Both answers come from the same lookup, because an invitation to an
// address this account does not hold is one it cannot answer either way.
func (s *Service) findInvitation(ctx context.Context, invitationID string) (rawInvitation, keys.Rings, string, error) {
	var r struct {
		Invitations []rawInvitation
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/calendar/v1/invitations",
	}, &r); err != nil {
		return rawInvitation{}, keys.Rings{}, "", err
	}
	u, err := s.keys(ctx)
	if err != nil {
		return rawInvitation{}, keys.Rings{}, "", err
	}
	for _, i := range r.Invitations {
		if i.CalendarInvitationID != invitationID {
			continue
		}
		for _, a := range u.Addresses {
			if !strings.EqualFold(a.Email, i.Email) {
				continue
			}
			rings, ok := u.AddrRings(a.ID)
			if !ok {
				return rawInvitation{}, keys.Rings{}, "", errs.Problemf(
					"The keys for %s will not open, so that invitation cannot be answered.", a.Email)
			}
			return i, rings, a.ID, nil
		}
		return rawInvitation{}, keys.Rings{}, "", errs.Problemf(
			"That invitation was sent to %s, which is not an address on this account.", i.Email)
	}
	return rawInvitation{}, keys.Rings{}, "", &errs.NotFound{Kind: "invitation", Ref: invitationID}
}
