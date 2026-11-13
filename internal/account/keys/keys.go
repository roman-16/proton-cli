package keys

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ProtonMail/go-srp"
	pgp "github.com/ProtonMail/gopenpgp/v3/crypto"
	"github.com/roman-16/proton-cli/internal/account/localkey"
	pgphelper "github.com/roman-16/proton-cli/internal/crypto/pgp"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/fetch"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/skip"
)

type Rings struct {
	Read  *pgp.KeyRing
	Write *pgp.KeyRing
}

type Unlocked struct {
	UserKR    *pgp.KeyRing
	AddrKRs   map[string]Rings
	Addresses []Address
	UserKeys  []Key
	PaidMail  bool
	PaidMeet  bool
	Username  string
	Now       func() time.Time

	keyPass        []byte
	recoveryPhrase bool
	private        bool
	role           int
	lockedID       map[uint64]bool
}

type LockedKey struct {
	Key       Key
	AddressID string
	Email     string
}

func (u *Unlocked) Locked() []LockedKey {
	out := u.LockedAccountKeys()
	for _, a := range u.Addresses {
		for _, k := range a.Keys {
			if k.Active == 0 {
				out = append(out, LockedKey{Key: k, AddressID: a.ID, Email: a.Email})
			}
		}
	}
	return out
}

func (u *Unlocked) LockedAccountKeys() []LockedKey {
	var out []LockedKey
	for _, k := range u.UserKeys {
		if k.Active == 0 {
			out = append(out, LockedKey{Key: k})
		}
	}
	return out
}

func (u *Unlocked) HasRecoveryPhrase() bool { return u.recoveryPhrase }

func (u *Unlocked) SealedToLocked(msg *pgp.PGPMessage) bool {
	if msg == nil || len(u.lockedID) == 0 {
		return false
	}
	ids, _ := msg.EncryptionKeyIDs()
	for _, id := range ids {
		if u.lockedID[id] {
			return true
		}
	}
	return false
}

func (u *Unlocked) Shut(msg *pgp.PGPMessage) skip.Reason {
	if u.SealedToLocked(msg) {
		return skip.Locked
	}
	return skip.Undecryptable
}

func (u *Unlocked) Explain(err error, kind string, msg *pgp.PGPMessage) error {
	if err == nil || !u.SealedToLocked(msg) {
		return err
	}
	return LockedData(kind)
}

func LockedData(kind string) error {
	return errs.Problemf("This %s is sealed to a key that a password reset locked.", kind).
		Hint("proton account keys reactivate")
}

type Get func(context.Context) (*Unlocked, error)

func (get Get) Alongside(ctx context.Context, request func(context.Context) error) (*Unlocked, error) {
	var u *Unlocked
	err := fetch.Together(ctx,
		request,
		func(ctx context.Context) error {
			var err error
			u, err = get(ctx)
			return err
		},
	)
	return u, err
}

type User struct {
	ID      string
	Name    string
	Email   string
	Keys    []Key
	Private int
	// Role is the part the account plays in an organization, as Proton numbers
	// it. Mirrors USER_ROLES in WebClients (packages/shared/lib/constants.ts).
	Role       int
	Subscribed int
	// MnemonicStatus is where the account stands with its recovery phrase, as
	// Proton numbers it. Mirrors MNEMONIC_STATUS in WebClients
	// (packages/shared/lib/interfaces/User.ts).
	MnemonicStatus int
}

const adminRole = 2

const (
	mnemonicOutdated = 2
	mnemonicSet      = 3
)

func (u User) hasRecoveryPhrase() bool {
	return u.MnemonicStatus == mnemonicOutdated || u.MnemonicStatus == mnemonicSet
}

// productMail is the bit of Subscribed that says the plan covers Mail. Mirrors
// PRODUCT_BIT.MAIL in WebClients (packages/shared/lib/constants.ts).
const productMail = 1

const productMeet = 256

func (u User) paidMail() bool { return u.Subscribed&productMail != 0 }

func (u User) paidMeet() bool { return u.Subscribed&productMeet != 0 }

// Address mirrors the fields of /core/v4/addresses the CLI needs. Order,
// Status, Send and Receive drive sender selection: Proton only allows sending
// from an address that is active and both sendable and receivable, and presents
// them in Order.
type Address struct {
	ID            string
	Email         string
	DisplayName   string
	Signature     string
	Order         int
	Status        int
	Send          int
	Receive       int
	Type          int
	Keys          []Key
	Flags         int
	ProtonMX      bool
	SignedKeyList *SignedKeyList
}

