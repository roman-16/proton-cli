package live

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/tests/account"
)

func TestAccountGetReportsBothHalves(t *testing.T) {
	stdout := runOK(t, "account", "get")
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
	if locked, ok := data["locked_keys"]; ok {
		t.Errorf("locked_keys = %v, want the field absent while no key is locked", locked)
	}
	assertNotContains(t, runOK(t, "account", "get"), "Locked keys:")
}

func TestAccountGetReportsTheUsername(t *testing.T) {
	data := runJSON(t, "account", "get")
	username, _ := data["username"].(string)
	if username == "" || username == data["email"] {
		t.Errorf("username = %q beside email %v, want the account's own username", username, data["email"])
	}
	assertContains(t, runOK(t, "account", "get"), "Username:")
}

func TestAccountDeleteDryRunAsksProtonFirst(t *testing.T) {
	_, stderr := runOKStderr(t, "--dry-run", "account", "delete",
		"--reason", "other", "--message", "Checking the preview")
	assertContains(t, stderr, "Dry run - would delete account")
	assertContains(t, stderr, "mail, contacts, events, files and passwords")
	if runJSON(t, "account", "get")["session"] != "valid" {
		t.Fatal("the dry run left the session unusable")
	}
}

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

func TestAccountSettingsGetAndList(t *testing.T) {
	stdout := runOK(t, "account", "settings", "get")
	for _, want := range []string{"Locale:", "Date Format:", "Telemetry:"} {
		assertContains(t, stdout, want)
	}

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

func TestAccountSettingsReportTwoPasswordMode(t *testing.T) {
	if mode := runJSON(t, "account", "settings", "get")["two_password_mode"]; mode != "off" {
		t.Errorf("the primary account's two_password_mode = %v, want off", mode)
	}
	if mode := runJSONSecondary(t, "account", "settings", "get")["two_password_mode"]; mode != "on" {
		t.Errorf("the secondary account's two_password_mode = %v, want on - the suite covers"+
			" two-password mode by signing that account in, so it has to stay in it", mode)
	}
}

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

func TestProfileFromEnv(t *testing.T) {
	acct := runJSONSecondary(t, "account", "get")
	email, _ := acct["email"].(string)
	if !strings.EqualFold(email, secondaryEmail()) {
		t.Errorf("PROTON_PROFILE=%s acted as %q, want %q", account.Secondary, email, secondaryEmail())
	}
}

func TestProfileNotSignedInIsRefusedLocally(t *testing.T) {
	_, stderr, code := runWithEnv(t, map[string]string{"PROTON_PROFILE": "no-such-" + testID()},
		"mail", "messages", "list")
	if code != 2 {
		t.Errorf("expected exit 2 for a profile with no session, got %d", code)
	}
	assertContains(t, stderr, "not signed in")
	assertContains(t, stderr, "account login")
}

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

type identity struct{ id, email string }

func paidIdentity(t *testing.T) identity {
	t.Helper()
	id, _ := runJSONPaid(t, "account", "get")["id"].(string)
	for _, r := range runJSONArray(t, "account", "profiles", "list") {
		m, _ := r.(map[string]interface{})
		if m["name"] == account.Paid {
			email, _ := m["email"].(string)
			return identity{id: id, email: email}
		}
	}
	t.Fatal("the paid profile is missing from the profile list")
	return identity{}
}

func TestSigningInAgainWithAnotherAddressOfTheAccountResumes(t *testing.T) {
	address := paidForwarder(t)
	before := paidIdentity(t)

	_, stderr, code, err := runAs(account.Paid, nil, loginArgs(accounts[account.Paid], address)...)
	if err != nil || code != 0 {
		t.Fatalf("signing the paid profile in with %s exited %d: %v %s", address, code, err, truncateOutput(stderr))
	}
	if after := paidIdentity(t); after != before {
		t.Errorf("the paid profile is %+v, want %+v as before", after, before)
	}
}

func TestSigningInAsAnotherAccountIsRefused(t *testing.T) {
	before := paidIdentity(t)

	_, stderr, code, err := runAs(account.Paid, nil, loginArgs(accounts[account.Secondary], secondaryEmail())...)
	if err != nil || code != 4 || !strings.Contains(stderr, "is signed in as") {
		t.Errorf("signing the paid profile in as the secondary account exited %d: %v %s", code, err, truncateOutput(stderr))
	}
	if after := paidIdentity(t); after != before {
		t.Errorf("the paid profile is %+v, want %+v as before", after, before)
	}
}

func TestAccountSettings(t *testing.T) {
	stdout := runOK(t, "account", "settings", "get")
	for _, want := range []string{"Locale", "Date Format", "Time Format", "Week Start"} {
		assertContains(t, stdout, want)
	}
}

func TestAccountSettingsJSON(t *testing.T) {
	data := runJSON(t, "account", "settings", "get")
	for _, want := range []string{"locale", "date_format", "week_start", "two_factor"} {
		if _, ok := data[want]; !ok {
			t.Errorf("expected %q in JSON output, got keys %v", want, keysOf(data))
		}
	}
}

func TestAccountSettingsListsKeys(t *testing.T) {
	stdout := runOK(t, "account", "settings", "list")
	for _, want := range []string{"Language and time", "locale", "week-start"} {
		assertContains(t, stdout, want)
	}
}

func TestAccountExportWritesEachAppAndRunsAgain(t *testing.T) {
	into := filepath.Join(t.TempDir(), "copy")
	_, stderr := runOKStderr(t, "account", "export", "pass", "contacts", "calendar", "--dest-dir", into)
	assertContains(t, stderr, "The Pass archive is not encrypted")
	for _, want := range []string{"pass/proton-pass-export.zip", "contacts/contacts.vcf"} {
		if fi, err := os.Stat(filepath.Join(into, filepath.FromSlash(want))); err != nil || fi.Size() == 0 {
			t.Errorf("%s was not written: %v", want, err)
		}
	}
	calendars, err := filepath.Glob(filepath.Join(into, "calendar", "*.ics"))
	if err != nil || len(calendars) == 0 {
		t.Errorf("no calendar was written: %v", err)
	}

	again := runJSON(t, "account", "export", "pass", "contacts", "calendar", "--dest-dir", into)
	if again["count"] != float64(0) {
		t.Errorf("running it again exported %v, want nothing: %v", again["count"], again)
	}
}

func TestAccountExportMailIsLaidOutForRestoring(t *testing.T) {
	msgID, _, subject := plainMail(t)
	into := filepath.Join(t.TempDir(), "copy")
	runOK(t, "account", "export", "mail", "--dest-dir", into)

	labels, err := os.ReadFile(filepath.Join(into, "mail", "labels.json"))
	if err != nil {
		t.Fatalf("labels.json was not written: %v", err)
	}
	assertContains(t, string(labels), `"Version": 1`)
	sidecars, err := filepath.Glob(filepath.Join(into, "mail", "*.metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	found := ""
	for _, sidecar := range sidecars {
		raw, err := os.ReadFile(sidecar)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), `"ID": "`+msgID+`"`) {
			found = sidecar
		}
	}
	if found == "" {
		t.Fatalf("no sidecar names the fixture message %s", msgID)
	}
	eml, err := os.ReadFile(strings.TrimSuffix(found, ".metadata.json") + ".eml")
	if err != nil {
		t.Fatalf("the sidecar has no message beside it: %v", err)
	}
	assertContains(t, string(eml), subject)

	again := runJSON(t, "account", "export", "mail", "--dest-dir", into)
	if again["count"] != float64(0) {
		t.Errorf("running it again exported %v messages, want none", again["count"])
	}
}

func TestAccountExportDriveFollowsAMove(t *testing.T) {
	folder := "/" + testID() + "-copy"
	src := filepath.Join(t.TempDir(), "kept.txt")
	if err := os.WriteFile(src, []byte("kept"), 0o644); err != nil {
		t.Fatal(err)
	}
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, "Delete folder: proton drive items delete "+folder, "drive", "items", "delete", folder)
	runOK(t, "drive", "items", "create", folder+"/moved")
	runOK(t, "drive", "items", "upload", src, folder)

	into := filepath.Join(t.TempDir(), "copy")
	runOK(t, "account", "export", "drive", "--dest-dir", into)
	top := filepath.Join(into, "drive", "My files", strings.TrimPrefix(folder, "/"))
	if got, err := os.ReadFile(filepath.Join(top, "kept.txt")); err != nil || string(got) != "kept" {
		t.Fatalf("the file did not land: %q, %v", got, err)
	}

	runOK(t, "drive", "items", "move", "--into", folder+"/moved", folder+"/kept.txt")
	again := runJSON(t, "account", "export", "drive", "--dest-dir", into)
	if again["count"] != float64(1) {
		t.Errorf("after one move the run exported %v, want 1", again["count"])
	}
	if _, err := os.Stat(filepath.Join(top, "kept.txt")); err == nil {
		t.Error("the file is still where it was before the move")
	}
	if got, err := os.ReadFile(filepath.Join(top, "moved", "kept.txt")); err != nil || string(got) != "kept" {
		t.Errorf("the file did not follow the move: %q, %v", got, err)
	}
}

func TestAccountExportRefusesAnotherAccountsCopy(t *testing.T) {
	into := filepath.Join(t.TempDir(), "copy")
	runOK(t, "account", "export", "contacts", "--dest-dir", into)
	_, stderr, code := runSecondary(t, "account", "export", "contacts", "--dest-dir", into)
	if code != 4 {
		t.Errorf("exit %d, want 4", code)
	}
	assertContains(t, stderr, "is a copy of")
}
