package mail

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/ref"
)

// The kinds of address Proton distinguishes, from its own ADDRESS_TYPE. The
// first Proton address of an account and its short-domain one are the two it
// keeps: neither can be turned off or deleted.
const (
	TypeOriginal     = 1
	TypeAlias        = 2
	TypeCustomDomain = 3
	TypePremium      = 4
	TypeExternal     = 5
)

// Whether an address sends and receives at all, from Proton's ADDRESS_STATUS.
const (
	StatusDisabled = 0
	StatusEnabled  = 1
)

// Address is one of the account's own email addresses - Proton's "Identity and
// addresses" settings page. Signature is stored as HTML.
type Address struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Signature   string `json:"signature,omitempty"`
	Type        int    `json:"type"`
	Status      int    `json:"status"`
	Order       int    `json:"order"`
	Send        int    `json:"send"`
	Receive     int    `json:"receive"`
	// HasKeys says whether Proton holds a key for the address. One without a key
	// is inert - it cannot send, cannot receive, and nothing can be encrypted to
	// it - which no other field of an enabled address shows.
	HasKeys bool `json:"has_keys"`
}

// CanSend reports whether Proton permits composing from this address.
func (a Address) CanSend() bool {
	return a.Status == StatusEnabled && a.Send == 1 && a.Receive == 1 && a.HasKeys
}

// Kept reports whether Proton keeps this address enabled: the account's first
// Proton address and its short-domain one are neither disabled nor deleted.
func (a Address) Kept() bool { return a.Type == TypeOriginal || a.Type == TypePremium }

// rawAddress is the address as Proton writes it, in Proton's own capitalisation
// and with its own numbers.
type rawAddress struct {
	ID          string
	Email       string
	DisplayName string
	Signature   string
	Type        int
	Status      int
	Order       int
	Send        int
	Receive     int
	HasKeys     int
}

func (s *Service) AddressesList(ctx context.Context) ([]Address, error) {
	var r struct{ Addresses []rawAddress }
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/core/v4/addresses"}, &r); err != nil {
		return nil, err
	}
	out := make([]Address, 0, len(r.Addresses))
	for _, a := range r.Addresses {
		out = append(out, Address{
			ID: a.ID, Email: a.Email, DisplayName: a.DisplayName, Signature: a.Signature,
			Type: a.Type, Status: a.Status, Order: a.Order,
			Send: a.Send, Receive: a.Receive, HasKeys: a.HasKeys != 0,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out, nil
}

// ResolveAddress accepts an address ID or an email address (exactly, or as the
// base of a plus alias), so every command that names one of your own addresses
// takes the form you already know.
func (s *Service) ResolveAddress(ctx context.Context, r string) (*Address, error) {
	addrs, err := s.AddressesList(ctx)
	if err != nil {
		return nil, err
	}
	if ref.Full(r) {
		for _, a := range addrs {
			if a.ID == r {
				return &a, nil
			}
		}
	}
	for _, a := range addrs {
		if strings.EqualFold(a.Email, r) || strings.EqualFold(a.Email, plusAliasBase(r)) {
			return &a, nil
		}
	}
	picked, err := ref.Pick("address", r, addrs,
		func(a Address) string { return a.ID },
		func(a Address) string { return a.Email })
	if err != nil {
		return nil, err
	}
	return &picked, nil
}

// AddressCreate adds an address to the account and gives it its first key.
//
// The key is what makes it an address rather than a record: Proton lets nothing
// through until one is published, so the two are one command. An address that
// exists without a key - what an interrupted run leaves - is given one instead
// of being refused, so the command that makes an address is also the one that
// finishes it.
func (s *Service) AddressCreate(ctx context.Context, local, domain, displayName string) (string, error) {
	u, err := s.keys(ctx)
	if err != nil {
		return "", err
	}
	postQuantum, err := keys.PostQuantum(ctx, s.C)
	if err != nil {
		return "", err
	}
	if postQuantum {
		return "", keys.UnsupportedPostQuantum()
	}
	email := local + "@" + domain
	if existing, ok := addressByEmail(u, email); ok {
		if len(existing.Keys) > 0 {
			return "", &errs.Exists{Kind: "address", Name: existing.Email}
		}
		return u.NewAddressKey(ctx, s.C, existing)
	}

	member, err := s.selfMember(ctx)
	if err != nil {
		return "", err
	}
	var created struct {
		Address rawAddress
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/core/v4/addresses",
		Body: map[string]any{
			"MemberID":    member,
			"Local":       local,
			"Domain":      domain,
			"DisplayName": displayName,
		},
	}, &created); err != nil {
		return "", s.orUnusableDomain(ctx, domain, err)
	}
	return u.NewAddressKey(ctx, s.C, keys.Address{ID: created.Address.ID, Email: created.Address.Email})
}

// selfMember is who an address belongs to.
//
// Proton files every address against a member, and an account that may add one
// is a member that may be asked about. An account that may not add addresses is
// refused here, which is the same answer one request later.
func (s *Service) selfMember(ctx context.Context) (string, error) {
	var r struct {
		Member struct{ ID string }
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/core/v4/members/me"}, &r); err != nil {
		return "", err
	}
	if r.Member.ID == "" {
		return "", errs.Problemf("This account cannot add addresses.").
			Hint("adding one needs a paid Mail plan")
	}
	return r.Member.ID, nil
}

// orUnusableDomain replaces Proton's refusal when the domain is the reason.
//
// Which domains an account may use is Proton's to say - its own, plus whatever
// custom domains the account has set up - so the list is asked for only once a
// create has already failed, and only to name what the address could have been.
// A domain that is on the list is somebody else's problem, and Proton's own
// sentence is left to say what it is.
func (s *Service) orUnusableDomain(ctx context.Context, domain string, refusal error) error {
	usable, err := s.usableDomains(ctx)
	if err != nil {
		// Recorded and not counted: Proton's own refusal is on the screen either
		// way, and this line is what says why it was not improved on.
		slog.DebugContext(ctx, "addresses: the usable domains could not be read", "error", err.Error())
		return refusal
	}
	for _, d := range usable {
		if strings.EqualFold(d, domain) {
			return refusal
		}
	}
	return errs.Problemf("This account cannot add an address on %s.", domain).
		Hint(strings.Join(usable, ", "))
}

// usableDomains are the domains an address may be added on: Proton's own, and
// the account's custom ones that are ready to carry mail.
func (s *Service) usableDomains(ctx context.Context) ([]string, error) {
	var protonOwn struct{ Domains []string }
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/domains/available"}, &protonOwn); err != nil {
		return nil, err
	}
	var custom struct {
		Domains []struct {
			DomainName string
			State      int
		}
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/domains"}, &custom); err != nil {
		return nil, err
	}
	out := protonOwn.Domains
	for _, d := range custom.Domains {
		// State 1 is a domain Proton has verified; an unverified one carries no
		// mail, so offering it would be offering a failure.
		if d.State == 1 {
			out = append(out, d.DomainName)
		}
	}
	return out, nil
}

