package live

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/tests/account"
)

// Items: every type Pass holds, the fields each carries, and the history behind
// one.
//
// A secret never reaches argv, so every test that stores one writes it to a file
// first - which is what a person does, and so the only path the suite exercises.

// secretFile writes one secret where a command can read it and answers with the
// NAME=FILE token that names it.
//
// argv may not carry a secret, so every test that stores one puts it in a file
// of its own first - which is the same thing a person does, and the only way the
// suite exercises the path they use.
func secretFile(t *testing.T, field, value string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), strings.ReplaceAll(field, "/", "-"))
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatalf("writing the %s secret: %v", field, err)
	}
	return field + "=" + path
}

func TestPassItemsCRUDLogin(t *testing.T) {
	name := testID() + "-login"
	url := "https://" + name + ".example.invalid/"

	createItem(t,
		"--type", "login",
		"--name", name,
		"--username", "tester",
		"--secret-file", secretFile(t, "password", "s3cret!"),
		"--url", url)
	cleanupRun(t, fmt.Sprintf("Delete item: proton pass items delete %s", name),
		"pass", "items", "delete", name)

	// Get by URL REF
	got := runOK(t, "pass", "items", "get", name+".example.invalid")
	assertField(t, got, "Name:", name)
	assertField(t, got, "Username:", "tester")
	assertField(t, got, "Password:", "s3cret!")

	// Edit password, this time over the stream rather than out of a file.
	runWithStdin(t, strings.NewReader("new-pass-v2"),
		"--yes", "pass", "items", "update", "--secret-stdin", "password", name)
	got2 := runOK(t, "pass", "items", "get", name)
	assertField(t, got2, "Password:", "new-pass-v2")
}

// A password the CLI makes never travels: it is stored, and reported beside the
// new item's ID rather than in it.
func TestPassItemsCreateGeneratedPassword(t *testing.T) {
	name := testID() + "-generated"
	ref, stderr := createItemReported(t, "--name", name,
		"--username", "tester", "--generate-password", "--words", "4")
	cleanupRun(t, fmt.Sprintf("Delete item: proton pass items delete -- %s", ref),
		"pass", "items", "delete", "--", ref)

	_, made, ok := strings.Cut(stderr, "Password  ")
	if !ok {
		t.Fatalf("the generated password was not reported: %s", truncateOutput(stderr))
	}
	made = strings.TrimSpace(strings.SplitN(made, "\n", 2)[0])
	if words := strings.Split(made, "-"); len(words) != 4 {
		t.Errorf("--words 4 made %q", made)
	}
	assertField(t, runOK(t, "pass", "items", "get", "--", ref), "Password:", made)
}

func TestPassItemsCreateNote(t *testing.T) {
	name := testID() + "-note"
	createItem(t, "--type", "note", "--name", name, "--note", "secret note content")
	cleanupRun(t, fmt.Sprintf("Delete note: proton pass items delete %s", name),
		"pass", "items", "delete", name)

	got := runOK(t, "pass", "items", "get", name)
	assertField(t, got, "Type:", "note")
	assertField(t, got, "Note:", "secret note content")
}

func TestPassItemsCreateCardShowsPIN(t *testing.T) {
	name := testID() + "-card"
	createItem(t,
		"--type", "credit-card",
		"--name", name,
		"--holder", "Test Holder",
		"--expiry", "2029-01",
		"--secret-file", secretFile(t, "number", "4111111111111111"),
		"--secret-file", secretFile(t, "cvv", "123"),
		"--secret-file", secretFile(t, "pin", "7890"))
	cleanupRun(t, fmt.Sprintf("Delete card: proton pass items delete %s", name),
		"pass", "items", "delete", name)

	got := runOK(t, "pass", "items", "get", name)
	assertField(t, got, "Cardholder:", "Test Holder")
	assertField(t, got, "Number:", "4111111111111111")
	assertField(t, got, "Expiry:", "2029-01")
	assertField(t, got, "CVV:", "123")
	assertField(t, got, "PIN:", "7890")
}

// One word for one type, wherever it appears: what --type accepts at create
// time is what a record shows and what the --type filter matches. Two spellings
// of the same type make `trash --type credit-card` match nothing, silently.
func TestPassCreditCardTypeConsistent(t *testing.T) {
	name := testID() + "-cc"
	ref := createItem(t, "--type", "credit-card",
		"--name", name, "--holder", "Roman", "--expiry", "2030-01",
		"--secret-file", secretFile(t, "number", "4111111111111111"))
	cleanupRun(t, fmt.Sprintf("Delete card: proton pass items delete %s", name),
		"pass", "items", "delete", name)

	// Display/JSON type uses the same kebab spelling as the create flag.
	// --output json before -- so the flag parses and ref stays positional.
	var item map[string]interface{}
	if err := json.Unmarshal([]byte(runOK(t, "pass", "items", "get", "--output", "json", "--", ref)), &item); err != nil {
		t.Fatalf("parse item JSON: %v", err)
	}
	if got := item["type"]; got != "credit-card" {
		t.Errorf("type = %v, want credit-card", got)
	}
	// The --type filter word == the create word, so it matches the item.
	_, stderr := runOKStderr(t, "--dry-run", "pass", "items", "trash", "--type", "credit-card")
	if !strings.Contains(stderr, ref) {
		t.Errorf("trash --type credit-card should match the credit-card item %s; stderr:\n%s", ref, stderr)
	}
}

