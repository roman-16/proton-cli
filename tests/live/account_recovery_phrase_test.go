package live

import (
	"fmt"
	"strings"
	"testing"
)

// The recovery phrase.
//
// Setting one is the only write in this collection that can be checked without
// resetting a password: the words come back, they are twelve, and Proton then
// reports a phrase where it reported none. What they open is checked from the
// other end, by `account keys reactivate --recovery-phrase`, which no test may
// reach - nothing but a password reset leaves a key for it to open.
//
// The account ends the run with no phrase, which is where it started.

func TestAccountRecoveryPhraseRoundTrip(t *testing.T) {
	// Whatever the account starts with is replaced and then removed. A phrase
	// found here is one an interrupted run left, and the words nobody wrote
	// down are worth nothing to anybody.
	cleanup(t, "the primary account has a recovery phrase this run made. Remove it with: "+
		"proton --profile primary account settings recovery-phrase disable",
		func() error { return removeRecoveryPhrase(t) })

	answer := runJSON(t, "account", "settings", "recovery-phrase", "set")
	phrase, _ := answer["recovery_phrase"].(string)
	if words := strings.Fields(phrase); len(words) != 12 {
		t.Fatalf("a recovery phrase is twelve words, got %d: %v", len(words), keysOf(answer))
	}

	set := runJSON(t, "account", "settings", "recovery-phrase", "get")
	if set["status"] != "on" {
		t.Errorf("status = %v after setting a phrase, want on", set["status"])
	}
	if changed, _ := set["changed"].(float64); changed <= 0 {
		t.Errorf("changed = %v, want when the phrase was set", set["changed"])
	}
	// The keys the phrase holds are the keys the account has, so everything
	// goes on opening with the password as well.
	assertContains(t, runOK(t, "mail", "messages", "list", "--limit", "1"), "ID")

	// Setting another replaces it, and says so.
	_, stderr := runOKStderr(t, "account", "settings", "recovery-phrase", "set")
	assertContains(t, stderr, "the previous one no longer works")

	_, stderr = runOKStderr(t, "account", "settings", "recovery-phrase", "disable")
	assertContains(t, stderr, "recovery phrase")
	if status := runJSON(t, "account", "settings", "recovery-phrase", "get")["status"]; status == "on" {
		t.Errorf("status = %v after removing the phrase, want anything but on", status)
	}
}

// An account with no phrase has none to remove, and says so rather than
// sending a request that would be refused.
func TestAccountRecoveryPhraseRefusesWhatItCannotAct(t *testing.T) {
	if status := runJSON(t, "account", "settings", "recovery-phrase", "get")["status"]; status != "off" {
		t.Skipf("the account's phrase is %v, so there is nothing here to refuse", status)
	}
	_, stderr, code := run(t, "account", "settings", "recovery-phrase", "disable")
	if code != 1 {
		t.Errorf("exit %d, want 1\nstderr: %s", code, truncateOutput(stderr))
	}
	assertContains(t, stderr, "no recovery phrase")
}

// The phrase is part of what `account settings get` reports, in the words
// `recovery-phrase get` uses.
func TestAccountSettingsReportsTheRecoveryPhrase(t *testing.T) {
	status, ok := runJSON(t, "account", "settings", "get")["recovery_phrase"].(string)
	if !ok {
		t.Fatal("account settings get says nothing about the recovery phrase")
	}
	if !contains([]string{"on", "outdated", "not set", "off"}, status) {
		t.Errorf("recovery_phrase = %q, want one of the four words", status)
	}
	assertContains(t, runOK(t, "account", "settings", "get"), "Recovery Phrase:")
}

func removeRecoveryPhrase(t *testing.T) error {
	t.Helper()
	if status := runJSON(t, "account", "settings", "recovery-phrase", "get")["status"]; status != "on" &&
		status != "outdated" {
		return nil
	}
	_, stderr, code := run(t, "account", "settings", "recovery-phrase", "disable")
	if code != 0 {
		return fmt.Errorf("exit %d: %s", code, strings.TrimSpace(stderr))
	}
	return nil
}
