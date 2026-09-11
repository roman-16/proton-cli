package app

import (
	"io"
	"os"
	"strings"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/ui"
)

// passwordSource says where a secret may be read from. A file path is the
// channel secret delivery already speaks - systemd's LoadCredential, Kubernetes
// secrets and Docker secrets all hand one over.
type passwordSource struct {
	// file is read whole, with surrounding whitespace stripped.
	file string
	// stdin is standard input, claimed the moment --password-stdin is seen rather
	// than when a password turns out to be wanted: a `-` argument would otherwise
	// read the stream first and quietly send the password wherever it pointed.
	stdin io.Reader
	// declared says the running command offers the flags this secret arrives
	// through, which is what decides how a missing one is answered.
	declared bool
}

// hint says how to supply this secret without a terminal.
//
// Few commands declare the flags a secret arrives through, and any command at all
// can find the session's key password missing - so pointing one of those at a
// flag it would reject is worse than saying nothing. Signing in is what puts the
// key password back, so that is what it says instead.
func (s passwordSource) hint(flag string) []string {
	if s.declared {
		return []string{"pass " + flag + ", or run this in a terminal"}
	}
	return []string{"proton account login", "or run this in a terminal"}
}

// Credentials resolves the values that identify and unlock an account.
//
// It is the only thing in the CLI that may ask a person for one. Every other
// command stays non-interactive and fails with a message, so a scheduled job can
// never hang waiting on a question nobody will answer.
//
// Resolution, most specific first:
//
//	email     the account this profile is signed in as, else a prompt
//	password  --password-file, else --password-stdin, else a prompt
//	second    --second-password-file, else --second-password-stdin, else a prompt
//	extra     --extra-password-file, else --extra-password-stdin, else a prompt
//	previous  --previous-password-file, else --previous-password-stdin, else a prompt
//	phrase    --recovery-phrase-file, else --recovery-phrase-stdin, else a prompt
//	code      --totp, else a prompt
//
// Only `account login` names an account, and it does so with its own --user.
//
// Each value is asked for at most once per invocation: a command that needs the
// password twice does not ask twice.
type Credentials struct {
	ui *ui.UI

	// signedInAs is the account this profile already holds a session for, so
	// elevating that session never asks for an address the CLI can see.
	signedInAs string
	flagTOTP   string
	source     passwordSource
	// passphrase locks an exported file rather than the account, so it has a
	// source and a memory of its own.
	passphrase struct {
		source passwordSource
		value  string
		have   bool
	}
	// second is the account's second password, which two-password mode locks the
	// keys with. It is not the password that signs in, so it has a source and a
	// memory of its own too.
	second struct {
		source passwordSource
		value  string
		have   bool
	}
	// extra is the password Pass itself is protected with. It signs nothing in and
	// opens no keys, so it is a third secret with a third source and memory.
	extra struct {
		source passwordSource
		value  string
		have   bool
	}
	// previous is the password from before a password reset, which is what the
	// keys the reset locked are still locked with. It opens nothing the account
	// has now, so nothing else may be handed it by mistake.
	previous struct {
		source passwordSource
		value  string
		have   bool
	}
	// phrase is the account's recovery phrase, the other secret that opens keys a
	// reset locked.
	phrase struct {
		source passwordSource
		value  string
		have   bool
	}
	// stdinOwner is set once the App exists, so Supply can claim standard input.
	stdinOwner func(claim string) (io.Reader, error)

	// prompt is built once and reused, so every question shares one reader and a
	// value typed ahead of its question survives.
	prompt *ui.Prompter

	user, password, totp string
	haveUser             bool
	havePassword         bool
	haveTOTP             bool
}

// The prompt labels.
const (
	labelEmail    = "Email"
	labelPassword = "Password"
	// labelSecondPassword is the secret that opens the keys of an account in
	// two-password mode. Proton's own sign-in calls it that, and it is the name
	// somebody has stored it under.
	labelSecondPassword = "Second password"
	// labelExtraPassword is what protects Pass and nothing else. Every Proton
	// client calls it the extra password, so this one does too.
	labelExtraPassword = "Extra password"
	// labelConfirm asks for a secret a second time, for the one secret this CLI
	// chooses rather than reads back: a hidden prompt shows nothing, and a typo
	// in an extra password is not something anybody can put right afterwards.
	labelConfirm = "Confirm"
	// labelPreviousPassword is the password from before a reset, under the name
	// Proton's own recovery dialog gives it. labelCurrentPassword is what the
	// account password is called in the one run that asks for both, so that
	// neither prompt can be taken for the other.
	labelPreviousPassword = "Previous password"
	labelCurrentPassword  = "Current password"
	// labelRecoveryPhrase is the twelve words, under Proton's name for them.
	labelRecoveryPhrase = "Recovery phrase"
	labelTOTP           = "Two-factor code"
	// labelSecurityKeyPIN is the PIN of the key itself, which is set on the key
	// and known only to it. It is never the account password, and a key counts
	// wrong answers, so it is worth being unmistakable about which is wanted.
	labelSecurityKeyPIN = "Security key PIN"
	// labelPassphrase is the secret that locks a file rather than the account.
	// It is never the account password, and calling it something else is what
	// keeps somebody from typing one where the other was meant.
	labelPassphrase = "Passphrase"
)

