package live

import (
	"encoding/json"
	"fmt"
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

	stdout := runOK(t, "pass", "items", "create",
		"--type", "login",
		"--name", name,
		"--username", "tester",
		"--secret-file", secretFile(t, "password", "s3cret!"),
		"--url", url)
	itemID := strings.TrimSpace(stdout)
	if !looksLikePairRef(itemID) {
		t.Fatalf("expected SHARE_ID/ITEM_ID on stdout, got %q", stdout)
	}
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
	stdout, stderr := runOKStderr(t, "pass", "items", "create", "--name", name,
		"--username", "tester", "--generate-password", "--words", "4")
	ref := strings.TrimSpace(stdout)
	if !looksLikePairRef(ref) {
		t.Fatalf("expected SHARE_ID/ITEM_ID on stdout, got %q", stdout)
	}
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
	assertBarePairRef(t, runOK(t, "pass", "items", "create",
		"--type", "note",
		"--name", name,
		"--note", "secret note content"), "pass items create")
	cleanupRun(t, fmt.Sprintf("Delete note: proton pass items delete %s", name),
		"pass", "items", "delete", name)

	got := runOK(t, "pass", "items", "get", name)
	assertField(t, got, "Type:", "note")
	assertField(t, got, "Note:", "secret note content")
}

func TestPassItemsCreateCardShowsPIN(t *testing.T) {
	name := testID() + "-card"
	stdout := runOK(t, "pass", "items", "create",
		"--type", "credit-card",
		"--name", name,
		"--holder", "Test Holder",
		"--expiry", "2029-01",
		"--secret-file", secretFile(t, "number", "4111111111111111"),
		"--secret-file", secretFile(t, "cvv", "123"),
		"--secret-file", secretFile(t, "pin", "7890"))
	id := strings.TrimSpace(stdout)
	if !looksLikeID(id) {
		t.Fatalf("expected bare ID on stdout, got %q", stdout)
	}
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
	ref := strings.TrimSpace(runOK(t, "pass", "items", "create", "--type", "credit-card",
		"--name", name, "--holder", "Roman", "--expiry", "2030-01",
		"--secret-file", secretFile(t, "number", "4111111111111111")))
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
	stdout := runOK(t, "pass", "items", "create",
		"--type", "login", "--name", name,
		"--username", "u", "--secret-file", secretFile(t, "password", "p"))
	// Creating answers with SHARE_ID/ITEM_ID, which is the reference every item
	// verb takes - and the only way to reach a trashed item, since searching by
	// name does not find one.
	ref := strings.TrimSpace(stdout)
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
	idRef := strings.TrimSpace(runOK(t, "pass", "items", "create", "--type", "identity",
		"--name", idName, "--full-name", "Jane Roe", "--email", "jane@example.com",
		"--organization", "Acme", "--field", "Note=hello-field",
		"--secret-file", secretFile(t, "PIN", "4321")))
	cleanupRun(t, fmt.Sprintf("Delete pass item: proton pass items delete %s", idRef),
		"pass", "items", "delete", "--", idRef)
	gotID := runOK(t, "pass", "items", "get", "--", idRef)
	assertContains(t, gotID, "Jane Roe")
	assertContains(t, gotID, "Acme")
	assertContains(t, gotID, "hello-field")

	// Wi-Fi.
	wifiRef := strings.TrimSpace(runOK(t, "pass", "items", "create", "--type", "wifi",
		"--name", testID()+"-wifi", "--ssid", "MyTestNet", "--security", "WPA2",
		"--secret-file", secretFile(t, "password", "pw")))
	cleanupRun(t, fmt.Sprintf("Delete pass item: proton pass items delete %s", wifiRef),
		"pass", "items", "delete", "--", wifiRef)
	assertContains(t, runOK(t, "pass", "items", "get", "--", wifiRef), "MyTestNet")

	// SSH key.
	sshRef := strings.TrimSpace(runOK(t, "pass", "items", "create", "--type", "ssh-key",
		"--name", testID()+"-ssh", "--public-key", "ssh-ed25519 AAAATESTKEY",
		"--secret-file", secretFile(t, "private-key", "PRIVATE-TEST")))
	cleanupRun(t, fmt.Sprintf("Delete pass item: proton pass items delete %s", sshRef),
		"pass", "items", "delete", "--", sshRef)
	assertContains(t, runOK(t, "pass", "items", "get", "--", sshRef), "ssh-ed25519 AAAATESTKEY")
}

