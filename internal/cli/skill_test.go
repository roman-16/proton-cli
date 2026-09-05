package cli

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The skill is a file another program parses, so what it promises is checked
// here rather than read.
//
// Three things can go wrong with it and none of them shows up in a diff: the
// frontmatter stops being the format an agent can load, a command line in the
// prose names something the tree no longer has, or a collection is added and the
// map an agent routes by never mentions it. The exact bytes are pinned as well,
// so a change to what an agent is told is reviewed as a diff rather than
// noticed.

// spec is what https://agentskills.io requires of the frontmatter.
const (
	specNameLimit          = 64
	specDescriptionLimit   = 1024
	specCompatibilityLimit = 500
)

// bodyLimit is the length the format's own guidance says to stay under, so that
// the whole of it costs an agent little to hold. The map grows with the tree, so
// this is the line at which a section has to earn its place.
const bodyLimit = 500

var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func TestTheSkillIsOneAnAgentCanLoad(t *testing.T) {
	s := newSkill(newRoot(), "dev")

	var front struct {
		Name          string `yaml:"name"`
		Description   string `yaml:"description"`
		Compatibility string `yaml:"compatibility"`
		License       string `yaml:"license"`
	}
	afterOpen, opened := strings.CutPrefix(s.Text(), "---\n")
	block, _, closed := strings.Cut(afterOpen, "\n---\n")
	if !opened || !closed {
		t.Fatalf("the document opens with no frontmatter block:\n%s", s.Text()[:min(200, len(s.Text()))])
	}
	if err := yaml.Unmarshal([]byte(block), &front); err != nil {
		t.Fatalf("the frontmatter is not YAML: %v\n%s", err, block)
	}

	// The name is the directory the skill is saved in, which the specification
	// requires it to match - so it is the name the command tells people to use.
	if front.Name != kit.Alias {
		t.Errorf("name is %q; it has to be %q, which is the directory it is saved in", front.Name, kit.Alias)
	}
	if !skillNamePattern.MatchString(front.Name) {
		t.Errorf("name %q is not lowercase alphanumerics separated by single hyphens", front.Name)
	}
	for _, f := range []struct {
		what  string
		value string
		limit int
	}{
		{"name", front.Name, specNameLimit},
		{"description", front.Description, specDescriptionLimit},
		{"compatibility", front.Compatibility, specCompatibilityLimit},
	} {
		if f.value == "" {
			t.Errorf("%s is empty", f.what)
		}
		if len(f.value) > f.limit {
			t.Errorf("%s is %d characters; the limit is %d", f.what, len(f.value), f.limit)
		}
	}
	if front.License == "" {
		t.Error("no license; a skill somebody installs says what it may be used under")
	}

	// An agent decides whether to read the rest from the description alone, so it
	// has to carry the words somebody would use for what this tool does.
	for _, word := range []string{kit.Program, kit.Alias, "Mail", "Drive", "Calendar", "Pass", "Contacts"} {
		if !strings.Contains(front.Description, word) {
			t.Errorf("the description never mentions %q, so an agent asked about it will not reach for this", word)
		}
	}
}

// --body-only is the same document without its frontmatter, which is what an
// agent reading it as it runs wants and what a skill of somebody's own points at.
func TestTheBodyIsTheDocumentWithoutItsFrontmatter(t *testing.T) {
	s := newSkill(newRoot(), "dev")
	if strings.HasPrefix(s.Body, "---") {
		t.Error("the body opens with a frontmatter fence")
	}
	if !strings.HasSuffix(s.Text(), s.Body) {
		t.Error("the document does not end in the body, so --body-only would print something else")
	}
	if lines := strings.Count(s.Body, "\n"); lines > bodyLimit {
		t.Errorf("the body is %d lines; keep it under %d by sending the reader to --help", lines, bodyLimit)
	}
}

