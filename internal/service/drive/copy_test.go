package drive

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/roman-16/proton-cli/internal/export"
	"github.com/roman-16/proton-cli/internal/localfile"
)

func TestAFileInTheCopyStaysFollowsOrIsOwed(t *testing.T) {
	c, err := export.Open(filepath.Join(t.TempDir(), "copy"), "user-1", "you@proton.me")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	l, err := c.Ledger(export.AppDrive)
	if err != nil {
		t.Fatal(err)
	}
	was := "drive/My files/Docs/a.txt"
	if err := os.MkdirAll(filepath.Dir(c.Path(was)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.Path(was), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := l.Put(export.Entry{ID: "link-1", Version: "rev-1", Path: was, Want: was}); err != nil {
		t.Fatal(err)
	}
	p := copyPart{}

	if got, err := p.held(c, l, "link-1", "rev-1", was); err != nil || got != stays {
		t.Errorf("an unchanged file = %v, %v; want it to stay", got, err)
	}
	if got, err := p.held(c, l, "link-1", "rev-2", was); err != nil || got != owes {
		t.Errorf("a file with a new revision = %v, %v; want it owed", got, err)
	}
	moved := "drive/My files/Archive/a.txt"
	if got, err := p.held(c, l, "link-1", "rev-1", moved); err != nil || got != follows {
		t.Errorf("a moved file = %v, %v; want it to follow", got, err)
	}
	if localfile.Exists(c.Path(was)) || !localfile.Exists(c.Path(moved)) {
		t.Error("the file did not move with it")
	}
	if got, err := p.held(c, l, "link-2", "rev-1", "drive/My files/b.txt"); err != nil || got != owes {
		t.Errorf("a file the copy never held = %v, %v; want it owed", got, err)
	}
}

func TestATreeLandsUnderItsOwnTop(t *testing.T) {
	if got := within(copyFiles, "/Docs/Re: budget/a.txt"); got != "drive/My files/Docs/Re budget/a.txt" {
		t.Errorf("within = %q", got)
	}
}
