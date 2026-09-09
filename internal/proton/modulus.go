package proton

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/ProtonMail/go-srp"
)

// The verifier Proton keeps in place of a password.
//
// Three things here put a password on something Proton will later check without
// ever holding the password: a message sent to somebody outside Proton, a public
// Drive link, and the extra password protecting Pass. All three write the same
// SRP credential - a group Proton signed, a salt made on this machine, and the
// verifier the two derive from the password - and Proton stores that instead of
// the secret.
//
// The group comes from Proton because its signature is what makes it usable:
// go-srp checks one before it will derive anything. The salt is ours and is
// fresh every time, so two things given the same password share no verifier.

const modulusPath = "/core/v4/auth/modulus"

const (
	// srpBits is the group size every Proton client works at.
	srpBits = 2048
	// srpSaltBytes is ten, not sixteen: hashing appends a six-byte suffix of its
	// own to fill bcrypt's salt slot.
	srpSaltBytes = 10
	// srpVerifierVersion is the hashing a verifier written today is read back
	// under. From version 3 the username is no longer part of it, which is what
	// lets a secret belonging to no address be proved at all.
	srpVerifierVersion = 4
)

// Modulus is the SRP group a verifier is built against, as Proton signed it.
type Modulus struct {
	ID string
	// Value is the signed armoured text, whose signature is checked before it is
	// used.
	Value string
}

// FetchModulus asks Proton for a group to derive verifiers against. One answer
// serves as many as are wanted: a send builds one verifier per recipient.
func FetchModulus(ctx context.Context, c Doer) (Modulus, error) {
	var r struct{ Modulus, ModulusID string }
	if err := c.Decode(ctx, Request{Method: "GET", Path: modulusPath}, &r); err != nil {
		return Modulus{}, fmt.Errorf("ask Proton for an SRP modulus: %w", err)
	}
	return Modulus{ID: r.ModulusID, Value: r.Modulus}, nil
}

// Verifier is a password as Proton stores it: the parameters it was derived
// under, and the derivation. A password can be checked against it and cannot be
// read out of it.
type Verifier struct {
	ModulusID string
	// Salt is base64, and is handed back when the password is proved.
	Salt string
	// Value is the verifier itself, base64.
	Value   string
	Version int
}

// Verifier derives one from a password, under a salt made here.
func (m Modulus) Verifier(password []byte) (Verifier, error) {
	salt := make([]byte, srpSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return Verifier{}, fmt.Errorf("SRP salt: %w", err)
	}
	auth, err := srp.NewAuthForVerifier(password, m.Value, salt)
	if err != nil {
		return Verifier{}, fmt.Errorf("SRP setup: %w", err)
	}
	value, err := auth.GenerateVerifier(srpBits)
	if err != nil {
		return Verifier{}, fmt.Errorf("SRP verifier: %w", err)
	}
	return Verifier{
		ModulusID: m.ID,
		Salt:      base64.StdEncoding.EncodeToString(salt),
		Value:     base64.StdEncoding.EncodeToString(value),
		Version:   srpVerifierVersion,
	}, nil
}
