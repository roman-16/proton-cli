package changelog

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

const shipped = `## [2.3.0] - 2026-08-15

### Added

- Something that did not exist before.

### Fixed

- Something that behaved wrongly.
`

func document(sections string) []byte {
	return []byte("# Changelog\n\nAll notable changes to this project will be documented in this file.\n\n" + sections)
}

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		sections string
		refuses  string
	}{
		{
			name:     "a released version",
			sections: shipped,
		},
		{
			name:     "nothing released yet",
			sections: "",
		},
		{
			name: "several versions",
			sections: `## [2.3.0] - 2026-08-15

### Added

- Something newer.

## [2.2.3] - 2026-08-12

### Fixed

- Something older.
`,
		},
		{
			name:     "an unreleased section above them",
			sections: "## [Unreleased]\n\n" + shipped,
		},
		{
			name:     "an unreleased section carrying entries",
			sections: "## [Unreleased]\n\n### Added\n\n- Something on its way.\n\n" + shipped,
		},
		{
			name: "a release that says what it is about",
			sections: `## [2.3.0] - 2026-08-15

### Highlights

- **The thing** - what it lets somebody do.

### Added

- Something that did not exist before.
`,
		},
		{
			name:     "highlights below a category",
			sections: "## [2.3.0] - 2026-08-15\n\n### Added\n\n- Something.\n\n### Highlights\n\n- **The thing** - what it does.\n",
			refuses:  "Highlights in [2.3.0] belongs above Added",
		},
		{
			name:     "highlights twice",
			sections: "## [2.3.0] - 2026-08-15\n\n### Highlights\n\n- One.\n\n### Highlights\n\n- Two.\n\n### Added\n\n- Something.\n",
			refuses:  "a second Highlights in [2.3.0]",
		},
		{
			name:     "highlights with no entries",
			sections: "## [2.3.0] - 2026-08-15\n\n### Highlights\n\n### Added\n\n- Something.\n",
			refuses:  "Highlights in [2.3.0] has no entries",
		},
		{
			name:     "highlights and no ledger under them",
			sections: "## [2.3.0] - 2026-08-15\n\n### Highlights\n\n- **The thing** - what it does.\n",
			refuses:  "[2.3.0] is Highlights and nothing else",
		},
		{
			name:     "highlights on the unreleased section",
			sections: "## [Unreleased]\n\n### Highlights\n\n- **The thing** - what it does.\n\n" + shipped,
			refuses:  "[Unreleased] carries no Highlights",
		},
		{
			name: "link references, for a file that keeps them",
			sections: shipped + `
[2.3.0]: https://example.test/compare/v2.2.3...v2.3.0
`,
		},
		{
			name: "a wrapped entry",
			sections: `## [2.3.0] - 2026-08-15

### Added

- Something that did not exist before, described at a length that the author
  chose to wrap.
`,
		},
		{
			name: "a pre-release and the version it precedes",
			sections: `## [2.3.0] - 2026-08-16

### Added

- The finished thing.

## [2.3.0-rc.1] - 2026-08-15

### Added

- The thing, for testing.
`,
		},
		{
			name:     "a yanked version",
			sections: "## [2.3.0] - 2026-08-15 [YANKED]\n\n### Added\n\n- Something that had to be withdrawn.\n",
		},
		{
			name:     "an unreleased section below a version",
			sections: shipped + "\n## [Unreleased]\n",
			refuses:  "[Unreleased] must sit above every version",
		},
		{
			name:     "an invented category",
			sections: "## [2.3.0] - 2026-08-15\n\n### Breaking\n\n- Something.\n",
			refuses:  `"Breaking" is not Highlights or one of Added, Changed`,
		},
		{
			name: "categories out of order",
			sections: `## [2.3.0] - 2026-08-15

### Fixed

- Something that behaved wrongly.

### Added

- Something that did not exist before.
`,
			refuses: "Added in [2.3.0] belongs above Fixed",
		},
		{
			name:     "a category with no entries",
			sections: "## [2.3.0] - 2026-08-15\n\n### Added\n\n### Fixed\n\n- Something.\n",
			refuses:  "Added in [2.3.0] has no entries",
		},
		{
			name:     "a version with no entries",
			sections: "## [2.3.0] - 2026-08-15\n",
			refuses:  "[2.3.0] has no entries",
		},
		{
			name:     "an entry outside a category",
			sections: "## [2.3.0] - 2026-08-15\n\n- Something.\n",
			refuses:  "sits outside a section",
		},
		{
			name:     "an undated version",
			sections: "## [2.3.0]\n\n### Added\n\n- Something.\n",
			refuses:  `expected "## [X.Y.Z] - YYYY-MM-DD"`,
		},
		{
			name:     "a day that does not exist",
			sections: "## [2.3.0] - 2026-13-45\n\n### Added\n\n- Something.\n",
			refuses:  "which is not a day",
		},
		{
			name:     "a version that is not one",
			sections: "## [2.3] - 2026-08-15\n\n### Added\n\n- Something.\n",
			refuses:  "[2.3] is not a semantic version",
		},
		{
			name:     "a dated unreleased section",
			sections: "## [Unreleased] - 2026-08-15\n\n### Added\n\n- Something.\n",
			refuses:  "[Unreleased] never carries a date",
		},
		{
			name: "the oldest version first",
			sections: `## [2.2.3] - 2026-08-12

### Fixed

- Something older.

## [2.3.0] - 2026-08-15

### Added

- Something newer.
`,
			refuses: "the latest version comes first",
		},
		{
			name: "a version reached by a typo",
			sections: `## [2.30.0] - 2026-08-15

### Added

- Something.

## [2.2.3] - 2026-08-12

### Fixed

- Something older.
`,
			refuses: "[2.30.0] does not follow [2.2.3], which is followed by 2.2.4 or 2.3.0 or 3.0.0",
		},
		{
			name:     "prose where an entry belongs",
			sections: "## [2.3.0] - 2026-08-15\n\n### Added\n\nSomething, at length.\n",
			refuses:  "unexpected line in [2.3.0]",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse("CHANGELOG.md", document(test.sections))
			switch {
			case test.refuses == "" && err != nil:
				t.Fatalf("refused a conformant changelog: %v", err)
			case test.refuses == "":
			case err == nil:
				t.Fatalf("accepted a changelog it should have refused, expected %q", test.refuses)
			case !strings.Contains(err.Error(), test.refuses):
				t.Fatalf("refused it for the wrong reason:\n got %v\nwant %s", err, test.refuses)
			}
		})
	}
}

