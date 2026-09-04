package live

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/tests/account"
	"github.com/roman-16/proton-cli/tests/fixture"
)

// What an account holds for the suite to read, brought about when something
// asks for it.
//
// Nothing is made up front, so a run that touches no aliases makes no alias and
// one test costs one lookup rather than a whole account being filled first.
// Asking twice in a run costs nothing: each fixture is found or made once and
// remembered.
//
// Only what the suite never changes belongs here. A listing remembered from
// before another test changed the thing would be a false pass, which is worse
// than a slow run - a test that mutates something makes its own.

// suiteRunner is how the fixture package runs the CLI as one account.
var suiteRunner fixture.Runner = func(profile string, args ...string) (string, error) {
	out, stderr, code, err := runAs(profile, nil, args...)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("exit %d: %s", code, truncateOutput(stderr))
	}
	return out, nil
}

var (
	fixturesMu sync.Mutex
	fixtures   = map[string]*sync.Once{}
	fixtureRow = map[string]map[string]any{}
	fixtureErr = map[string]error{}
)

// pinned returns the row for one of the fixture's pins on one account, making it
// if the account may be written to and has not got it.
func pinned(t *testing.T, profile, what, name string) map[string]any {
	t.Helper()
	row, err := ensurePinned(profile, what, name)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return row
}

// ensurePinned is the same without a *testing.T, so TestMain can settle the
// fixtures an account must already hold before anything reads it - and get the
// declaration's own message rather than a listing that failed for no stated
// reason.
//
// The two accounts kept for the suite declare fixtures with a way to make them;
// the paid account declares one without, so a run that cannot find it says what
// to run by hand. One accessor reads either, because which of the two it is is a
// property of the declaration rather than of the caller.
func ensurePinned(profile, what, name string) (map[string]any, error) {
	c, p, ok := declaredPin(profile, what, name)
	if !ok {
		return nil, fmt.Errorf("the %s account's fixture declares no %s called %q", profile, what, name)
	}
	key := profile + "/" + what + "/" + name

	fixturesMu.Lock()
	once, seen := fixtures[key]
	if !seen {
		once = &sync.Once{}
		fixtures[key] = once
	}
	fixturesMu.Unlock()

	once.Do(func() {
		list, err := fixture.Rows(suiteRunner, profile, c.List...)
		if err == nil {
			// What an interrupted run left behind is cleared from the listing
			// this had to make anyway, so hygiene costs nothing where a fixture
			// is wanted. A collection with no way to remove a row - the paid
			// account's - is swept over without a request being sent.
			fixture.Sweep(suiteRunner, profile, c, list)
		}
		var row map[string]any
		if err == nil {
			row, err = fixture.Ensure(suiteRunner, profile, c, p, list)
		}
		fixturesMu.Lock()
		fixtureRow[key], fixtureErr[key] = row, err
		fixturesMu.Unlock()
	})

	fixturesMu.Lock()
	defer fixturesMu.Unlock()
	return fixtureRow[key], fixtureErr[key]
}

// requirePaidFixtures settles what the paid account has to hold already, before
// the photograph is taken or any test runs.
func requirePaidFixtures() {
	for _, c := range fixture.Paid() {
		for _, p := range c.Pins {
			if _, err := ensurePinned(account.Paid, c.What, p.ID); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				os.Exit(1)
			}
		}
	}
}

// declaredPin finds a pin in the declaration for that account, so a test names
// what it wants rather than repeating how to make it.
func declaredPin(profile, what, name string) (fixture.Collection, fixture.Pin, bool) {
	declared := fixture.Free("")
	if profile == account.Paid {
		declared = fixture.Paid()
	}
	for _, c := range declared {
		if c.What != what {
			continue
		}
		if p, ok := c.Pin(name); ok {
			return c, p, true
		}
	}
	return fixture.Collection{}, fixture.Pin{}, false
}

// ── aliases ──

// alias is the Pass alias on the primary account that the suite reads rather
// than makes: making one is what Proton meters hardest here, a handful an hour
// against several tests that each want one.
func alias(t *testing.T) (ref, address string) {
	t.Helper()
	row := pinned(t, account.Primary, "pass item", fixture.AliasName)
	share, _ := row["share_id"].(string)
	id, _ := row["item_id"].(string)
	addr, _ := row["alias"].(string)
	return share + "/" + id, addr
}