// What an address's Flags say about it. Mirrors ADDRESS_FLAGS in WebClients
// (packages/shared/lib/constants.ts).
const (
	addressEncryptionOff   = 1 << 4
	addressExpectSignedOff = 1 << 5
)

func EndToEnd(flags int) bool { return flags&addressEncryptionOff == 0 }

func ExpectsSigned(flags int) bool { return flags&addressExpectSignedOff == 0 }

// SignedKeyList is the account's own statement of which keys an address holds:
// the list, and a signature over it by the address's primary key.
//
// Proton serves it beside the keys and Key Transparency audits it, so every
// client that adds a key to an address publishes the list again. Its contents
// are Data verbatim - a signature is over bytes, not over a reading of them.
type SignedKeyList struct {
	Data      string
	Signature string
}

func (a Address) CanSend() bool {
	return a.Status == 1 && a.Send == 1 && a.Receive == 1
}

type Key struct {
	ID             string
	PrivateKey     string
	Token          string
	Signature      string
	Primary        int
	Active         int
	Flags          int
	RecoverySecret string
}

type salt struct {
	ID      string
	KeySalt string
}

type KeyPassword func(twoPassword bool) (string, error)

var errWrongKeyPass = errors.New("the key password did not open the user keys")

func Unlock(ctx context.Context, c *proton.Client, ask KeyPassword) (*Unlocked, error) {
	var (
		sealed string
		user   *User
		addrs  []Address
	)
	if err := fetch.Together(ctx,
		func(ctx context.Context) error {
			sealed = sealedKeyPass(ctx, c)
			return nil
		},
		func(ctx context.Context) error {
			var err error
			if user, err = getUser(ctx, c); err != nil {
				return fmt.Errorf("get user: %w", err)
			}
			return nil
		},
		func(ctx context.Context) error {
			var err error
			if addrs, err = getAddresses(ctx, c); err != nil {
				return fmt.Errorf("get addresses: %w", err)
			}
			return nil
		},
	); err != nil {
		return nil, err
	}

	if sealed != "" {
		u, err := open(ctx, c.Now, user, addrs, sealed)
		if err == nil {
			return u, nil
		}
		var unopened *unopenable
		if errors.As(err, &unopened) {
			return nil, unopenedKeys(ctx, askTwoPassword(ctx, c), unopened)
		}
		if !errors.Is(err, errWrongKeyPass) {
			return nil, err
		}
		slog.Debug("keys: the sealed key password no longer opens the keys; deriving a fresh one")
	}

	d, err := derive(ctx, c, user, ask)
	if err != nil {
		return nil, err
	}
	u, err := open(ctx, c.Now, user, addrs, d.pass)
	if err != nil {
		if errors.Is(err, errWrongKeyPass) {
			return nil, wrongSecret(d.twoPassword)
		}
		var unopened *unopenable
		if errors.As(err, &unopened) {
			return nil, unopenedKeys(ctx, d.twoPassword, unopened)
		}
		return nil, err
	}
	wrapAndPersist(ctx, c, d.pass)
	return u, nil
}

