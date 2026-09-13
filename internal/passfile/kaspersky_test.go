package passfile

import (
	"strings"
	"testing"
)

const kasperskyExport = `Websites

Website name: Proton
Website URL: https://account.proton.me
Login name: Personal
Login: jane@example.test
Password: proton123
Comment: a note

---

Applications

Application: Proton Pass.app
Login name: App login
Login: jane
Password: apppass
Comment:

---

Other Accounts

Account name: New account
Login name:
Login: someone
Password:
Comment:

---

Notes

Name: A note
Text: line 1
line 2
Password: this is part of the note

---
`

func kasperskyFile(t *testing.T) string {
	t.Helper()
	return written(t, "kaspersky.txt", []byte(kasperskyExport))
}

func TestAKasperskyExportIsRead(t *testing.T) {
	doc := opened(t, kasperskyFile(t), "kaspersky", nil)
	got := kinds(doc)
	if got["login"] != 3 || got["note"] != 1 {
		t.Errorf("read %v, want three logins and one note", got)
	}
}

// A website's item is called after the account, and an application's after the
// application and the account together.
func TestAKasperskyItemIsNamed(t *testing.T) {
	doc := opened(t, kasperskyFile(t), "kaspersky", nil)
	itemNamed(t, doc, "Personal")
	itemNamed(t, doc, "Proton Pass.app App login")
	itemNamed(t, doc, "New account")
}

func TestAKasperskyLoginIsRead(t *testing.T) {
	login := itemNamed(t, opened(t, kasperskyFile(t), "kaspersky", nil), "Personal")
	content := login.Item.GetContent().GetLogin()
	if content.GetItemEmail() != "jane@example.test" || content.GetPassword() != "proton123" {
		t.Errorf("the login came back as %v", content)
	}
	if got := LoginURLs(content); len(got) != 1 || got[0] != "https://account.proton.me" {
		t.Errorf("the URLs came back as %v", got)
	}
	if got := login.Item.GetMetadata().GetNote(); got != "a note" {
		t.Errorf("the note came back as %q", got)
	}
}

// Once a note has begun, every line belongs to it - a note may itself look like
// the labels around it.
func TestAKasperskyNoteKeepsItsLines(t *testing.T) {
	note := itemNamed(t, opened(t, kasperskyFile(t), "kaspersky", nil), "A note")
	got := note.Item.GetMetadata().GetNote()
	for _, want := range []string{"line 1", "line 2", "Password: this is part of the note"} {
		if !strings.Contains(got, want) {
			t.Errorf("the note leaves out %q: %q", want, got)
		}
	}
}

func TestSomethingThatIsNotAKasperskyExportIsRefused(t *testing.T) {
	path := written(t, "notes.txt", []byte("just some text\nand more of it\n"))
	if _, err := Open(path, "kaspersky", nil); err == nil {
		t.Error("a text file was read as a Kaspersky export")
	}
}
