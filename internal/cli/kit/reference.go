package kit

import (
	"strings"

	"github.com/spf13/cobra"
)

// The root's command groups: what the tool does to your account, what it does
// to the account itself, and what it does to its own installation.
//
// They are declared here rather than beside the root because they are the top
// level of the published reference as well as of a help screen, and the two have
// to agree about which page a command is on.
const (
	GroupApps    = "apps"
	GroupAccount = "account"
	GroupSelf    = "self"
)

// SelfPage is where the commands that act on this installation are published.
// They share one page: five commands with a dozen flags between them are a
// section rather than a chapter.
const SelfPage = Program

// ReferenceScope is the command whose page a command is published on.
//
// A collection earns a page, because `proton mail messages` is the unit a reader
// looks things up by: the page is one command line's worth of commands. An app's
// own verbs - `contacts create`, `account login` - have no collection above them,
// so the app is their scope.
//
// It is nil for the root and for the commands that act on this installation,
// which share SelfPage.
func ReferenceScope(c *cobra.Command) *cobra.Command {
	var chain []*cobra.Command
	for n := c; n != nil && n.Parent() != nil; n = n.Parent() {
		chain = append([]*cobra.Command{n}, chain...)
	}
	if len(chain) == 0 {
		return nil
	}
	if app := chain[0]; app.GroupID == GroupSelf {
		return nil
	}
	if len(chain) > 1 && chain[1].HasSubCommands() {
		return chain[1]
	}
	return chain[0]
}

// ReferencePage is the slug of the page a command's full entry is published on.
//
// The slug is the command line with the program dropped and the spaces turned
// into slashes, so `proton mail messages send` is published at `mail/messages`
// and a reader can guess a URL from a command. An app that holds other commands
// is the exception: it is published at its guide, which is what somebody typing
// `proton mail --help` is looking for.
func ReferencePage(c *cobra.Command) string {
	scope := ReferenceScope(c)
	if scope == nil {
		if c.Parent() == nil {
			return ""
		}
		return SelfPage
	}
	app := scope
	for app.Parent() != nil && app.Parent().Parent() != nil {
		app = app.Parent()
	}
	if c == app && app.HasSubCommands() {
		return app.Name()
	}
	return app.Name() + "/" + scope.Name()
}

// ReferenceHeading is what a command is called on its page: its path, with the
// page's own command line dropped.
//
// The page is already titled `proton mail messages`, so writing that in front of
// every heading on it is the repetition a reference exists to spare somebody. It
// is empty only when the command is the page, which has no heading of its own
// because the page is it.
//
// SelfPage gathers top-level commands, so it covers no command line of its own
// and nothing is dropped: `update` heads its own entry.
func ReferenceHeading(c *cobra.Command) string {
	if c.Parent() == nil {
		return ""
	}
	scope := ReferenceScope(c)
	if scope == nil {
		return commandPath(c)
	}
	if c == scope {
		return ""
	}
	return strings.TrimPrefix(commandPath(c), commandPath(scope)+" ")
}

// ReferenceAnchor is where a command's heading is linked to.
//
// Both GitHub and the site slugify a heading by hyphenating its words, so the
// heading is the anchor, which is what lets one string serve a link in a page
// and a line on a help screen.
func ReferenceAnchor(c *cobra.Command) string {
	return strings.ReplaceAll(ReferenceHeading(c), " ", "-")
}

// Index is the page listing every command, which is where the root points: a
// screen that has just shown the whole tree is answering "where is all of this
// written down".
const Index = "about/commands"

// Reference is where a command is documented in full.
func Reference(c *cobra.Command) string {
	page := ReferencePage(c)
	if page == "" {
		return Docs + "/" + Index + "/"
	}
	url := Docs + "/" + page + "/"
	if anchor := ReferenceAnchor(c); anchor != "" {
		url += "#" + anchor
	}
	return url
}

// commandPath is the invocation with the program name dropped, which is how
// every command is written once a screen has already said which program it is.
func commandPath(c *cobra.Command) string {
	return strings.TrimPrefix(c.CommandPath(), Program+" ")
}

// Synopsis is the whole command line, the way a manual page opens: the path,
// then whatever arguments the command declares for itself.
func Synopsis(c *cobra.Command) string {
	path := commandPath(c)
	if args := strings.Fields(c.Use); len(args) > 1 {
		path += " " + strings.Join(args[1:], " ")
	}
	return Program + " " + path
}
