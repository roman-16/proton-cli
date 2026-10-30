package kit

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/internal/localfile"
)

func TestDestinationCollisionPolicy(t *testing.T) {
	t.Run("an explicit path is refused when it exists", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "report.pdf")
		write(t, path, "original")

		d := &Destination{dest: path}
		_, err := d.Write(nil, "ignored", []byte("new"))
		if err == nil {
			t.Fatal("want a refusal, got none")
		}
		if !strings.Contains(err.Error(), "already exists") {
			t.Errorf("unhelpful message: %v", err)
		}
		if got := read(t, path); got != "original" {
			t.Errorf("the existing file was touched: %q", got)
		}
	})

	t.Run("--force replaces it", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "report.pdf")
		write(t, path, "original")

		d := &Destination{dest: path, force: true}
		got, err := d.Write(nil, "ignored", []byte("new"))
		if err != nil {
			t.Fatal(err)
		}
		if got != path {
			t.Errorf("wrote to %q, want %q", got, path)
		}
		if read(t, path) != "new" {
			t.Error("--force should have replaced the contents")
		}
	})

	t.Run("a chosen name gets a suffix instead", func(t *testing.T) {
		dir := t.TempDir()
		write(t, filepath.Join(dir, "report.pdf"), "first")

		d := &Destination{destDir: dir}
		got, err := d.Write(nil, "report.pdf", []byte("second"))
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(dir, "report (2).pdf")
		if got != want {
			t.Errorf("wrote to %q, want %q", got, want)
		}
		if read(t, filepath.Join(dir, "report.pdf")) != "first" {
			t.Error("the first file should be untouched")
		}
	})

	t.Run("suffixes keep counting", func(t *testing.T) {
		dir := t.TempDir()
		write(t, filepath.Join(dir, "a.txt"), "1")
		write(t, filepath.Join(dir, "a (2).txt"), "2")

		d := &Destination{destDir: dir}
		got, err := d.Write(nil, "a.txt", []byte("3"))
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(dir, "a (3).txt"); got != want {
			t.Errorf("wrote to %q, want %q", got, want)
		}
	})
}

func TestValidateRejectsImpossibleCombinations(t *testing.T) {
	t.Run("both destinations", func(t *testing.T) {
		d := &Destination{dest: "a", destDir: "b"}
		if err := d.Validate(true); err == nil {
			t.Error("want a refusal")
		}
	})
	t.Run("one file for many items", func(t *testing.T) {
		d := &Destination{dest: "a"}
		if err := d.Validate(false); err == nil {
			t.Error("want a refusal")
		}
	})
	t.Run("stdout for many items", func(t *testing.T) {
		d := &Destination{dest: "-"}
		if err := d.Validate(false); err == nil {
			t.Error("several files down one stream is one unusable run of bytes")
		}
	})
	t.Run("stdout for one item", func(t *testing.T) {
		d := &Destination{dest: "-"}
		if err := d.Validate(true); err != nil {
			t.Errorf("one item is exactly what a stream carries: %v", err)
		}
	})
}

func TestDirCreatesTheDestination(t *testing.T) {
	nested := filepath.Join(t.TempDir(), "pics", "2026")
	d := &Destination{destDir: nested}

	got, err := d.Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if got != nested {
		t.Errorf("got %q want %q", got, nested)
	}
	if fi, err := os.Stat(nested); err != nil || !fi.IsDir() {
		t.Errorf("%s was not created: %v", nested, err)
	}
	if _, err := os.CreateTemp(got, ".probe-*"); err != nil {
		t.Errorf("a temporary file cannot be opened there: %v", err)
	}
}

func TestDirRefusesAPathThatIsAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-dir")
	write(t, path, "x")

	if _, err := (&Destination{destDir: path}).Dir(); err == nil {
		t.Fatal("want an error for a destination that is a file")
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestStreamCommitsOnlyAFinishedPayload(t *testing.T) {
	dir := t.TempDir()

	target := filepath.Join(dir, "mail.mbox")
	d := &Destination{dest: target}
	got, err := d.Stream(nil, "ignored", func(w io.Writer) error {
		if _, err := io.WriteString(w, "first "); err != nil {
			return err
		}
		_, err := io.WriteString(w, "second")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != target || read(t, target) != "first second" {
		t.Errorf("Stream wrote %q to %q", read(t, target), got)
	}

	failed := filepath.Join(dir, "failed.mbox")
	d = &Destination{dest: failed}
	if _, err := d.Stream(nil, "ignored", func(w io.Writer) error {
		_, _ = io.WriteString(w, "partial")
		return errors.New("stop")
	}); err == nil {
		t.Fatal("Stream ignored the producer's error")
	}
	if localfile.Exists(failed) {
		t.Error("Stream left a partial file at the destination")
	}
	if names := entryNames(t, dir); len(names) != 1 || names[0] != "mail.mbox" {
		t.Errorf("the directory holds %v, want only the finished payload", names)
	}
}

func TestStreamDiscoveredUsesTheNameTheProducerFound(t *testing.T) {
	dir := t.TempDir()
	d := &Destination{destDir: dir}
	got, err := d.StreamDiscovered(nil, func(w io.Writer) (string, error) {
		_, err := io.WriteString(w, "jpeg")
		return "IMG_0001.jpg", err
	})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "IMG_0001.jpg" {
		t.Errorf("StreamDiscovered wrote to %q", got)
	}
}

func entryNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}
