package drive

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/skip"
)

const (
	sigContextInviter = "drive.share-member.inviter"
	sigContextOutside = "drive.share-member.external-invitation"
	featureDisabled   = 2032
	outsideRegistered = 2
)

type Member struct {
	MemberID   string `json:"member_id"`
	Email      string `json:"email"`
	Role       string `json:"role"`
	CreateTime int64  `json:"create_time"`
}

// Stage is how far an offer nobody has accepted has got.
//
// An address outside Proton publishes no key, so there is nothing to seal the
// share to and the offer is held: Proton emails an invitation to create an
// account, and only once there is one can the key travel - handed over by the
// inviter, since nobody else holds it. That is two states a Proton address never
// passes through, and every command that acts on "whoever this address is" has
// to tell them apart.
type Stage string

const (
	// StageOffered is an invitation to a Proton address, waiting to be accepted.
	StageOffered Stage = "offered"
	// StageNoAccount is an offer to an address outside Proton, held until the
	// person creates an account.
	StageNoAccount Stage = "no-account"
	// StageReady is such an offer whose invitee has since created one, so the key
	// is the inviter's to hand over.
	StageReady Stage = "ready"
)

// PendingInvite is somebody who has been offered a share and has not taken it,
// whichever of the three stages they are at.
type PendingInvite struct {
	InvitationID string `json:"invitation_id"`
	Email        string `json:"email"`
	Role         string `json:"role"`
	CreateTime   int64  `json:"create_time"`
	Stage        Stage  `json:"stage"`
}

// outside reports whether the offer is held for somebody with no Proton account,
// which is what decides the endpoint every verb reaches it through.
func (p PendingInvite) outside() bool { return p.Stage != StageOffered }

func roleLabel(perms int) string {
	if perms&permWrite != 0 {
		return "editor"
	}
	return "viewer"
}

// grantedRole is what somebody else's grant lets you do, and nothing at all
// where there is no grant: a share of your own was not shared with you.
func grantedRole(permissions int) string {
	if permissions == 0 {
		return ""
	}
	return roleLabel(permissions)
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

// ListOutgoingInvites is everybody offered this share who has not taken it.
//
// Proton keeps the two kinds on endpoints of their own, but they are one
// question - who is waiting - and answering it in halves is how a listing comes
// to show fewer people than hold the file. Either half failing fails the whole,
// so the caller records one loss rather than presenting a part as the total.
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
		out = append(out, PendingInvite{
			InvitationID: p.InvitationID, Email: p.InviteeEmail,
			Role: roleLabel(p.Permissions), CreateTime: p.CreateTime, Stage: StageOffered,
		})
	}
	held, err := s.listOutsideInvites(ctx, shareID)
	if err != nil {
		return nil, err
	}
	return append(out, held...), nil
}

// outsideInvite is an offer to an address outside Proton as Proton keeps it. The
// signature is the inviter's own, over the address and the session key together,
// and is what confirming checks before the key goes anywhere.
type outsideInvite struct {
	ExternalInvitationID        string
	InviteeEmail                string
	Permissions                 int
	CreateTime                  int64
	State                       int
	ExternalInvitationSignature string
}

func (s *Service) listOutsideInvites(ctx context.Context, shareID string) ([]PendingInvite, error) {
	held, err := s.readOutsideInvites(ctx, shareID)
	if err != nil {
		return nil, err
	}
	out := make([]PendingInvite, 0, len(held))
	for _, p := range held {
		out = append(out, PendingInvite{
			InvitationID: p.ExternalInvitationID, Email: p.InviteeEmail,
			Role: roleLabel(p.Permissions), CreateTime: p.CreateTime, Stage: outsideStage(p.State),
		})
	}
	return out, nil
}

func (s *Service) readOutsideInvites(ctx context.Context, shareID string) ([]outsideInvite, error) {
	var r struct{ ExternalInvitations []outsideInvite }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/drive/v2/shares/" + shareID + "/external-invitations",
	}, &r); err != nil {
		return nil, err
	}
	return r.ExternalInvitations, nil
}

// outsideStage reads Proton's number, and calls a state this build has not been
// told about one that is not ready: confirming sends a key, and a stage nobody
// recognises is not grounds to send one.
func outsideStage(state int) Stage {
	if state == outsideRegistered {
		return StageReady
	}
	return StageNoAccount
}

