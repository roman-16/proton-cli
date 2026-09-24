package keys

import (
	"context"

	"github.com/roman-16/proton-cli/internal/proton"
)

// Whether mail arriving at an address is end-to-end encrypted.
//
// It is a property of the address and it is published: what says so is the key
// list, one flag bit per key, so changing it means signing the list again. That
// is the whole of what happens here, and it happens for one reason - a
// forwarding to somewhere outside Proton has to hand a message to somebody who
// holds no Proton key, and cannot while the address's mail is sealed to keys
// only this account has.

// SetEncryption publishes whether mail arriving at an address is end-to-end
// encrypted.
//
// The list that says so is the one Proton already serves, with the flag flipped
// and nothing else touched, and it is checked against the keys the account holds
// before it is signed: a list that does not describe the address is not one to
// re-publish under a fresh signature.
func (u *Unlocked) SetEncryption(ctx context.Context, c proton.Doer, addr Address, on bool) error {
	signers, err := u.writers(addr)
	if err != nil {
		return err
	}
	list, err := u.relisted(addr, noKeyList(addr, "changing its encryption"),
		func(data string) (string, error) { return withEncryption(data, on) }, signers)
	if err != nil {
		return err
	}
	return c.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/core/v4/addresses/" + addr.ID + "/encryption",
		Body: map[string]any{
			"Encrypt": boolBit(on),
			// Whether a signature is expected on what arrives is a second thing
			// the same request carries, and no business of this one: it goes back
			// as the address already has it.
			"Sign":          boolBit(ExpectsSigned(addr.Flags)),
			"SignedKeyList": list.body(),
		},
	}, nil)
}

func boolBit(b bool) int {
	if b {
		return 1
	}
	return 0
}
