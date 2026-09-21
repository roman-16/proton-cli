package account

import (
	"context"

	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/fetch"
	"github.com/roman-16/proton-cli/internal/proton"
)

// What the account is protected with: the passwords it keeps, the second
// factors it answers with, and the ways back in if they are lost.
//
// It is one reading rather than six, because Proton answers nearly all of it in
// one settings record and because the parts decide each other: whether a
// recovery address may be used for recovery depends on there being one, and
// what a password change has to do depends on whether the account keeps two.

const settingsPath = "/core/v4/settings"

// Security is the state of the account's credentials.
type Security struct {
	TwoFactor       TwoFactor      `json:"two_factor"`
	TwoPasswordMode bool           `json:"two_password_mode"`
	RecoveryEmail   RecoveryEmail  `json:"recovery_email"`
	RecoveryPhone   RecoveryPhone  `json:"recovery_phone"`
	RecoveryPhrase  RecoveryPhrase `json:"recovery_phrase"`
	// Sentinel is Proton's heightened protection programme, which takes the
	// choice of recovery methods out of the account's hands.
	Sentinel bool `json:"sentinel"`
}

// TwoFactor is what the account is asked for at a sign-in beyond its password.
type TwoFactor struct {
	AuthenticatorApp bool `json:"authenticator_app"`
	// SecurityKeys is the keys registered, by the names they were given.
	SecurityKeys []string `json:"security_keys"`
}

// RecoveryEmail is the address Proton writes to if the account is lost.
type RecoveryEmail struct {
	Address  string `json:"address"`
	Verified bool   `json:"verified"`
	// AllowRecovery says the address may be used to reset the password, which is
	// a separate choice from having one: an unverified or excluded address still
	// receives the security notices Proton sends.
	AllowRecovery bool `json:"allow_recovery"`
}

// RecoveryPhone is the number Proton texts if the account is lost.
type RecoveryPhone struct {
	Number        string `json:"number"`
	Verified      bool   `json:"verified"`
	AllowRecovery bool   `json:"allow_recovery"`
}

// RecoveryPhrase is where the account stands with its twelve words.
type RecoveryPhrase struct {
	// Status is one of the four words below.
	Status string `json:"status"`
	// Changed is when the phrase now in force was set, as a Unix time.
	Changed int64 `json:"changed,omitempty"`
}

// What a recovery phrase can be. `outdated` is the one worth telling apart: the
// phrase somebody wrote down still opens the keys it was made for, and those
// are exactly the keys a password reset left behind, so it recovers data and
// cannot recover the account.
const (
	PhraseOn       = "on"
	PhraseOutdated = "outdated"
	PhraseNotSet   = "not set"
	PhraseOff      = "off"
)

// Proton's own numbering of the same thing (MNEMONIC_STATUS,
// packages/shared/lib/interfaces/User.ts).
const (
	mnemonicDisabled = 0
	mnemonicEnabled  = 1
	mnemonicOutdated = 2
	mnemonicSet      = 3
	mnemonicPrompt   = 4
)

// verifiedStatus is what Proton marks a recovery address with once its owner
// has proved it is theirs (SETTINGS_STATUS.VERIFIED).
const verifiedStatus = 1

// Two-factor methods, as the bitfield Proton keeps them in
// (SETTINGS_2FA_ENABLED, packages/shared/lib/interfaces/UserSettings.ts).
const (
	twoFactorTOTP  = 1
	twoFactorFIDO2 = 2
)

// settings is the part of Proton's settings record the credentials live in.
type settings struct {
	Email struct {
		Value  string
		Status int
		Reset  int
	}
	Phone struct {
		Value  string
		Status int
		Reset  int
	}
	Password     struct{ Mode int }
	Mnemonic     struct{ UpdateTime int64 }
	HighSecurity struct{ Value int }
	// Proton answers with a scalar TwoFactor beside this one, and an untagged
	// field would bind to that and fail to decode.
	TwoFactor struct {
		Enabled        int
		RegisteredKeys []struct{ Name string }
	} `json:"2FA"`
}

// Security reads the whole state of the account's credentials.
//
// The settings and the account itself are asked for together: the recovery
// phrase is the one part Proton keeps with the account rather than with the
// settings, and neither answer is needed to ask for the other.
func (s *Service) Security(ctx context.Context) (*Security, error) {
	var (
		env  struct{ UserSettings settings }
		user struct {
			User struct{ MnemonicStatus int }
		}
	)
	if err := fetch.Together(ctx,
		func(ctx context.Context) error {
			return s.C.Decode(ctx, proton.Request{Method: "GET", Path: settingsPath}, &env)
		},
		func(ctx context.Context) error {
			return s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/core/v4/users"}, &user)
		},
	); err != nil {
		return nil, err
	}
	set := env.UserSettings
	keyNames := make([]string, 0, len(set.TwoFactor.RegisteredKeys))
	for _, k := range set.TwoFactor.RegisteredKeys {
		keyNames = append(keyNames, k.Name)
	}
	return &Security{
		TwoFactor: TwoFactor{
			AuthenticatorApp: set.TwoFactor.Enabled&twoFactorTOTP != 0,
			SecurityKeys:     keyNames,
		},
		TwoPasswordMode: set.Password.Mode == keys.PasswordModeTwo,
		RecoveryEmail: RecoveryEmail{
			Address:       set.Email.Value,
			Verified:      set.Email.Status == verifiedStatus,
			AllowRecovery: set.Email.Reset == 1,
		},
		RecoveryPhone: RecoveryPhone{
			Number:        set.Phone.Value,
			Verified:      set.Phone.Status == verifiedStatus,
			AllowRecovery: set.Phone.Reset == 1,
		},
		RecoveryPhrase: phrase(user.User.MnemonicStatus, set.Mnemonic.UpdateTime),
		Sentinel:       set.HighSecurity.Value == 1,
	}, nil
}

// phrase is where the account stands with its phrase, read off the two numbers
// Proton keeps it in.
func phrase(status int, changed int64) RecoveryPhrase {
	p := RecoveryPhrase{Status: PhraseStatus(status), Changed: changed}
	if !p.Set() {
		p.Changed = 0
	}
	return p
}

// PhraseStatus names what Proton numbers.
//
// A number on a screen would leave the one distinction that matters - a phrase
// that opens the keys the account has now against one that opens the keys it
// was made for - to whoever remembered the table.
func PhraseStatus(status int) string {
	switch status {
	case mnemonicSet:
		return PhraseOn
	case mnemonicOutdated:
		return PhraseOutdated
	case mnemonicEnabled, mnemonicPrompt:
		return PhraseNotSet
	}
	return PhraseOff
}

// Set reports whether there is a phrase to recover with, in either of the two
// states one exists in.
func (p RecoveryPhrase) Set() bool { return p.Status == PhraseOn || p.Status == PhraseOutdated }