func newCredentials(u *ui.UI, signedInAs string) *Credentials {
	return &Credentials{ui: u, signedInAs: signedInAs}
}

// SupplyPassphrase records where the passphrase that locks an exported file may
// be read from.
//
// It is kept apart from the account password because it is a different secret
// with a different life: one unlocks the account, the other unlocks one file,
// and a person who exports a backup chooses it themselves.
func (c *Credentials) SupplyPassphrase(file string, stdin bool) error {
	c.passphrase.source.file = file
	if !stdin {
		return nil
	}
	r, err := c.stdinOwner("--passphrase-stdin")
	if err != nil {
		return err
	}
	c.passphrase.source.stdin = r
	return nil
}

// Passphrase returns the passphrase for a file, asking for it if there is
// somebody to ask. reason completes "A passphrase is required to <reason>".
func (c *Credentials) Passphrase(reason string) (string, error) {
	if c.passphrase.have {
		return c.passphrase.value, nil
	}
	v, err := c.read(c.passphrase.source, labelPassphrase,
		errs.Problemf("A passphrase is required to %s.", reason).
			Hint("pass --passphrase-file, or run this in a terminal"))
	if err != nil {
		return "", err
	}
	c.passphrase.value, c.passphrase.have = v, true
	return v, nil
}

// Supply records the credentials a command was given. Only the commands that can
// be asked to re-authenticate declare them, so this is the one place standard
// input is claimed for a password.
func (c *Credentials) Supply(passwordFile string, passwordStdin bool, totp string) error {
	c.source.file = passwordFile
	c.source.declared = true
	c.flagTOTP = totp
	if !passwordStdin {
		return nil
	}
	r, err := c.stdinOwner("--password-stdin")
	if err != nil {
		return err
	}
	c.source.stdin = r
	return nil
}

// SupplySecondPassword records where the account's second password may be read
// from. Only signing in declares it: every other command proves who it is with
// the password, which is a different secret.
func (c *Credentials) SupplySecondPassword(file string, stdin bool) error {
	c.second.source.file = file
	c.second.source.declared = true
	if !stdin {
		return nil
	}
	r, err := c.stdinOwner("--second-password-stdin")
	if err != nil {
		return err
	}
	c.second.source.stdin = r
	return nil
}

// SupplyExtraPassword records where the password protecting Pass may be read
// from. Signing in declares it for the scope it buys, and turning it on or off
// declares it because it is the subject.
func (c *Credentials) SupplyExtraPassword(file string, stdin bool) error {
	c.extra.source.file = file
	c.extra.source.declared = true
	if !stdin {
		return nil
	}
	r, err := c.stdinOwner("--extra-password-stdin")
	if err != nil {
		return err
	}
	c.extra.source.stdin = r
	return nil
}

// ExtraPasswordOffered reports whether one was named at all, which is what
// decides whether signing in has anything to unlock Pass with.
func (c *Credentials) ExtraPasswordOffered() bool {
	return c.extra.source.file != "" || c.extra.source.stdin != nil
}

// ExtraPassword returns the password Pass is protected with, asking for it if
// there is somebody to ask.
func (c *Credentials) ExtraPassword() (string, error) {
	if c.extra.have {
		return c.extra.value, nil
	}
	v, err := c.read(c.extra.source, labelExtraPassword,
		errs.Problemf("Pass is protected with an extra password, and there is nobody here to ask.").
			Hint(extraPasswordHint(c.extra.source)...).Exit(2))
	if err != nil {
		return "", err
	}
	c.extra.value, c.extra.have = v, true
	return v, nil
}