// InviteMember offers a file or folder to somebody, and says which kind of offer
// it turned out to be.
//
// An address Proton publishes a key for is sent the share's session key sealed
// to it. One it publishes no key for cannot be sent anything, so what goes out is
// a signature over the address and the session key together - a commitment the
// key this offer is for is the key the person named gets, which is what makes it
// safe to hand over later against whatever key Proton publishes for them then.
func (s *Service) InviteMember(ctx context.Context, dc *Context, path, email string, canEdit bool, message string) (Stage, error) {
	res, err := s.ResolvePath(ctx, dc, path)
	if err != nil {
		return "", err
	}
	linkShareID, sk, err := s.shareForLink(ctx, dc, res)
	if err != nil {
		return "", err
	}
	inviteeKR, err := keys.Published(ctx, s.C, email)
	if err != nil {
		return "", err
	}
	if inviteeKR == nil {
		return StageNoAccount, s.inviteOutside(ctx, dc, linkShareID, email, res.Name, canEdit, message, sk)
	}
	return StageOffered, s.inviteProton(ctx, dc, linkShareID, email, res.Name, canEdit, message, sk, inviteeKR, "")
}

// inviteProton seals the session key to the invitee's published key.
//
// held names the offer this one replaces, when the invitee is somebody who had
// no account when they were first invited. Proton retires that offer against
// this one, so the person is invited once rather than twice.
func (s *Service) inviteProton(ctx context.Context, dc *Context, shareID, email, name string,
	canEdit bool, message string, sk *pgp.SessionKey, inviteeKR *pgp.KeyRing, held string) error {
	keyPacket, err := inviteeKR.EncryptSessionKey(sk)
	if err != nil {
		return fmt.Errorf("encrypt session key for invitee: %w", err)
	}
	sig, err := dc.Addr.Write.SignDetachedWithContext(pgp.NewPlainMessage(keyPacket), pgp.NewSigningContext(sigContextInviter, true))
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
	if held != "" {
		invitation["ExternalInvitationID"] = held
	}
	body := map[string]any{"Invitation": invitation}
	if details := emailDetails(message, name); details != nil {
		body["EmailDetails"] = details
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/drive/v2/shares/" + shareID + "/invitations", Body: body,
	}, nil)
}

// inviteOutside holds an offer for somebody with no Proton account.
//
// Nothing encrypted travels: there is no key to travel to. What Proton stores is
// the signature binding the address to this share's session key, and what it
// sends is an email inviting them to make an account.
func (s *Service) inviteOutside(ctx context.Context, dc *Context, shareID, email, name string,
	canEdit bool, message string, sk *pgp.SessionKey) error {
	sig, err := dc.Addr.Write.SignDetachedWithContext(
		outsideSigned(email, sk), pgp.NewSigningContext(sigContextOutside, true))
	if err != nil {
		return fmt.Errorf("sign the invitation: %w", err)
	}
	body := map[string]any{"ExternalInvitation": map[string]any{
		"InviterAddressID":            dc.AddrID,
		"InviteeEmail":                email,
		"Permissions":                 permFor(canEdit),
		"ExternalInvitationSignature": base64.StdEncoding.EncodeToString(sig.GetBinary()),
	}}
	if details := emailDetails(message, name); details != nil {
		body["EmailDetails"] = details
	}
	return outsideRefused(s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/drive/v2/shares/" + shareID + "/external-invitations", Body: body,
	}, nil))
}

// Proton keeps an offer to an address outside Proton on an endpoint of its own,
// so withdrawing one, changing what it grants and sending its email again are the
// same three verbs twice over. Each path is written out rather than assembled,
// because a path built behind a call is a request nothing can see this CLI is
// able to send.

func (s *Service) revokeInvite(ctx context.Context, shareID string, p PendingInvite) error {
	if p.outside() {
		return s.C.Decode(ctx, proton.Request{Method: "DELETE",
			Path: fmt.Sprintf("/drive/v2/shares/%s/external-invitations/%s", shareID, p.InvitationID)}, nil)
	}
	return s.C.Decode(ctx, proton.Request{Method: "DELETE",
		Path: fmt.Sprintf("/drive/v2/shares/%s/invitations/%s", shareID, p.InvitationID)}, nil)
}

