package mail

import (
	"context"
	"fmt"

	"github.com/roman-16/proton-cli/internal/proton"
)

// Send stores Content as a draft and delivers it. If anything after draft
// creation fails, the draft is discarded, so an aborted send leaves nothing
// behind in Drafts.
func (s *Service) Send(ctx context.Context, c Content, del Delivery) (string, error) {
	if !c.HasRecipients() {
		return "", fmt.Errorf("at least one recipient is required")
	}
	d, err := s.DraftCreate(ctx, c)
	if err != nil {
		return "", err
	}
	if err := s.SendDraft(ctx, d, del); err != nil {
		s.discard(ctx, d.ID)
		return "", err
	}
	return d.ID, nil
}

// SendDraft delivers an existing draft. Recipients are classified once, then the
// body is packaged per scheme: internal, encrypted-for-outside and cleartext
// recipients share one symmetrically encrypted body, while PGP/MIME and
// PGP-Inline recipients each get one encrypted to their own key.
func (s *Service) SendDraft(ctx context.Context, d *Draft, del Delivery) error {
	c := d.Content
	if !c.HasRecipients() {
		return fmt.Errorf("draft %s has no recipients", d.ID)
	}
	if c.From == nil {
		return fmt.Errorf("draft %s has no sending address", d.ID)
	}

	plans, needBody, hasEO, err := s.planRecipients(ctx, c, del)
	if err != nil {
		return err
	}

	var eoModulus proton.Modulus
	if hasEO {
		if eoModulus, err = proton.FetchModulus(ctx, s.C); err != nil {
			return err
		}
	}

	var packages []map[string]any
	if needBody {
		pkgs, err := s.buildBodyPackages(c, del, d.Attachments, plans, eoModulus)
		if err != nil {
			return err
		}
		packages = append(packages, pkgs...)
	}
	if pkg, ok, err := s.buildPGPMIMEPackage(ctx, c, d.Attachments, plans); err != nil {
		return err
	} else if ok {
		packages = append(packages, pkg)
	}
	if pkg, ok, err := s.buildInlinePackage(c, d.Attachments, plans); err != nil {
		return err
	} else if ok {
		packages = append(packages, pkg)
	}

	body := map[string]any{"ExpirationTime": nil, "AutoSaveContacts": 0, "Packages": packages}
	if del.At > 0 {
		body["DeliveryTime"] = del.At
	}
	expiresIn := del.ExpiresInSeconds
	if expiresIn == 0 && hasEO {
		expiresIn = defaultEOExpirationSeconds
	}
	if expiresIn > 0 {
		body["ExpiresIn"] = expiresIn
	}
	resp, err := s.C.Do(ctx, proton.Request{Method: "POST", Path: "/mail/v4/messages/" + d.ID, Body: body})
	if err != nil {
		return err
	}
	if resp.Status >= 400 {
		return fmt.Errorf("send failed: %s", string(resp.Body))
	}
	return nil
}
