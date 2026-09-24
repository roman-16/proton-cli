package pass

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"

	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/crypto/aead"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

// Sharing a vault with somebody else.
//
// A vault is opened by its share key, and every item in it is sealed under that
// key. Sharing is therefore handing somebody the key itself - every rotation of
// it, since older items are sealed under older ones - encrypted to their key and
// signed with yours.
//
// Proton passes it along without being able to read it, and the signature is
// what tells the recipient the vault really came from you rather than from
// whoever happened to send the request - which is why an offer is only ever
// opened against the keys Proton publishes for the person it names as sender,
// under the context the sender signed it with. A key that anybody could have put
// in the record is a key nobody should accept.

// inviteContext is the signature context on an invitation to somebody who
// already has a Proton account. Marking it critical means a client that does not
// understand the notation refuses the signature rather than trusting it blind.
//
// It names vaults because that is what sharing was when Proton wrote it; an item
// invitation is signed under the same context, so both open with one rule.
const inviteContext = "pass.invite.vault.existing-user"

// newUserContext is the context on an offer to somebody with no Proton account.
// Nothing encrypted travels under it - there is no key to travel to - so what it
// signs is the address and the share key together.
const newUserContext = "pass.invite.vault.new-user"

// The states Proton gives such an offer: it waits for the person to create an
// account, and then for the key.
const (
	newUserWaiting = 1
	newUserReady   = 2
)

// What a vault can be shared as. Proton sends these as strings.
const (
	roleManager = "1"
	roleWrite   = "2"
	roleRead    = "3"
)

// roleWords name what somebody may do, the way --access reads.
var roleWords = map[string]string{
	roleManager: "manager", roleWrite: "editor", roleRead: "viewer",
}

// VaultRoles are the ways a vault can be shared, for --access.
func VaultRoles() []string { return []string{"viewer", "editor", "manager"} }

// Stage is how far an offer nobody has accepted has got.
//
// An address outside Proton publishes no key, so there is nothing to encrypt the
// share key to and the offer is held: Proton emails an invitation to create an
// account, and only once there is one can the key travel - sent by the person who
// offered it, since nobody else holds it. That is two states a Proton address
// never passes through, and every command that acts on "whoever this address is"
// has to tell them apart.
type Stage string

const (
	// StageOffered is an invitation to a Proton address, waiting to be accepted.
	StageOffered Stage = "offered"
	// StageNoAccount is an offer to an address outside Proton, held until the
	// person creates an account.
	StageNoAccount Stage = "no-account"
	// StageReady is such an offer whose invitee has since created one, so the keys
	// are the inviter's to hand over.
	StageReady Stage = "ready"
)

// roleFor turns the word somebody typed into what Proton wants.
func roleFor(access string) (string, error) {
	for id, word := range roleWords {
		if word == access {
			return id, nil
		}
	}
	return "", fmt.Errorf("unknown access %q", access)
}

// Invite is somebody who has been offered a vault or an item, or offered you one.
type Invite struct {
	ID string `json:"id"`
	// ShareID is the share the invitation was made on. It is only known on your
	// own: an invitation you received names a share you cannot see yet.
	ShareID string `json:"share_id,omitempty"`
	// ItemID is the item offered, and is empty on an invitation to a whole vault.
	ItemID string `json:"item_id,omitempty"`
	// Vault is what the sender calls it, which is what an invitation you received
	// shows instead.
	Vault   string `json:"vault,omitempty"`
	Email   string `json:"email"`
	Inviter string `json:"inviter,omitempty"`
	Access  string `json:"access"`
	// Items is how many things are in the vault, as the sender counted them.
	Items int `json:"items,omitempty"`
	// Stage is how far an offer of yours has got, and is empty on one you were
	// sent: how far it has got is whether you have answered it.
	Stage Stage `json:"stage,omitempty"`
}

// outside reports whether the offer is held for somebody with no Proton account,
// which is what decides the endpoint every verb reaches it through.
func (i Invite) outside() bool { return i.Stage == StageNoAccount || i.Stage == StageReady }

// Kind is what the invitation offers, for a listing that carries both.
func (i Invite) Kind() string {
	if i.ItemID != "" {
		return "item"
	}
	return "vault"
}

