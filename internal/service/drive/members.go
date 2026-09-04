package drive

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/skip"
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
	for _, sid := range res.Link.ShareIDs {
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

// addressKeyRing is the key Proton publishes for an address, refused with a
// sentence rather than an empty ring when there is none.
func (s *Service) addressKeyRing(ctx context.Context, email string) (*pgp.KeyRing, error) {
	kr, err := keys.Published(ctx, s.C, email)
	if err != nil {
		return nil, err
	}
	if kr == nil {
		return nil, errs.Problemf("%s is not a Proton address, so there is no key to share with.", email).
			Hint("a file can only be shared with another Proton account")
	}
	return kr, nil
}

// ── changing what somebody may do ──

// SetMemberRole changes an existing member's access, or a pending invitation's.
//
// Proton keeps the two apart - somebody who has accepted is a member, somebody
// who has not is an invitation - but the question a person asks is the same one
// either way, so this answers it against whichever holds the address.
//
// The permission bits are the same ones an invitation carries, so nothing has to
// be re-encrypted: the key packet the member already holds still opens the share,
// and only what they are allowed to do with it changes.
func (s *Service) SetMemberRole(ctx context.Context, dc *Context, path, email string, edit bool) error {
	res, err := s.ResolvePath(ctx, dc, path)
	if err != nil {
		return err
	}
	perms := permView
	if edit {
		perms = permEdit
	}
	for _, sid := range res.Link.ShareIDs {
		if sid == dc.ShareID {
			continue
		}
		if members, err := s.ListMembers(ctx, sid); err == nil {
			for _, m := range members {
				if !strings.EqualFold(m.Email, email) {
					continue
				}
				return s.C.Decode(ctx, proton.Request{
					Method: "PUT",
					Path:   fmt.Sprintf("/drive/v2/shares/%s/members/%s", sid, m.MemberID),
					Body:   map[string]any{"Permissions": perms},
				}, nil)
			}
		}
		if invites, err := s.ListOutgoingInvites(ctx, sid); err == nil {
			for _, p := range invites {
				if !strings.EqualFold(p.Email, email) {
					continue
				}
				return s.C.Decode(ctx, proton.Request{
					Method: "PUT",
					Path:   fmt.Sprintf("/drive/v2/shares/%s/invitations/%s", sid, p.InvitationID),
					Body:   map[string]any{"Permissions": perms},
				}, nil)
			}
		}
	}
	return &errs.NotFound{Kind: "member", Ref: email}
}

// ResendInvite asks Proton to send an invitation's email again.
//
// An invitation that was never answered is usually one that was never seen, and
// the alternative - cancel it and invite again - churns the invitation's identity
// for no reason.
func (s *Service) ResendInvite(ctx context.Context, dc *Context, path, email string) error {
	res, err := s.ResolvePath(ctx, dc, path)
	if err != nil {
		return err
	}
	for _, sid := range res.Link.ShareIDs {
		if sid == dc.ShareID {
			continue
		}
		invites, err := s.ListOutgoingInvites(ctx, sid)
		if err != nil {
			skip.Record(ctx, skip.KindShare, sid, skip.Unreadable, err)
			continue
		}
		for _, p := range invites {
			if !strings.EqualFold(p.Email, email) {
				continue
			}
			return s.C.Decode(ctx, proton.Request{
				Method: "POST",
				Path: fmt.Sprintf("/drive/v2/shares/%s/invitations/%s/sendemail",
					sid, p.InvitationID),
			}, nil)
		}
	}
	// Somebody who has already accepted has nothing to resend, and saying so is
	// more use than a generic miss.
	return errs.Problemf("no invitation to %s is waiting for an answer.", email).
		Hint("`drive items share get` shows who has accepted and who has not.").Exit(3)
}
