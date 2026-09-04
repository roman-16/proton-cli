package live

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/tests/account"
)

// The account itself: whose it is, what this machine's session can do with it,
// which profiles are signed in here, and the account-wide preferences.
//
// The two free accounts are in the two password modes, and this is the only
// place that covers the second one - so a check on the accounts as much as on
// the commands.

func TestAccountGetReportsBothHalves(t *testing.T) {
	stdout := runOK(t, "account", "get")
	// The question people ask is "whose account, and can this machine act as it",
	// so one command has to answer both.
	for _, want := range []string{"Email:", "Storage:", "Profile:", "Session:", "Unlocked:"} {
		assertContains(t, stdout, want)
	}
}

func TestAccountGetJSON(t *testing.T) {
	data := runJSON(t, "account", "get")
	for _, key := range []string{"email", "used_space", "max_space", "profile", "session", "unlocked"} {
		if _, ok := data[key]; !ok {
			t.Errorf("missing %q in %v", key, keysOf(data))
		}
	}
	if unlocked, ok := data["unlocked"].(bool); !ok {
		t.Errorf("unlocked should be a boolean, got %T", data["unlocked"])
	} else if !unlocked {
		t.Error("the suite runs with an unlocked session, so this should be true")
	}
}

// Storage is reported as a share of a total, which is how a person reads it.
func TestAccountGetStorageIsHumanReadable(t *testing.T) {
	stdout := runOK(t, "account", "get")
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "Storage:") {
			if !strings.Contains(line, " of ") || !strings.Contains(line, "%") {
				t.Errorf("storage should read as a share of a total: %q", line)
			}
			return
		}
	}
	t.Error("no Storage line")
}

func TestAccountSessionsListMarksTheCurrentOne(t *testing.T) {
	sessions := runJSONArray(t, "account", "sessions", "list")
	if len(sessions) == 0 {
		t.Fatal("a signed-in account has at least this session")
	}
	current := 0
	for _, s := range sessions {
		row := s.(map[string]interface{})
		for _, key := range []string{"uid", "client_id", "create_time", "current"} {
			if _, ok := row[key]; !ok {
				t.Errorf("missing %q in %v", key, keysOf(row))
			}
		}
		if row["current"] == true {
			current++
		}
	}
	if current != 1 {
		t.Errorf("exactly one session is the current one, got %d", current)
	}
}

func TestAccountSettingsGetAndList(t *testing.T) {
	stdout := runOK(t, "account", "settings", "get")
	for _, want := range []string{"Locale:", "Date Format:", "Telemetry:"} {
		assertContains(t, stdout, want)
	}

	// The listing is a collection, so it comes out of --output json as one.
	keys := runJSONArray(t, "account", "settings", "list")
	if len(keys) == 0 {
		t.Fatal("there are writable account settings")
	}
	seen := map[string]bool{}
	for _, k := range keys {
		row := k.(map[string]interface{})
		for _, field := range []string{"key", "values", "page", "description"} {
			if _, ok := row[field]; !ok {
				t.Errorf("missing %q in %v", field, keysOf(row))
			}
		}
		seen[row["key"].(string)] = true
	}
	for _, want := range []string{"locale", "date-format", "week-start", "telemetry"} {
		if !seen[want] {
			t.Errorf("expected %q among the writable keys", want)
		}
	}
}

// The two accounts are in the two password modes, and `get` says which.
//
// This is the suite's only check on two-password mode, and it is a check on the
// accounts as much as on the command: the secondary is signed in through the
// second password every run, so an account switched back to one password would
// take that coverage away without anything failing. This is what fails.
func TestAccountSettingsReportTwoPasswordMode(t *testing.T) {
	if mode := runJSON(t, "account", "settings", "get")["two_password_mode"]; mode != "off" {
		t.Errorf("the primary account's two_password_mode = %v, want off", mode)
	}
	if mode := runJSONSecondary(t, "account", "settings", "get")["two_password_mode"]; mode != "on" {
		t.Errorf("the secondary account's two_password_mode = %v, want on - the suite covers"+
			" two-password mode by signing that account in, so it has to stay in it", mode)
	}
}

