package pgp

import (
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/roman-16/proton-cli/internal/errs"
)

// Proton's post-quantum keys are version 6 OpenPGP keys signed with ML-DSA-65
// and encrypted to ML-KEM-768. The OpenPGP libraries pinned here implement
// neither, and a parser meeting an algorithm it does not know skips the packet
// and then reports the armour as structurally wrong - so "Proton wrote this with
// something newer than this build" and "this is not a key at all" arrive as one
// error.
//
// Reading the algorithm out of the packet is what tells them apart, which is the
// difference between a sentence somebody can act on and an invitation to report
// a gap that is already written down.
const (
	algoMLDSA65Ed25519 packet.PublicKeyAlgorithm = 30
	algoMLKEM768X25519 packet.PublicKeyAlgorithm = 35
)

// PostQuantum reports whether an armoured key is one of them.
//
// A key that parses is not one, whatever it holds: these algorithms are exactly
// the ones that do not parse here.
func PostQuantum(armored string) bool {
	block, err := armor.Decode(strings.NewReader(armored))
	if err != nil {
		return false
	}
	first, err := packet.NewReader(block.Body).NextWithUnsupported()
	if err != nil {
		return false
	}
	unsupported, ok := first.(*packet.UnsupportedPacket)
	if !ok {
		return false
	}
	// The version and the algorithm are read off the packet before the parser
	// reaches the key material it cannot handle, so they are there to be asked
	// for on the packet it gave up on.
	var algo packet.PublicKeyAlgorithm
	switch key := unsupported.IncompletePacket.(type) {
	case *packet.PublicKey:
		algo = key.PubKeyAlgo
	case *packet.PrivateKey:
		algo = key.PubKeyAlgo
	default:
		return false
	}
	return algo == algoMLDSA65Ed25519 || algo == algoMLKEM768X25519
}

// NotSupported refuses what a post-quantum key blocks, phrased once for
// everything that runs into one.
//
// Nothing about the command changes the answer and there is no bug to report:
// the gap is proton's own, and it is written down where the other gaps are.
// whose names what carries the key, as the reader would name it - their account,
// somebody's address, a key they passed.
func NotSupported(whose string) error {
	return errs.Unsupportedf("%s uses post-quantum encryption, which is not supported yet.", whose).
		Hint("Proton's own apps can read these keys")
}