// VaultShare offers a vault to somebody, and says which kind of offer it turned
// out to be.
//
// Every rotation of the share key is sent, because an item made before the last
// rotation is still sealed under the older one - somebody given only the newest
// key would see a vault half of which will not open.
func (s *Service) VaultShare(ctx context.Context, shareID, email, access string) (Stage, error) {
	sk, err := s.decryptShareKeys(ctx, shareID)
	if err != nil {
		return "", err
	}
	return s.invite(ctx, shareID, "", email, access, sk.keys)
}

// ItemShare offers one item to somebody, leaving the vault around it alone.
//
// What travels is the item's own key rather than the vault's, which is what
// makes the difference: the person invited can open that item and has no way to
// reach anything else sealed under the same share.
func (s *Service) ItemShare(ctx context.Context, shareID, itemID, email, access string) (Stage, error) {
	item, err := s.itemKeys(ctx, shareID, itemID)
	if err != nil {
		return "", err
	}
	return s.invite(ctx, shareID, itemID, email, access, item)
}

// itemKeys opens every rotation of one item's key.
//
// An item invitation carries all of them for the reason a vault one does: a
// revision written under an older rotation still needs that rotation to open.
func (s *Service) itemKeys(ctx context.Context, shareID, itemID string) (map[int][]byte, error) {
	sk, err := s.decryptShareKeys(ctx, shareID)
	if err != nil {
		return nil, err
	}
	var r struct {
		Keys struct {
			Keys []struct {
				Key         string
				KeyRotation int
			}
		}
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: fmt.Sprintf("/pass/v1/share/%s/item/%s/key", shareID, itemID),
	}, &r); err != nil {
		return nil, err
	}
	out := make(map[int][]byte, len(r.Keys.Keys))
	// Recorded and not counted: the caller refuses outright when none of them
	// opens, so nothing is being hidden - the log is here to say which of the
	// rotations was the problem.
	for _, k := range r.Keys.Keys {
		shareKey, ok := sk.keys[k.KeyRotation]
		if !ok {
			slog.DebugContext(ctx, "pass: no share key for an item key's rotation",
				"item", itemID, "count", k.KeyRotation)
			continue
		}
		sealed, err := base64.StdEncoding.DecodeString(k.Key)
		if err != nil {
			slog.DebugContext(ctx, "pass: an item key is not base64", "item", itemID, "error", err)
			continue
		}
		key, err := aead.Decrypt(shareKey, sealed, []byte(aead.TagItemKey))
		if err != nil {
			slog.DebugContext(ctx, "pass: an item key will not open with its share key",
				"item", itemID, "error", err)
			continue
		}
		out[k.KeyRotation] = key
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no key of that item will open")
	}
	return out, nil
}

// invite sends the keys that open something to somebody else, encrypted to them
// and signed with this account's address key.
//
// A vault invitation and an item invitation differ only in which keys travel and
// what the request says they are for, so they are one request built twice.
//
// An address Proton publishes no key for cannot be sent anything, so the offer is
// held instead: a signature over the address and the share key together, which
// commits the keys this offer is for to the person named, so that handing them
// over later against whatever key Proton publishes for them then is safe.
func (s *Service) invite(ctx context.Context, shareID, itemID, email, access string, open map[int][]byte) (Stage, error) {
	role, err := roleFor(access)
	if err != nil {
		return "", err
	}
	u, err := s.keys(ctx)
	if err != nil {
		return "", err
	}
	addrRings, _, err := u.PrimaryAddr()
	if err != nil {
		return "", err
	}
	inviteeKR, err := keys.Published(ctx, s.C, email)
	if err != nil {
		return "", err
	}
	body := map[string]any{"Email": email, "ShareRoleID": role, "TargetType": targetVault}
	if itemID != "" {
		body["TargetType"], body["ItemID"] = targetItem, itemID
	}
	if inviteeKR == nil {
		sig, err := s.signForNewUser(ctx, shareID, email, addrRings.Write)
		if err != nil {
			return "", err
		}
		body["Signature"] = sig
		return StageNoAccount, s.C.Decode(ctx, proton.Request{
			Method: "POST", Path: "/pass/v1/share/" + shareID + "/invite/new_user", Body: body,
		}, nil)
	}
	sealed, err := sealKeys(open, inviteeKR, addrRings.Write, email)
	if err != nil {
		return "", err
	}
	body["Keys"] = sealed
	return StageOffered, s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/pass/v1/share/" + shareID + "/invite",
		Body: body,
	}, nil)
}