func TestPassLoginTOTPRoundTrips(t *testing.T) {
	name := testID() + "-totp"
	secret := "JBSWY3DPEHPK3PXP"
	ref := strings.TrimSpace(runOK(t, "pass", "items", "create", "--type", "login",
		"--name", name, "--username", "me@example.com",
		"--secret-file", secretFile(t, "totp-uri", "otpauth://totp/Example:me?secret="+secret+"&issuer=Example")))
	cleanupRun(t, fmt.Sprintf("Delete pass item: proton pass items delete %s", ref),
		"pass", "items", "delete", "--", ref)

	assertContains(t, runOK(t, "pass", "items", "get", "--", ref), secret)
}

// Pass keeps every edit, so a password changed by mistake can be read back.
func TestPassItemRevisionsShowWhatItUsedToBe(t *testing.T) {
	name := testID() + "-history"
	ref := strings.TrimSpace(runOK(t, "pass", "items", "create",
		"--name", name, "--username", "first",
		"--secret-file", secretFile(t, "password", "first-secret")))
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

// An item is sealed under the key of the vault it is in, so moving it means
// sealing it again under another's. It keeps what it holds and is given a new
// ID, because an item in Pass is only unique together with its vault.
func TestPassItemsMoveBetweenVaults(t *testing.T) {
	name := testID() + "-move"
	ref := strings.TrimSpace(runOK(t, "pass", "items", "create", "--name", name,
		"--username", "tester", "--secret-file", secretFile(t, "password", "travels-with-it")))
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
	ref := strings.TrimSpace(runOK(t, "pass", "items", "create",
		"--name", name, "--username", "someone"))
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
	ref := strings.TrimSpace(runOK(t, "pass", "items", "create",
		"--name", name, "--username", "someone",
		"--secret-file", secretFile(t, "totp-uri",
			"otpauth://totp/Example:someone?secret="+secret+"&issuer=Example")))
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
	ref := strings.TrimSpace(runOK(t, "pass", "items", "create",
		"--name", name, "--username", "someone"))
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
	ref := strings.TrimSpace(runOK(t, "pass", "items", "create", "--type", "custom", "--name", name,
		"--field", "Network/SSID=home", "--secret-file", secretFile(t, "Network/Key", "hunter2"),
		"--field", "Admin/URL=http://192.168.0.1", "--field", "Loose=1"))
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
	ref := strings.TrimSpace(runOK(t, "pass", "items", "create", "--name", name,
		"--username", "jane", "--url", "https://example.com",
		"--secret-file", secretFile(t, "password", "hunter2"),
		"--field", "Recovery codes=abc-def"))
	cleanupRun(t, fmt.Sprintf("Delete item: proton pass items delete %s", ref),
		"pass", "items", "delete", "--", ref)

	dir := t.TempDir()
	archive := filepath.Join(dir, "backup.zip")
	runOK(t, "pass", "export", "--dest", archive)

	raw, err := os.ReadFile(archive)
	if err != nil {
		t.Fatalf("read the archive: %v", err)
	}
	// Without a passphrase the archive is readable, which is what the warning
	// says and what makes this assertion possible at all.
	if !strings.Contains(string(raw), "Proton Pass/data.json") {
		t.Errorf("the archive does not hold the file Proton Pass looks for")
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
	for _, row := range runJSONArray(t, "pass", "items", "list") {
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

// The secondary account's Pass is protected with an extra password, which is how
// this suite covers the feature at all: the sign-in TestMain performs hands it
// over, and Proton then lets that session reach Pass.
//
// What a run can check is that outcome, not the exchange behind it. Proton grants
// the scope for the life of the session and offers nothing that takes it back, so
// the exchange happens once per session - see `unreachable` in
// internal/cli/coverage_test.go, which says so and why.
//
// The first assertion is the one that earns the test: it fails if the account is
// ever left without an extra password, because the coverage would otherwise be
// gone with nothing saying so.
func TestPassExtraPasswordProtectsTheSecondaryAccount(t *testing.T) {
	srp := runJSONSecondary(t, "api", "GET", "/pass/v1/user/srp")
	if has, _ := srp["HasSRP"].(bool); !has {
		t.Fatal("the secondary account has no extra password, so nothing here covers unlocking Pass with one")
	}

	scopes := runJSONSecondary(t, "api", "GET", "/core/v4/auth/scopes")
	held, _ := scopes["Scopes"].([]interface{})
	unlocked := false
	for _, s := range held {
		if name, _ := s.(string); name == "pass" {
			unlocked = true
		}
	}
	if !unlocked {
		t.Errorf("the session holds %v, and none of it is the Pass scope the extra password buys", held)
	}

	// And what the scope is for.
	assertContains(t, runOKSecondary(t, "pass", "vaults", "list"), "ID")
}