func TestPassItemsTrashRestoreDelete(t *testing.T) {
	name := testID() + "-trash"
	// Creating answers with SHARE_ID/ITEM_ID, which is the reference every item
	// verb takes - and the only way to reach a trashed item, since searching by
	// name does not find one.
	ref := createItem(t,
		"--type", "login", "--name", name,
		"--username", "u", "--secret-file", secretFile(t, "password", "p"))
	cleanupRun(t, fmt.Sprintf("Delete item: proton pass items delete -- %s", ref),
		"pass", "items", "delete", "--", ref)

	runOK(t, "pass", "items", "trash", name)
	runOK(t, "pass", "trash", "restore", "--", ref)

	// It should be searchable again
	got := runOK(t, "pass", "items", "get", name)
	assertField(t, got, "Name:", name)
}

func TestPassItemsListVaultFilter(t *testing.T) {
	vault, _ := pinned(t, account.Primary, "vault", "Personal")["name"].(string)
	runOK(t, "pass", "items", "list", "--vault", vault)
}

// Pass Monitor's password health: every check over the account's real logins.
//
// What each one finds is whatever is in the vaults, so the assertion is on the
// shape - a row is a login, and it says which check it failed - rather than on
// how many there are. Two logins are made first so the reuse check has a pair to
// find whatever else is there.
func TestPassItemsListByRisk(t *testing.T) {
	// A secret never arrives as a flag value, so the password the pair shares is
	// written where the binary reads one from.
	shared := filepath.Join(t.TempDir(), "shared-password")
	if err := os.WriteFile(shared, []byte("proton-cli-test-Shared-8!"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{testID() + "-risk-a", testID() + "-risk-b"} {
		runOK(t, "pass", "items", "create", "--type", "login", "--name", name,
			"--secret-file", "password="+shared, "--url", "https://github.com")
		cleanupRun(t, "Delete: proton pass items delete "+name,
			"pass", "items", "delete", name)
	}

	for _, risk := range []string{"compromised", "missing-2fa", "reused", "weak"} {
		rows := runJSONArray(t, "pass", "items", "list", "--risk", risk)
		for _, row := range rows {
			m, _ := row.(map[string]interface{})
			if got, _ := m["type"].(string); got != "login" {
				t.Errorf("--risk %s returned an item of type %q", risk, got)
			}
			if got, _ := m["risk"].(string); got != risk {
				t.Errorf("--risk %s returned a row marked %q", risk, got)
			}
			// A listing never prints a secret, whatever it was selected by.
			if _, printed := m["password"]; printed {
				t.Fatalf("--risk %s put a password in the listing", risk)
			}
		}
	}

	// The two made above share a password and point at a site that offers a code,
	// so both checks have something they must find.
	for _, risk := range []string{"reused", "missing-2fa"} {
		var found int
		for _, row := range runJSONArray(t, "pass", "items", "list", "--risk", risk) {
			m, _ := row.(map[string]interface{})
			if name, _ := m["name"].(string); strings.Contains(name, "-risk-") {
				found++
			}
		}
		if found != 2 {
			t.Errorf("--risk %s found %d of the two logins this test made", risk, found)
		}
	}

	// Grouping is what makes a reuse listing readable, so the pair has to carry
	// one number between them.
	groups := map[float64]int{}
	for _, row := range runJSONArray(t, "pass", "items", "list", "--risk", "reused") {
		m, _ := row.(map[string]interface{})
		if name, _ := m["name"].(string); strings.Contains(name, "-risk-") {
			n, _ := m["reuse_group"].(float64)
			groups[n]++
		}
	}
	if len(groups) != 1 {
		t.Errorf("the two logins sharing one password landed in %d groups", len(groups))
	}
}

func TestPassBatchTrashDryRunByType(t *testing.T) {
	_, stderr, code := run(t, "--dry-run", "pass", "items", "trash", "--type", "note")
	if code != 0 {
		t.Fatalf("dry-run should succeed, got exit %d: %s", code, stderr)
	}
	assertContains(t, stderr, "Dry run")
}

func TestPassBatchTrashDryRunOlderThanYear(t *testing.T) {
	_, stderr, code := run(t, "--dry-run", "pass", "items", "trash",
		"--older-than", "1y", "--type", "login")
	if code != 0 {
		t.Fatalf("dry-run should succeed, got exit %d: %s", code, stderr)
	}
	// Either a "would trash" line or nothing to trash; at minimum doesn't crash
	_ = stderr
}

func TestPassBatchTrashDurationUnitMonths(t *testing.T) {
	// "6mo" must parse without error.
	_, _, code := run(t, "--dry-run", "pass", "items", "trash",
		"--older-than", "6mo", "--type", "login")
	if code != 0 {
		t.Errorf("--older-than 6mo should parse, got exit %d", code)
	}
}

func TestPassItemTypesAndFields(t *testing.T) {
	// Identity with core fields plus custom text/hidden fields.
	idName := testID() + "-identity"
	idRef := createItem(t, "--type", "identity",
		"--name", idName, "--full-name", "Jane Roe", "--email", "jane@example.com",
		"--organization", "Acme", "--field", "Note=hello-field",
		"--secret-file", secretFile(t, "PIN", "4321"))
	cleanupRun(t, fmt.Sprintf("Delete pass item: proton pass items delete %s", idRef),
		"pass", "items", "delete", "--", idRef)
	gotID := runOK(t, "pass", "items", "get", "--", idRef)
	assertContains(t, gotID, "Jane Roe")
	assertContains(t, gotID, "Acme")
	assertContains(t, gotID, "hello-field")

	// Wi-Fi.
	wifiRef := createItem(t, "--type", "wifi",
		"--name", testID()+"-wifi", "--ssid", "MyTestNet", "--security", "WPA2",
		"--secret-file", secretFile(t, "password", "pw"))
	cleanupRun(t, fmt.Sprintf("Delete pass item: proton pass items delete %s", wifiRef),
		"pass", "items", "delete", "--", wifiRef)
	assertContains(t, runOK(t, "pass", "items", "get", "--", wifiRef), "MyTestNet")

	// SSH key.
	sshRef := createItem(t, "--type", "ssh-key",
		"--name", testID()+"-ssh", "--public-key", "ssh-ed25519 AAAATESTKEY",
		"--secret-file", secretFile(t, "private-key", "PRIVATE-TEST"))
	cleanupRun(t, fmt.Sprintf("Delete pass item: proton pass items delete %s", sshRef),
		"pass", "items", "delete", "--", sshRef)
	assertContains(t, runOK(t, "pass", "items", "get", "--", sshRef), "ssh-ed25519 AAAATESTKEY")
}

func TestPassLoginTOTPRoundTrips(t *testing.T) {
	name := testID() + "-totp"
	secret := "JBSWY3DPEHPK3PXP"
	ref := createItem(t, "--type", "login",
		"--name", name, "--username", "me@example.com",
		"--secret-file", secretFile(t, "totp-uri", "otpauth://totp/Example:me?secret="+secret+"&issuer=Example"))
	cleanupRun(t, fmt.Sprintf("Delete pass item: proton pass items delete %s", ref),
		"pass", "items", "delete", "--", ref)

	assertContains(t, runOK(t, "pass", "items", "get", "--", ref), secret)
}

// Pass keeps every edit, so a password changed by mistake can be read back.
func TestPassItemRevisionsShowWhatItUsedToBe(t *testing.T) {
	name := testID() + "-history"
	ref := createItem(t,
		"--name", name, "--username", "first",
		"--secret-file", secretFile(t, "password", "first-secret"))
	cleanupRun(t, fmt.Sprintf("Delete item: proton pass items delete %s", ref),
		"pass", "items", "delete", "--", ref)

	runOK(t, "pass", "items", "update", "--username", "second",
		"--secret-file", secretFile(t, "password", "second-secret"), "--", ref)

	revs := runJSONArray(t, "pass", "items", "revisions", "list", "--", ref)
	if len(revs) < 2 {
		t.Fatalf("after one edit there should be at least two revisions, got %d", len(revs))
	}
	// Newest first, which is the order somebody asking "what did it used to be"
	// reads in.
	newest, _ := revs[0].(map[string]interface{})
	if n, _ := newest["revision"].(float64); int(n) < 2 {
		t.Errorf("the first row is revision %v; the newest should lead", n)
	}
	// A listing says what changed and when, and carries nothing that was locked
	// away: reading one revision back is a command of its own.
	var earlier int
	for _, row := range revs {
		m, _ := row.(map[string]interface{})
		item, _ := m["item"].(map[string]interface{})
		if item == nil {
			continue
		}
		if _, leaked := item["password"]; leaked {
			t.Error("a revision listing carries the password that revision held")
		}
		if u, _ := item["username"].(string); u == "first" {
			earlier = int(m["revision"].(float64))
		}
	}
	if earlier == 0 {
		t.Fatal("the earlier username should be readable in the history")
	}

	got := runOK(t, "pass", "items", "revisions", "get", ref, strconv.Itoa(earlier))
	assertField(t, got, "Username:", "first")
	assertField(t, got, "Password:", "first-secret")
}

// Restoring puts the fields back as they were and is written as the newest
// version, so the history keeps the restore too.
func TestPassItemRevisionsRestoreWhatItUsedToBe(t *testing.T) {
	name := testID() + "-restore"
	ref := createItem(t,
		"--name", name, "--username", "first",
		"--secret-file", secretFile(t, "password", "first-secret"))
	cleanupRun(t, fmt.Sprintf("Delete item: proton pass items delete %s", ref),
		"pass", "items", "delete", "--", ref)

	runOK(t, "pass", "items", "update", "--username", "second",
		"--secret-file", secretFile(t, "password", "second-secret"), "--", ref)

	before := runJSONArray(t, "pass", "items", "revisions", "list", "--", ref)
	if len(before) < 2 {
		t.Fatalf("an item edited once has %d versions", len(before))
	}
	oldest, _ := before[len(before)-1].(map[string]interface{})
	first, _ := oldest["revision"].(float64)

	runOK(t, "--yes", "pass", "items", "revisions", "restore", ref, strconv.Itoa(int(first)))

	got := runOK(t, "pass", "items", "get", "--", ref)
	assertField(t, got, "Username:", "first")
	assertField(t, got, "Password:", "first-secret")

	// Nothing in the history is lost: the restore is another version on top.
	after := runJSONArray(t, "pass", "items", "revisions", "list", "--", ref)
	if len(after) <= len(before) {
		t.Errorf("restoring wrote no new version: %d before, %d after", len(before), len(after))
	}
}

// An item is sealed under the key of the vault it is in, so moving it means
// sealing it again under another's. It keeps what it holds and is given a new
// ID, because an item in Pass is only unique together with its vault.
func TestPassItemsMoveBetweenVaults(t *testing.T) {
	name := testID() + "-move"
	ref := createItem(t, "--name", name,
		"--username", "tester", "--secret-file", secretFile(t, "password", "travels-with-it"))
	cleanupRun(t, fmt.Sprintf("Delete item: proton pass items delete -- %s", ref),
		"pass", "items", "delete", "--", ref)

	vault := testID() + "-elsewhere"
	shareID := createVault(t, vault)
	cleanupRun(t, fmt.Sprintf("Delete vault: proton pass vaults delete -- %s", shareID),
		"pass", "vaults", "delete", "--", shareID)

	moved := strings.TrimSpace(runOK(t, "pass", "items", "move", "--into", vault, "--", ref))
	if !looksLikePairRef(moved) {
		t.Fatalf("expected the new SHARE_ID/ITEM_ID on stdout, got %q", moved)
	}
	if moved == ref {
		t.Error("a moved item keeps the reference it had, and it is in another vault now")
	}
	cleanupRun(t, fmt.Sprintf("Delete moved item: proton pass items delete -- %s", moved),
		"pass", "items", "delete", "--", moved)

	got := runOK(t, "pass", "items", "get", "--", moved)
	assertField(t, got, "Name:", name)
	assertField(t, got, "Password:", "travels-with-it")
	if !strings.HasPrefix(moved, shareID+"/") {
		t.Errorf("%s is not in the vault it was moved into (%s)", moved, shareID)
	}
}

// Pinning carries no content, so nothing is encrypted: it is the vault recording
// that one of its items is wanted often.
func TestPassItemPinAndUnpin(t *testing.T) {
	name := testID() + "-pin"
	ref := createItem(t,
		"--name", name, "--username", "someone")
	cleanupRun(t, fmt.Sprintf("Delete item: proton pass items delete %s", ref),
		"pass", "items", "delete", "--", ref)

	runOK(t, "pass", "items", "pin", "--", ref)
	cleanupRun(t, fmt.Sprintf("Unpin item: proton pass items unpin %s", ref),
		"pass", "items", "unpin", "--", ref)
	runOK(t, "pass", "items", "unpin", "--", ref)
}

// Pass stores the secret, not the code, so the code is worked out here. The
// arithmetic is checked against RFC 6238's own vectors in internal/otp; this
// checks it reaches a stored item.
func TestPassItemTOTPCode(t *testing.T) {
	name := testID() + "-totp"
	secret := "GEZDGNBVGY3TQOJQ"
	ref := createItem(t,
		"--name", name, "--username", "someone",
		"--secret-file", secretFile(t, "totp-uri",
			"otpauth://totp/Example:someone?secret="+secret+"&issuer=Example"))
	cleanupRun(t, fmt.Sprintf("Delete item: proton pass items delete %s", ref),
		"pass", "items", "delete", "--", ref)

	// Under a full run's load Pass does not always have a just-created item ready
	// to read - it answers 2501 for a moment - so the code is asked for until the
	// item is there rather than on the first try.
	waitFor(30*time.Second, 2*time.Second, func() bool {
		_, _, code := run(t, "pass", "items", "totp", "--", ref)
		return code == 0
	})

	got := runJSON(t, "pass", "items", "totp", "--", ref)
	code, _ := got["code"].(string)
	if len(code) != 6 {
		t.Errorf("code = %q, want six digits", code)
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			t.Errorf("code %q is not all digits", code)
			break
		}
	}
	if left, _ := got["expires_in_seconds"].(float64); left < 1 || left > 30 {
		t.Errorf("expires in %v seconds, want between 1 and 30", left)
	}
}

// An item with no second factor says so rather than printing a code for nothing.
func TestPassItemTOTPWithoutASecret(t *testing.T) {
	name := testID() + "-nototp"
	ref := createItem(t,
		"--name", name, "--username", "someone")
	cleanupRun(t, fmt.Sprintf("Delete item: proton pass items delete %s", ref),
		"pass", "items", "delete", "--", ref)

	_, stderr, code := run(t, "pass", "items", "totp", "--", ref)
	if code == 0 {
		t.Error("an item with no second factor should not produce a code")
	}
	if !strings.Contains(stderr, "no two-factor secret") {
		t.Errorf("the refusal should say there is no secret, got: %s", stderr)
	}
}

// A field can name the heading it sits under, and what a record shows is what
// --field accepts.
func TestPassItemFieldsCarryTheirSection(t *testing.T) {
	name := testID() + "-sections"
	ref := createItem(t, "--type", "custom", "--name", name,
		"--field", "Network/SSID=home", "--secret-file", secretFile(t, "Network/Key", "hunter2"),
		"--field", "Admin/URL=http://192.168.0.1", "--field", "Loose=1")
	cleanupRun(t, fmt.Sprintf("Delete item: proton pass items delete %s", ref),
		"pass", "items", "delete", "--", ref)

	got := runJSON(t, "pass", "items", "get", "--", ref)
	rows, _ := got["fields"].([]interface{})
	fields := map[string]string{}
	kinds := map[string]string{}
	for _, row := range rows {
		m, _ := row.(map[string]interface{})
		n, _ := m["name"].(string)
		section, _ := m["section"].(string)
		v, _ := m["value"].(string)
		k, _ := m["type"].(string)
		key := n
		if section != "" {
			key = section + "/" + n
		}
		fields[key], kinds[key] = v, k
	}
	for key, want := range map[string]string{
		"Network/SSID": "home", "Network/Key": "hunter2",
		"Admin/URL": "http://192.168.0.1", "Loose": "1",
	} {
		if fields[key] != want {
			t.Errorf("%s = %q, want %q (got %v)", key, fields[key], want, fields)
		}
	}
	if kinds["Network/Key"] != "hidden" {
		t.Errorf("a hidden field in a section came back as %q", kinds["Network/Key"])
	}

	// A patch names one field and leaves the rest alone, and two sections may
	// hold a field of the same name. A field can hold a two-factor secret too.
	runOK(t, "pass", "items", "update",
		"--secret-file", secretFile(t, "Network/Key", "hunter3"),
		"--field", "Admin/URL=http://10.0.0.1",
		"--secret-file", secretFile(t, "Admin/Code", "otpauth://totp/Admin?secret=JBSWY3DPEHPK3PXP"),
		"--", ref)
	after := runOK(t, "pass", "items", "get", "--", ref)
	assertContains(t, after, "hunter3")
	assertContains(t, after, "http://10.0.0.1")
	assertContains(t, after, "home")
	assertNotContains(t, after, "hunter2")

	// A two-factor field is where a second factor for the same login lands, and
	// `totp` reads one wherever it sits.
	shown := runOK(t, "pass", "items", "totp", "--", ref)
	if !regexp.MustCompile(`\b\d{6}\b`).MatchString(shown) {
		t.Errorf("no six-digit code came out of the custom field:\n%s", shown)
	}
}

// A backup is only a backup if it can be read back, so the archive this writes
// is the one it reads. The format is Proton Pass's own, which is what lets the
// app open it too.
//
// An export holds the whole account and reading it back adds all of it, so this
// test creates a copy of every item there is. What it cleans up is therefore
// everything that was not there before it ran, not just the item it made: taking
// only its own would leave the account doubled, and doubled again next run.
func TestPassExportAndImportRoundTrip(t *testing.T) {
	name := testID() + "-backup"
	ref := createItem(t, "--name", name,
		"--username", "jane", "--url", "https://example.com",
		"--secret-file", secretFile(t, "password", "hunter2"),
		"--field", "Recovery codes=abc-def")
	cleanupRun(t, fmt.Sprintf("Delete item: proton pass items delete %s", ref),
		"pass", "items", "delete", "--", ref)

	dir := t.TempDir()
	archive := filepath.Join(dir, "backup.zip")
	runOK(t, "pass", "export", "--dest", archive)

	// Without a passphrase the document is readable, which is what the warning
	// says and what makes this assertion possible at all.
	document := archivedEntry(t, archive, "Proton Pass/data.json")
	// Every item says what it carries, so an item with nothing attached says so
	// rather than leaving the app to guess.
	if !strings.Contains(string(document), `"files":[]`) {
		t.Errorf("the items in the archive do not say what they carry")
	}

	// A dry run says what it would do and does none of it.
	_, stderr := runOKStderr(t, "--dry-run", "pass", "import", archive)
	assertContains(t, stderr, "Dry run")

	before := passItemRefs(t)
	cleanup(t, "Delete the items a restored backup added: proton pass items list, "+
		"then delete every duplicate", func() error {
		for ref := range passItemRefs(t) {
			if before[ref] {
				continue
			}
			if _, stderr, code, err := runArgs(nil, "--yes", "pass", "items", "delete", "--", ref); err != nil {
				return err
			} else if code != 0 {
				return fmt.Errorf("exit %d: %s", code, stderr)
			}
		}
		return nil
	})

	runOK(t, "pass", "import", archive)
	after := passItemRefs(t)
	if len(after) <= len(before) {
		t.Fatalf("the read-back added nothing: %d items before, %d after", len(before), len(after))
	}

	// Reading it back adds the items again rather than matching them, so there
	// are now two of everything - including two of the one this test made.
	restored := ""
	for _, row := range listAll(t, "pass", "items", "list") {
		m, _ := row.(map[string]interface{})
		if n, _ := m["name"].(string); n != name {
			continue
		}
		share, _ := m["share_id"].(string)
		id, _ := m["item_id"].(string)
		if r := share + "/" + id; r != ref {
			restored = r
		}
	}
	if restored == "" {
		t.Fatalf("the backup did not bring %s back", name)
	}
	// What the item held has to come back with it.
	shown := runOK(t, "pass", "items", "get", "--", restored)
	for _, want := range []string{"hunter2", "jane", "https://example.com", "Recovery codes", "abc-def"} {
		assertContains(t, shown, want)
	}
}

// archivedEntry reads one file out of an archive, which is the only way to see
// what it holds: the names are in the clear and the contents are not.
func archivedEntry(t *testing.T, archive, name string) []byte {
	t.Helper()
	z, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatalf("open %s: %v", archive, err)
	}
	defer func() { _ = z.Close() }()
	for _, entry := range z.File {
		if entry.Name != name {
			continue
		}
		r, err := entry.Open()
		if err != nil {
			t.Fatalf("open %s in %s: %v", name, archive, err)
		}
		defer func() { _ = r.Close() }()
		body, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("read %s in %s: %v", name, archive, err)
		}
		return body
	}
	t.Fatalf("%s holds no %s", archive, name)
	return nil
}