// ChooseExtraPassword returns an extra password to protect Pass with, which is
// the one secret this CLI takes rather than reads back.
//
// A typed one is asked for twice. Nothing echoes it, nothing stores it, and an
// account whose Pass is protected with a password nobody knows cannot be put
// right - so a mistyped one is worth catching here rather than never.
func (c *Credentials) ChooseExtraPassword() (string, error) {
	if c.extra.have {
		return c.extra.value, nil
	}
	missing := errs.Problemf("An extra password is required to protect Pass with one.").
		Hint(extraPasswordHint(c.extra.source)...)
	typed := !c.ExtraPasswordOffered()
	if typed && c.ui.CanPrompt() {
		c.ui.Instruct("Choose at least eight characters and keep them safe: " +
			"without them nothing opens Pass, on any device.")
	}
	v, err := c.read(c.extra.source, labelExtraPassword, missing)
	if err != nil {
		return "", err
	}
	if typed {
		again, err := c.ask(labelConfirm, true, missing)
		if err != nil {
			return "", err
		}
		if again != v {
			return "", errs.Problemf("The two extra passwords differ.")
		}
	}
	c.extra.value, c.extra.have = v, true
	return v, nil
}

// extraPasswordHint says how to hand the extra password over without a terminal.
//
// A command that takes the flags names them. Any other Pass command can be the
// one that finds the session locked, and none of them offers a flag - so what
// that run needs is a sign-in that carries the password, which buys the scope for
// the life of the session.
func extraPasswordHint(src passwordSource) []string {
	if src.declared {
		return []string{"pass --extra-password-file, or run this in a terminal"}
	}
	return []string{"proton account login --extra-password-file FILE", "or run this in a terminal"}
}

// User returns the account email.
//
// Everything but signing in reaches this with a session already in hand, so the
// address is known and nothing is asked. `account login` passes its own --user
// rather than going through here, which is what keeps a stray address from
// reaching the SRP exchange that elevates a session.
func (c *Credentials) User() (string, error) {
	if c.haveUser {
		return c.user, nil
	}
	v := c.signedInAs
	if v == "" {
		var err error
		v, err = c.ask(labelEmail, false, errs.Problemf("An account email is required.").
			Hint("proton account login"))
		if err != nil {
			return "", err
		}
	}
	c.user, c.haveUser = v, true
	return v, nil
}

// Password returns the account password. reason completes the sentence "Your
// password is required to <reason>" reported when there is nobody to ask, so it
// reads as a clause: "sign in", "unlock your keys", "delete a calendar".
func (c *Credentials) Password(reason string) (string, error) {
	if c.havePassword {
		return c.password, nil
	}
	v, err := c.readPassword(reason)
	if err != nil {
		return "", err
	}
	c.password, c.havePassword = v, true
	return v, nil
}

func (c *Credentials) readPassword(reason string) (string, error) {
	label := labelPassword
	if c.previous.have {
		label = labelCurrentPassword
	}
	return c.read(c.source, label,
		errs.Problemf("Your password is required to %s.", reason).
			Hint(c.source.hint("--password-file")...))
}

// SupplyPreviousPassword records where the password from before a reset may be
// read from. Only reactivating keys declares it: nothing else the account does
// wants a password that opens nothing it has now.
func (c *Credentials) SupplyPreviousPassword(file string, stdin bool) error {
	c.previous.source.file = file
	c.previous.source.declared = true
	if !stdin {
		return nil
	}
	r, err := c.stdinOwner("--previous-password-stdin")
	if err != nil {
		return err
	}
	c.previous.source.stdin = r
	return nil
}

// PreviousPassword returns the password from before the reset, asking for it if
// there is somebody to ask.
func (c *Credentials) PreviousPassword() (string, error) {
	if c.previous.have {
		return c.previous.value, nil
	}
	v, err := c.read(c.previous.source, labelPreviousPassword,
		errs.Problemf("The password from before the reset is required to reactivate your keys.").
			Hint(c.previous.source.hint("--previous-password-file")...))
	if err != nil {
		return "", err
	}
	c.previous.value, c.previous.have = v, true
	return v, nil
}

// SupplyRecoveryPhrase records where the recovery phrase may be read from.
func (c *Credentials) SupplyRecoveryPhrase(file string, stdin bool) error {
	c.phrase.source.file = file
	c.phrase.source.declared = true
	if !stdin {
		return nil
	}
	r, err := c.stdinOwner("--recovery-phrase-stdin")
	if err != nil {
		return err
	}
	c.phrase.source.stdin = r
	return nil
}

// RecoveryPhrase returns the account's recovery phrase, asking for it if there
// is somebody to ask. It is read the way every other secret is, without echo:
// twelve words open the account as surely as a password does.
func (c *Credentials) RecoveryPhrase() (string, error) {
	if c.phrase.have {
		return c.phrase.value, nil
	}
	v, err := c.read(c.phrase.source, labelRecoveryPhrase,
		errs.Problemf("The recovery phrase is required to reactivate your keys with it.").
			Hint(c.phrase.source.hint("--recovery-phrase-file")...))
	if err != nil {
		return "", err
	}
	c.phrase.value, c.phrase.have = v, true
	return v, nil
}

