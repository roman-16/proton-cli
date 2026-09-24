package account

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/fetch"
	"github.com/roman-16/proton-cli/internal/proton"
)

// jsonInt64 is a number Proton returns as a number from one endpoint and as a
// string from another: the delegated-access list gives CreateTime as an integer
// and the create response gives it as a string. It reads either.
type jsonInt64 int64

func (n *jsonInt64) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return err
	}
	*n = jsonInt64(v)
	return nil
}

// Trusted contacts: the two safety nets Proton builds on delegated access.
//
// Emergency access is the people who may get into the whole account if its owner
// cannot, after a waiting period the owner sets. Data-recovery contacts are the
// people who may help the owner reactivate locked keys. Proton keeps both as one
// kind of record - a delegated access - told apart by a Types bitfield, and each
// record has a direction: one this account granted (outgoing) or one granted to
// it (incoming).

// The two things a delegated access can be, as a bitfield. Mirrors
// DelegatedAccessTypeEnum in WebClients: a single record may carry both.
const (
	typeEmergency = 1
	typeRecovery  = 2
)

// The states a delegated access moves through. Mirrors DelegatedAccessStateEnum.
const (
	stateDisabled    = 0
	stateEnabled     = 1
	stateAccessible  = 2 // emergency: the contact may sign in now
	stateRecoverable = 3 // recovery: the contact has been asked to help
)

// Kind is which safety net a record belongs to, as the CLI speaks it.
type Kind string

const (
	KindEmergency Kind = "emergency"
	KindRecovery  Kind = "recovery"
)

func (k Kind) code() int {
	if k == KindRecovery {
		return typeRecovery
	}
	return typeEmergency
}

// Direction says whether this account granted the access or was granted it.
type Direction string

const (
	Outgoing Direction = "outgoing"
	Incoming Direction = "incoming"
)

// DelegatedAccess is one trusted-contact relationship as the CLI reports it.
type DelegatedAccess struct {
	ID        string    `json:"id"`
	Kind      Kind      `json:"kind"`
	Direction Direction `json:"direction"`
	// Contact is the other party: whom this account granted, or who granted it.
	Contact string `json:"contact"`
	Status  string `json:"status"`
	// Wait is the delay before a requested emergency access opens, in seconds. It
	// is zero for recovery contacts, which have no wait.
	Wait int `json:"wait,omitempty"`
	// AccessibleAt is when a pending emergency access opens, as a Unix time, or 0.
	AccessibleAt int64 `json:"accessible_at,omitempty"`
	Created      int64 `json:"created"`

	state        int
	types        int
	targetAddrID string
}

// Pending reports whether a request is in flight against this access, which is
// what a grant acts on and a cancel takes back.
func (d DelegatedAccess) Pending() bool {
	return d.state == stateAccessible || d.state == stateRecoverable || d.AccessibleAt != 0
}

// Accessible reports whether an emergency access is open, so the contact may
// sign in to the account that granted it.
func (d DelegatedAccess) Accessible() bool { return d.state == stateAccessible }

type outgoingRaw struct {
	DelegatedAccessID string
	State             jsonInt64
	Types             jsonInt64
	TargetEmail       string
	SourceAddressID   string
	AccessibleTime    *jsonInt64
	TriggerDelay      jsonInt64
	CreateTime        jsonInt64
}

type incomingRaw struct {
	DelegatedAccessID string
	State             jsonInt64
	Types             jsonInt64
	TargetAddressID   string
	SourceEmail       string
	AccessibleTime    *jsonInt64
	TriggerDelay      jsonInt64
	CreateTime        jsonInt64
}

const accessPath = "/account/v1/access"

// DelegatedAccesses lists every trusted-contact relationship of the given kind,
// both the ones this account granted and the ones granted to it.
//
// Proton keeps the two directions on two endpoints, so both are asked at once
// and the answers are tagged with which way they run. A record that carries both
// types is reported under whichever kind is asked for.
func (s *Service) DelegatedAccesses(ctx context.Context, kind Kind) ([]DelegatedAccess, error) {
	var out struct{ DelegatedAccesses []outgoingRaw }
	var in struct{ DelegatedAccesses []incomingRaw }
	if err := fetch.Together(ctx,
		func(ctx context.Context) error {
			return s.C.Decode(ctx, proton.Request{Method: "GET", Path: accessPath + "/outgoing"}, &out)
		},
		func(ctx context.Context) error {
			return s.C.Decode(ctx, proton.Request{Method: "GET", Path: accessPath + "/incoming"}, &in)
		},
	); err != nil {
		return nil, err
	}
	bit := kind.code()
	var das []DelegatedAccess
	for _, r := range out.DelegatedAccesses {
		if int(r.Types)&bit != 0 {
			das = append(das, r.view(kind))
		}
	}
	for _, r := range in.DelegatedAccesses {
		if int(r.Types)&bit != 0 {
			das = append(das, r.view(kind))
		}
	}
	sort.SliceStable(das, func(i, j int) bool { return das[i].Created > das[j].Created })
	return das, nil
}

