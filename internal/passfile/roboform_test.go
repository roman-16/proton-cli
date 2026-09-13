package passfile

import (
	"strings"
	"testing"
)

func roboformFile(t *testing.T) string {
	t.Helper()
	rows := []string{
		"\ufeffName,Url,MatchUrl,Login,Pwd,Note,Folder,RfFieldsV2",
		`Example,http://example.test,http://example.test,jane,'@secret,a note,work/logins,"User ID$,,,txt,jane","TOTP KEY$,,,txt,JBSWY3DPEHPK3PXP"`,
		"Bookmark,http://bookmark.test,,,,just a link,,",
		"A note,,,,,the note itself,,",
	}
	return written(t, "roboform.csv", []byte(strings.Join(rows, "\n")+"\n"))
}

func TestARoboFormExportIsRead(t *testing.T) {
	doc := opened(t, roboformFile(t), "roboform", nil)
	got := kinds(doc)
	if got["login"] != 2 || got["note"] != 1 {
		t.Errorf("read %v, want two logins and one note", got)
	}
}

// A value that starts with @ or a quote is written with a quote in front of it,
// which is the file's own escaping.
func TestARoboFormPasswordKeepsItsFirstCharacter(t *testing.T) {
	login := itemNamed(t, opened(t, roboformFile(t), "roboform", nil), "Example").
		Item.GetContent().GetLogin()
	if login.GetPassword() != "@secret" {
		t.Errorf("the password came back as %q", login.GetPassword())
	}
	if login.GetTotpUri() == "" {
		t.Error("the one-time code did not come back")
	}
}

// An address with nothing to sign in with is a bookmark, and still an item worth
// keeping.
func TestARoboFormBookmarkIsALogin(t *testing.T) {
	entry := itemNamed(t, opened(t, roboformFile(t), "roboform", nil), "Bookmark")
	if entry.Kind != "login" {
		t.Errorf("a bookmark came back as a %s", entry.Kind)
	}
	if got := LoginURLs(entry.Item.GetContent().GetLogin()); len(got) != 1 {
		t.Errorf("the URLs came back as %v", got)
	}
}

func TestARoboFormFolderBecomesAVault(t *testing.T) {
	doc := opened(t, roboformFile(t), "roboform", nil)
	if names := vaultNames(doc); names[0] != "logins" {
		t.Errorf("the vaults came back as %v", names)
	}
}
