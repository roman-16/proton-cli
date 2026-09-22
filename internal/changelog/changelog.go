// Package changelog reads CHANGELOG.md and holds it to Keep a Changelog 1.1.0.
//
// The file is the release trigger - a version section reaching main is what
// publishes that version - and it is also what `proton changelog` prints. Both
// read it through here, so the rules the release obeys and the notes a reader
// sees can never come from two different ideas of what the file says.
package changelog

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

// categories are Keep a Changelog's six types of changes, in the order the
// specification lists them. The order is not alphabetical and is not ours to pick.
var categories = []string{"Added", "Changed", "Deprecated", "Removed", "Fixed", "Security"}

// highlights is the section a release may open with: the few bullets somebody
// skims to learn what the release is about, above the ledger saying exactly what
// moved. It is not a seventh category and holds nothing that is not also an entry
// below it, so a version section can be read either way round.
const highlights = "Highlights"

var (
	releaseHeading = regexp.MustCompile(`^## \[([^\]]+)\] - (\d{4}-\d{2}-\d{2})( \[YANKED\])?$`)
	linkReference  = regexp.MustCompile(`^\[[^\]]+\]: \S+$`)
	versionPattern = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([0-9A-Za-z.-]+))?$`)
)

// Section is one category of a release and the entries filed under it.
type Section struct {
	Category string   `json:"category"`
	Entries  []string `json:"entries"`
}

// Release is one version's section: what it is called, when it shipped, what it
// is about, the bullets that go onto its release page verbatim, and those same
// bullets taken apart so a reader can be shown them.
type Release struct {
	Version    string    `json:"version"`
	Date       string    `json:"date"`
	Yanked     bool      `json:"yanked,omitempty"`
	Highlights []string  `json:"highlights"`
	Changes    []Section `json:"changes"`
	Body       string    `json:"-"`
	headline   int
}

// Changelog is a parsed CHANGELOG.md, newest release first.
type Changelog struct {
	Releases []Release
}

// Version returns one release by version, with or without a leading "v".
func (c *Changelog) Version(version string) (Release, bool) {
	want := strings.TrimPrefix(version, "v")
	for _, r := range c.Releases {
		if r.Version == want {
			return r, true
		}
	}
	return Release{}, false
}

// Between returns the releases newer than since and no newer than until, in the
// file's own order. An empty bound is unbounded on that side.
//
// since is exclusive because it names where a reader already is: the answer to
// "what have I missed while on 2.3.0" does not include 2.3.0. until is inclusive
// because it names a release they want to read.
func (c *Changelog) Between(since, until string) []Release {
	var out []Release
	for _, r := range c.Releases {
		if since != "" && !Newer(r.Version, since) {
			continue
		}
		if until != "" && Newer(r.Version, until) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// Oldest is the earliest release the file carries, which is as far back as it
// can answer for.
func (c *Changelog) Oldest() (Release, bool) {
	if len(c.Releases) == 0 {
		return Release{}, false
	}
	return c.Releases[len(c.Releases)-1], true
}

// Releasable is the version the file asks for: the newest one, unless it was
// yanked. A yank withdraws a release, so republishing it is the one thing a rule
// that converges on the file must never do.
func (c *Changelog) Releasable() (Release, bool) {
	if len(c.Releases) == 0 || c.Releases[0].Yanked {
		return Release{}, false
	}
	return c.Releases[0], true
}

// Valid reports whether a version is one this file could name: three numbers and
// an optional pre-release, with no leading "v".
func Valid(version string) bool { return versionPattern.MatchString(version) }

// Newer reports whether one version supersedes another.
func Newer(version, than string) bool {
	return semver.Compare("v"+version, "v"+than) > 0
}

// Parse reads a changelog and holds it to Keep a Changelog 1.1.0. It is strict
// where the specification is only principled - category order, no empty sections,
// versions that move one step at a time - because this file decides what gets
// published, and a rule nothing enforces is a rule that drifts.
//
// An `[Unreleased]` section is allowed and never releasable, but not required:
// here a section is written when a release is cut, so between releases the file
// has nothing to say.
func Parse(path string, source []byte) (*Changelog, error) {
	p := &parser{path: path, category: -1}
	lines := strings.Split(strings.ReplaceAll(string(source), "\r\n", "\n"), "\n")
	if len(lines) == 0 || lines[0] != "# Changelog" {
		return nil, p.at(0, `the file must start with "# Changelog"`)
	}
	for n, line := range lines {
		if err := p.read(n, line); err != nil {
			return nil, err
		}
	}
	return p.done()
}

type parser struct {
	path     string
	releases []Release
	linked   bool

	unreleased bool
	name       string
	heading    int
	body       []string
	highlights []string
	sections   []Section
	open       string
	openAt     int
	category   int
	bullets    int
	current    *Release
}

func (p *parser) read(n int, line string) error {
	switch {
	case linkReference.MatchString(line):
		if err := p.close(); err != nil {
			return err
		}
		p.linked = true
	case strings.HasPrefix(line, "## "):
		return p.section(n, line)
	case p.name == "":
		return nil
	case strings.HasPrefix(line, "### "):
		return p.begin(n, strings.TrimPrefix(line, "### "))
	case strings.HasPrefix(line, "- "):
		return p.entry(n, line)
	case strings.TrimSpace(line) == "":
		p.body = append(p.body, line)
	case strings.HasPrefix(line, "  ") && p.bullets > 0:
		p.body = append(p.body, line)
		p.continuation(line)
	default:
		return p.at(n, "unexpected line in [%s]: %q", p.name, line)
	}
	return nil
}

func (p *parser) section(n int, line string) error {
	if p.linked {
		return p.at(n, "version heading below the link references")
	}
	if err := p.close(); err != nil {
		return err
	}
	if line == "## [Unreleased]" {
		if p.unreleased {
			return p.at(n, "a second [Unreleased] section")
		}
		if len(p.releases) > 0 {
			return p.at(n, "[Unreleased] must sit above every version")
		}
		p.unreleased, p.name, p.heading = true, "Unreleased", n
		return nil
	}
	match := releaseHeading.FindStringSubmatch(line)
	if match == nil {
		return p.at(n, `expected "## [X.Y.Z] - YYYY-MM-DD", got %q`, line)
	}
	version, date := match[1], match[2]
	if version == "Unreleased" {
		return p.at(n, "[Unreleased] never carries a date")
	}
	if !versionPattern.MatchString(version) {
		return p.at(n, "[%s] is not a semantic version", version)
	}
	if _, err := time.Parse(time.DateOnly, date); err != nil {
		return p.at(n, "[%s] is dated %s, which is not a day", version, date)
	}
	p.name, p.heading = version, n
	p.current = &Release{Version: version, Date: date, Yanked: match[3] != "", headline: n}
	return nil
}

func (p *parser) begin(n int, name string) error {
	if err := p.settled(); err != nil {
		return err
	}
	if name == highlights {
		switch {
		case p.unreleased:
			return p.at(n, "[Unreleased] carries no %s: they say what a release is about", highlights)
		case len(p.highlights) > 0:
			return p.at(n, "a second %s in [%s]", highlights, p.name)
		case p.category >= 0:
			return p.at(n, "%s in [%s] belongs above %s: a release says what it is about before what moved",
				highlights, p.name, categories[p.category])
		}
		p.open, p.openAt, p.bullets = highlights, n, 0
		p.body = append(p.body, "### "+name)
		return nil
	}
	next := slices.Index(categories, name)
	if next < 0 {
		return p.at(n, "%q is not %s or one of %s", name, highlights, strings.Join(categories, ", "))
	}
	if next <= p.category {
		return p.at(n, "%s in [%s] belongs above %s: the order is %s",
			name, p.name, categories[p.category], strings.Join(categories, ", "))
	}
	p.category, p.open, p.openAt, p.bullets = next, name, n, 0
	p.body = append(p.body, "### "+name)
	p.sections = append(p.sections, Section{Category: name})
	return nil
}

// settled reports the section being read as finished, which it is not while
// nothing has been filed under it.
func (p *parser) settled() error {
	if p.open != "" && p.bullets == 0 {
		return p.at(p.openAt, "%s in [%s] has no entries", p.open, p.name)
	}
	return nil
}

func (p *parser) entry(n int, line string) error {
	if p.open == "" {
		return p.at(n, "entry in [%s] sits outside a section", p.name)
	}
	text := strings.TrimSpace(strings.TrimPrefix(line, "- "))
	if text == "" {
		return p.at(n, "empty entry in [%s]", p.name)
	}
	p.bullets++
	p.body = append(p.body, line)
	if p.open == highlights {
		p.highlights = append(p.highlights, text)
		return nil
	}
	last := &p.sections[len(p.sections)-1]
	last.Entries = append(last.Entries, text)
	return nil
}

// continuation folds a wrapped entry's later lines back into the entry, so a
// reader is shown one sentence rather than the author's line breaks.
func (p *parser) continuation(line string) {
	if p.open == highlights {
		p.highlights[len(p.highlights)-1] += " " + strings.TrimSpace(line)
		return
	}
	last := &p.sections[len(p.sections)-1]
	i := len(last.Entries) - 1
	last.Entries[i] += " " + strings.TrimSpace(line)
}

// close finishes the section being read. An empty [Unreleased] is a section with
// nothing in it yet; an empty version section is a release with nothing to say,
// which means the wrong thing was published.
func (p *parser) close() error {
	if p.name == "" {
		return nil
	}
	if err := p.settled(); err != nil {
		return err
	}
	if p.current != nil {
		switch {
		case len(p.sections) > 0:
		case len(p.highlights) > 0:
			return p.at(p.heading, "[%s] is %s and nothing else: each of them restates an entry below it",
				p.name, highlights)
		default:
			return p.at(p.heading, "[%s] has no entries", p.name)
		}
		p.current.Body = strings.Join(trimBlank(p.body), "\n")
		p.current.Highlights = append([]string{}, p.highlights...)
		p.current.Changes = p.sections
		p.releases = append(p.releases, *p.current)
	}
	p.name, p.body, p.highlights, p.sections = "", nil, nil, nil
	p.open, p.category, p.bullets, p.current = "", -1, 0, nil
	return nil
}

func (p *parser) done() (*Changelog, error) {
	if err := p.close(); err != nil {
		return nil, err
	}
	for i := 1; i < len(p.releases); i++ {
		newest, older := p.releases[i-1], p.releases[i]
		if !Newer(newest.Version, older.Version) {
			return nil, p.at(newest.headline, "[%s] is not newer than [%s]: the latest version comes first",
				newest.Version, older.Version)
		}
		if !follows(older.Version, newest.Version) {
			return nil, p.at(newest.headline, "[%s] does not follow [%s], which is followed by %s",
				newest.Version, older.Version, strings.Join(successors(older.Version), " or "))
		}
	}
	return &Changelog{Releases: p.releases}, nil
}

func (p *parser) at(n int, format string, args ...any) error {
	return fmt.Errorf("%s:%d: %s", p.path, n+1, fmt.Sprintf(format, args...))
}

// successors are the versions semantic versioning allows after this one. There
// are three, which is what makes a slip of the finger catchable: 2.30.0 is not
// one of them, and a version once tagged cannot be taken back.
func successors(version string) []string {
	match := versionPattern.FindStringSubmatch(version)
	if match == nil {
		return nil
	}
	major, minor, patch := number(match, 1), number(match, 2), number(match, 3)
	core := fmt.Sprintf("%d.%d.%d", major, minor, patch)
	if match[4] != "" {
		return []string{core, core + "-<pre-release>"}
	}
	return []string{
		fmt.Sprintf("%d.%d.%d", major, minor, patch+1),
		fmt.Sprintf("%d.%d.0", major, minor+1),
		fmt.Sprintf("%d.0.0", major+1),
	}
}

func follows(previous, next string) bool {
	match := versionPattern.FindStringSubmatch(next)
	if match == nil {
		return false
	}
	core := fmt.Sprintf("%s.%s.%s", match[1], match[2], match[3])
	if before := versionPattern.FindStringSubmatch(previous); before != nil && before[4] != "" {
		if core == fmt.Sprintf("%s.%s.%s", before[1], before[2], before[3]) {
			return true
		}
	}
	return slices.Contains(successors(previous), core)
}

func number(match []string, i int) int {
	n, _ := strconv.Atoi(match[i])
	return n
}

func trimBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