// open unlocks the hierarchy with a passphrase: the user keys, then the address
// keys they hold the tokens for.
//
// Every address is tried and every failure is written to the log, because "none
// of them opened" is four different problems wearing one sentence - keys Proton
// has retired, tokens this user key cannot decrypt, armour that will not parse, a
// passphrase that is simply wrong - and they have four different answers. The
// shape of the account is written down beside them, since a hierarchy in trouble
// is a thing you diagnose by counting.
//
// Recorded and not counted. An address that will not open hides nothing by
// itself: a listing of messages, contacts or vaults is not one address shorter
// for it. What it hides is whatever was sealed to it, and that is counted where
// it fails to open - the item, the share, the card - so counting the address as
// well would say a listing is short when it is whole.
func open(ctx context.Context, now func() time.Time, user *User, addrs []Address, skp string) (*Unlocked, error) {
	userRings, err := unlockKeyRing(ctx, user.Keys, []byte(skp), nil)
	if err != nil {
		return nil, errWrongKeyPass
	}
	userKR := userRings.Read

	addrKRs := map[string]Rings{}
	active, addressKeys := 0, 0
	for _, a := range addrs {
		if a.Status != 0 {
			active++
		}
		addressKeys += len(a.Keys)
		rings, err := unlockKeyRing(ctx, a.Keys, []byte(skp), userKR)
		if err != nil {
			slog.DebugContext(ctx, "keys: an address did not open",
				"kind", string(skip.KindAddress), "reason", string(reasonFor(err)), "ref", a.ID)
			continue
		}
		addrKRs[a.ID] = rings
	}
	shape := &unopenable{
		addresses: len(addrs), active: active,
		userKeys: len(user.Keys), addressKeys: addressKeys,
	}
	slog.DebugContext(ctx, "keys: address keys opened",
		"addresses", shape.addresses, "addresses_active", shape.active,
		"user_keys", shape.userKeys, "address_keys", shape.addressKeys,
		"opened", len(addrKRs))
	if len(addrKRs) == 0 {
		return nil, shape
	}
	u := &Unlocked{
		UserKR: userKR, AddrKRs: addrKRs, Addresses: addrs, UserKeys: user.Keys,
		PaidMail: user.paidMail(), PaidMeet: user.paidMeet(), Username: user.Name, Now: now,
		recoveryPhrase: user.hasRecoveryPhrase(),
		private:        user.Private == 1, role: user.Role,
		keyPass: []byte(skp), lockedID: map[uint64]bool{},
	}
	locked := u.Locked()
	for _, k := range locked {
		u.noteLocked(ctx, k)
	}
	if len(locked) > 0 {
		slog.DebugContext(ctx, "keys: a password reset left keys locked", "keys_locked", len(locked))
	}
	return u, nil
}

// noteLocked remembers the PGP key IDs a locked key holds. The public half of a
// locked key reads without its passphrase, which is all that naming its IDs
// takes.
//
// Recorded and not counted: a locked key that will not even parse cannot be
// matched against a message, so a failure sealed to it reads as this build's
// rather than the reset's - which is the worse of the two mistakes, and the
// only one this line leaves anything to diagnose from.
func (u *Unlocked) noteLocked(ctx context.Context, k LockedKey) {
	key, err := pgp.NewKeyFromArmored(k.Key.PrivateKey)
	if err != nil {
		slog.DebugContext(ctx, "keys: a locked key could not be read",
			"kind", string(skip.KindKey), "reason", string(skip.Malformed), "ref", k.Key.ID, "error", err.Error())
		return
	}
	entity := key.GetEntity()
	u.lockedID[entity.PrimaryKey.KeyId] = true
	for _, sub := range entity.Subkeys {
		u.lockedID[sub.PublicKey.KeyId] = true
	}
}

type unopenable struct {
	addresses, active, userKeys, addressKeys int
}

func (e *unopenable) Error() string {
	return fmt.Sprintf("none of %d addresses' keys could be opened", e.addresses)
}

func unopenedKeys(ctx context.Context, twoPassword bool, shape *unopenable) error {
	slog.DebugContext(ctx, "keys: no address key opened",
		"addresses", shape.addresses, "addresses_active", shape.active,
		"user_keys", shape.userKeys, "address_keys", shape.addressKeys,
		"two_password", twoPassword)

	problem := errs.Problemf("None of your addresses' keys could be opened.").Exit(errs.ExitBug)
	if twoPassword {
		problem = problem.Hint("if you changed your second password recently, sign in again")
	}
	return problem
}

func askTwoPassword(ctx context.Context, c *proton.Client) bool {
	twoPassword, err := twoPasswordMode(ctx, c)
	if err != nil {
		slog.DebugContext(ctx, "keys: could not ask whether this account keeps two passwords",
			"error", err.Error())
		return false
	}
	return twoPassword
}

func reasonFor(err error) skip.Reason {
	switch {
	case errors.Is(err, errNoActiveKey):
		return skip.Inactive
	case errors.Is(err, errTokenUndecryptable):
		return skip.Untokenized
	case errors.Is(err, errKeyMalformed):
		return skip.Malformed
	}
	return skip.Unlockable
}

func wrongSecret(twoPassword bool) error {
	if twoPassword {
		return errs.Problemf("Incorrect second password.")
	}
	return errs.Problemf("Your password did not unlock your keys.")
}

