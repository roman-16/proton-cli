package keys

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"

	"github.com/roman-16/proton-cli/internal/crypto/bip39"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

// Setting the recovery phrase.
//
// The phrase is twelve words standing for sixteen random bytes, and what Proton
// keeps for them is a second copy of the user keys: the same keys, locked under
// those bytes instead of under the password, plus a verifier so the bytes can
// be proved at a sign-in nobody has the password for. Recovering is opening
// that copy, which reactivation already does from the other end.
//
// So a phrase is only ever as good as the keys it was made from. Set one today
// and it holds today's keys; a key made after it is not in the copy, which is
// what Proton means by an outdated phrase and why setting a new one replaces
// rather than adds.

// mnemonicEntropyBytes is how much randomness a phrase carries, and what makes
// it twelve words (generateMnemonicBase64RandomBytes,
// packages/shared/lib/mnemonic/bip39Wrapper.ts).
const mnemonicEntropyBytes = 16

// NewRecoveryPhrase makes a phrase, locks every open user key under it, and
// registers the lot with Proton. It answers with the words, which are the only
// copy: nothing here or at Proton can show them again.
//
// One endpoint takes all of it, whether the account has a phrase already or
// has never had one, and it answers at Proton's account host alone.
func (u *Unlocked) NewRecoveryPhrase(ctx context.Context, c proton.Doer) (string, error) {
	if err := u.canSetRecoveryPhrase(); err != nil {
		return "", err
	}
	entropy := make([]byte, mnemonicEntropyBytes)
	if _, err := rand.Read(entropy); err != nil {
		return "", fmt.Errorf("recovery phrase entropy: %w", err)
	}
	phrase, err := bip39.Words(entropy)
	if err != nil {
		return "", fmt.Errorf("write the recovery phrase: %w", err)
	}
	salt := make([]byte, keySaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("recovery phrase salt: %w", err)
	}
	// The secret the copy is locked with is the bytes as Proton writes them
	// down, not the words: base64 is what its own clients hash and what a
	// recovery reads back.
	secret := base64.StdEncoding.EncodeToString(entropy)
	encodedSalt := base64.StdEncoding.EncodeToString(salt)
	pass, err := stretch(secret, encodedSalt)
	if err != nil {
		return "", fmt.Errorf("derive the recovery phrase's key password: %w", err)
	}
	locked, err := u.relockedUserKeys([]byte(pass))
	if err != nil {
		return "", err
	}
	auth, err := proton.AuthFor(ctx, c, secret)
	if err != nil {
		return "", err
	}
	if err := c.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/core/v4/settings/mnemonic", AccountHost: true,
		Body: map[string]any{
			"MnemonicUserKeys": locked,
			"MnemonicSalt":     encodedSalt,
			"MnemonicAuth":     auth,
		},
	}, nil); err != nil {
		return "", err
	}
	slog.DebugContext(ctx, "keys: set a recovery phrase", "user_keys", len(locked))
	return phrase, nil
}

// canSetRecoveryPhrase refuses the accounts Proton offers no phrase to.
//
// A phrase opens the user keys, so an account whose keys its organization holds
// has nothing of its own to hand out, and an account whose address keys predate
// the move under the user key would get a phrase that opens the account and not
// its mail. Mirrors getIsMnemonicAvailable
// (packages/shared/lib/mnemonic/helpers.ts).
func (u *Unlocked) canSetRecoveryPhrase() error {
	switch {
	case !u.private:
		return errs.Problemf("This account's keys belong to its organization, so it has no recovery phrase of its own.").
			Hint("your administrator recovers the account for you")
	case !u.migratedAddressKeys():
		return errs.Problemf("This account's address keys are from before Proton moved them under the account key, " +
			"so a recovery phrase would not open your mail.").
			Hint("set one at https://account.proton.me/recovery, which brings the keys forward as it goes")
	}
	return nil
}
