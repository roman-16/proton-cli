package pass

import (
	"fmt"
	"strings"
	"testing"
)

// login builds one decrypted login the way a listing hands them to the checks.
func login(name, password string, urls ...string) FullItem {
	it := FullItem{Password: password}
	it.Type = "login"
	it.Name = name
	it.ItemID = name
	it.URLs = urls
	it.monitored = true
	return it
}

func names(rows []Item) string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.ReuseGroup > 0 {
			out = append(out, fmt.Sprintf("%s#%d", r.Name, r.ReuseGroup))
			continue
		}
		out = append(out, r.Name)
	}
	return strings.Join(out, " ")
}

func TestOnlyLoginsProtonWouldCheckAreChecked(t *testing.T) {
	excluded := login("excluded", "admin")
	excluded.monitored = false

	note := login("note", "admin")
	note.Type = "note"

	alias := login("alias", "")
	alias.Type = "alias"

	got := checkable([]FullItem{login("router", "admin"), excluded, note, alias})
	if len(got) != 1 || got[0].Name != "router" {
		t.Errorf("checkable returned %d items, want just router", len(got))
	}
}

func TestReusedGroupsTheLoginsThatShareAPassword(t *testing.T) {
	rows := reused([]FullItem{
		login("gitlab", "shared-one-9!"),
		login("nas", "shared-two-4!"),
		login("github", "shared-one-9!"),
		login("router", "shared-two-4!"),
		login("alone", "nobody-else-7!"),
		login("blank", ""),
	})

	// Groups are numbered by their first login's name, so github's pair leads.
	if got, want := names(rows), "github#1 gitlab#1 nas#2 router#2"; got != want {
		t.Errorf("reused returned %q, want %q", got, want)
	}
	for _, r := range rows {
		if r.Risk != RiskReused {
			t.Errorf("%s came back as %q", r.Name, r.Risk)
		}
	}
}

func TestReusedNumbersGroupsTheSameWayWhateverOrderTheyArriveIn(t *testing.T) {
	first := reused([]FullItem{
		login("github", "shared-one-9!"), login("gitlab", "shared-one-9!"),
		login("nas", "shared-two-4!"), login("router", "shared-two-4!"),
	})
	second := reused([]FullItem{
		login("router", "shared-two-4!"), login("gitlab", "shared-one-9!"),
		login("nas", "shared-two-4!"), login("github", "shared-one-9!"),
	})
	if names(first) != names(second) {
		t.Errorf("the same vault numbered two ways: %q then %q", names(first), names(second))
	}
}

func TestWeakReportsThePasswordsThisCLIWouldNotAccept(t *testing.T) {
	rows := weak([]FullItem{
		login("router", "admin"),
		login("github", "M3ssier-Object-88!"),
		login("blank", ""),
	})
	if got, want := names(rows), "router"; got != want {
		t.Errorf("weak returned %q, want %q", got, want)
	}
	if len(rows) > 0 && rows[0].Risk != RiskWeak {
		t.Errorf("router came back as %q", rows[0].Risk)
	}
}

func TestMissing2FAWantsASiteThatOffersOneAndNothingStoredAgainstIt(t *testing.T) {
	withCode := login("has-code", "M3ssier-Object-88!", "https://github.com")
	withCode.TOTP = "otpauth://totp/github"

	withField := login("has-field", "M3ssier-Object-88!", "https://github.com")
	withField.Fields = []ItemField{{Name: "2fa", Type: "totp", Value: "otpauth://totp/x"}}

	withPasskey := login("has-passkey", "M3ssier-Object-88!", "https://github.com")
	withPasskey.Passkeys = []Passkey{{KeyID: "7f3a1c9d", Domain: "github.com", Username: "roman"}}

	rows := missing2FA([]FullItem{
		login("bare", "M3ssier-Object-88!", "https://github.com"),
		login("deep", "M3ssier-Object-88!", "https://gist.github.com/roman"),
		login("nowhere", "M3ssier-Object-88!"),
		login("unlisted", "M3ssier-Object-88!", "https://nothing-listed.example"),
		withCode, withField, withPasskey,
	})

	if got, want := names(rows), "bare deep"; got != want {
		t.Errorf("missing2FA returned %q, want %q", got, want)
	}
}

func TestAnEmptyCustomCodeFieldIsNotASecondFactor(t *testing.T) {
	it := login("empty-field", "M3ssier-Object-88!", "https://github.com")
	it.Fields = []ItemField{{Name: "2fa", Type: "totp"}}
	if hasSecondFactor(it) {
		t.Error("a code field with nothing in it protects nothing")
	}
}
