package app

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/ui"
)

// An account with both a code and a key is asked once, as the code prompt: an
// answer is a code, an empty answer is the key. Everything else about the choice
// follows from what the run already has - a code passed as a flag, or nobody
// there to ask.
func TestWhichSecondFactorAnswers(t *testing.T) {
	for _, tc := range []struct {
		name     string
		typed    string
		flag     string
		noInput  bool
		alsoTOTP bool
		wantKey  bool
		wantCode string
	}{
		{name: "only a key is registered", alsoTOTP: false, wantKey: true},
		{name: "a code was typed", alsoTOTP: true, typed: "123456\n", wantCode: "123456"},
		{name: "the prompt was answered with a bare newline", alsoTOTP: true, typed: "\n", wantKey: true},
		{name: "a code came from --totp", alsoTOTP: true, flag: "654321", wantCode: "654321"},
		{name: "nobody to ask", alsoTOTP: true, noInput: true, wantCode: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			creds := newCredentials(ui.New(ui.Options{
				Format: ui.FormatText, Err: &out, Out: &out,
				In: strings.NewReader(tc.typed), NoInput: tc.noInput,
			}), "")
			creds.flagTOTP = tc.flag

			if got := creds.prefersSecurityKey(tc.alsoTOTP); got != tc.wantKey {
				t.Fatalf("prefersSecurityKey = %v, want %v", got, tc.wantKey)
			}
			if tc.wantKey {
				return
			}
			// Whatever was typed at that one prompt is the code, and is not asked
			// for a second time.
			code, err := creds.TOTP()
			if tc.wantCode == "" {
				if err == nil {
					t.Errorf("a run with nothing to answer with returned %q", code)
				}
				return
			}
			if err != nil || code != tc.wantCode {
				t.Errorf("TOTP = %q, %v; want %q", code, err, tc.wantCode)
			}
		})
	}
}

// The key is only worth mentioning where it is a choice: an account that has
// nothing else says nothing, because there is nothing to choose.
func TestTheKeyIsOfferedOnlyWhereItIsAChoice(t *testing.T) {
	said := func(alsoTOTP bool, typed string) string {
		var out bytes.Buffer
		creds := newCredentials(ui.New(ui.Options{
			Format: ui.FormatText, Err: &out, Out: &out, In: strings.NewReader(typed),
		}), "")
		creds.prefersSecurityKey(alsoTOTP)
		return out.String()
	}
	if got := said(true, "\n"); !strings.Contains(got, "security key") {
		t.Errorf("prompt = %q, want the key offered as the other way in", got)
	}
	if got := said(false, ""); got != "" {
		t.Errorf("prompt = %q, want nothing said where there is no choice", got)
	}
}

// The extra password is the one secret this CLI takes rather than reads back, so
// a typed one is asked for twice and a mistyped one is refused. What arrived from
// a file was typed somewhere it can be read again, so it is taken as it is.
func TestChoosingAnExtraPasswordAsksTwice(t *testing.T) {
	for _, tc := range []struct {
		name    string
		typed   string
		noInput bool
		want    string
		refused string
	}{
		{name: "the same password twice", typed: "correct horse\ncorrect horse\n", want: "correct horse"},
		{name: "two different passwords", typed: "correct horse\ncorrect house\n", refused: "differ"},
		{name: "nothing to ask", noInput: true, refused: "required to protect Pass"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			creds := newCredentials(ui.New(ui.Options{
				Format: ui.FormatText, Err: &out, Out: &out,
				In: strings.NewReader(tc.typed), NoInput: tc.noInput,
			}), "")

			got, err := creds.ChooseExtraPassword()
			if tc.refused != "" {
				if err == nil {
					t.Fatalf("ChooseExtraPassword = %q, want a refusal", got)
				}
				if !strings.Contains(err.Error(), tc.refused) {
					t.Errorf("err = %v, want one saying %q", err, tc.refused)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("ChooseExtraPassword = %q, %v; want %q", got, err, tc.want)
			}
			// Asked once per run: the command reads it before the state check and
			// again when the change goes ahead.
			if again, err := creds.ChooseExtraPassword(); err != nil || again != tc.want {
				t.Errorf("asked a second time = %q, %v; want the first answer", again, err)
			}
		})
	}
}

