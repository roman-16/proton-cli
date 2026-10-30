package export

import (
	"os"
	"path/filepath"
	"testing"
)

func openTest(t *testing.T) (*Copy, *Ledger) {
	t.Helper()
	c, err := Open(filepath.Join(t.TempDir(), "copy"), "user-1", "you@proton.me")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	l, err := c.Ledger(AppDrive)
	if err != nil {
		t.Fatal(err)
	}
	return c, l
}

func TestALedgerReadsBackWhatItRecorded(t *testing.T) {
	c, l := openTest(t)
	if err := l.Put(Entry{ID: "a", Version: "1", Path: "drive/a.txt", Want: "drive/a.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := l.Put(Entry{ID: "b", Version: "1"}); err != nil {
		t.Fatal(err)
	}
	if err := l.Forget("b"); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := c.Ledger(AppDrive)
	if err != nil {
		t.Fatal(err)
	}
	if e, ok := again.Get("a"); !ok || e.Version != "1" || e.Path != "drive/a.txt" {
		t.Errorf("a = %+v, %v", e, ok)
	}
	if _, ok := again.Get("b"); ok {
		t.Error("a forgotten entry came back")
	}
}

func TestALineCutShortIsPassedOver(t *testing.T) {
	c, l := openTest(t)
	if err := l.Put(Entry{ID: "a", Version: "1"}); err != nil {
		t.Fatal(err)
	}
	_ = l.file.Close()
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"id":"b","vers`); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	again, err := c.Ledger(AppDrive)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := again.Get("a"); !ok {
		t.Error("the whole line before the cut was lost")
	}
	if _, ok := again.Get("b"); ok {
		t.Error("a torn line was read as an entry")
	}
}

func TestPlaceKeepsTwoThingsApartWhoseNamesDifferOnlyInCase(t *testing.T) {
	_, l := openTest(t)
	first, _, err := l.Place("a", "drive/Report.pdf")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := l.Place("b", "drive/report.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if first != "drive/Report.pdf" || second != "drive/report (2).pdf" {
		t.Errorf("placed at %q and %q", first, second)
	}
	again, _, err := l.Place("b", "drive/report.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if again != second {
		t.Errorf("a second run moved b to %q", again)
	}
}

func TestPlaceLeavesAFileOfYoursAlone(t *testing.T) {
	c, l := openTest(t)
	mine := c.Path("drive/notes.txt")
	if err := os.MkdirAll(filepath.Dir(mine), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mine, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, _, err := l.Place("a", "drive/notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got != "drive/notes (2).txt" {
		t.Errorf("placed at %q, over a file the export did not write", got)
	}
}

func TestPlaceSaysWhereAMovedThingWas(t *testing.T) {
	c, l := openTest(t)
	now, _, err := l.Place("a", "drive/old/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(c.Path(now)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.Path(now), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := l.Put(Entry{ID: "a", Version: "r1", Path: now, Want: "drive/old/a.txt"}); err != nil {
		t.Fatal(err)
	}
	moved, was, err := l.Place("a", "drive/new/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if moved != "drive/new/a.txt" || was != "drive/old/a.txt" {
		t.Errorf("Place = %q from %q", moved, was)
	}
	if e, _ := l.Get("a"); e.Version != "r1" {
		t.Errorf("the version was dropped on the move: %+v", e)
	}
}