func (s *Service) setInviteRole(ctx context.Context, shareID string, p PendingInvite, edit bool) error {
	body := map[string]any{"Permissions": permFor(edit)}
	if p.outside() {
		return s.C.Decode(ctx, proton.Request{Method: "PUT", Body: body,
			Path: fmt.Sprintf("/drive/v2/shares/%s/external-invitations/%s", shareID, p.InvitationID)}, nil)
	}
	return s.C.Decode(ctx, proton.Request{Method: "PUT", Body: body,
		Path: fmt.Sprintf("/drive/v2/shares/%s/invitations/%s", shareID, p.InvitationID)}, nil)
}

func (s *Service) resendInvite(ctx context.Context, shareID string, p PendingInvite) error {
	if p.outside() {
		return s.C.Decode(ctx, proton.Request{Method: "POST",
			Path: fmt.Sprintf("/drive/v2/shares/%s/external-invitations/%s/sendemail", shareID, p.InvitationID)}, nil)
	}
	return s.C.Decode(ctx, proton.Request{Method: "POST",
		Path: fmt.Sprintf("/drive/v2/shares/%s/invitations/%s/sendemail", shareID, p.InvitationID)}, nil)
}

// outsideSigned is what an offer to an address outside Proton commits to: the
// address and the session key, together, so neither can be changed under the
// other between the invitation and the key.
func outsideSigned(email string, sk *pgp.SessionKey) *pgp.PlainMessage {
	return pgp.NewPlainMessageFromString(email + "|" + base64.StdEncoding.EncodeToString(sk.Key))
}

// outsideRefused phrases the killswitch Proton keeps over this feature, which
// otherwise surfaces as a bare 422 and reads as a bug in this program.
func outsideRefused(err error) error {
	var api *proton.APIError
	if errors.As(err, &api) && api.Code == featureDisabled {
		return errs.Problemf("Proton has turned off invitations to addresses outside Proton.").
			Hint("`proton drive links create PATH` shares it by public link instead",
				"this is temporary - try the invitation again later").
			Exit(4)
	}
	return err
}

func emailDetails(message, name string) map[string]any {
	if message == "" && name == "" {
		return nil
	}
	return map[string]any{"Message": message, "ItemName": name}
}

// ConfirmInvite hands the key to somebody who was invited before they had a
// Proton account and has since made one.
//
// The signature checked here is this account's own, made when the offer went
// out. It says which address this share's session key was promised to, so
// checking it against the address Proton now reports is what stops a substituted
// invitee from being handed the key. It is required and it is contextual: a
// signature made for anything else vouches for nothing.
func (s *Service) ConfirmInvite(ctx context.Context, dc *Context, path, email string) error {
	res, err := s.ResolvePath(ctx, dc, path)
	if err != nil {
		return err
	}
	for _, sid := range res.Link.ShareIDs {
		if sid == dc.ShareID {
			continue
		}
		standing, err := s.readOutsideInvites(ctx, sid)
		if err != nil {
			skip.Record(ctx, skip.KindShare, sid, skip.Unreadable, err)
			continue
		}
		for _, held := range standing {
			if !strings.EqualFold(held.InviteeEmail, email) {
				continue
			}
			return s.confirm(ctx, dc, sid, res, held)
		}
	}
	return errs.Problemf("Nobody at %s was invited to %s before they had a Proton account.", email, path).
		Hint("`proton drive items share get " + path + "` shows who is waiting").Exit(3)
}