// sealedKeyPass recovers the key password the session carries, if it carries one
// that still opens.
//
// The blob is wrapped with a key that lives server-side, so a revoked session, a
// sign-in over the top of another and a rotated client key all leave one that
// cannot be opened. None of those is a reason to fail: the blob is a cache of
// something derivable, and a cache that cannot be read is a miss.
func sealedKeyPass(ctx context.Context, c *proton.Client) string {
	blob := c.EncKeyBlob()
	if blob == "" {
		return ""
	}
	key, err := localkey.Get(ctx, c)
	if err != nil {
		slog.Debug("localkey: the session's client key could not be fetched; deriving the key password instead", "error", err)
		return ""
	}
	skp, err := localkey.Unwrap(blob, key)
	if err != nil {
		slog.Debug("localkey: the sealed key password could not be opened; deriving one instead", "error", err)
		return ""
	}
	return skp
}

type derived struct {
	pass        string
	twoPassword bool
}

// derive stretches the account's own secret into the passphrase its keys are
// locked with.
//
// What that secret is depends on the account, and Proton's settings are what say
// so - the salt and the mode are asked for together, because the answer to
// neither is needed to ask the other.
func derive(ctx context.Context, c *proton.Client, user *User, ask KeyPassword) (derived, error) {
	var (
		salts       []salt
		twoPassword bool
	)
	if err := fetch.Together(ctx,
		func(ctx context.Context) error {
			var err error
			if salts, err = getKeySalts(ctx, c); err != nil {
				return fmt.Errorf("get key salts: %w", err)
			}
			return nil
		},
		func(ctx context.Context) error {
			var err error
			if twoPassword, err = twoPasswordMode(ctx, c); err != nil {
				return fmt.Errorf("get password mode: %w", err)
			}
			return nil
		},
	); err != nil {
		return derived{}, err
	}

	secret, err := ask(twoPassword)
	if err != nil {
		return derived{}, err
	}
	pass, err := stretch(secret, saltOf(user.Keys, salts))
	if err != nil {
		return derived{}, fmt.Errorf("derive key password: %w", err)
	}
	return derived{pass: pass, twoPassword: twoPassword}, nil
}

func stretch(secret, keySalt string) (string, error) {
	if keySalt == "" {
		return secret, nil
	}
	raw, err := base64.StdEncoding.DecodeString(keySalt)
	if err != nil {
		return "", err
	}
	sp, err := srp.MailboxPassword([]byte(secret), raw)
	if err != nil {
		return "", err
	}
	return string(sp[len(sp)-31:]), nil
}

// saltOf returns the salt the primary user key was locked with.
//
// Proton salts every key of its own and locks the whole hierarchy with the one
// belonging to the primary, so taking whichever salt came first is a coin flip
// on an account that has more than one key. Mirrors getPrimaryKeyWithSalt in
// WebClients (packages/shared/lib/keys/keys.ts), including its tolerance of a
// key with no salt at all.
func saltOf(keys []Key, salts []salt) string {
	if len(keys) == 0 {
		return ""
	}
	for _, s := range salts {
		if s.ID == keys[0].ID {
			return s.KeySalt
		}
	}
	return ""
}

func Seal(ctx context.Context, c *proton.Client, skp string) error {
	key, err := localkey.Generate()
	if err != nil {
		return fmt.Errorf("generate a client key: %w", err)
	}
	if err := localkey.Put(ctx, c, key); err != nil {
		return fmt.Errorf("store the client key: %w", err)
	}
	blob, err := localkey.Wrap(skp, key)
	if err != nil {
		return fmt.Errorf("wrap the key password: %w", err)
	}
	c.SetEncKeyBlob(blob)
	return c.Persist(ctx)
}

func wrapAndPersist(ctx context.Context, c *proton.Client, skp string) {
	if err := Seal(ctx, c, skp); err != nil {
		// Recorded and not counted: this run is unlocked already, and the next
		// one asks for the password again.
		slog.DebugContext(ctx, "localkey: key password not persisted this run", "error", err)
	}
}

func (u *Unlocked) PrimaryUserKey() (*pgp.KeyRing, error) {
	return u.UserKR.FirstKey()
}

func (u *Unlocked) KeyPassword() string { return string(u.keyPass) }

func (u *Unlocked) PrimaryAddr() (Rings, Address, error) {
	for _, a := range u.Addresses {
		if rings, ok := u.AddrKRs[a.ID]; ok {
			e := a.Email
			if strings.HasSuffix(e, "@proton.me") || strings.HasSuffix(e, "@pm.me") || strings.HasSuffix(e, "@protonmail.com") {
				return rings, a, nil
			}
		}
	}
	return u.FirstAddr()
}

