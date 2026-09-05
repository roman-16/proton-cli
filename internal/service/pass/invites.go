package pass

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"sort"

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
}

// Kind is what the invitation offers, for a listing that carries both.
func (i Invite) Kind() string {
	if i.ItemID != "" {
		return "item"
	}
	return "vault"
}

// VaultShare offers a vault to somebody.
//
// Every rotation of the share key is sent, because an item made before the last
// rotation is still sealed under the older one - somebody given only the newest
// key would see a vault half of which will not open.
func (s *Service) VaultShare(ctx context.Context, shareID, email, access string) error {
	sk, err := s.decryptShareKeys(ctx, shareID)
	if err != nil {
		return err
	}
	return s.invite(ctx, shareID, "", email, access, sk.keys)
}

// ItemShare offers one item to somebody, leaving the vault around it alone.
//
// What travels is the item's own key rather than the vault's, which is what
// makes the difference: the person invited can open that item and has no way to
// reach anything else sealed under the same share.
func (s *Service) ItemShare(ctx context.Context, shareID, itemID, email, access string) error {
	keys, err := s.itemKeys(ctx, shareID, itemID)
	if err != nil {
		return err
	}
	return s.invite(ctx, shareID, itemID, email, access, keys)
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
func (s *Service) invite(ctx context.Context, shareID, itemID, email, access string, keys map[int][]byte) error {
	role, err := roleFor(access)
	if err != nil {
		return err
	}
	u, err := s.keys(ctx)
	if err != nil {
		return err
	}
	addrKR, _, err := u.PrimaryAddr()
	if err != nil {
		return err
	}
	inviteeKR, err := s.publicKeyRing(ctx, email)
	if err != nil {
		return err
	}

	rotations := make([]int, 0, len(keys))
	for r := range keys {
		rotations = append(rotations, r)
	}
	sort.Ints(rotations)

	sealedKeys := make([]map[string]any, 0, len(rotations))
	for _, rotation := range rotations {
		sealed, err := inviteeKR.EncryptWithContext(
			pgp.NewPlainMessage(keys[rotation]), addrKR,
			pgp.NewSigningContext(inviteContext, true),
		)
		if err != nil {
			return fmt.Errorf("encrypt the key for %s: %w", email, err)
		}
		sealedKeys = append(sealedKeys, map[string]any{
			"Key":         base64.StdEncoding.EncodeToString(sealed.GetBinary()),
			"KeyRotation": rotation,
		})
	}

	body := map[string]any{
		"Keys": sealedKeys, "Email": email,
		"ShareRoleID": role, "TargetType": targetVault,
	}
	if itemID != "" {
		body["TargetType"], body["ItemID"] = targetItem, itemID
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/pass/v1/share/" + shareID + "/invite",
		Body: body,
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
			Hint("a vault can only be shared with another Proton account")
	}
	return kr, nil
}

// InvitesSent lists who has been offered something of yours and has not answered.
//
// Proton keeps an item's invitations on the share the item lives in, so one
// request answers for the vault and for everything in it; itemID narrows that to
// the invitations about one item.
func (s *Service) InvitesSent(ctx context.Context, shareID, itemID string) ([]Invite, error) {
	var r struct {
		Invites []struct {
			InviteID     string
			InvitedEmail string
			InviterEmail string
			ShareRoleID  string
			TargetType   int
			TargetID     string
		}
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/pass/v1/share/" + shareID + "/invite",
	}, &r); err != nil {
		return nil, err
	}
	out := make([]Invite, 0, len(r.Invites))
	for _, i := range r.Invites {
		invite := Invite{
			ID: i.InviteID, ShareID: shareID, Email: i.InvitedEmail,
			Inviter: i.InviterEmail, Access: roleWord(i.ShareRoleID),
		}
		if i.TargetType == targetItem {
			invite.ItemID = i.TargetID
		}
		if invite.ItemID != itemID {
			continue
		}
		out = append(out, invite)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out, nil
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

// InviteRevoke withdraws an offer nobody has answered.
func (s *Service) InviteRevoke(ctx context.Context, shareID, inviteID string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "DELETE", Path: "/pass/v1/share/" + shareID + "/invite/" + inviteID,
	}, nil)
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
	kr, err := keys.Signing(ctx, s.C, email)
	if err != nil {
		return nil, err
	}
	if kr == nil {
		return nil, errs.Problemf("Proton publishes no key for %s, so nothing can vouch for this offer.", email)
	}
	return kr, nil
}

// openInviteKey unseals one rotation of the vault key an invitation carries,
// checking on the way that the sender signed it.
//
// The check is under the same context the sender signed with, and it is
// required: a signature made for anything else, or by anybody else, opens
// nothing. This is the one place an offered key is opened, so the preview and
// the acceptance cannot come to disagree about who sent it.
func (s *Service) openInviteKey(i rawUserInvite, u *keys.Unlocked, inviter *pgp.KeyRing, rotation int) ([]byte, error) {
	addrKR, ok := u.AddrKR(i.InvitedAddressID)
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
		opened, err := addrKR.DecryptWithContext(pgp.NewPGPMessage(raw), inviter, pgp.GetUnixTime(),
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
	if _, ok := u.AddrKR(invite.InvitedAddressID); !ok {
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