func (r outgoingRaw) view(kind Kind) DelegatedAccess {
	d := DelegatedAccess{
		ID: r.DelegatedAccessID, Kind: kind, Direction: Outgoing,
		Contact: r.TargetEmail, Wait: int(r.TriggerDelay), Created: int64(r.CreateTime),
		state: int(r.State), types: int(r.Types),
	}
	d.AccessibleAt = deref(r.AccessibleTime)
	d.Status = accessStatus(kind, Outgoing, int(r.State), d.AccessibleAt)
	return d
}

func (r incomingRaw) view(kind Kind) DelegatedAccess {
	d := DelegatedAccess{
		ID: r.DelegatedAccessID, Kind: kind, Direction: Incoming,
		Contact: r.SourceEmail, Wait: int(r.TriggerDelay), Created: int64(r.CreateTime),
		state: int(r.State), types: int(r.Types), targetAddrID: r.TargetAddressID,
	}
	d.AccessibleAt = deref(r.AccessibleTime)
	d.Status = accessStatus(kind, Incoming, int(r.State), d.AccessibleAt)
	return d
}

func deref(p *jsonInt64) int64 {
	if p == nil {
		return 0
	}
	return int64(*p)
}

// accessStatus names the state a person reads it in, which is not the same word
// in each direction: an access this account granted is "open" once the contact
// may use it, while one granted to it is what this account may now do.
func accessStatus(kind Kind, dir Direction, state int, accessibleAt int64) string {
	switch state {
	case stateDisabled:
		return "disabled"
	case stateAccessible:
		if dir == Outgoing {
			return "access open"
		}
		return "ready to access"
	case stateRecoverable:
		if dir == Outgoing {
			return "recovery requested"
		}
		return "asked to help"
	}
	if accessibleAt != 0 {
		return "requested"
	}
	if kind == KindRecovery {
		return "standing by"
	}
	return "enabled"
}

// AddDelegatedAccess appoints a contact of the given kind, sealing a copy of the
// account's keys to them so the safety net can do its work later. wait is the
// emergency delay and is ignored for a recovery contact.
func (s *Service) AddDelegatedAccess(ctx context.Context, kind Kind, email string, wait time.Duration) (DelegatedAccess, error) {
	body, err := s.sealFor(ctx, kind, email, waitSeconds(kind, wait))
	if err != nil {
		return DelegatedAccess{}, err
	}
	var r struct{ DelegatedAccess outgoingRaw }
	if err := s.C.Decode(ctx, proton.Request{Method: "POST", Path: accessPath, Body: body}, &r); err != nil {
		return DelegatedAccess{}, err
	}
	return r.DelegatedAccess.view(kind), nil
}

// UpdateDelegatedAccess changes an emergency contact's wait, re-sealing the
// account's keys to them under a fresh token as the edit endpoint requires.
func (s *Service) UpdateDelegatedAccess(ctx context.Context, id string, kind Kind, email string, wait time.Duration) (DelegatedAccess, error) {
	body, err := s.sealFor(ctx, kind, email, waitSeconds(kind, wait))
	if err != nil {
		return DelegatedAccess{}, err
	}
	var r struct{ DelegatedAccess outgoingRaw }
	if err := s.C.Decode(ctx, proton.Request{Method: "PUT", Path: accessPath + "/" + id, Body: body}, &r); err != nil {
		return DelegatedAccess{}, err
	}
	return r.DelegatedAccess.view(kind), nil
}

// GrantDelegatedAccess lets a pending emergency request in at once, by setting
// the wait to nothing.
func (s *Service) GrantDelegatedAccess(ctx context.Context, id, email string) error {
	body, err := s.sealFor(ctx, KindEmergency, email, 0)
	if err != nil {
		return err
	}
	return s.C.Decode(ctx, proton.Request{Method: "PUT", Path: accessPath + "/" + id, Body: body}, nil)
}

