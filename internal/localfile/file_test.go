package localfile

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSplitExtKeepsThePartThatNamesTheFormat(t *testing.T) {
	for _, tc := range []struct{ in, stem, ext string }{
		{"report.pdf", "report", ".pdf"},
		{"archive.tar.gz", "archive.tar", ".gz"},
		{"README", "README", ""},
		{".bashrc", ".bashrc", ""},
	} {
		stem, ext := splitExt(tc.in)
		if stem != tc.stem || ext != tc.ext {
			t.Errorf("splitExt(%q) = (%q, %q), want (%q, %q)", tc.in, stem, ext, tc.stem, tc.ext)
		}
	}
}

func TestSafeNameIsAcceptedOnEveryPlatform(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Invoice #2291 is ready", "Invoice #2291 is ready"},
		{"Re: budget/2026", "Re budget 2026"},
		{`a"b<c>d|e?f*g:h\i`, "a b c d e f g h i"},
		{"  collapsed   spaces  ", "collapsed spaces"},
		{"trailing dots...", "trailing dots"},
		{"", "download"},
		{"../../etc/passwd", ".. .. etc passwd"},
	} {
		if got := SafeName(tc.in); got != tc.want {
			t.Errorf("SafeName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if n := len(SafeName(strings.Repeat("x", 500))); n > 120 {
		t.Errorf("length not capped: %d", n)
	}
}

func TestFreeOfCountsPastWhatIsTaken(t *testing.T) {
	taken := map[string]bool{"a.txt": true, "a (2).txt": true}
	got, err := FreeOf("a.txt", func(p string) bool { return taken[p] })
	if err != nil {
		t.Fatal(err)
	}
	if got != "a (3).txt" {
		t.Errorf("got %q, want a (3).txt", got)
	}
}

func TestStreamStampsAndPublishesOnlyAFinishedPayload(t *testing.T) {
	dir := t.TempDir()
	when := time.Date(2026, 3, 12, 9, 41, 0, 0, time.UTC)
	done := filepath.Join(dir, "nested", "done.txt")
	if err := Stream(done, when, func(w io.Writer) error {
		_, err := io.WriteString(w, "whole")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !Holds(done, 5, when) {
		t.Error("the published file does not carry the size and time it was given")
	}
	failed := filepath.Join(dir, "failed.txt")
	if err := Stream(failed, when, func(w io.Writer) error {
		_, _ = io.WriteString(w, "half")
		return errors.New("stop")
	}); err == nil {
		t.Fatal("the producer's error was swallowed")
	}
	if Exists(failed) {
		t.Error("a failed payload was published")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "nested" {
		t.Errorf("the directory holds %v, want only what finished", entries)
	}
}

func TestHoldsComparesSizeAndTime(t *testing.T) {
	dir := t.TempDir()
	at := filepath.Join(dir, "f")
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := Publish(at, []byte("abc"), when); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		size int64
		when time.Time
		want bool
	}{
		{"the same", 3, when, true},
		{"within the two seconds FAT rounds to", 3, when.Add(time.Second), true},
		{"a size nobody recorded", -1, when, true},
		{"another size", 4, when, false},
		{"another time", 3, when.Add(time.Minute), false},
		{"no time", 3, time.Time{}, false},
	} {
		if got := Holds(at, tc.size, tc.when); got != tc.want {
			t.Errorf("%s: Holds = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestSweepRemovesWhatAnInterruptedPayloadLeft(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	left, err := Temp(filepath.Join(dir, "a"))
	if err != nil {
		t.Fatal(err)
	}
	_ = left.Close()
	kept := filepath.Join(dir, "a", "kept.txt")
	if err := os.WriteFile(kept, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Sweep(dir); err != nil {
		t.Fatal(err)
	}
	if Exists(left.Name()) {
		t.Error("the unfinished payload is still there")
	}
	if !Exists(kept) {
		t.Error("a finished file was swept")
	}
}

func TestOnlyOneHolderOfALock(t *testing.T) {
	at := filepath.Join(t.TempDir(), "lock")
	held, err := Claim(at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Claim(at); !errors.Is(err, ErrBusy) {
		t.Errorf("second claim = %v, want ErrBusy", err)
	}
	held.Release()
	again, err := Claim(at)
	if err != nil {
		t.Fatalf("claim after release: %v", err)
	}
	again.Release()
}

func TestRoomAnswersForAPathNotMadeYet(t *testing.T) {
	free, err := Room(filepath.Join(t.TempDir(), "not", "yet"))
	if err != nil {
		t.Fatal(err)
	}
	if free <= 0 {
		t.Errorf("Room = %d, want the free space of the nearest directory that exists", free)
	}
}