// paidAlias is the alias on the paid account, which somebody created once by
// hand. Alias contacts need a subscription and an alias address cannot be
// un-minted, so the suite hangs contacts off this one and never makes another.
func paidAlias(t *testing.T) (ref, address string) {
	t.Helper()
	row := pinned(t, account.Paid, "Pass alias", fixture.PaidAlias)
	share, _ := row["share_id"].(string)
	id, _ := row["item_id"].(string)
	addr, _ := row["alias"].(string)
	return share + "/" + id, addr
}

// ── mail ──
//
// Most mail tests need *a* delivered message of a particular shape rather than a
// freshly sent one. The accounts are on the free plan, which allows fifty
// messages an hour, and that - not the wall clock - is what decides how often
// the suite can run. So a message is only sent when the sending is the thing
// being tested; everything else reads one the account already holds.

// seeded is a fixture message as the suite needs it: what to address, what
// thread it is in, and - for the one carrying attachments - which attachment to
// act on.
type seeded struct {
	msgID   string
	convID  string
	subject string
	attID   string
	attName string
}

// findMail locates one fixture message, sending it if the account has not got
// it, and reads whatever else a test will ask of it.
//
// It sends rather than failing because a fixture is brought about when something
// needs it: a run that reads no mail sends none, and an account that has never
// been seeded fills itself as the tests that need it ask.
func findMail(m fixture.Mail) (seeded, error) {
	id := messageIDInFolder("inbox", m.Subject)
	if id == "" {
		if err := deliver(m); err != nil {
			return seeded{}, err
		}
		id = messageIDInFolder("inbox", m.Subject)
	}
	if id == "" {
		return seeded{}, fmt.Errorf("the inbox holds no message subject %q and one could not be sent", m.Subject)
	}
	s := seeded{msgID: id, subject: m.Subject, convID: conversationIDOf(id)}
	if m.Attach == "" {
		return s, nil
	}
	out, _, code, err := runArgs(nil, "--output", "json", "mail", "messages", "attachments", "list", id)
	if err != nil || code != 0 {
		return seeded{}, fmt.Errorf("list the attachments of %q (exit %d): %v", m.Subject, code, err)
	}
	var env struct {
		Attachments []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"attachments"`
	}
	if json.Unmarshal([]byte(out), &env) != nil || len(env.Attachments) == 0 {
		return seeded{}, fmt.Errorf("the fixture message %q carries no attachment", m.Subject)
	}
	// The listing leaves inline parts out by default, so this is the regular one.
	s.attID, s.attName = env.Attachments[0].ID, env.Attachments[0].Name
	return s, nil
}

// deliver sends one of the fixture's messages and waits for it to arrive.
func deliver(m fixture.Mail) error {
	send := []string{"mail", "messages", "send", "--to", selfEmail(), "--subject", m.Subject, "--body", m.Body}
	if m.HTML {
		send = append(send, "--html")
	}
	for flag, name := range map[string]string{"--attach": m.Attach, "--attach-inline": m.Inline} {
		if name == "" {
			continue
		}
		path, err := fixtureFile(name)
		if err != nil {
			return err
		}
		send = append(send, flag, path)
	}
	if _, stderr, code, err := runArgs(nil, send...); err != nil || code != 0 {
		return fmt.Errorf("send the %q fixture (exit %d): %v: %s", m.Subject, code, err, stderr)
	}
	if !waitFor(90*time.Second, 3*time.Second, func() bool { return messageIDInFolder("inbox", m.Subject) != "" }) {
		return fmt.Errorf("the %q fixture was sent and has not arrived", m.Subject)
	}
	return nil
}

// fixtureFile writes one of the files the fixture attaches or uploads, once per
// run, and says where it is. The fixture declares their contents rather than a
// path, so the suite and the seed write the same bytes wherever each needs them.
var fixtureDir = sync.OnceValues(func() (string, error) { return os.MkdirTemp("", "proton-cli-fixture-*") })

func fixtureFile(name string) (string, error) {
	dir, err := fixtureDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	body := fixture.Files()[name]
	if body == "" {
		// Some bulk, so a listing shows a size worth reading.
		body = strings.Repeat("proton-cli\n", 4000)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// The lookups happen at most once per run, and only if something asks.
var (
	plainFixture       = sync.OnceValues(func() (seeded, error) { return findMail(fixture.Plain) })
	quotedFixture      = sync.OnceValues(func() (seeded, error) { return findMail(fixture.Quoted) })
	attachmentsFixture = sync.OnceValues(func() (seeded, error) { return findMail(fixture.Attachments) })
)

func fixtureOr(t *testing.T, load func() (seeded, error)) seeded {
	t.Helper()
	s, err := load()
	if err != nil {
		t.Fatalf("mail fixture: %v", err)
	}
	return s
}

// plainMail is a delivered self-mail with a plain body: no quote markers, no
// attachments, and a body that contains its own subject. Read-only.
func plainMail(t *testing.T) (msgID, convID, subject string) {
	t.Helper()
	s := fixtureOr(t, plainFixture)
	return s.msgID, s.convID, s.subject
}

// quotedMail is a delivered self-mail whose body carries the canonical
// "On <date>, <name> <addr> wrote:" reply block. Read-only.
func quotedMail(t *testing.T) (msgID, subject string) {
	t.Helper()
	s := fixtureOr(t, quotedFixture)
	return s.msgID, s.subject
}

// attachedMail is a delivered self-mail carrying one regular attachment and one
// inline image, plus the regular attachment's ID and name.
//
// It is one message because a mail with an inline image and an attachment is one
// shape rather than two - which is what the tests about telling the two
// dispositions apart need. Read-only.
func attachedMail(t *testing.T) (msgID, convID, attID, attName string) {
	t.Helper()
	s := fixtureOr(t, attachmentsFixture)
	return s.msgID, s.convID, s.attID, s.attName
}

// ── the mutable pool ──
//
// A test that marks, stars, moves or trashes a message needs one it may change,
// not a freshly sent one: what it proves is that the change happens and can be
// undone, and a fixture message proves that exactly as well. The pool hands them
// out one at a time, so two tests never change the same message.
//
// A test that finishes as it should leaves its message as it found it. One that
// fails may not have got that far, so the state is put back here rather than
// trusted - otherwise the next run would find the message somewhere it does not
// look for it, and send another.
var mutablePool = func() chan fixture.Mail {
	pool := make(chan fixture.Mail, len(fixture.Mutable))
	for _, m := range fixture.Mutable {
		pool <- m
	}
	return pool
}()

func mutableMail(t *testing.T) string {
	t.Helper()
	m := <-mutablePool
	id := messageIDInFolder("inbox", m.Subject)
	if id == "" {
		if err := deliver(m); err != nil {
			mutablePool <- m
			t.Fatalf("mail fixture: %v", err)
		}
		id = messageIDInFolder("inbox", m.Subject)
	}
	t.Cleanup(func() {
		if t.Failed() && id != "" {
			for _, args := range [][]string{
				{"mail", "messages", "move", "--into", "inbox", "--", id},
				{"mail", "messages", "unstar", "--", id},
				{"mail", "messages", "mark", "read", "--", id},
			} {
				_, _, _, _ = runArgs(nil, consenting(args)...)
			}
		}
		mutablePool <- m
	})
	if id == "" {
		t.Fatalf("the inbox holds no message subject %q", m.Subject)
	}
	return id
}

// ── looking mail up ──

// messageIDInFolder returns the ID of the first message in one of the primary
// account's folders whose subject matches exactly, or "". It takes no
// *testing.T, so the fixtures and cleanups that run outside a test can call it.
func messageIDInFolder(folder, subject string) string {
	return messageIDInFolderAs(account.Primary, folder, subject)
}

// messageIDInFolderAs is the same for any account.
//
// It reads the listing rather than the search index, because a message sent
// moments ago is in the mailbox before it is findable in the index. A page is
// the largest Proton allows, so a fixture is found however much else the account
// holds.
func messageIDInFolderAs(profile, folder, subject string) string {
	stdout, _, code, err := runAs(profile, nil, "--output", "json", "mail", "messages", "list",
		"--folder", folder, "--page-size", "150")
	if err != nil || code != 0 {
		return ""
	}
	var data struct {
		Messages []struct {
			ID      string `json:"id"`
			Subject string `json:"subject"`
		} `json:"messages"`
	}
	if json.Unmarshal([]byte(stdout), &data) != nil {
		return ""
	}
	for _, m := range data.Messages {
		if m.Subject == subject {
			return m.ID
		}
	}
	return ""
}

// conversationIDOf is the thread a message belongs to, or "" on failure.
func conversationIDOf(msgID string) string {
	stdout, _, code, err := runArgs(nil, "--output", "json", "mail", "messages", "get", msgID)
	if err != nil || code != 0 {
		return ""
	}
	var v struct {
		ConversationID string `json:"conversation_id"`
	}
	if json.Unmarshal([]byte(stdout), &v) != nil {
		return ""
	}
	return v.ConversationID
}

// findMessage polls one of the primary account's folders for a message with
// this subject.
func findMessage(t *testing.T, folder, subject string) string {
	t.Helper()
	return findMessageAs(t, account.Primary, folder, subject)
}

func findMessageAs(t *testing.T, profile, folder, subject string) string {
	t.Helper()
	var id string
	waitFor(25*time.Second, 750*time.Millisecond, func() bool {
		id = messageIDInFolderAs(profile, folder, subject)
		return id != ""
	})
	return id
}

// ── sending ──

// sendTestMail sends a mail to self, waits for delivery, registers cleanup for
// the sent and inbox copies, and returns the inbox message ID.
//
// Use it only when a test exercises the send path itself or needs a parent whose
// flags it will change: a fixture message would already be flagged and the
// assertion would pass for the wrong reason.
func sendTestMail(t *testing.T, subject string) string {
	t.Helper()
	return sentToSelf(t, account.Primary, subject)
}

// sentToSelfPaid is the same on the paid account, which is how a paid test gets
// a message of its own to act on: it acts only on what it made, and both copies
// are deleted afterwards.
func sentToSelfPaid(t *testing.T, subject string) string {
	t.Helper()
	return sentToSelf(t, account.Paid, subject)
}

func sentToSelf(t *testing.T, profile, subject string) string {
	t.Helper()
	address := accounts[profile].Address()
	runOKProfile(t, profile, "mail", "messages", "send",
		"--to", address, "--subject", subject, "--body", "Integration test body: "+subject)
	if sentID := findMessageAs(t, profile, "sent", subject); sentID != "" {
		cleanupAs(t, profile, "Delete sent mail: proton mail messages delete "+sentID,
			"mail", "messages", "delete", "--", sentID)
	}
	inboxID := findMessageAs(t, profile, "inbox", subject)
	if inboxID == "" {
		t.Fatalf("mail %q was not delivered to the %s account", subject, profile)
	}
	cleanupAs(t, profile, "Delete inbox mail: proton mail messages delete "+inboxID,
		"mail", "messages", "delete", "--", inboxID)
	return inboxID
}

// sendTestMailSecondary sends a message from the second account to the primary,
// so a watch on the primary sees a genuine arrival.
func sendTestMailSecondary(t *testing.T, subject string) string {
	t.Helper()
	runOKSecondary(t, "mail", "messages", "send",
		"--to", selfEmail(), "--subject", subject, "--body", "Integration test body: "+subject)
	inboxID := findMessage(t, "inbox", subject)
	if inboxID == "" {
		t.Fatalf("secondary send %q never reached the primary's inbox", subject)
	}
	cleanupRun(t, "Delete inbox mail: proton mail messages delete "+inboxID,
		"mail", "messages", "delete", "--", inboxID)
	return inboxID
}

// secondaryMailContaining finds an inbox message on the second account from
// `from` whose decrypted body contains `needle`, returning its ID or "".
func secondaryMailContaining(t *testing.T, from, needle string) string {
	t.Helper()
	list := runJSONSecondary(t, "mail", "messages", "list", "--folder", "inbox", "--page-size", "20")
	msgs, _ := list["messages"].([]interface{})
	for _, m := range msgs {
		mm := m.(map[string]interface{})
		if addr, _ := mm["from_address"].(string); addr != from {
			continue
		}
		id, _ := mm["id"].(string)
		if body, _, code := runSecondary(t, "mail", "messages", "get", "--body-only", id); code == 0 &&
			strings.Contains(body, needle) {
			return id
		}
	}
	return ""
}