// sealKeys encrypts every rotation to the invitee and signs each with this
// account's address key, which is the one thing an offer carries whether it was
// made to somebody with an account or to somebody who has since got one.
func sealKeys(open map[int][]byte, to, signWith *pgp.KeyRing, email string) ([]map[string]any, error) {
	rotations := make([]int, 0, len(open))
	for r := range open {
		rotations = append(rotations, r)
	}
	sort.Ints(rotations)

	sealed := make([]map[string]any, 0, len(rotations))
	for _, rotation := range rotations {
		msg, err := to.EncryptWithContext(
			pgp.NewPlainMessage(open[rotation]), signWith,
			pgp.NewSigningContext(inviteContext, true),
		)
		if err != nil {
			return nil, fmt.Errorf("encrypt the key for %s: %w", email, err)
		}
		sealed = append(sealed, map[string]any{
			"Key":         base64.StdEncoding.EncodeToString(msg.GetBinary()),
			"KeyRotation": rotation,
		})
	}
	return sealed, nil
}

// newUserSigned is what an offer to an address outside Proton commits to: the
// address and the share's newest key, together, so neither can be changed under
// the other between the offer and the keys.
func newUserSigned(email string, shareKey []byte) *pgp.PlainMessage {
	body := make([]byte, 0, len(email)+1+len(shareKey))
	body = append(body, email...)
	body = append(body, '|')
	return pgp.NewPlainMessage(append(body, shareKey...))
}

func (s *Service) signForNewUser(ctx context.Context, shareID, email string, signWith *pgp.KeyRing) (string, error) {
	sk, err := s.decryptShareKeys(ctx, shareID)
	if err != nil {
		return "", err
	}
	shareKey, _ := sk.latest()
	sig, err := signWith.SignDetachedWithContext(
		newUserSigned(email, shareKey), pgp.NewSigningContext(newUserContext, true))
	if err != nil {
		return "", fmt.Errorf("sign the offer to %s: %w", email, err)
	}
	return base64.StdEncoding.EncodeToString(sig.GetBinary()), nil
}

// sentInvite is an offer to somebody who already had a Proton account.
type sentInvite struct {
	InviteID     string
	InvitedEmail string
	InviterEmail string
	ShareRoleID  string
	TargetType   int
	TargetID     string
}

// newUserInvite is an offer made to an address outside Proton. The signature is
// this account's own, over the address and the share key together, and is what
// confirming checks before any key goes anywhere.
type newUserInvite struct {
	NewUserInviteID string
	InvitedEmail    string
	InviterEmail    string
	ShareRoleID     string
	TargetType      int
	TargetID        string
	State           int
	Signature       string
}

// invitesOn is every offer standing on a share, in Proton's two shapes. One
// request carries both, so who is waiting is never answered in halves.
func (s *Service) invitesOn(ctx context.Context, shareID string) ([]sentInvite, []newUserInvite, error) {
	var r struct {
		Invites        []sentInvite
		NewUserInvites []newUserInvite
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/pass/v1/share/" + shareID + "/invite",
	}, &r); err != nil {
		return nil, nil, err
	}
	return r.Invites, r.NewUserInvites, nil
}

// newUserStage reads Proton's number, and calls a state this build has not been
// told about one that is not ready: confirming sends keys, and a state nobody
// recognises is not grounds to send any.
func newUserStage(state int) Stage {
	if state == newUserReady {
		return StageReady
	}
	return StageNoAccount
}