func (u *Unlocked) FirstAddr() (Rings, Address, error) {
	for _, a := range u.Addresses {
		if rings, ok := u.AddrKRs[a.ID]; ok {
			return rings, a, nil
		}
	}
	return Rings{}, Address{}, fmt.Errorf("no address key rings available")
}

func (u *Unlocked) AddrRings(addrID string) (Rings, bool) {
	rings, ok := u.AddrKRs[addrID]
	return rings, ok
}

func getKeySalts(ctx context.Context, c proton.Doer) ([]salt, error) {
	var r struct{ KeySalts []salt }
	if err := c.Decode(ctx, proton.Request{Method: "GET", Path: "/core/v4/keys/salts"}, &r); err != nil {
		return nil, err
	}
	return r.KeySalts, nil
}

// PasswordModeTwo is the account setting Proton calls two-password mode: the
// password that proves who the account is, and the one that opens its keys, are
// two different secrets. Mirrors PASSWORD_MODE.TWO_PASSWORD in WebClients
// (packages/shared/lib/constants.ts).
const PasswordModeTwo = 2

// twoPasswordMode reports whether the account keeps the password that proves who
// it is apart from the one that opens its keys.
//
// Proton calls it two-password mode and reports it with the account's settings,
// which is where its own clients read it from as well - the sign-in response
// carries the same fact, but a first unlock does not always follow a sign-in, and
// one fact wants one source.
func twoPasswordMode(ctx context.Context, c *proton.Client) (bool, error) {
	var r struct {
		UserSettings struct {
			Password struct{ Mode int }
		}
	}
	if err := c.Decode(ctx, proton.Request{Method: "GET", Path: "/core/v4/settings"}, &r); err != nil {
		return false, err
	}
	return r.UserSettings.Password.Mode == PasswordModeTwo, nil
}

func getUser(ctx context.Context, c *proton.Client) (*User, error) {
	var r struct{ User User }
	if err := c.Decode(ctx, proton.Request{Method: "GET", Path: "/core/v4/users"}, &r); err != nil {
		return nil, err
	}
	return &r.User, nil
}

func getAddresses(ctx context.Context, c proton.Doer) ([]Address, error) {
	var r struct{ Addresses []Address }
	if err := c.Decode(ctx, proton.Request{Method: "GET", Path: "/core/v4/addresses"}, &r); err != nil {
		return nil, err
	}
	return r.Addresses, nil
}

var (
	errNoActiveKey        = errors.New("no active key")
	errTokenUndecryptable = errors.New("token not decryptable by the user key")
	errKeyMalformed       = errors.New("key is not readable armour")
	errKeyLocked          = errors.New("key did not unlock with the passphrase")
)

// unlockKeyRing opens every key of one owner that will open.
//
// Recorded and not counted. A key that stays shut is the ordinary shape of an
// account that has changed its password or retired a key, and the ring still
// opens on the others; whatever was sealed to the shut key alone is counted at
// the moment it fails to open. The log keeps each key's own reason, which is
// what tells a retired key from a hierarchy in trouble.
func unlockKeyRing(ctx context.Context, keys []Key, passphrase []byte, userKR *pgp.KeyRing) (Rings, error) {
	kr, err := pgp.NewKeyRing(nil)
	if err != nil {
		return Rings{}, err
	}
	writing, err := pgp.NewKeyRing(nil)
	if err != nil {
		return Rings{}, err
	}
	last := errNoActiveKey
	shut := func(k Key, reason skip.Reason, cause error) {
		detail := ""
		if cause != nil {
			detail = cause.Error()
		}
		slog.DebugContext(ctx, "keys: a key did not open",
			"kind", string(skip.KindKey), "reason", string(reason), "ref", k.ID, "error", detail)
	}
	for _, k := range keys {
		if k.Active == 0 {
			shut(k, skip.Inactive, nil)
			continue
		}
		secret := passphrase
		if k.Token != "" && k.Signature != "" && userKR != nil {
			s, err := decryptToken(k.Token, k.Signature, userKR)
			if err != nil {
				last = errTokenUndecryptable
				shut(k, skip.Untokenized, err)
				continue
			}
			secret = s
		}
		locked, err := pgp.NewKeyFromArmored(k.PrivateKey)
		if err != nil {
			last = errKeyMalformed
			shut(k, skip.Malformed, err)
			continue
		}
		unlocked, err := locked.Unlock(secret)
		if err != nil {
			last = errKeyLocked
			shut(k, skip.Unlockable, err)
			continue
		}
		_ = kr.AddKey(unlocked)
		if k.Primary == 1 {
			_ = writing.AddKey(unlocked)
		}
	}
	if kr.CountEntities() == 0 {
		return Rings{}, fmt.Errorf("no keys could be unlocked: %w", last)
	}
	return Rings{Read: kr, Write: writing}, nil
}