// KeyPassword returns the secret the account's keys are locked with.
//
// Two-password mode keeps that secret apart from the one that signs in, so which
// of the two is asked for is the account's to decide - it is answered by Proton
// rather than by the command, and reaches here as twoPassword.
func (c *Credentials) KeyPassword(twoPassword bool) (string, error) {
	if twoPassword {
		return c.secondPassword()
	}
	return c.Password("unlock your keys")
}

// secondPassword returns the account's second password, asking for it if there
// is somebody to ask.
func (c *Credentials) secondPassword() (string, error) {
	if c.second.have {
		return c.second.value, nil
	}
	v, err := c.read(c.second.source, labelSecondPassword,
		errs.Problemf("This account uses two-password mode, so your second password is required.").
			Hint(c.second.source.hint("--second-password-file")...))
	if err != nil {
		return "", err
	}
	c.second.value, c.second.have = v, true
	return v, nil
}

// read takes a secret from wherever it was told to look, and asks for it only
// when it was told nothing. A file that exists but holds nothing is an error
// rather than an empty secret: somebody meant to put one there.
func (c *Credentials) read(src passwordSource, label string, missing error) (string, error) {
	if src.file != "" {
		b, err := os.ReadFile(src.file)
		if err != nil {
			return "", errs.Problemf("Could not read %s: %v", src.file, err)
		}
		if v := strings.TrimSpace(string(b)); v != "" {
			return v, nil
		}
		return "", errs.Problemf("%s is empty.", src.file)
	}
	if src.stdin != nil {
		b, err := io.ReadAll(src.stdin)
		if err != nil {
			return "", errs.Problemf("Could not read from stdin: %v", err)
		}
		if v := strings.TrimSpace(string(b)); v != "" {
			return v, nil
		}
		return "", errs.Problemf("Nothing arrived on stdin.")
	}
	return c.ask(label, true, missing)
}

// TOTP returns the current two-factor code.
//
// A code is single-use and expires within thirty seconds, which is why it is the
// one credential a flag may carry and the value a prompt helps with most.
func (c *Credentials) TOTP() (string, error) {
	if c.haveTOTP {
		return c.totp, nil
	}
	v := c.flagTOTP
	if v == "" {
		var err error
		v, err = c.ask(labelTOTP, false,
			errs.Problemf("This account has two-factor authentication enabled, so a code is required.").
				Hint("pass --totp, or run this in a terminal").Exit(2))
		if err != nil {
			return "", err
		}
	}
	c.totp, c.haveTOTP = v, true
	return v, nil
}

// SecurityKeyPIN returns the PIN that unlocks the key itself, asked for only
// when a key has said it will not answer without one.
func (c *Credentials) SecurityKeyPIN() (string, error) {
	return c.ask(labelSecurityKeyPIN, true,
		errs.Problemf("This security key asks for its PIN, and there is nobody here to ask.").
			Hint("run this in a terminal, or sign in with --totp instead").Exit(2))
}

// prefersSecurityKey reports whether the key answers this challenge, for an
// account where it is not the only thing that could.
//
// The question is put once, as the code prompt itself: an answer is a code, and
// an empty one is the key. A code that arrived as a flag has already answered it,
// and a run with nobody to ask is left to the code path, which is the one that
// can say what flag to pass.
func (c *Credentials) prefersSecurityKey(alsoTOTP bool) bool {
	switch {
	case !alsoTOTP:
		return true
	case c.haveTOTP || c.flagTOTP != "":
		return false
	case !c.ui.CanPrompt():
		return false
	}
	c.ui.Instruct("This account also has a security key. Press Enter to use it instead of a code.")
	code, err := c.prompter().Line(labelTOTP)
	if err != nil || code == "" {
		return true
	}
	c.totp, c.haveTOTP = code, true
	return false
}

// prompter is the one question block a sign-in asks, built on first use.
func (c *Credentials) prompter() *ui.Prompter {
	if c.prompt == nil {
		c.prompt = c.ui.Ask()
	}
	return c.prompt
}

// ask prompts, or reports missing when there is nobody to ask.
func (c *Credentials) ask(label string, secret bool, missing error) (string, error) {
	if !c.ui.CanPrompt() {
		return "", missing
	}
	p := c.prompter()
	var (
		v   string
		err error
	)
	if secret {
		v, err = p.Secret(label)
	} else {
		v, err = p.Line(label)
	}
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", missing
	}
	return v, nil
}
