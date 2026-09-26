package contacts

import (
	"strings"
	"testing"

	ctsvc "github.com/roman-16/proton-cli/internal/service/contacts"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/vcard"
)

func ptr[T any](v T) *T { return &v }

func TestDecideEmailPreferences(t *testing.T) {
	outside, provider, proton := mailsvc.Destination{}, mailsvc.Destination{ProviderKey: true}, mailsvc.Destination{Proton: true}
	pinned := ctsvc.EmailSettings{Keys: []string{"KEY"}}
	tests := []struct {
		name     string
		dest     mailsvc.Destination
		defaults mailsvc.SendDefaults
		current  ctsvc.EmailSettings
		change   emailChange
		want     ctsvc.EmailPreferences
		refusal  string
	}{
		{name: "a format for a Proton address", dest: proton,
			current: ctsvc.EmailSettings{EmailPreferences: ctsvc.EmailPreferences{Sign: ptr(false)}},
			change:  emailChange{format: formatPlainText},
			want:    ctsvc.EmailPreferences{PlainText: true}},
		{name: "signing a Proton address", dest: proton, change: emailChange{sign: settingOff},
			refusal: "is a Proton address"},
		{name: "a format for unsigned mail", dest: outside, change: emailChange{format: formatPlainText},
			want: ctsvc.EmailPreferences{PlainText: true}},
		{name: "signing under PGP/Inline fixes plain text", dest: outside,
			change: emailChange{sign: settingOn, scheme: vcard.SchemeInline},
			want:   ctsvc.EmailPreferences{Sign: ptr(true), Scheme: vcard.SchemeInline, PlainText: true}},
		{name: "signing under PGP/MIME fixes the written format", dest: outside,
			current: ctsvc.EmailSettings{EmailPreferences: ctsvc.EmailPreferences{PlainText: true}},
			change:  emailChange{sign: settingOn},
			want:    ctsvc.EmailPreferences{Sign: ptr(true)}},
		{name: "a format PGP/Inline cannot send", dest: outside,
			current: ctsvc.EmailSettings{EmailPreferences: ctsvc.EmailPreferences{Sign: ptr(true), Scheme: vcard.SchemeInline}},
			change:  emailChange{format: formatAutomatic}, refusal: "PGP/Inline"},
		{name: "a format PGP/MIME cannot send, signed by the account", dest: outside,
			defaults: mailsvc.SendDefaults{Sign: true},
			change:   emailChange{format: formatPlainText}, refusal: "PGP/MIME"},
		{name: "encrypted mail cannot go unsigned", dest: provider, change: emailChange{sign: settingOff},
			refusal: "always signed"},
		{name: "encrypted mail is recorded as signed", dest: outside, current: pinned,
			change: emailChange{scheme: vcard.SchemeInline},
			want:   ctsvc.EmailPreferences{Sign: ptr(true), Scheme: vcard.SchemeInline, PlainText: true}},
		{name: "turning encryption off hands signing back to the account", dest: outside,
			current: ctsvc.EmailSettings{Keys: []string{"KEY"}, EmailPreferences: ctsvc.EmailPreferences{Sign: ptr(true)}},
			change:  emailChange{encrypt: settingOff},
			want:    ctsvc.EmailPreferences{Encrypt: ptr(false)}},
		{name: "turning encryption off and signing in one go", dest: provider,
			change: emailChange{encrypt: settingOff, sign: settingOn},
			want:   ctsvc.EmailPreferences{Encrypt: ptr(false), Sign: ptr(true)}},
		{name: "encrypting with nothing to encrypt to", dest: outside, change: emailChange{encrypt: settingOn},
			refusal: "no key to encrypt"},
		{name: "a default takes the choice away", dest: outside,
			current: ctsvc.EmailSettings{EmailPreferences: ctsvc.EmailPreferences{Sign: ptr(false), Scheme: vcard.SchemeMIME}},
			change:  emailChange{sign: settingDefault, scheme: settingDefault},
			want:    ctsvc.EmailPreferences{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decideEmailPreferences("jane@example.com", tc.dest, tc.defaults, tc.current, tc.change)
			if tc.refusal != "" {
				if err == nil || !strings.Contains(err.Error(), tc.refusal) {
					t.Fatalf("err = %v, want a refusal saying %q", err, tc.refusal)
				}
				return
			}
			if err != nil {
				t.Fatalf("decideEmailPreferences: %v", err)
			}
			if !samePreferences(got, tc.want) {
				t.Errorf("stored %s, want %s", describe(got), describe(tc.want))
			}
		})
	}
}

func samePreferences(a, b ctsvc.EmailPreferences) bool {
	return describe(a) == describe(b)
}

func describe(p ctsvc.EmailPreferences) string {
	flag := func(b *bool) string {
		if b == nil {
			return "nil"
		}
		return onOff(*b)
	}
	return "encrypt=" + flag(p.Encrypt) + " sign=" + flag(p.Sign) + " scheme=" + p.Scheme +
		" plain=" + onOff(p.PlainText)
}