// InvitesSent lists who has been offered something of yours and has not answered.
//
// Proton keeps an item's invitations on the share the item lives in, so one
// request answers for the vault and for everything in it; itemID narrows that to
// the invitations about one item.
func (s *Service) InvitesSent(ctx context.Context, shareID, itemID string) ([]Invite, error) {
	sent, held, err := s.invitesOn(ctx, shareID)
	if err != nil {
		return nil, err
	}
	out := make([]Invite, 0, len(sent)+len(held))
	for _, i := range sent {
		out = append(out, Invite{
			ID: i.InviteID, ShareID: shareID, Email: i.InvitedEmail, ItemID: itemOf(i.TargetType, i.TargetID),
			Inviter: i.InviterEmail, Access: roleWord(i.ShareRoleID), Stage: StageOffered,
		})
	}
	for _, i := range held {
		out = append(out, Invite{
			ID: i.NewUserInviteID, ShareID: shareID, Email: i.InvitedEmail, ItemID: itemOf(i.TargetType, i.TargetID),
			Inviter: i.InviterEmail, Access: roleWord(i.ShareRoleID), Stage: newUserStage(i.State),
		})
	}
	out = slices.DeleteFunc(out, func(i Invite) bool { return i.ItemID != itemID })
	sort.SliceStable(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out, nil
}

// itemOf is the item an offer is about, and nothing at all for one about a whole
// vault: a vault offer's target is the vault, which is not an item ID.
func itemOf(targetType int, targetID string) string {
	if targetType == targetItem {
		return targetID
	}
	return ""
}

// ConfirmInvite hands the keys to somebody who was offered this before they had
// a Proton account and has since made one.
//
// The signature checked here is this account's own, made when the offer went out.
// It says which address these keys were promised to, so checking it against the
// address Proton now reports is what stops a substituted invitee from being
// handed them. It is required and it is contextual: a signature made for anything
// else vouches for nothing.
//
// What travels is the keys of whatever the offer was about - the item's when it
// was one item, the vault's when it was the vault - which is the same rule an
// offer to a Proton address follows.
func (s *Service) ConfirmInvite(ctx context.Context, shareID, itemID, email string) error {
	_, held, err := s.invitesOn(ctx, shareID)
	if err != nil {
		return err
	}
	for _, i := range held {
		if !strings.EqualFold(i.InvitedEmail, email) || itemOf(i.TargetType, i.TargetID) != itemID {
			continue
		}
		return s.confirm(ctx, shareID, i)
	}
	return errs.Problemf("Nobody at %s was offered this before they had a Proton account.", email).
		Hint("`share get` shows who is waiting").Exit(3)
}

func (s *Service) confirm(ctx context.Context, shareID string, held newUserInvite) error {
	if newUserStage(held.State) != StageReady {
		return errs.Problemf("%s has not created a Proton account yet.", held.InvitedEmail).
			Hint("Proton has emailed them an invitation to create one").Exit(3)
	}
	u, err := s.keys(ctx)
	if err != nil {
		return err
	}
	addrRings, _, err := u.PrimaryAddr()
	if err != nil {
		return err
	}
	sk, err := s.decryptShareKeys(ctx, shareID)
	if err != nil {
		return err
	}
	shareKey, _ := sk.latest()
	raw, err := base64.StdEncoding.DecodeString(held.Signature)
	if err != nil {
		return fmt.Errorf("the offer's signature is not base64: %w", err)
	}
	if err := addrRings.Write.VerifyDetachedWithContext(
		newUserSigned(held.InvitedEmail, shareKey), pgp.NewPGPSignature(raw), pgp.GetUnixTime(),
		pgp.NewVerificationContext(newUserContext, true, 0)); err != nil {
		return errs.Problemf(
			"The offer to %s is not the one this account signed, so no key will be handed over.",
			held.InvitedEmail).
			Hint("`share remove` withdraws it; offer it to them again afterwards").Exit(3)
	}
	inviteeKR, err := keys.Published(ctx, s.C, held.InvitedEmail)
	if err != nil {
		return err
	}
	if inviteeKR == nil {
		return errs.Problemf("Proton publishes no key for %s yet, so there is nothing to hand the keys to.",
			held.InvitedEmail).Exit(3)
	}
	open := sk.keys
	if itemID := itemOf(held.TargetType, held.TargetID); itemID != "" {
		if open, err = s.itemKeys(ctx, shareID, itemID); err != nil {
			return err
		}
	}
	sealed, err := sealKeys(open, inviteeKR, addrRings.Write, held.InvitedEmail)
	if err != nil {
		return err
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "POST",
		Path:   "/pass/v1/share/" + shareID + "/invite/new_user/" + held.NewUserInviteID + "/keys",
		Body:   map[string]any{"Keys": sealed},
	}, nil)
}

// roleWord names a role, falling back to the number for one this version has
// not been told about rather than guessing.
func roleWord(id string) string {
	if id == "" {
		return ""
	}
	if w, ok := roleWords[id]; ok {
		return w
	}
	return "role " + id
}