// AddressDisable stops an address sending and receiving, and keeps everything
// it holds.
func (s *Service) AddressDisable(ctx context.Context, id string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/core/v4/addresses/" + id + "/disable",
	}, nil)
}

// AddressEnable lets a disabled address send and receive again.
func (s *Service) AddressEnable(ctx context.Context, id string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/core/v4/addresses/" + id + "/enable",
	}, nil)
}

// AddressDelete removes an address for good.
//
// Proton takes down an address that is still enabled before it will delete it,
// which is one command rather than two: nothing is served by leaving somebody
// with a disabled address they asked to be rid of.
func (s *Service) AddressDelete(ctx context.Context, a Address) error {
	if a.Status == StatusEnabled {
		if err := s.AddressDisable(ctx, a.ID); err != nil {
			return err
		}
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/core/v4/addresses/" + a.ID + "/delete",
	}, nil)
}

// AddressDeletionAllowed reports whether the account may still delete an
// address this year.
//
// Proton grants one deletion a year for an address on one of its own domains,
// and answers for the account rather than for the address. An address on a
// custom domain is not counted against it.
func (s *Service) AddressDeletionAllowed(ctx context.Context) (bool, error) {
	var r struct {
		AddressDeletion struct{ Allow bool }
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/core/v4/addresses/allowAddressDeletion",
	}, &r); err != nil {
		return false, err
	}
	return r.AddressDeletion.Allow, nil
}

func addressByEmail(u *keys.Unlocked, email string) (keys.Address, bool) {
	for _, a := range u.Addresses {
		if strings.EqualFold(a.Email, email) {
			return a, true
		}
	}
	return keys.Address{}, false
}

// AddressUpdate writes the display name and signature Proton shows on outgoing
// mail. A nil field is left untouched; a non-nil empty string clears it.
func (s *Service) AddressUpdate(ctx context.Context, id string, displayName, signature *string) error {
	body := map[string]any{}
	if displayName != nil {
		body["DisplayName"] = *displayName
	}
	if signature != nil {
		body["Signature"] = *signature
	}
	if len(body) == 0 {
		return fmt.Errorf("nothing to update")
	}
	return s.C.Decode(ctx, proton.Request{Method: "PUT", Path: "/core/v4/addresses/" + id, Body: body}, nil)
}

