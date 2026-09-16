package search

import (
	"context"
	"errors"
	"fmt"
	"os"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/crypto/aead"
)

// KeyRing is the account's own keys, named here so the layers above hand theirs
// over without this package reaching into the account for them.
type KeyRing = pgp.KeyRing

// signedFor binds the index key's signature to what the key is for, so a
// message this account signed for something else cannot be presented as the key
// that opens its mail. It is critical, so a verifier that does not understand
// the notation refuses rather than shrugging - the same shape Proton's own
// clients seal their search keys under.
const signedFor = "proton-cli.search.index"

// key is the symmetric key every record in this directory is sealed under,
// itself sealed to the account.
//
// One key covers the whole directory rather than one per app, because it is the
// account that the copy belongs to: a per-app key would be the same secret, kept
// in more places, for a separation nothing acts on.
func (s *Store) indexKey(ctx context.Context) ([]byte, error) {
	if s.key != nil {
		return s.key, nil
	}
	rings, err := s.keys(ctx)
	if err != nil {
		return nil, err
	}
	armored, err := os.ReadFile(s.keyPath())
	switch {
	case err == nil:
		key, err := unsealKey(string(armored), rings)
		if err != nil {
			return nil, err
		}
		s.key = key
		return key, nil
	case errors.Is(err, os.ErrNotExist):
		return s.newKey(rings)
	default:
		return nil, err
	}
}

// newKey makes the key this directory will be sealed under and writes it out.
func (s *Store) newKey(rings Keys) ([]byte, error) {
	key, err := aead.NewKey()
	if err != nil {
		return nil, err
	}
	armored, err := sealKey(key, rings)
	if err != nil {
		return nil, err
	}
	if err := s.ensureDir(); err != nil {
		return nil, err
	}
	if err := writeFile(s.keyPath(), []byte(armored)); err != nil {
		return nil, err
	}
	s.key = key
	return key, nil
}

func (s *Store) keys(ctx context.Context) (Keys, error) {
	if s.unlock == nil {
		return Keys{}, errors.New("search: no account keys available")
	}
	rings, err := s.unlock(ctx)
	if err != nil {
		return Keys{}, err
	}
	if rings.Open == nil || rings.Seal == nil {
		return Keys{}, errors.New("search: no account keys available")
	}
	return rings, nil
}

func sealKey(key []byte, rings Keys) (string, error) {
	msg, err := rings.Seal.EncryptWithContext(
		pgp.NewPlainMessage(key), rings.Seal, pgp.NewSigningContext(signedFor, true))
	if err != nil {
		return "", fmt.Errorf("search: seal index key: %w", err)
	}
	return msg.GetArmored()
}

// unsealKey opens the index key, checking that this account signed it for this
// purpose. A key that will not open is the end of the index: there is nothing to
// fall back to, and a new one would leave a log of records nothing reads.
func unsealKey(armored string, rings Keys) ([]byte, error) {
	msg, err := pgp.NewPGPMessageFromArmored(armored)
	if err != nil {
		return nil, fmt.Errorf("search: read index key: %w", err)
	}
	plain, err := rings.Open.DecryptWithContext(msg, rings.Open, pgp.GetUnixTime(),
		pgp.NewVerificationContext(signedFor, true, 0))
	if err != nil {
		return nil, fmt.Errorf("search: open index key: %w", err)
	}
	key := plain.GetBinary()
	if len(key) != aead.KeyLen {
		return nil, fmt.Errorf("search: index key is %d bytes, want %d", len(key), aead.KeyLen)
	}
	return key, nil
}

// aad names what a record is, so one cannot be opened as though it belonged to
// another app or another account. Both halves are already in the clear beside
// the log; sealing them in is what stops a record being moved between them.
func (s *Store) aad(app App) []byte {
	account := ""
	if s.account != nil {
		account = s.account()
	}
	return []byte(signedFor + "." + string(app) + "." + account)
}
