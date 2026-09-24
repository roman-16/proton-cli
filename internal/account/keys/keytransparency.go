package keys

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	pgperrors "github.com/ProtonMail/go-crypto/openpgp/errors"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/ProtonMail/gopenpgp/v2/constants"
	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/skip"
)

// Keeping an address's earlier key lists checkable once its primary changes.
//
// Key Transparency keeps every list an address has published, and the account's
// own audit checks each one against the key that signed it. A list signed by a
// key the address later deletes or marks compromised is one nothing can check
// any more, so when the primary changes Proton's clients sign the address's
// earlier lists again with the new primary, dated when the old one signed them
// (resignSKLWithPrimaryKey, packages/key-transparency/lib/shared). Only the lists
// published since the last epoch the account verified are asked for: the ones
// before it are already vouched for.
//
// It is best effort, as it is for Proton's clients. The change it follows has
// already been made, and a list that keeps its first signature stays checkable
// for as long as the key that made it lasts.

// verifiedEpochContext is the notation an account signs the epoch it verified
// under. Mirrors KT_VE_VERIFICATION_CONTEXT in WebClients
// (packages/key-transparency/lib/constants/constants.ts).
const verifiedEpochContext = "key-transparency.verified-epoch.1"

// pastList is one list an address published, as Key Transparency keeps it.
type pastList struct {
	Data      string
	Signature string
	Revision  int
}

// resignHistory signs the address's earlier lists again with current, wherever
// former signed them.
func (u *Unlocked) resignHistory(ctx context.Context, c proton.Doer, addr Address, former, current []*pgp.Key) {
	after := u.verifiedRevision(ctx, c, addr)
	var r struct{ SignedKeyLists []pastList }
	if err := c.Decode(ctx, proton.Request{
		Method: "GET", Path: "/core/v4/keys/signedkeylists",
		Query: proton.Query("AfterRevision", strconv.Itoa(after), "Identifier", canonicalEmail(addr.Email)),
	}, &r); err != nil {
		// Recorded and not counted: the change this follows has been made, and
		// the earlier lists keep the signatures they had.
		slog.DebugContext(ctx, "keys: the address's earlier key lists could not be fetched",
			"kind", string(skip.KindAddress), "reason", string(skip.Unreadable), "ref", addr.ID, "error", err.Error())
		return
	}
	signers := openpgp.EntityList{}
	for _, k := range former {
		signers = append(signers, k.GetEntity())
	}
	var resigned int
	for _, list := range r.SignedKeyLists {
		if list.Data == "" || list.Signature == "" {
			continue
		}
		signed, ok := signedAt(signers, list)
		if !ok || createdAfter(current, signed) {
			continue
		}
		signature, err := signKeyList(list.Data, current, func() time.Time { return signed })
		if err != nil {
			slog.DebugContext(ctx, "keys: an earlier key list could not be signed again",
				"kind", string(skip.KindAddress), "reason", string(skip.Unlockable), "ref", addr.ID, "error", err.Error())
			continue
		}
		if err := c.Decode(ctx, proton.Request{
			Method: "PUT", Path: "/core/v4/keys/signedkeylists/signature",
			Body: map[string]any{"AddressID": addr.ID, "Revision": list.Revision, "Signature": signature},
		}, nil); err != nil {
			slog.DebugContext(ctx, "keys: Proton refused an earlier key list's new signature",
				"kind", string(skip.KindAddress), "reason", string(skip.Unreadable), "ref", addr.ID, "error", err.Error())
			continue
		}
		resigned++
	}
	slog.DebugContext(ctx, "keys: signed earlier key lists again",
		"ref", addr.ID, "count", len(r.SignedKeyLists), "keys_resigned", resigned)
}

// verifiedRevision is the revision of the address's list the account last
// verified, or none when the account has verified nothing it can vouch for.
func (u *Unlocked) verifiedRevision(ctx context.Context, c proton.Doer, addr Address) int {
	var r struct{ Data, Signature string }
	if err := c.Decode(ctx, proton.Request{
		Method: "GET", Path: "/kt/v1/verifiedepoch/" + addr.ID,
	}, &r); err != nil {
		// Recorded and not counted: with no verified epoch every earlier list is
		// asked for, which is what Proton's clients ask for too.
		slog.DebugContext(ctx, "keys: no verified epoch for the address",
			"kind", string(skip.KindAddress), "reason", string(skip.Unreadable), "ref", addr.ID, "error", err.Error())
		return 0
	}
	signature, err := pgp.NewPGPSignatureFromArmored(r.Signature)
	if err == nil {
		err = u.UserKR.VerifyDetachedWithContext(pgp.NewPlainMessageFromString(r.Data), signature,
			pgp.GetUnixTime(), pgp.NewVerificationContext(verifiedEpochContext, true, 0))
	}
	var epoch struct{ Revision int }
	if err == nil {
		err = json.Unmarshal([]byte(r.Data), &epoch)
	}
	if err != nil {
		// Recorded and not counted: an epoch the account's own keys do not vouch
		// for is one a password reset left behind, and every earlier list is asked
		// for instead.
		slog.DebugContext(ctx, "keys: the address's verified epoch is not the account's",
			"kind", string(skip.KindAddress), "reason", string(skip.Undecryptable), "ref", addr.ID, "error", err.Error())
		return 0
	}
	return epoch.Revision
}

// signedAt is when one of signers signed a list, if one of them did.
//
// The check is of the signature and not of when it was made: a list from years
// ago was signed by a key valid then, which is all that is being asked.
func signedAt(signers openpgp.EntityList, list pastList) (time.Time, bool) {
	signature, err := pgp.NewPGPSignatureFromArmored(list.Signature)
	if err != nil {
		return time.Time{}, false
	}
	config := &packet.Config{
		Time:           func() time.Time { return time.Unix(0, 0) },
		KnownNotations: map[string]bool{constants.SignatureContextName: true},
	}
	sig, signer, err := openpgp.VerifyDetachedSignature(signers,
		strings.NewReader(list.Data), bytes.NewReader(signature.GetBinary()), config)
	if sig != nil && signer != nil &&
		(errors.Is(err, pgperrors.ErrSignatureExpired) || errors.Is(err, pgperrors.ErrKeyExpired)) {
		err = nil
	}
	if err != nil || sig == nil {
		return time.Time{}, false
	}
	return sig.CreationTime, true
}

// createdAfter reports whether any of the keys was made after a moment, which
// makes it unable to sign as of then.
func createdAfter(keys []*pgp.Key, moment time.Time) bool {
	for _, k := range keys {
		if k.GetEntity().PrimaryKey.CreationTime.After(moment) {
			return true
		}
	}
	return false
}

// canonicalEmail is an address as Key Transparency files it: the plus alias
// dropped, and dots, dashes and underscores taken out of the local part. Mirrors
// canonicalizeInternalEmail in WebClients (packages/shared/lib/helpers/email.ts).
func canonicalEmail(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return strings.ToLower(email)
	}
	local, domain := email[:at], email[at+1:]
	local, _, _ = strings.Cut(local, "+")
	local = strings.NewReplacer(".", "", "-", "", "_", "").Replace(local)
	return strings.ToLower(local) + "@" + strings.ToLower(domain)
}