func (s *Service) confirm(ctx context.Context, dc *Context, shareID string, res *Resolved, held outsideInvite) error {
	if held.State != outsideRegistered {
		return errs.Problemf("%s has not created a Proton account yet.", held.InviteeEmail).
			Hint("Proton has emailed them an invitation to create one").Exit(3)
	}
	sk, err := s.shareSessionKey(ctx, dc, shareID, res)
	if err != nil {
		return err
	}
	raw, err := base64.StdEncoding.DecodeString(held.ExternalInvitationSignature)
	if err != nil {
		return fmt.Errorf("the invitation's signature is not base64: %w", err)
	}
	if err := dc.Addr.Write.VerifyDetachedWithContext(
		outsideSigned(held.InviteeEmail, sk), pgp.NewPGPSignature(raw), pgp.GetUnixTime(),
		pgp.NewVerificationContext(sigContextOutside, true, 0)); err != nil {
		return errs.Problemf(
			"The invitation to %s is not the one this account signed, so the key will not be handed over.",
			held.InviteeEmail).
			Hint("`proton drive items share remove` withdraws it; invite them again afterwards").Exit(3)
	}
	inviteeKR, err := keys.Published(ctx, s.C, held.InviteeEmail)
	if err != nil {
		return err
	}
	if inviteeKR == nil {
		return errs.Problemf("Proton publishes no key for %s yet, so there is nothing to hand the key to.",
			held.InviteeEmail).Exit(3)
	}
	return s.inviteProton(ctx, dc, shareID, held.InviteeEmail, res.Name,
		held.Permissions&permWrite != 0, "", sk, inviteeKR, held.ExternalInvitationID)
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
		members, invites, err := s.whoHolds(ctx, sid)
		if err != nil {
			skip.Record(ctx, skip.KindShare, sid, skip.Unreadable, err)
			continue
		}
		for _, m := range members {
			if strings.EqualFold(m.Email, email) {
				return s.C.Decode(ctx, proton.Request{
					Method: "DELETE", Path: fmt.Sprintf("/drive/v2/shares/%s/members/%s", sid, m.MemberID),
				}, nil)
			}
		}
		for _, p := range invites {
			if strings.EqualFold(p.Email, email) {
				return s.revokeInvite(ctx, sid, p)
			}
		}
	}
	return &errs.NotFound{Kind: "member", Ref: email}
}

// whoHolds is everybody with a claim on a share, accepted or not.
//
// The two halves are read together because every command that acts on an address
// has to look in both, and because a half that failed is a wrong answer rather
// than a short one: "nobody by that name" and "could not read who is there" lead
// a person in opposite directions.
func (s *Service) whoHolds(ctx context.Context, shareID string) ([]Member, []PendingInvite, error) {
	members, err := s.ListMembers(ctx, shareID)
	if err != nil {
		return nil, nil, err
	}
	invites, err := s.ListOutgoingInvites(ctx, shareID)
	if err != nil {
		return nil, nil, err
	}
	return members, invites, nil
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
// and only what they are allowed to do with it changes. An offer to somebody with
// no account carries no key at all, so the same is true of it.
func (s *Service) SetMemberRole(ctx context.Context, dc *Context, path, email string, edit bool) error {
	res, err := s.ResolvePath(ctx, dc, path)
	if err != nil {
		return err
	}
	for _, sid := range res.Link.ShareIDs {
		if sid == dc.ShareID {
			continue
		}
		members, invites, err := s.whoHolds(ctx, sid)
		if err != nil {
			skip.Record(ctx, skip.KindShare, sid, skip.Unreadable, err)
			continue
		}
		for _, m := range members {
			if !strings.EqualFold(m.Email, email) {
				continue
			}
			return s.C.Decode(ctx, proton.Request{
				Method: "PUT",
				Path:   fmt.Sprintf("/drive/v2/shares/%s/members/%s", sid, m.MemberID),
				Body:   map[string]any{"Permissions": permFor(edit)},
			}, nil)
		}
		for _, p := range invites {
			if !strings.EqualFold(p.Email, email) {
				continue
			}
			return s.setInviteRole(ctx, sid, p, edit)
		}
	}
	return &errs.NotFound{Kind: "member", Ref: email}
}

// ResendInvite asks Proton to send an invitation's email again.
//
// An invitation that was never answered is usually one that was never seen, and
// the alternative - cancel it and invite again - churns the invitation's identity
// for no reason. An offer to somebody with no Proton account is the email far
// more than it is anything else, so it is the one most worth sending twice.
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
			return s.resendInvite(ctx, sid, p)
		}
	}
	// Somebody who has already accepted has nothing to resend, and saying so is
	// more use than a generic miss.
	return errs.Problemf("no invitation to %s is waiting for an answer.", email).
		Hint("`proton drive items share get` shows who has accepted and who has not.").Exit(3)
}