// Reads and writes speak the same vocabulary: what `get` shows is what `set` takes.
func TestAccountSettingsRoundTripsNames(t *testing.T) {
	before := runJSON(t, "account", "settings", "get")
	original, ok := before["week_start"].(string)
	if !ok {
		t.Fatalf("week_start should be a name, got %T", before["week_start"])
	}
	target := "monday"
	if original == "monday" {
		target = "saturday"
	}
	cleanupRun(t, "Restore week-start", "account", "settings", "set", "week-start", original)

	runOK(t, "account", "settings", "set", "week-start", target)
	after := runJSON(t, "account", "settings", "get")
	if after["week_start"] != target {
		t.Errorf("week_start = %v, want %q", after["week_start"], target)
	}
}

// An account in the paid slot has to actually be on a paid plan.
//
// Proton says so in the session's scopes, and this is the cheapest possible way
// to find out: without it, free credentials put in the paid variables would make
// every gated test fail with Proton's refusal, which reads exactly like the
// feature being broken.
func TestThePaidAccountIsOnAPaidPlan(t *testing.T) {
	scopes, _ := runJSONPaid(t, "account", "get")["scopes"].([]interface{})
	for _, s := range scopes {
		if s == "paid" {
			return
		}
	}
	t.Fatalf("the account in PROTON_CLI_TEST_PAID_USER is not on a paid plan; "+
		"its scopes are %v", scopes)
}

// TestProfileFromEnv: PROTON_PROFILE selects the profile with no --profile flag.
// The harness runs every command that way, so this asserts the account it lands
// on is the one the variable names.
func TestProfileFromEnv(t *testing.T) {
	acct := runJSONSecondary(t, "account", "get")
	email, _ := acct["email"].(string)
	if !strings.EqualFold(email, secondaryEmail()) {
		t.Errorf("PROTON_PROFILE=%s acted as %q, want %q", account.Secondary, email, secondaryEmail())
	}
}

// A profile nobody signed in acts as nobody, and says so before the network -
// so a mistyped profile name is a refusal rather than a command that quietly
// reached whichever account the environment happened to name.
func TestProfileNotSignedInIsRefusedLocally(t *testing.T) {
	_, stderr, code := runWithEnv(t, map[string]string{"PROTON_PROFILE": "no-such-" + testID()},
		"mail", "messages", "list")
	if code != 2 {
		t.Errorf("expected exit 2 for a profile with no session, got %d", code)
	}
	assertContains(t, stderr, "not signed in")
	assertContains(t, stderr, "account login")
}

// TestProfileSessionsAreSeparateFiles: each profile keeps its own session, so
// two accounts on one machine cannot clobber each other.
func TestProfileSessionsAreSeparateFiles(t *testing.T) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("user config dir: %v", err)
	}
	for _, name := range []string{account.Primary, account.Secondary} {
		path := filepath.Join(configDir, "proton-cli", "sessions", name+".json")
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected a session file for %q at %s: %v", name, path, err)
		}
	}
}

// TestProfilesListNamesEverySignedInAccount: the session directory is the whole
// truth about who is signed in here, so listing it needs no API call and cannot
// disagree with what a command would act as.
func TestProfilesListNamesEverySignedInAccount(t *testing.T) {
	rows := runJSONArray(t, "account", "profiles", "list")
	seen := map[string]string{}
	for _, r := range rows {
		m := r.(map[string]interface{})
		name, _ := m["name"].(string)
		email, _ := m["email"].(string)
		seen[name] = email
	}
	for name, want := range map[string]string{account.Primary: selfEmail(), account.Secondary: secondaryEmail()} {
		if got, ok := seen[name]; !ok {
			t.Errorf("profile %q is signed in but missing from the list", name)
		} else if !strings.EqualFold(got, want) {
			t.Errorf("profile %q lists %q, want %q", name, got, want)
		}
	}
}

func TestAccountSettings(t *testing.T) {
	stdout := runOK(t, "account", "settings", "get")
	for _, want := range []string{"Locale", "Date Format", "Time Format", "Week Start"} {
		assertContains(t, stdout, want)
	}
}

// The JSON is the view the command renders, in the CLI's own snake_case names,
// not Proton's envelope passed through.
func TestAccountSettingsJSON(t *testing.T) {
	data := runJSON(t, "account", "settings", "get")
	for _, want := range []string{"locale", "date_format", "week_start", "two_factor"} {
		if _, ok := data[want]; !ok {
			t.Errorf("expected %q in JSON output, got keys %v", want, keysOf(data))
		}
	}
}

// `set` writes one key; `list` is what shows which keys there are.
func TestAccountSettingsListsKeys(t *testing.T) {
	stdout := runOK(t, "account", "settings", "list")
	for _, want := range []string{"Language and time", "locale", "week-start"} {
		assertContains(t, stdout, want)
	}
}