func decryptToken(tokenArm, sigArm string, kr *pgp.KeyRing) ([]byte, error) {
	dec, err := pgphelper.Decrypt(kr, nil, []byte(tokenArm), pgp.Armor)
	if err != nil {
		return nil, err
	}
	verifier, err := pgphelper.PGP.Verify().VerificationKeys(kr).DisableVerifyTimeCheck().New()
	if err != nil {
		return nil, err
	}
	verified, err := verifier.VerifyDetached(dec.Bytes(), []byte(sigArm), pgp.Armor)
	if err != nil {
		return nil, err
	}
	if err := verified.SignatureError(); err != nil {
		return nil, err
	}
	return dec.Bytes(), nil
}

func Unreadable(email string) error {
	return errs.Problemf("Proton publishes a key for %s that this build cannot read.", email).
		Hint("run `proton report`").Exit(errs.ExitBug)
}

type publicKey struct {
	PublicKey string
	Primary   int
	Flags     int
}

func published(ctx context.Context, c proton.Doer, email string) ([]publicKey, error) {
	var r struct {
		Address struct{ Keys []publicKey }
	}
	q := proton.Query()
	q.Set("Email", email)
	// Internal only: an address outside Proton has no key here, and asking for
	// external ones would return records that cannot be encrypted to or that
	// nobody at Proton vouches for.
	q.Set("InternalOnly", "1")
	if err := c.Decode(ctx, proton.Request{
		Method: "GET", Path: "/core/v4/keys/all", Query: q,
	}, &r); err != nil {
		if proton.NoSuchAddress(err) {
			return nil, nil
		}
		return nil, err
	}
	return r.Address.Keys, nil
}

func Published(ctx context.Context, c proton.Doer, email string) (*pgp.KeyRing, error) {
	keys, err := published(ctx, c, email)
	if err != nil {
		return nil, err
	}
	kr, err := pgp.NewKeyRing(nil)
	if err != nil {
		return nil, err
	}
	for _, k := range keys {
		if k.Primary != 1 {
			continue
		}
		key, err := pgp.NewKeyFromArmored(k.PublicKey)
		if err != nil {
			// Recorded and not counted: the run stops here with a sentence naming
			// the address, so nothing is hidden and there is no short listing to
			// warn about. The log is what carries which key it was.
			slog.DebugContext(ctx, "keys: a published key is not readable armour",
				"signer", email, "error", err)
			return nil, Unreadable(email)
		}
		_ = kr.AddKey(key)
	}
	if len(kr.GetKeys()) == 0 {
		return nil, nil
	}
	return kr, nil
}

type Signers struct {
	Trusted     *pgp.KeyRing
	Compromised *pgp.KeyRing
}

func (s *Signers) Vouching(email string) (*pgp.KeyRing, error) {
	if s.Trusted == nil {
		return nil, errs.Problemf(
			"Every key Proton publishes for %s is marked compromised, so nothing it signed can be trusted.", email)
	}
	return s.Trusted, nil
}

func Signing(ctx context.Context, c proton.Doer, email string) (*Signers, error) {
	keys, err := published(ctx, c, email)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, nil
	}
	var trusted, compromised []*pgp.Key
	for _, k := range keys {
		key, err := pgp.NewKeyFromArmored(k.PublicKey)
		if err != nil {
			// Recorded and not counted: a signature that does not check out is
			// said on the screen by whoever asked, and this line is what tells a
			// key that was never there from one this build could not read.
			slog.DebugContext(ctx, "keys: a published key is not readable armour",
				"signer", email, "error", err)
			continue
		}
		if k.Flags&keyNotCompromised == 0 {
			compromised = append(compromised, key)
			continue
		}
		trusted = append(trusted, key)
	}
	if len(trusted) == 0 && len(compromised) == 0 {
		return nil, Unreadable(email)
	}
	s := &Signers{}
	if len(trusted) > 0 {
		s.Trusted = ringHolding(trusted)
	}
	if len(compromised) > 0 {
		s.Compromised = ringHolding(compromised)
	}
	return s, nil
}