func TestParseRejectsAFileThatIsNotAChangelog(t *testing.T) {
	if _, err := Parse("CHANGELOG.md", []byte("# Release notes\n")); err == nil {
		t.Fatal("accepted a file that does not start with # Changelog")
	}
}

func TestReleasable(t *testing.T) {
	tests := []struct {
		name     string
		sections string
		want     string
	}{
		{"a released version", shipped, "2.3.0"},
		{"nothing released yet", "", ""},
		{"an unreleased section above them", "## [Unreleased]\n\n" + shipped, "2.3.0"},
		{"only an unreleased section", "## [Unreleased]\n", ""},
		{
			name:     "a yanked version",
			sections: "## [2.3.0] - 2026-08-15 [YANKED]\n\n### Added\n\n- Something.\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, err := Parse("CHANGELOG.md", document(test.sections))
			if err != nil {
				t.Fatal(err)
			}
			target, ok := document.Releasable()
			if ok != (test.want != "") {
				t.Fatalf("releasable = %v, want %v", ok, test.want != "")
			}
			if target.Version != test.want {
				t.Fatalf("version = %q, want %q", target.Version, test.want)
			}
		})
	}
}

func TestNotes(t *testing.T) {
	document, err := Parse("CHANGELOG.md", document(shipped))
	if err != nil {
		t.Fatal(err)
	}
	target, ok := document.Releasable()
	if !ok {
		t.Fatal("nothing to release")
	}
	want := `### Added

- Something that did not exist before.

### Fixed

- Something that behaved wrongly.`
	if target.Body != want {
		t.Fatalf("notes:\n got %q\nwant %q", target.Body, want)
	}
}

func TestChanges(t *testing.T) {
	document, err := Parse("CHANGELOG.md", document(`## [2.3.0] - 2026-08-15

### Added

- Something that did not exist before, described at a length that the author
  chose to wrap.
- Something else.

### Fixed

- Something that behaved wrongly.
`))
	if err != nil {
		t.Fatal(err)
	}
	want := []Section{
		{Category: "Added", Entries: []string{
			"Something that did not exist before, described at a length that the author chose to wrap.",
			"Something else.",
		}},
		{Category: "Fixed", Entries: []string{"Something that behaved wrongly."}},
	}
	if !reflect.DeepEqual(document.Releases[0].Changes, want) {
		t.Fatalf("changes:\n got %#v\nwant %#v", document.Releases[0].Changes, want)
	}
}