// Every command line the skill shows is one this build has.
//
// It is the same check the documentation is held to, over the same tree, because
// the failure is the same one: a renamed command leaves an instruction telling
// somebody - here, something - to run what is gone. The difference is that an
// agent will run it.
func TestTheSkillNamesOnlyCommandsThatExist(t *testing.T) {
	root := newRoot()
	found := invocations(root, newSkill(root, "dev").Text())
	if len(found) == 0 {
		t.Fatal("read no command lines out of the skill; the checker is not looking at it")
	}
	for _, inv := range found {
		if err := resolves(root, inv.words); err != nil {
			t.Errorf("`%s` - %v", inv.text, err)
			continue
		}
		for _, flag := range unknownFlags(root, inv) {
			t.Errorf("`%s` - no --%s on that command", inv.text, flag)
		}
	}
}

// The map holds everything the tree holds, so a collection or a command added
// later cannot be invisible to an agent that routes by it.
//
// The tree is walked here rather than through what writes the map, because a
// check that asks the generator what the answer is agrees with it whatever it
// says.
func TestTheMapNamesEverythingTheTreeHolds(t *testing.T) {
	root := newRoot()
	body := newSkill(root, "dev").Body

	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		var verbs []string
		for _, sub := range c.Commands() {
			if sub.Hidden || sub.Name() == "help" {
				continue
			}
			if sub.Runnable() {
				verbs = append(verbs, sub.Name())
				// A command with nothing above it but the root has no collection to
				// be listed under, so it stands on its own line, said by what it does.
				if c == root {
					if line := mapLine(body, sub.CommandPath()); !strings.HasSuffix(line, sub.Short) {
						t.Errorf("the map lists `%s` as %q, not by what it does", sub.CommandPath(), line)
					}
				}
			}
			walk(sub)
		}
		if len(verbs) == 0 || c == root {
			return
		}
		line := mapLine(body, c.CommandPath())
		if line == "" {
			t.Errorf("the map never names `%s`, which holds %s",
				c.CommandPath(), strings.Join(verbs, ", "))
			return
		}
		sort.Strings(verbs)
		if want := strings.Join(verbs, ", "); !strings.HasSuffix(line, want) {
			t.Errorf("the map lists `%s` as %q; its verbs are %q", c.CommandPath(), line, want)
		}
	}
	walk(root)
}

// Every listing that can be narrowed says so, because a filter an agent has
// never heard of is the one mistake that answers wrongly and exits zero.
func TestEveryListingThatTakesFiltersShowsThem(t *testing.T) {
	root := newRoot()
	body := newSkill(root, "dev").Body

	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if sub.Hidden || sub.Name() == "help" {
				continue
			}
			if sub.Name() == "list" {
				var want []string
				sub.LocalFlags().VisitAll(func(f *pflag.Flag) {
					if !f.Hidden && f.Name != "help" {
						want = append(want, "--"+f.Name)
					}
				})
				line := filterLine(body, sub.CommandPath())
				shown := map[string]bool{}
				for _, token := range strings.Fields(strings.ReplaceAll(line, "`", " ")) {
					shown[token] = true
				}
				for _, flag := range want {
					if !shown[flag] {
						t.Errorf("`%s` takes %s and the skill does not say so (line: %q)",
							sub.CommandPath(), flag, line)
					}
				}
				if len(want) == 0 && line != "" {
					t.Errorf("`%s` takes no filters but is listed as one that does", sub.CommandPath())
				}
			}
			walk(sub)
		}
	}
	walk(root)
}

// filterLine is the skill's line for a listing.
func filterLine(body, path string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "- `"+path+" ") {
			return line
		}
	}
	return ""
}

func TestTheSkillReadsTheSame(t *testing.T) {
	checkGolden(t, "skill", newSkill(newRoot(), "dev").Text())
}

// mapLine is the skill's line for a command path, so a missing verb is reported
// against the line that should have held it.
func mapLine(body, path string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "- `"+path+"`") {
			return line
		}
	}
	return ""
}
