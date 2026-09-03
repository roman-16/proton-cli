package drive

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
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
	Role         string `json:"role"`
	CreateTime   int64  `json:"create_time"`
}

// ListInvitations fetches per-invitation details because the listing endpoint
// returns only IDs (no inviter/role/timestamp).
func (s *Service) ListInvitations(ctx context.Context) ([]Invitation, error) {
	var out []Invitation
	anchor := ""
	for {
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
			return nil, err
		}
		for _, inv := range r.Invitations {
			d, err := s.invitationDetails(ctx, inv.InvitationID)
			if err != nil {
				skip.Record(ctx, skip.KindInvitation, inv.InvitationID, skip.Unreadable, err)
				continue
			}
			out = append(out, d)
		}
		if !r.More || r.AnchorID == "" {
			break
		}
		anchor = r.AnchorID
	}
	return out, nil
}

func (s *Service) invitationDetails(ctx context.Context, id string) (Invitation, error) {
	var r struct {
		Invitation struct {
			InvitationID string
			InviterEmail string
			InviteeEmail string
			Permissions  int
			CreateTime   int64
		}
		Share struct{ ShareID, VolumeID string }
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/drive/v2/shares/invitations/" + id}, &r); err != nil {
		return Invitation{}, err
	}
	return Invitation{
		InvitationID: r.Invitation.InvitationID,
		InviterEmail: r.Invitation.InviterEmail,
		InviteeEmail: r.Invitation.InviteeEmail,
		ShareID:      r.Share.ShareID,
		VolumeID:     r.Share.VolumeID,
		Role:         roleLabel(r.Invitation.Permissions),
		CreateTime:   r.Invitation.CreateTime,
	}, nil
}

func (s *Service) AcceptInvitation(ctx context.Context, invitationID string) error {
	var details struct {
		Invitation struct {
			KeyPacket    string
			InviteeEmail string
		}
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/drive/v2/shares/invitations/" + invitationID}, &details); err != nil {
		return fmt.Errorf("get invitation: %w", err)
	}
	u, err := s.keys(ctx)
	if err != nil {
		return err
	}
	addrKR := inviteeAddrKR(u, details.Invitation.InviteeEmail)
	if addrKR == nil {
		return fmt.Errorf("no usable address key for %s", details.Invitation.InviteeEmail)
	}
	keyPacket, err := base64.StdEncoding.DecodeString(details.Invitation.KeyPacket)
	if err != nil {
		return fmt.Errorf("decode key packet: %w", err)
	}
	sessionKey, err := addrKR.DecryptSessionKey(keyPacket)
	if err != nil {
		return fmt.Errorf("decrypt session key: %w", err)
	}
	sig, err := addrKR.SignDetachedWithContext(pgp.NewPlainMessage(sessionKey.Key), pgp.NewSigningContext(sigContextMember, true))
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

func inviteeAddrKR(u *keys.Unlocked, email string) *pgp.KeyRing {
	for _, a := range u.Addresses {
		if strings.EqualFold(a.Email, email) {
			if kr, ok := u.AddrKR(a.ID); ok {
				return kr
			}
		}
	}
	kr, _, err := u.FirstAddr()
	if err != nil {
		return nil
	}
	return kr
}
