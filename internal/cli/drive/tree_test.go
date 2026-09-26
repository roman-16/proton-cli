package drive

import (
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Which trees a command offers is decided by what it needs of one, so the set of
// commands that take a link is a property of the design rather than of what each
// command remembered to register.
//
// A link is read always, reported always, written to when it allows editing, and
// changed for what you uploaded into it yourself. What is under an item - its
// history, who else it is shared with, the trash it would land in - a link
// answers for nothing, so those commands take no URL at all.
func TestTheCommandsThatCanBePointedAtALink(t *testing.T) {
	want := []string{
		"drive items abuse",
		"drive items create",
		"drive items delete",
		"drive items download",
		"drive items get",
		"drive items list",
		"drive items update",
		"drive items upload",
	}
	var got []string
	walk(New(), func(c *cobra.Command) {
		if c.Flags().Lookup("link") != nil {
			got = append(got, named(c))
		}
	})
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("--link is offered by:\n  %s\nwant:\n  %s",
			strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// A link its owner put a password on opens with it or not at all, so every
// command that can be pointed at a link can be handed one.
func TestEveryCommandTakingALinkTakesItsPassword(t *testing.T) {
	walk(New(), func(c *cobra.Command) {
		if c.Flags().Lookup("link") == nil {
			return
		}
		if c.Flags().Lookup("link-password-file") == nil {
			t.Errorf("%s takes --link but not --link-password-file, "+
				"so a link with a password cannot be opened", named(c))
		}
	})
}

// A report is only ever about something shared with you, so the commands that
// make one offer no way into your own files or your computers, and refuse to run
// without being pointed somewhere else.
func TestAReportIsNeverPointedAtYourOwnFiles(t *testing.T) {
	walk(New(), func(c *cobra.Command) {
		if c.Name() != "abuse" {
			return
		}
		if c.Flags().Lookup("computer") != nil {
			t.Errorf("%s offers --computer, and a computer is always your own", named(c))
		}
	})
}

func walk(c *cobra.Command, visit func(*cobra.Command)) {
	if !c.HasSubCommands() {
		visit(c)
		return
	}
	for _, sub := range c.Commands() {
		walk(sub, visit)
	}
}

func named(c *cobra.Command) string {
	var parts []string
	for at := c; at != nil; at = at.Parent() {
		parts = append([]string{at.Name()}, parts...)
	}
	return strings.Join(parts, " ")
}