// InviteRevoke withdraws an offer nobody has answered, of either kind.
//
// Proton keeps an offer to an address outside Proton on an endpoint of its own,
// and each path is written out rather than assembled: a path built behind a call
// is a request nothing can see this CLI is able to send.
func (s *Service) InviteRevoke(ctx context.Context, shareID string, invite Invite) error {
	if invite.outside() {
		return s.C.Decode(ctx, proton.Request{Method: "DELETE",
			Path: fmt.Sprintf("/pass/v1/share/%s/invite/new_user/%s", shareID, invite.ID)}, nil)
	}
	return s.C.Decode(ctx, proton.Request{Method: "DELETE",
		Path: fmt.Sprintf("/pass/v1/share/%s/invite/%s", shareID, invite.ID)}, nil)
}

// InvitesReceived lists what other people have offered you.
func (s *Service) InvitesReceived(ctx context.Context) ([]Invite, error) {
	var r struct {
		Invites []rawUserInvite
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/pass/v1/invite"}, &r); err != nil {
		return nil, err
	}
	u, err := s.keys(ctx)
	if err != nil {
		return nil, err
	}
	// One sender's keys serve every offer they made.
	inviters := map[string]*pgp.KeyRing{}
	out := make([]Invite, 0, len(r.Invites))
	for _, i := range r.Invites {
		invite := Invite{
			ID: i.InviteToken, Email: i.InvitedEmail, Inviter: i.InviterEmail,
			Access: roleWord(i.ShareRoleID), Items: i.VaultData.ItemCount,
		}
		if i.TargetType == targetItem {
			invite.ItemID = i.TargetID
		}
		// The vault's name is readable before the offer is taken: the invitation
		// carries the key that opens it, encrypted to the address it was sent to
		// and signed by the sender. A name that will not come out - the key will
		// not open, or was not signed by the sender - is left empty rather than
		// guessed at; the offer is still there to answer, and answering it is
		// where a key nobody vouches for is refused.
		invite.Vault = s.previewName(ctx, i, u, inviters)
		out = append(out, invite)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Inviter < out[j].Inviter })
	return out, nil
}

// previewName is the vault's name as the invitation lets it be read, or "".
//
// Recorded and not counted. The row is on the screen with its name blank, so
// nothing is hidden; the log says which of the ways it failed to come out this
// was, which the blank cell cannot.
func (s *Service) previewName(ctx context.Context, i rawUserInvite, u *keys.Unlocked, inviters map[string]*pgp.KeyRing) string {
	inviter, ok := inviters[i.InviterEmail]
	if !ok {
		kr, err := s.inviterKeys(ctx, i.InviterEmail)
		if err != nil {
			slog.DebugContext(ctx, "pass: an inviter's keys could not be read",
				"signer", i.InviterEmail, "error", err.Error())
		}
		inviter, inviters[i.InviterEmail] = kr, kr
	}
	if inviter == nil {
		return ""
	}
	key, err := s.openInviteKey(i, u, inviter, i.VaultData.ContentKeyRotation)
	if err != nil {
		slog.DebugContext(ctx, "pass: an invitation's key did not open for its preview",
			"signer", i.InviterEmail, "error", err.Error())
		return ""
	}
	vault, err := decryptVault(i.VaultData.Content, key)
	if err != nil {
		slog.DebugContext(ctx, "pass: an invitation's preview did not decrypt",
			"signer", i.InviterEmail, "error", err.Error())
		return ""
	}
	return vault.Name
}

// rawUserInvite is an invitation as it reaches the person offered it. The vault's
// name is in encrypted content they cannot open until they accept, so Proton
// sends a preview alongside.
type rawUserInvite struct {
	InviteToken      string
	InvitedEmail     string
	InvitedAddressID string
	InviterEmail     string
	ShareRoleID      string
	TargetType       int
	TargetID         string
	Keys             []struct {
		Key         string
		KeyRotation int
	}
	VaultData struct {
		Content            string
		ContentKeyRotation int
		ItemCount          int
		MemberCount        int
	}
}

// errUnsigned is an invitation whose key was not signed by the person it names
// as sender - or was signed for something other than an invitation.
var errUnsigned = errors.New("the key was not signed by the inviter")

// inviterKeys are the keys an invitation's sender may have signed it with.
//
// Every key Proton publishes for the address goes in, because the invitation
// may be older than the sender's current key. An address Proton publishes no
// key for cannot have sent an invitation, and one whose keys this build cannot
// read is reported as such rather than as a forgery.
func (s *Service) inviterKeys(ctx context.Context, email string) (*pgp.KeyRing, error) {
	signers, err := keys.Signing(ctx, s.C, email)
	if err != nil {
		return nil, err
	}
	if signers == nil {
		return nil, errs.Problemf("Proton publishes no key for %s, so nothing can vouch for this offer.", email)
	}
	return signers.Vouching(email)
}