// Highlights are the release's own, kept apart from the ledger they summarise,
// and they go onto the release page with it.
func TestHighlights(t *testing.T) {
	summarised, err := Parse("CHANGELOG.md", document(`## [2.3.0] - 2026-08-15

### Highlights

- **The thing** - what it lets somebody do, described at a length that the
  author chose to wrap.

### Added

- Something that did not exist before.
`))
	if err != nil {
		t.Fatal(err)
	}
	release := summarised.Releases[0]
	want := []string{"**The thing** - what it lets somebody do, described at a length that the author chose to wrap."}
	if !reflect.DeepEqual(release.Highlights, want) {
		t.Fatalf("highlights:\n got %#v\nwant %#v", release.Highlights, want)
	}
	if !reflect.DeepEqual(release.Changes, []Section{{Category: "Added", Entries: []string{"Something that did not exist before."}}}) {
		t.Fatalf("changes: %#v", release.Changes)
	}
	if !strings.HasPrefix(release.Body, "### Highlights\n") {
		t.Fatalf("notes do not open with the highlights:\n%s", release.Body)
	}
	plain, err := Parse("CHANGELOG.md", document(shipped))
	if err != nil {
		t.Fatal(err)
	}
	if plain.Releases[0].Highlights == nil {
		t.Fatal("a release with nothing to say about itself has no list to iterate")
	}
}

const three = `## [2.4.0] - 2026-08-16

### Added

- The newest.

## [2.3.1] - 2026-08-15

### Fixed

- The middle one.

## [2.3.0] - 2026-08-14

### Added

- The oldest.
`

func TestVersion(t *testing.T) {
	document, err := Parse("CHANGELOG.md", document(three))
	if err != nil {
		t.Fatal(err)
	}
	for _, asked := range []string{"2.3.1", "v2.3.1"} {
		found, ok := document.Version(asked)
		if !ok || found.Version != "2.3.1" {
			t.Fatalf("Version(%q) = %q, %v", asked, found.Version, ok)
		}
	}
	if _, ok := document.Version("1.0.0"); ok {
		t.Fatal("found a version the file does not carry")
	}
}

func TestBetween(t *testing.T) {
	document, err := Parse("CHANGELOG.md", document(three))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name         string
		since, until string
		want         []string
	}{
		{name: "unbounded", want: []string{"2.4.0", "2.3.1", "2.3.0"}},
		{name: "since excludes the version named", since: "2.3.0", want: []string{"2.4.0", "2.3.1"}},
		{name: "until includes the version named", until: "2.3.1", want: []string{"2.3.1", "2.3.0"}},
		{name: "both", since: "2.3.0", until: "2.3.1", want: []string{"2.3.1"}},
		{name: "a window holding nothing", since: "2.4.0", want: nil},
		{name: "a bound below the file", since: "1.0.0", want: []string{"2.4.0", "2.3.1", "2.3.0"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got []string
			for _, r := range document.Between(test.since, test.until) {
				got = append(got, r.Version)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("between(%q, %q) = %v, want %v", test.since, test.until, got, test.want)
			}
		})
	}
}

func TestOldest(t *testing.T) {
	document, err := Parse("CHANGELOG.md", document(three))
	if err != nil {
		t.Fatal(err)
	}
	oldest, ok := document.Oldest()
	if !ok || oldest.Version != "2.3.0" {
		t.Fatalf("Oldest = %q, %v, want 2.3.0", oldest.Version, ok)
	}
	empty, err := Parse("CHANGELOG.md", []byte("# Changelog\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := empty.Oldest(); ok {
		t.Fatal("an empty changelog reported an oldest release")
	}
}

func TestValid(t *testing.T) {
	for _, version := range []string{"1.0.0", "2.4.1", "2.3.0-rc.1", "0.0.1"} {
		if !Valid(version) {
			t.Errorf("%q is a version and was refused", version)
		}
	}
	for _, version := range []string{"", "2.4", "v2.4.1", "latest", "2.4.1.0", "01.0.0"} {
		if Valid(version) {
			t.Errorf("%q is not a version and was accepted", version)
		}
	}
}

// The repository's own changelog is the release trigger, so it is held to the
// same rules as any other, in a test that runs without credentials or a network.
func TestRepositoryChangelog(t *testing.T) {
	path := "../../CHANGELOG.md"
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(path, source); err != nil {
		t.Fatal(err)
	}
}