// passItemRefs is every item in the account, as the references a command takes.
//
// It pages, because a listing stops at fifty. Reading only the first page makes
// a vault that outgrew it look unchanged however much was added to it - and
// leaves whatever is past the cap out of the cleanup that reads this too.
func passItemRefs(t *testing.T) map[string]bool {
	t.Helper()
	const pageSize = 100
	out := map[string]bool{}
	for page := 0; ; page++ {
		rows := runJSONArray(t, "pass", "items", "list",
			"--page", strconv.Itoa(page), "--page-size", strconv.Itoa(pageSize))
		for _, row := range rows {
			m, _ := row.(map[string]interface{})
			share, _ := m["share_id"].(string)
			id, _ := m["item_id"].(string)
			out[share+"/"+id] = true
		}
		if len(rows) < pageSize {
			return out
		}
	}
}

// A passphrase locks the archive, and without it nothing can be read back.
func TestPassExportWithAPassphrase(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "passphrase")
	if err := os.WriteFile(secret, []byte("correct horse battery staple"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	archive := filepath.Join(dir, "locked.zip")
	runOK(t, "pass", "export", "--dest", archive, "--passphrase-file", secret)

	raw, err := os.ReadFile(archive)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(raw), "Proton Pass/data.pgp") {
		t.Error("a locked archive should hold data.pgp")
	}
	if strings.Contains(string(raw), "\"vaults\"") {
		t.Error("the document is readable inside a locked archive")
	}

	// The passphrase is what opens it, and a dry run proves it was opened without
	// writing anything.
	_, stderr := runOKStderr(t, "--dry-run", "pass", "import", archive, "--passphrase-file", secret)
	assertContains(t, stderr, "Dry run")

	wrong := filepath.Join(dir, "wrong")
	if err := os.WriteFile(wrong, []byte("nope"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, stderr, code := run(t, "pass", "import", archive, "--passphrase-file", wrong)
	if code == 0 {
		t.Error("the wrong passphrase opened the archive")
	}
	assertContains(t, stderr, "passphrase")
}

// ── moving items in from a file ──

// A backup keeps what the account knows about an item beyond its content: when
// it was made, when it last changed, and whether it was in the trash.
//
// Everything lands in a vault of this test's own, so deleting that vault is the
// whole cleanup however much the account holds.
func TestPassImportKeepsDatesAndTrash(t *testing.T) {
	name := testID() + "-dated"
	ref := createItem(t, "--name", name,
		"--username", "jane", "--url", "https://example.com",
		"--secret-file", secretFile(t, "password", "hunter2"))
	cleanupRun(t, fmt.Sprintf("Delete item: proton pass items delete %s", ref),
		"pass", "items", "delete", "--", ref)
	runOK(t, "--yes", "pass", "items", "trash", "--", ref)

	document := filepath.Join(t.TempDir(), "export.json")
	runOK(t, "pass", "export", "--format", "json", "--dest", document)
	body, err := os.ReadFile(document)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// The document is the one the archive holds, so it says what each item
	// carries and how a login fills.
	for _, want := range []string{`"vaults"`, `"files":[]`, `"autofillUrls"`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("the document leaves out %s", want)
		}
	}
	was := exportedItem(t, body, "", name)
	if was["state"] != float64(2) {
		t.Fatalf("the export says the item is in state %v, want it trashed", was["state"])
	}

	into := importVault(t)
	runOK(t, "pass", "import", document, "--vault", into)

	// An item that was in the trash goes back to the trash, so a listing cannot
	// see it. What the account now holds is read the same way it was written:
	// exporting again says where the item landed and what state it is in.
	second := filepath.Join(t.TempDir(), "after.json")
	runOK(t, "pass", "export", "--format", "json", "--dest", second)
	after, err := os.ReadFile(second)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	restored := exportedItem(t, after, into, name)
	if restored["state"] != float64(2) {
		t.Errorf("the item came back in state %v, want it trashed", restored["state"])
	}
	for _, field := range []string{"createTime", "modifyTime"} {
		if restored[field] != was[field] {
			t.Errorf("%s came back as %v, want %v", field, restored[field], was[field])
		}
	}
}

// exportedItem is what a document says about one item: the one of that name in
// the vault named, or in any vault when that is empty.
func exportedItem(t *testing.T, body []byte, vault, name string) map[string]interface{} {
	t.Helper()
	var document struct {
		Vaults map[string]struct {
			Name  string
			Items []map[string]interface{}
		}
	}
	if err := json.Unmarshal(body, &document); err != nil {
		t.Fatalf("the document will not parse: %v", err)
	}
	for _, holder := range document.Vaults {
		if vault != "" && holder.Name != vault {
			continue
		}
		for _, item := range holder.Items {
			data, _ := item["data"].(map[string]interface{})
			metadata, _ := data["metadata"].(map[string]interface{})
			if n, _ := metadata["name"].(string); n == name {
				return item
			}
		}
	}
	t.Fatalf("the document holds no item called %s in %s", name, vault)
	return nil
}

// The spreadsheet is Proton Pass's own columns, so what this writes it reads.
func TestPassExportCSVRoundTrip(t *testing.T) {
	name := testID() + "-csv"
	ref := createItem(t, "--name", name,
		"--username", "jane", "--url", "https://example.com",
		"--secret-file", secretFile(t, "password", "hunter2"))
	cleanupRun(t, fmt.Sprintf("Delete item: proton pass items delete %s", ref),
		"pass", "items", "delete", "--", ref)

	sheet := filepath.Join(t.TempDir(), "pass.csv")
	_, stderr := runOKStderr(t, "pass", "export", "--format", "csv", "--dest", sheet)
	// A file nobody locked holds every password, and a CSV cannot hold
	// everything an item does. Both are said as it is written.
	assertContains(t, stderr, "not encrypted")
	assertContains(t, stderr, "custom fields")

	body, err := os.ReadFile(sheet)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	header := strings.SplitN(string(body), "\n", 2)[0]
	assertContains(t, header, "type,name,url,autofillUrls,email,username,password,note,totp,createTime,modifyTime,vault")
	assertContains(t, string(body), name)

	// A passphrase cannot lock a CSV, and that is refused before anything is
	// written.
	secret := filepath.Join(t.TempDir(), "passphrase")
	if err := os.WriteFile(secret, []byte("correct horse"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, stderr, code := run(t, "pass", "export", "--format", "csv",
		"--dest", filepath.Join(t.TempDir(), "locked.csv"), "--passphrase-file", secret)
	if code == 0 {
		t.Error("a CSV was written with a passphrase")
	}
	assertContains(t, stderr, "cannot be encrypted")

	into := importVault(t)
	runOK(t, "pass", "import", sheet, "--vault", into)
	restored := itemInVault(t, into, name)
	if restored["username"] != "jane" {
		t.Errorf("the login came back as %v", restored)
	}
	shown := runOK(t, "pass", "items", "get", "--",
		fmt.Sprint(restored["share_id"], "/", restored["item_id"]))
	for _, want := range []string{"hunter2", "https://example.com"} {
		assertContains(t, shown, want)
	}
}

// A file another password manager wrote comes in whole: its folders become
// vaults, and the kinds Pass has an item for become that item.
func TestPassImportFromBitwarden(t *testing.T) {
	name := testID() + "-bitwarden"
	export := filepath.Join(t.TempDir(), "bitwarden.json")
	body := strings.ReplaceAll(`{
  "encrypted": false,
  "folders": [{"id": "f-1", "name": "NAME"}],
  "items": [
    {"id": "i-1", "type": 1, "name": "NAME-login", "notes": "a note", "folderId": "f-1",
     "fields": [{"name": "Ticket", "type": 0, "value": "T-1"}],
     "login": {"username": "jane@example.com", "password": "hunter2",
               "totp": "JBSWY3DPEHPK3PXP",
               "uris": [{"uri": "https://example.com", "match": null}]}},
    {"id": "i-2", "type": 2, "name": "NAME-note", "notes": "the note", "folderId": "f-1"},
    {"id": "i-3", "type": 3, "name": "NAME-card", "folderId": "f-1",
     "card": {"cardholderName": "Jane Doe", "number": "4242424242424242",
              "code": "123", "expMonth": "3", "expYear": "2027"}}
  ]
}`, "NAME", name)
	if err := os.WriteFile(export, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// A dry run says what would land, and does none of it.
	stdout, stderr := runOKStderr(t, "--dry-run", "pass", "import", export, "--manager", "bitwarden")
	assertContains(t, stderr, "Dry run")
	for _, want := range []string{name + "-login", "credit-card", name} {
		assertContains(t, stdout+stderr, want)
	}

	// The folder in the file becomes a vault of that name, which is what the
	// cleanup takes away again.
	cleanup(t, fmt.Sprintf("Delete the vault an import made: proton pass vaults delete -- %s", name),
		func() error {
			_, stderr, code, err := runArgs(nil, "--yes", "pass", "vaults", "delete", "--", name)
			if err != nil {
				return err
			}
			if code != 0 && !strings.Contains(stderr, "not found") {
				return fmt.Errorf("exit %d: %s", code, stderr)
			}
			return nil
		})
	runOK(t, "pass", "import", export, "--manager", "bitwarden")

	rows := listAll(t, "pass", "items", "list", "--vault", name)
	if len(rows) != 3 {
		t.Fatalf("the import put %d items in %s, want 3", len(rows), name)
	}
	kinds := map[string]bool{}
	for _, row := range rows {
		m, _ := row.(map[string]interface{})
		kind, _ := m["type"].(string)
		kinds[kind] = true
	}
	for _, want := range []string{"login", "note", "credit-card"} {
		if !kinds[want] {
			t.Errorf("no %s came in: %v", want, kinds)
		}
	}

	login := itemInVault(t, name, name+"-login")
	shown := runOK(t, "pass", "items", "get", "--",
		fmt.Sprint(login["share_id"], "/", login["item_id"]))
	for _, want := range []string{"hunter2", "jane@example.com", "https://example.com", "Ticket"} {
		assertContains(t, shown, want)
	}
}

// A browser's export has no folders in it, and --vault says where it goes.
func TestPassImportChromeIntoVault(t *testing.T) {
	name := testID() + "-chrome"
	export := filepath.Join(t.TempDir(), "chrome.csv")
	body := "name,url,username,password,note\n" +
		name + ",https://example.com,jane@example.com,hunter2,a note\n"
	if err := os.WriteFile(export, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	into := importVault(t)
	runOK(t, "pass", "import", export, "--manager", "chrome", "--vault", into)

	row := itemInVault(t, into, name)
	if row["type"] != "login" || row["email"] != "jane@example.com" {
		t.Errorf("the login came back as %v", row)
	}

	// Without --manager the file is not one Proton Pass wrote, and the refusal
	// names the flag that would read it.
	_, stderr, code := run(t, "pass", "import", export)
	if code == 0 {
		t.Error("a Chrome export was read as a Proton Pass one")
	}
	assertContains(t, stderr, "--manager")
}

// importVault is a vault made for one import to land in, and taken away
// afterwards with everything the import put in it.
func importVault(t *testing.T) string {
	t.Helper()
	name := testID() + "-import"
	shareID := strings.TrimSpace(createVault(t, name))
	cleanupRun(t, fmt.Sprintf("Delete vault: proton pass vaults delete -- %s", shareID),
		"pass", "vaults", "delete", "--", shareID)
	return name
}

// itemInVault is one item of a vault, by name, once Pass is listing it.
//
// An import returns when Proton has taken the items, which is a moment before a
// listing of that vault has them - so reading one back is a race that waiting
// settles and retrying the whole test would not.
func itemInVault(t *testing.T, vault, name string) map[string]interface{} {
	t.Helper()
	var found map[string]interface{}
	if !waitFor(30*time.Second, time.Second, func() bool {
		for _, row := range listAll(t, "pass", "items", "list", "--vault", vault) {
			m, _ := row.(map[string]interface{})
			if n, _ := m["name"].(string); n == name {
				found = m
				return true
			}
		}
		return false
	}) {
		t.Fatalf("%s holds no item called %s", vault, name)
	}
	return found
}

// createItem makes an item and does not return until Proton will answer about
// it.
//
// Pass takes a write before every reader of it agrees the thing is there, so a
// test that creates an item and immediately updates, moves or reads it is racing
// a propagation it has no part in - and loses with a 422 saying the item does
// not exist. Waiting here rather than in each test is what keeps that race out
// of all of them.
func createItem(t *testing.T, args ...string) string {
	t.Helper()
	ref, _ := createItemReported(t, args...)
	return ref
}

// createItemReported is createItem for a test whose subject is what creating one
// says on the commentary stream - the password it made up, most of all.
func createItemReported(t *testing.T, args ...string) (ref, stderr string) {
	t.Helper()
	stdout, stderr := runOKStderr(t, append([]string{"pass", "items", "create"}, args...)...)
	ref = assertBarePairRef(t, stdout, "pass items create")
	if !waitFor(30*time.Second, time.Second, func() bool {
		_, _, code := run(t, "pass", "items", "get", "--", ref)
		return code == 0
	}) {
		t.Fatalf("pass never answered about the item it had just made: %s", ref)
	}
	return ref, stderr
}