// DeleteDelegatedAccess removes a contact from one safety net. A contact who is
// on both keeps the other.
func (s *Service) DeleteDelegatedAccess(ctx context.Context, id string, kind Kind) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: accessPath + "/" + id + "/delete",
		Body: map[string]any{"Types": kind.code()},
	}, nil)
}

// RequestDelegatedAccess starts the wait to use emergency access an account
// granted this one. Once the wait runs out the access opens, unless the granting
// account cancels it first.
func (s *Service) RequestDelegatedAccess(ctx context.Context, id string) error {
	return s.C.Decode(ctx, proton.Request{Method: "PUT", Path: accessPath + "/" + id + "/trigger"}, nil)
}

// AccessSession is what signing in to an account through emergency access yields:
// the tokens for its session, and the passphrase that opens its keys.
type AccessSession struct {
	UID          string
	AccessToken  string
	RefreshToken string
	// KeyPassword is the delegated token, which is what the granting account's
	// user keys were re-locked under, so it opens them.
	KeyPassword string
}

// AccessDelegatedAccess signs in to the account that granted this one emergency
// access, once the access is open. It hands back the session material for the
// caller to persist, and the passphrase that opens the accessed account's keys.
func (s *Service) AccessDelegatedAccess(ctx context.Context, incoming DelegatedAccess) (AccessSession, error) {
	var r struct {
		UID          string
		AccessToken  string
		RefreshToken string
		UserKeyToken string
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "POST", Path: accessPath + "/" + incoming.ID + "/auth"}, &r); err != nil {
		return AccessSession{}, err
	}
	u, err := s.keys(ctx)
	if err != nil {
		return AccessSession{}, err
	}
	rings, ok := u.AddrRings(incoming.targetAddrID)
	if !ok {
		return AccessSession{}, errs.Problemf("The address this access was granted to has no key on this machine.")
	}
	signers, err := keys.Signing(ctx, s.C, incoming.Contact)
	if err != nil {
		return AccessSession{}, err
	}
	if signers == nil {
		return AccessSession{}, errs.Problemf("%s publishes no key, so the token it signed cannot be trusted.", incoming.Contact)
	}
	verify, err := signers.Vouching(incoming.Contact)
	if err != nil {
		return AccessSession{}, err
	}
	keyPassword, err := keys.OpenDelegatedToken(r.UserKeyToken, rings.Read, verify)
	if err != nil {
		return AccessSession{}, err
	}
	return AccessSession{UID: r.UID, AccessToken: r.AccessToken, RefreshToken: r.RefreshToken, KeyPassword: keyPassword}, nil
}

// CancelDelegatedAccess takes back a request in flight - the one the contact
// made, or the one this account is answering - returning the access to standing
// by.
func (s *Service) CancelDelegatedAccess(ctx context.Context, id string) error {
	return s.C.Decode(ctx, proton.Request{Method: "PUT", Path: accessPath + "/" + id + "/reset"}, nil)
}

// sealFor builds the request body that hands a contact a copy of the account's
// keys: the user keys re-locked under a fresh token, and the token sealed to the
// contact and signed by this account.
func (s *Service) sealFor(ctx context.Context, kind Kind, email string, wait int) (map[string]any, error) {
	u, err := s.keys(ctx)
	if err != nil {
		return nil, err
	}
	addrRings, addr, err := u.PrimaryAddr()
	if err != nil {
		return nil, err
	}
	targetKR, err := keys.Published(ctx, s.C, email)
	if err != nil {
		return nil, err
	}
	if targetKR == nil {
		return nil, errs.Problemf("%s is not a Proton account, so it cannot be a trusted contact.", email).
			Hint("a trusted contact has to have a Proton account of their own")
	}
	token, err := keys.DelegatedToken()
	if err != nil {
		return nil, err
	}
	sealed, err := keys.SealDelegatedToken(token, targetKR, addrRings.Write)
	if err != nil {
		return nil, err
	}
	userKeys, err := u.UserKeysUnder(ctx, token)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"Types":           kind.code(),
		"TargetEmail":     email,
		"SourceAddressID": addr.ID,
		"UserKeys":        userKeys,
		"UserKeyToken":    sealed,
		"TriggerDelay":    wait,
	}, nil
}

// waitSeconds is the delay in the units Proton keeps it, and nothing for a
// recovery contact, which has no wait.
func waitSeconds(kind Kind, wait time.Duration) int {
	if kind == KindRecovery {
		return 0
	}
	return int(wait / time.Second)
}
