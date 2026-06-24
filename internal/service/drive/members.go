package drive

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/crypto/keys"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

const sigContextInviter = "drive.share-member.inviter"

type Member struct {
	MemberID   string `json:"member_id"`
	Email      string `json:"email"`
	Role       string `json:"role"`
	CreateTime int64  `json:"create_time"`
}

type PendingInvite struct {
	InvitationID string `json:"invitation_id"`
	Email        string `json:"email"`
	Role         string `json:"role"`
	CreateTime   int64  `json:"create_time"`
}

func roleLabel(perms int) string {
	if perms&2 != 0 {
		return "editor"
	}
	return "viewer"
}

func (s *Service) ListMembers(ctx context.Context, shareID string) ([]Member, error) {
	var r struct {
		Members []struct {
			MemberID    string
			Email       string
			Permissions int
			CreateTime  int64
		}
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/drive/v2/shares/" + shareID + "/members"}, &r); err != nil {
		return nil, err
	}
	out := make([]Member, 0, len(r.Members))
	for _, m := range r.Members {
		out = append(out, Member{MemberID: m.MemberID, Email: m.Email, Role: roleLabel(m.Permissions), CreateTime: m.CreateTime})
	}
	return out, nil
}

func (s *Service) ListOutgoingInvites(ctx context.Context, shareID string) ([]PendingInvite, error) {
	var r struct {
		Invitations []struct {
			InvitationID string
			InviteeEmail string
			Permissions  int
			CreateTime   int64
		}
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/drive/v2/shares/" + shareID + "/invitations"}, &r); err != nil {
		return nil, err
	}
	out := make([]PendingInvite, 0, len(r.Invitations))
	for _, p := range r.Invitations {
		out = append(out, PendingInvite{InvitationID: p.InvitationID, Email: p.InviteeEmail, Role: roleLabel(p.Permissions), CreateTime: p.CreateTime})
	}
	return out, nil
}

func (s *Service) InviteMember(ctx context.Context, dc *Context, path, email string, canEdit bool, message string) error {
	res, err := s.ResolvePath(ctx, dc, path)
	if err != nil {
		return err
	}
	linkShareID, sk, err := s.shareForLink(ctx, dc, res)
	if err != nil {
		return err
	}
	inviteeKR, err := s.addressKeyRing(ctx, email)
	if err != nil {
		return err
	}
	keyPacket, err := inviteeKR.EncryptSessionKey(sk)
	if err != nil {
		return fmt.Errorf("encrypt session key for invitee: %w", err)
	}
	sig, err := dc.AddrKR.SignDetachedWithContext(pgp.NewPlainMessage(keyPacket), pgp.NewSigningContext(sigContextInviter, true))
	if err != nil {
		return fmt.Errorf("sign key packet: %w", err)
	}
	invitation := map[string]any{
		"InviteeEmail":       email,
		"InviterEmail":       dc.AddrEmail,
		"Permissions":        permFor(canEdit),
		"KeyPacket":          base64.StdEncoding.EncodeToString(keyPacket),
		"KeyPacketSignature": base64.StdEncoding.EncodeToString(sig.GetBinary()),
	}
	body := map[string]any{"Invitation": invitation}
	if message != "" || res.Name != "" {
		body["EmailDetails"] = map[string]any{"Message": message, "ItemName": res.Name}
	}
	return s.C.Decode(ctx, proton.Request{Method: "POST", Path: "/drive/v2/shares/" + linkShareID + "/invitations", Body: body}, nil)
}

func (s *Service) RemoveMember(ctx context.Context, dc *Context, path, email string) error {
	res, err := s.ResolvePath(ctx, dc, path)
	if err != nil {
		return err
	}
	link, err := s.getLink(ctx, res.ShareID, res.LinkID)
	if err != nil {
		return err
	}
	for _, sid := range link.ShareIDs {
		if sid == dc.ShareID {
			continue
		}
		members, err := s.ListMembers(ctx, sid)
		if err == nil {
			for _, m := range members {
				if strings.EqualFold(m.Email, email) {
					return s.C.Decode(ctx, proton.Request{
						Method: "DELETE", Path: fmt.Sprintf("/drive/v2/shares/%s/members/%s", sid, m.MemberID),
					}, nil)
				}
			}
		}
		invites, err := s.ListOutgoingInvites(ctx, sid)
		if err == nil {
			for _, p := range invites {
				if strings.EqualFold(p.Email, email) {
					return s.C.Decode(ctx, proton.Request{
						Method: "DELETE", Path: fmt.Sprintf("/drive/v2/shares/%s/invitations/%s", sid, p.InvitationID),
					}, nil)
				}
			}
		}
	}
	return &errs.NotFound{Kind: "member", Ref: email}
}

func (s *Service) addressKeyRing(ctx context.Context, email string) (*pgp.KeyRing, error) {
	var r struct {
		Address struct {
			Keys []struct{ PublicKey string }
		}
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/core/v4/keys/all", Query: keys.Query("Email", email)}, &r); err != nil {
		return nil, err
	}
	if len(r.Address.Keys) == 0 {
		return nil, fmt.Errorf("%s is not a Proton user (external invitations are not supported)", email)
	}
	key, err := pgp.NewKeyFromArmored(r.Address.Keys[0].PublicKey)
	if err != nil {
		return nil, err
	}
	return pgp.NewKeyRing(key)
}
