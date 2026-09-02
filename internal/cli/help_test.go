package cli

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/internal/cli/kit"
)

// A help screen is the documentation most people read, and there are 164 of
// them built from one renderer. So they are pinned the way the responses are:
// exact bytes, in a file, for the three shapes a screen can take.
//
// Regenerate with:  just golden      (go test ./internal/cli -update)

var update = flag.Bool("update", false, "rewrite the golden files")

func TestHelpReadsTheSameOnEveryShapeOfCommand(t *testing.T) {
	for _, c := range []struct {
		name string
		path []string
	}{
		// The root, which is where the grammar and the global flags are taught.
		{name: "help_root"},
		// A group, which holds commands and acts on nothing.
		{name: "help_group", path: []string{"mail", "messages"}},
		// A leaf, which is the only shape with flags of its own.
		{name: "help_leaf", path: []string{"mail", "messages", "send"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := newRoot()
			cmd, _, err := root.Find(c.path)
			if err != nil {
				t.Fatalf("find %v: %v", c.path, err)
			}
			var out bytes.Buffer
			writeHelp(&out, cmd)
			checkGolden(t, c.name, out.String())
		})
	}
}

// Every screen points at a heading that the generated reference actually wrote.
//
// A help screen offering a dead link is worse than one offering none, and the
// two are written by different programs from the same function - so this is
// where they are held to having agreed. Regenerating the reference is what fixes
// a failure here, never editing the page.
func TestEveryHelpScreenPointsAtSomethingThatWasWritten(t *testing.T) {
	leaves, groups := partition(t)
	pages := map[string]string{}
	for _, c := range append(leaves, groups...) {
		page := kit.ReferencePage(c)
		if page == "" {
			continue
		}
		if _, seen := pages[page]; !seen {
			file := pageFile(page)
			src, err := os.ReadFile(filepath.Join("..", "..", file))
			if err != nil {
				t.Errorf("%s points at %s, which is not there (run `just docs`)", cmdPath(c), file)
				pages[page] = ""
				continue
			}
			pages[page] = string(src)
		}
		heading := kit.ReferenceHeading(c)
		if heading == "" || pages[page] == "" {
			continue
		}
		// Whatever a heading is decorated with, an anchor is its words hyphenated,
		// which is what both GitHub and the site derive.
		if !strings.Contains(pages[page], "\n## `"+heading+"`\n") &&
			!strings.Contains(pages[page], "\n### `"+heading+"`\n") {
			t.Errorf("%s links to %s, but %s has no heading %q (run `just docs`)",
				cmdPath(c), kit.Reference(c), pageFile(page), heading)
		}
	}
}

// pageFile is where a reference page slug lives in the repository. An app that
// holds other commands is published at its guide, which is the README of the
// app's own directory; everything else is a generated file named for the
// command line it documents.
func pageFile(page string) string {
	if !strings.Contains(page, "/") && page != kit.SelfPage {
		return filepath.Join("docs", page, "README.md")
	}
	return filepath.Join("docs", page+".md")
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (run `just golden` to create it)", err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s\n\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}