// openInviteKey unseals one rotation of the vault key an invitation carries,
// checking on the way that the sender signed it.
//
// The check is under the same context the sender signed with, and it is
// required: a signature made for anything else, or by anybody else, opens
// nothing. This is the one place an offered key is opened, so the preview and
// the acceptance cannot come to disagree about who sent it.
func (s *Service) openInviteKey(i rawUserInvite, u *keys.Unlocked, inviter *pgp.KeyRing, rotation int) ([]byte, error) {
	addrRings, ok := u.AddrRings(i.InvitedAddressID)
	if !ok {
		return nil, fmt.Errorf("no key for the address this was sent to")
	}
	for _, k := range i.Keys {
		if k.KeyRotation != rotation {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(k.Key)
		if err != nil {
			return nil, err
		}
		opened, err := addrRings.Read.DecryptWithContext(pgp.NewPGPMessage(raw), inviter, pgp.GetUnixTime(),
			pgp.NewVerificationContext(inviteContext, true, 0))
		if err != nil {
			var unsigned pgp.SignatureVerificationError
			if errors.As(err, &unsigned) {
				return nil, fmt.Errorf("%w: %v", errUnsigned, err)
			}
			return nil, err
		}
		return opened.GetBinary(), nil
	}
	return nil, fmt.Errorf("the invitation carries no key for rotation %d", rotation)
}

// InviteAccept takes what somebody offered, whether a vault or one item.
//
// The keys arrive encrypted to the address the offer was sent to and signed by
// the sender. Each is checked against the sender's published keys and moved onto
// the account's own primary user key, which is where the CLI reads a share's
// keys from afterwards - so accepting is what turns an offer into something that
// opens like anything else. A key the sender did not sign is refused: Proton
// could not read it, so only the signature says who put it there.
func (s *Service) InviteAccept(ctx context.Context, token string) error {
	invite, u, err := s.findInvite(ctx, token)
	if err != nil {
		return err
	}
	if _, ok := u.AddrRings(invite.InvitedAddressID); !ok {
		return errs.Problemf("The keys for %s will not open, so that offer cannot be taken.", invite.InvitedEmail)
	}
	inviter, err := s.inviterKeys(ctx, invite.InviterEmail)
	if err != nil {
		return err
	}
	ownKey, err := u.PrimaryUserKey()
	if err != nil {
		return err
	}

	sealedKeys := make([]map[string]any, 0, len(invite.Keys))
	for _, k := range invite.Keys {
		opened, err := s.openInviteKey(invite, u, inviter, k.KeyRotation)
		if errors.Is(err, errUnsigned) {
			return errs.Problemf("The key in this offer was not signed by %s, so it cannot be taken.", invite.InviterEmail)
		}
		if err != nil {
			return fmt.Errorf("open the vault key sent to you: %w", err)
		}
		sealed, err := ownKey.Encrypt(pgp.NewPlainMessage(opened), ownKey)
		if err != nil {
			return err
		}
		sealedKeys = append(sealedKeys, map[string]any{
			"Key":         base64.StdEncoding.EncodeToString(sealed.GetBinary()),
			"KeyRotation": k.KeyRotation,
		})
	}

	return s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/pass/v1/invite/" + token,
		Body: map[string]any{"Keys": sealedKeys},
	}, nil)
}

// InviteReject turns an offer down. Nothing is opened: declining is saying no to
// the offer rather than reading it first.
func (s *Service) InviteReject(ctx context.Context, token string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "DELETE", Path: "/pass/v1/invite/" + token,
	}, nil)
}

// findInvite reads one offer whole, with the account's keys.
func (s *Service) findInvite(ctx context.Context, token string) (rawUserInvite, *keys.Unlocked, error) {
	var r struct {
		Invites []rawUserInvite
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/pass/v1/invite"}, &r); err != nil {
		return rawUserInvite{}, nil, err
	}
	u, err := s.keys(ctx)
	if err != nil {
		return rawUserInvite{}, nil, err
	}
	for _, i := range r.Invites {
		if i.InviteToken == token {
			return i, u, nil
		}
	}
	return rawUserInvite{}, nil, &errs.NotFound{Kind: "invitation", Ref: token}
}
