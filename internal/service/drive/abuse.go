package drive

import (
	"context"
	"encoding/base64"
	"fmt"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

// AbuseCategories are the kinds of abuse a report can be about, as Proton
// names them.
var AbuseCategories = []string{
	"spam", "copyright", "child-abuse", "non-consensual-intimate", "stolen-data", "malware", "other",
}

// Abuse is what a report says: the kind of abuse, and optionally where Proton
// can reach whoever reports it and what they have to add.
type Abuse struct {
	Category string
	Email    string
	Message  string
}

// ReportAbuse hands an item to Proton as abuse. The item is in something
// somebody shared with you, or behind a link.
func (s *Service) ReportAbuse(ctx context.Context, res *Resolved, a Abuse) error {
	return s.reportAbuse(ctx, res, "", a)
}

// ReportRevisionAbuse hands one version of a file to Proton as abuse.
func (s *Service) ReportRevisionAbuse(ctx context.Context, fr *FileRevision, a Abuse) error {
	return s.reportAbuse(ctx, fr.res, fr.ID, a)
}

// reportAbuse sends the report for an item of whichever tree it was resolved in.
//
// A report carries the key to the share the item is in, which is what lets
// Proton open it: the share's passphrase, and for a member the session key their
// membership opens it with, or for a link the link and its password.
func (s *Service) reportAbuse(ctx context.Context, res *Resolved, revisionID string, a Abuse) error {
	dc := res.dc
	body := abuseReport(dc.sharePassphrase, a)
	body["LinkID"] = res.LinkID
	if revisionID != "" {
		body["RevisionID"] = revisionID
	}
	if dc.Public() {
		shareID, err := s.linkShareID(ctx, dc)
		if err != nil {
			return err
		}
		body["ShareID"] = shareID
		body["ShareURL"] = dc.URL
		body["ShareURLPassword"] = dc.linkProof
		return s.C.Decode(ctx, proton.Request{Method: "POST", Path: "/drive/unauth/report/share", Body: body}, nil)
	}
	if dc.memberKeyPacket == "" {
		return errs.Problemf("Only something shared with you can be reported.")
	}
	sessionKey, err := memberSessionKey(dc.Addr.Read, dc.memberKeyPacket)
	if err != nil {
		return err
	}
	body["ShareID"] = dc.ShareID
	body["MemberSessionKey"] = base64.StdEncoding.EncodeToString(sessionKey.Key)
	return s.C.Decode(ctx, proton.Request{Method: "POST", Path: "/drive/report/share", Body: body}, nil)
}

// ReportInvitationAbuse hands what an invitation offers to Proton as abuse,
// without accepting it.
func (s *Service) ReportInvitationAbuse(ctx context.Context, invitationID string, a Abuse) error {
	r, u, err := s.invitation(ctx, invitationID)
	if err != nil {
		return err
	}
	_, sessionKey, err := invitationKey(u, r)
	if err != nil {
		return err
	}
	passphrase, err := invitationPassphrase(sessionKey, r)
	if err != nil {
		return err
	}
	body := abuseReport(passphrase, a)
	body["ShareID"] = r.Share.ShareID
	body["LinkID"] = r.Link.LinkID
	body["MemberSessionKey"] = base64.StdEncoding.EncodeToString(sessionKey.Key)
	return s.C.Decode(ctx, proton.Request{Method: "POST", Path: "/drive/report/share", Body: body}, nil)
}

// abuseReport is the body every report shares, with nothing yet naming what it
// is about.
func abuseReport(sharePassphrase []byte, a Abuse) map[string]any {
	return map[string]any{
		"AbuseCategory":    a.Category,
		"BonaFide":         true,
		"LinkID":           nil,
		"MemberSessionKey": nil,
		"ReporterEmail":    orNull(a.Email),
		"ReporterMessage":  orNull(a.Message),
		"RevisionID":       nil,
		"ShareURL":         nil,
		"ShareURLPassword": nil,
		"SharePassphrase":  base64.StdEncoding.EncodeToString(sharePassphrase),
	}
}

// orNull is a string field Proton takes as null when there is nothing in it.
func orNull(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// memberSessionKey opens the key packet a membership carries, with the address
// the share was shared to.
func memberSessionKey(addrKR *pgp.KeyRing, keyPacket string) (*pgp.SessionKey, error) {
	raw, err := base64.StdEncoding.DecodeString(keyPacket)
	if err != nil {
		return nil, fmt.Errorf("decode the membership's key packet: %w", err)
	}
	sessionKey, err := addrKR.DecryptSessionKey(raw)
	if err != nil {
		return nil, fmt.Errorf("open the membership's key packet: %w", err)
	}
	return sessionKey, nil
}

// linkShareID is the share a link belongs to, which the link's root names when
// it is read.
func (s *Service) linkShareID(ctx context.Context, dc *Context) (string, error) {
	details, err := s.linkDetails(ctx, dc, []string{dc.RootLinkID})
	if err != nil {
		return "", err
	}
	if len(details) == 0 || details[0].Sharing == nil || details[0].Sharing.ShareID == "" {
		return "", fmt.Errorf("the root of link %s names no share", dc.Token)
	}
	return details[0].Sharing.ShareID, nil
}