// A password from a file is not confirmed, and the remedy a run without one is
// given names the flags the command actually offers.
func TestAnExtraPasswordFromAFlagIsTakenAsItIs(t *testing.T) {
	var out bytes.Buffer
	creds := newCredentials(ui.New(ui.Options{
		Format: ui.FormatText, Err: &out, Out: &out, In: strings.NewReader(""), NoInput: true,
	}), "")
	creds.stdinOwner = func(string) (io.Reader, error) { return strings.NewReader("correct horse\n"), nil }
	if err := creds.SupplyExtraPassword("", true); err != nil {
		t.Fatalf("SupplyExtraPassword: %v", err)
	}

	got, err := creds.ChooseExtraPassword()
	if err != nil || got != "correct horse" {
		t.Fatalf("ChooseExtraPassword = %q, %v; want the value on stdin", got, err)
	}

	declared := newCredentials(ui.New(ui.Options{
		Format: ui.FormatText, Err: &out, Out: &out, NoInput: true,
	}), "")
	if err := declared.SupplyExtraPassword("", false); err != nil {
		t.Fatalf("SupplyExtraPassword: %v", err)
	}
	_, err = declared.ChooseExtraPassword()
	var hinter errs.Hinter
	if !errors.As(err, &hinter) {
		t.Fatalf("err = %v, want one that says what to do next", err)
	}
	if !strings.Contains(strings.Join(hinter.Hints(), " "), "--extra-password-file") {
		t.Errorf("hints = %v, want the flag this command takes", hinter.Hints())
	}
}

// The one run that asks for two passwords labels them apart: the one from
// before the reset by Proton's name for it, and the account's own as the current
// one, so that neither prompt can be taken for the other.
func TestAPreviousPasswordRenamesTheCurrentOne(t *testing.T) {
	var out bytes.Buffer
	creds := newCredentials(ui.New(ui.Options{
		Format: ui.FormatText, Err: &out, Out: &out,
		In: strings.NewReader("old horse\nnew horse\n"),
	}), "alice@proton.me")

	previous, err := creds.PreviousPassword()
	if err != nil || previous != "old horse" {
		t.Fatalf("PreviousPassword = %q, %v; want the first line typed", previous, err)
	}
	current, err := creds.Password("reactivate your keys")
	if err != nil || current != "new horse" {
		t.Fatalf("Password = %q, %v; want the second line typed", current, err)
	}
	prompts := out.String()
	if !strings.Contains(prompts, "Previous password") || !strings.Contains(prompts, "Current password") {
		t.Errorf("prompts were:\n%s\nwant one for the previous and one for the current password", prompts)
	}
	if again, err := creds.PreviousPassword(); err != nil || again != previous {
		t.Errorf("asked a second time = %q, %v; want the first answer", again, err)
	}
}

// Alone, the account password keeps its ordinary name.
func TestThePasswordIsCurrentOnlyBesideAPreviousOne(t *testing.T) {
	var out bytes.Buffer
	creds := newCredentials(ui.New(ui.Options{
		Format: ui.FormatText, Err: &out, Out: &out, In: strings.NewReader("new horse\n"),
	}), "alice@proton.me")
	if _, err := creds.Password("delete a calendar"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Current password") {
		t.Errorf("prompt was %q; the account password is only current beside a previous one", out.String())
	}
}

// A run with nobody to ask is told the flags the command takes.
func TestARecoverySecretWithNobodyToAskNamesItsFlag(t *testing.T) {
	var out bytes.Buffer
	creds := newCredentials(ui.New(ui.Options{
		Format: ui.FormatText, Err: &out, Out: &out, NoInput: true,
	}), "alice@proton.me")
	if err := creds.SupplyPreviousPassword("", false); err != nil {
		t.Fatal(err)
	}
	if err := creds.SupplyRecoveryPhrase("", false); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		ask  func() (string, error)
		flag string
	}{
		{name: "previous password", ask: creds.PreviousPassword, flag: "--previous-password-file"},
		{name: "recovery phrase", ask: creds.RecoveryPhrase, flag: "--recovery-phrase-file"},
	} {
		_, err := tc.ask()
		var hinter errs.Hinter
		if !errors.As(err, &hinter) {
			t.Fatalf("%s: err = %v, want one that says what to do next", tc.name, err)
		}
		if !strings.Contains(strings.Join(hinter.Hints(), " "), tc.flag) {
			t.Errorf("%s: hints = %v, want %s", tc.name, hinter.Hints(), tc.flag)
		}
	}
}
