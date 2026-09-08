package self

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/changelog"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/selfmanage"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

// releasesPage is where the releases older than the changelog are written down.
const releasesPage = "https://github.com/roman-16/proton-cli/releases"

// ChangelogCmd prints what each release changed.
//
// It reads the project's published CHANGELOG.md rather than a copy compiled in,
// because the question that makes the command worth having - what is in the
// release being offered - is always about a version newer than the binary being
// asked, and no build can carry notes written after it.
func ChangelogCmd() *cobra.Command {
	var since, until string
	cmd := &cobra.Command{
		Use:   "changelog [VERSION]",
		Short: "Print what each release changed",
		Long: `Print the release notes, newest first: the whole changelog, one
version, or the releases between two of them.

--since names the version you are on and is not included; --until
names where to stop and is. Releases older than the changelog itself
are on the releases page.`,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			target := ""
			if len(c.Args) == 1 {
				target = strings.TrimPrefix(c.Args[0], "v")
			}
			return runChangelog(c, target, strings.TrimPrefix(since, "v"), strings.TrimPrefix(until, "v"))
		}),
	}
	cmd.Flags().StringVar(&since, "since", "", "Only releases after this version")
	cmd.Flags().StringVar(&until, "until", "", "Only releases up to and including this version")
	return cmd
}

func runChangelog(c *kit.Invocation, target, since, until string) error {
	// Both of these are answerable from the command line alone, so neither costs
	// a round trip to discover.
	if target != "" && (since != "" || until != "") {
		return kit.Fail("A version and a range ask for different things.").
			Hint(kit.Program+" changelog "+target,
				kit.Program+" changelog --since "+target)
	}
	for _, bound := range []struct{ flag, value string }{
		{"", target}, {"--since ", since}, {"--until ", until},
	} {
		if bound.value != "" && !changelog.Valid(bound.value) {
			return kit.Fail("%q is not a version.", bound.value).
				Hint(kit.Program + " changelog " + bound.flag + "2.4.1")
		}
	}

	document, err := published(c)
	if err != nil {
		return err
	}
	// A version named outright and not found is a miss, and exits like one. A
	// range holding nothing is an answer: there are no such releases, which is
	// what someone already on the newest one should hear.
	if target != "" {
		found, ok := document.Version(target)
		if !ok {
			return missing(target, document)
		}
		return kit.Read(c, notes([]changelog.Release{found}))
	}
	releases := document.Between(since, until)
	if len(releases) == 0 {
		c.Note("%s", nothing(since, until))
	}
	// A bound reaching below the file asked for releases it cannot show, which is
	// worth saying out loud rather than answering with a shorter list.
	if oldest, ok := document.Oldest(); ok && since != "" && changelog.Newer(oldest.Version, since) {
		c.Warn("The changelog starts at %s, so anything older is on the releases page:\n%s",
			oldest.Version, releasesPage)
	}
	return kit.Read(c, notes(releases))
}

// nothing says which window came up empty, in the words it was asked for.
func nothing(since, until string) string {
	switch {
	case since != "" && until != "":
		return fmt.Sprintf("No releases after %s and up to %s.", since, until)
	case since != "":
		return fmt.Sprintf("No releases after %s.", since)
	case until != "":
		return fmt.Sprintf("No releases up to %s.", until)
	}
	return "No releases."
}

// published fetches and parses the project's changelog.
func published(c *kit.Invocation) (*changelog.Changelog, error) {
	source, err := selfmanage.Changelog(c.Ctx, &http.Client{Timeout: 30 * time.Second})
	if err != nil {
		return nil, kit.Fail("Could not reach GitHub to read the changelog: %v", err).
			Hint("check your connection", releasesPage).Exit(5)
	}
	document, err := changelog.Parse("CHANGELOG.md", source)
	if err != nil {
		return nil, kit.Fail("The published changelog could not be read: %v", err).
			Hint(releasesPage).Exit(5)
	}
	return document, nil
}

// notes lays the releases out to be read: one part each, headed by the version
// and the day it shipped, exactly as a thread of messages is laid out.
func notes(releases []changelog.Release) ui.DocumentSpec {
	if releases == nil {
		releases = []changelog.Release{}
	}
	parts := make([]ui.Part, 0, len(releases))
	for _, r := range releases {
		header := []ui.Field{{Label: "Version", Value: r.Version}, {Label: "Released", Value: r.Date}}
		if r.Yanked {
			header = append(header, ui.Field{Label: "Withdrawn", Value: "yes"})
		}
		var body strings.Builder
		for i, section := range r.Changes {
			if i > 0 {
				body.WriteString("\n")
			}
			body.WriteString(section.Category + "\n")
			for _, entry := range section.Entries {
				body.WriteString("- " + entry + "\n")
			}
		}
		parts = append(parts, ui.Part{Header: header, Body: strings.TrimRight(body.String(), "\n")})
	}
	return ui.DocumentSpec{
		Parts:  parts,
		Object: map[string]any{"releases": releases, "count": len(releases)},
	}
}

// missing says the changelog has no entry, and where the answer lives instead.
func missing(version string, document *changelog.Changelog) error {
	problem := kit.Fail("No changelog entry for %s.", version)
	if oldest, ok := document.Oldest(); ok {
		problem = problem.Hint("the changelog starts at " + oldest.Version)
	}
	return problem.Hint(releasesPage + "/tag/v" + version).Exit(3)
}
