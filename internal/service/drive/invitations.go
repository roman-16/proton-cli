package drive

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/fetch"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/skip"
)

const sigContextMember = "drive.share-member.member"

type Invitation struct {
	InvitationID string `json:"invitation_id"`
	InviterEmail string `json:"inviter_email"`
	InviteeEmail string `json:"invitee_email"`
	ShareID      string `json:"share_id"`
	VolumeID     string `json:"volume_id"`
	// Name is what the thing being offered is called, and Type what it is. A
	// name that would not decrypt is empty, and the invitation is still listed
	// and still answerable.
	Name       string `json:"name"`
	Type       string `json:"type"`
	Role       string `json:"role"`
	CreateTime int64  `json:"create_time"`
}

// invitationDetail is one invitation as Proton describes it: who offered what,
// the share it hangs from, and the key packet that opens that share.
type invitationDetail struct {
	Invitation struct {
		InvitationID string
		InviterEmail string
		InviteeEmail string
		Permissions  int
		CreateTime   int64
		KeyPacket    string
	}
	Share struct {
		ShareID    string
		VolumeID   string
		ShareKey   string
		Passphrase string
	}
	Link struct {
		LinkID string
		Name   string
		Type   int
	}
}

// ListInvitations fetches per-invitation details because the listing endpoint
// returns only IDs (no inviter/role/timestamp).
func (s *Service) ListInvitations(ctx context.Context) ([]Invitation, error) {
	// The endpoint hands back the anchor its next answer starts from.
	anchor := ""
	return proton.All(ctx, func(ctx context.Context, _ int) ([]Invitation, bool, error) {
		var r struct {
			Invitations []struct{ VolumeID, ShareID, InvitationID string }
			AnchorID    string
			More        bool
		}
		req := proton.Request{Method: "GET", Path: "/drive/v2/shares/invitations"}
		if anchor != "" {
			req.Query = proton.Query("AnchorID", anchor)
		}
		if err := s.C.Decode(ctx, req, &r); err != nil {
			return nil, false, err
		}
		out := make([]Invitation, 0, len(r.Invitations))
		for _, inv := range r.Invitations {
			d, err := s.invitationDetails(ctx, inv.InvitationID)
			if err != nil {
				skip.Record(ctx, skip.KindInvitation, inv.InvitationID, skip.Unreadable, err)
				continue
			}
			out = append(out, d)
		}
		anchor = r.AnchorID
		return out, r.More && r.AnchorID != "", nil
	})
}

// GetInvitation reads one invitation waiting for an answer.
func (s *Service) GetInvitation(ctx context.Context, id string) (Invitation, error) {
	return s.invitationDetails(ctx, id)
}

func (s *Service) invitationDetails(ctx context.Context, id string) (Invitation, error) {
	r, u, err := s.invitation(ctx, id)
	if err != nil {
		return Invitation{}, err
	}
	name, err := offeredName(u, r)
	if err != nil {
		// Recorded and not counted: the invitation is in the listing, answerable
		// by the ID beside it, and the empty name is the screen saying as much.
		slog.DebugContext(ctx, "drive: the name an invitation offers could not be read",
			"invitation", id, "share", r.Share.ShareID, "error", err)
	}
	return Invitation{
		InvitationID: r.Invitation.InvitationID,
		InviterEmail: r.Invitation.InviterEmail,
		InviteeEmail: r.Invitation.InviteeEmail,
		ShareID:      r.Share.ShareID,
		VolumeID:     r.Share.VolumeID,
		Name:         name,
		Type:         linkType(r.Link.Type),
		Role:         roleLabel(r.Invitation.Permissions),
		CreateTime:   r.Invitation.CreateTime,
	}, nil
}

// invitation is one invitation as Proton describes it, and the keys that open
// it, asked for together.
func (s *Service) invitation(ctx context.Context, id string) (invitationDetail, *keys.Unlocked, error) {
	var r invitationDetail
	var u *keys.Unlocked
	if err := fetch.Together(ctx,
		func(ctx context.Context) error {
			return s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/drive/v2/shares/invitations/" + id}, &r)
		},
		func(ctx context.Context) error {
			var err error
			u, err = s.keys(ctx)
			return err
		},
	); err != nil {
		return invitationDetail{}, nil, err
	}
	return r, u, nil
}