// ── sender selection ──

// Sender is a resolved sending identity: which address a message goes out from,
// and the unlocked key ring that signs and encrypts it.
type Sender struct {
	Address keys.Address
	KR      *pgp.KeyRing
}

// SenderRequest describes how to pick a sending address. Explicit is the user's
// --from, if any. ParentAddress and ParentAddressID come from the message being
// replied to or forwarded, so a reply leaves from the address that received it.
type SenderRequest struct {
	Explicit        string
	ParentAddress   string
	ParentAddressID string
}

// ResolveSender mirrors the web client's getFromAddresses/getFromAddress: only
// active, sendable, receivable addresses can compose, they are ordered by the
// account's own Order, and a plus alias resolves through its base address so a
// reply to "me+tag@proton.me" leaves from that alias.
func (s *Service) ResolveSender(ctx context.Context, req SenderRequest) (*Sender, error) {
	u, err := s.keys(ctx)
	if err != nil {
		return nil, err
	}
	return resolveSender(u, req)
}

func resolveSender(u *keys.Unlocked, req SenderRequest) (*Sender, error) {
	sendable := make([]keys.Address, 0, len(u.Addresses))
	for _, a := range u.Addresses {
		if _, ok := u.AddrKR(a.ID); ok && a.CanSend() {
			sendable = append(sendable, a)
		}
	}
	sort.SliceStable(sendable, func(i, j int) bool { return sendable[i].Order < sendable[j].Order })
	if len(sendable) == 0 {
		// Every address is disabled or its keys would not unlock; fall back to
		// whatever did unlock so single-address edge cases still send.
		kr, addr, err := u.FirstAddr()
		if err != nil {
			return nil, err
		}
		if req.Explicit != "" {
			return nil, unknownSender(req.Explicit, []keys.Address{addr})
		}
		return &Sender{Address: addr, KR: kr}, nil
	}

	if req.Explicit != "" {
		picked, err := matchSender(sendable, req.Explicit)
		if err != nil {
			return nil, err
		}
		return withKeyRing(u, *picked)
	}
	if alias := aliasOf(sendable, req.ParentAddress); alias != nil {
		return withKeyRing(u, *alias)
	}
	for _, a := range sendable {
		if a.ID == req.ParentAddressID {
			return withKeyRing(u, a)
		}
	}
	for _, a := range sendable {
		if strings.EqualFold(a.Email, req.ParentAddress) {
			return withKeyRing(u, a)
		}
	}
	return withKeyRing(u, sendable[0])
}

// matchSender resolves --from against an address ID or email, accepting a plus
// alias of one of the account's addresses.
func matchSender(sendable []keys.Address, want string) (*keys.Address, error) {
	for _, a := range sendable {
		if a.ID == want || strings.EqualFold(a.Email, want) {
			return &a, nil
		}
	}
	if alias := aliasOf(sendable, want); alias != nil {
		return alias, nil
	}
	return nil, unknownSender(want, sendable)
}

// aliasOf returns the address behind a plus alias ("me+tag@x" -> "me@x"), with
// Email rewritten to the alias so recipients see the address that was used.
func aliasOf(sendable []keys.Address, email string) *keys.Address {
	base := plusAliasBase(email)
	if base == "" || strings.EqualFold(base, email) {
		return nil
	}
	for _, a := range sendable {
		if strings.EqualFold(a.Email, base) {
			alias := a
			alias.Email = email
			return &alias
		}
	}
	return nil
}

// plusAliasBase strips a "+tag" from the local part, returning "" when the input
// is not an address.
func plusAliasBase(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return ""
	}
	plus := strings.Index(email[:at], "+")
	if plus < 0 {
		return email
	}
	return email[:plus] + email[at:]
}

func withKeyRing(u *keys.Unlocked, a keys.Address) (*Sender, error) {
	kr, ok := u.AddrKR(a.ID)
	if !ok {
		return nil, fmt.Errorf("no unlocked key for address %s", a.Email)
	}
	return &Sender{Address: a, KR: kr}, nil
}

// unknownSender reports a --from that matches none of the account's sendable
// addresses, listing the ones that would have worked.
func unknownSender(want string, sendable []keys.Address) error {
	emails := make([]string, 0, len(sendable))
	for _, a := range sendable {
		emails = append(emails, a.Email)
	}
	return errs.WithExit(3, fmt.Errorf("no address matching %q can send mail; available: %s",
		want, strings.Join(emails, ", ")))
}