// offeredName is what the thing on offer is called.
//
// Everything it takes is in the answer that describes the invitation: the key
// packet this account's address opens yields the share's session key, which
// opens the passphrase, which unlocks the share key the item's name is sealed
// to. So a listing can say what is being offered without asking again.
func offeredName(u *keys.Unlocked, r invitationDetail) (string, error) {
	_, sessionKey, err := invitationKey(u, r)
	if err != nil {
		return "", err
	}
	passphrase, err := invitationPassphrase(sessionKey, r)
	if err != nil {
		return "", err
	}
	locked, err := pgp.NewKeyFromArmored(r.Share.ShareKey)
	if err != nil {
		return "", err
	}
	unlocked, err := locked.Unlock(passphrase)
	if err != nil {
		return "", fmt.Errorf("unlock share key: %w", err)
	}
	shareKR, err := pgp.NewKeyRing(unlocked)
	if err != nil {
		return "", err
	}
	return decryptName(r.Link.Name, shareKR)
}

// invitationKey opens the key packet an invitation carries, with the address it
// was sent to, which yields the session key the share's passphrase is sealed
// under.
func invitationKey(u *keys.Unlocked, r invitationDetail) (keys.Rings, *pgp.SessionKey, error) {
	addr, ok := inviteeAddr(u, r.Invitation.InviteeEmail)
	if !ok {
		return keys.Rings{}, nil, fmt.Errorf("no usable address key for %s", r.Invitation.InviteeEmail)
	}
	keyPacket, err := base64.StdEncoding.DecodeString(r.Invitation.KeyPacket)
	if err != nil {
		return keys.Rings{}, nil, fmt.Errorf("decode key packet: %w", err)
	}
	sessionKey, err := addr.Read.DecryptSessionKey(keyPacket)
	if err != nil {
		return keys.Rings{}, nil, fmt.Errorf("decrypt session key: %w", err)
	}
	return addr, sessionKey, nil
}

// invitationPassphrase is the share passphrase an invitation's session key
// opens.
func invitationPassphrase(sessionKey *pgp.SessionKey, r invitationDetail) ([]byte, error) {
	enc, err := pgp.NewPGPMessageFromArmored(r.Share.Passphrase)
	if err != nil {
		return nil, err
	}
	split, err := enc.SplitMessage()
	if err != nil {
		return nil, err
	}
	passphrase, err := sessionKey.Decrypt(split.GetBinaryDataPacket())
	if err != nil {
		return nil, fmt.Errorf("decrypt share passphrase: %w", err)
	}
	return passphrase.GetBinary(), nil
}

func (s *Service) AcceptInvitation(ctx context.Context, invitationID string) error {
	details, u, err := s.invitation(ctx, invitationID)
	if err != nil {
		return err
	}
	addr, sessionKey, err := invitationKey(u, details)
	if err != nil {
		return err
	}
	sig, err := addr.Write.SignDetachedWithContext(pgp.NewPlainMessage(sessionKey.Key), pgp.NewSigningContext(sigContextMember, true))
	if err != nil {
		return fmt.Errorf("sign session key: %w", err)
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/drive/v2/shares/invitations/" + invitationID + "/accept",
		Body: map[string]any{"SessionKeySignature": base64.StdEncoding.EncodeToString(sig.GetBinary())},
	}, nil)
}

func (s *Service) RejectInvitation(ctx context.Context, invitationID string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/drive/v2/shares/invitations/" + invitationID + "/reject",
	}, nil)
}

func inviteeAddr(u *keys.Unlocked, email string) (keys.Rings, bool) {
	for _, a := range u.Addresses {
		if strings.EqualFold(a.Email, email) {
			if rings, ok := u.AddrRings(a.ID); ok {
				return rings, true
			}
		}
	}
	rings, _, err := u.FirstAddr()
	if err != nil {
		return keys.Rings{}, false
	}
	return rings, true
}
