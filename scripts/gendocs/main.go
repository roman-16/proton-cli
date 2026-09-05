// Command gendocs writes the command reference from the command tree itself.
//
// The reference is generated because prose drifts and a tree does not. Every
// command gets its whole entry - what it does, how it is invoked, what it takes,
// and the examples it already carries - so the answer to "what does this flag
// do" exists somewhere other than a terminal. CI regenerates it and fails on a
// diff, so a command that exists is a command that is documented, and one that
// was renamed cannot keep its old name here.
//
// Where a command is published is kit's answer, not this file's: the same
// function tells a help screen which URL to print, so a heading and a link
// cannot disagree. A page is one command line's worth of commands, so
// `proton mail messages send` is documented at docs/mail/messages.md under the
// heading `send`, and the URL a reader lands on reads like what they typed.
//
// Inside an app's directory, README.md is the hand-written guide and every other
// markdown file is generated. That invariant is what lets this rewrite the
// reference without touching the prose beside it.
//
// Prose here is written as whole paragraphs on one line, as the hand-written
// pages are: a hard wrap is a decision about somebody else's window.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/roman-16/proton-cli/internal/cli"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// indexPage is the flat list of every command, which is the one place a reader
// who knows a word but not which app it belongs to can start.
const indexPage = "about/commands"

func main() {
	root := cli.Root()
	pages := collect(root)

	clean(root)
	for slug, commands := range pages {
		if err := write(slug, page(root, slug, commands)); err != nil {
			fail(err)
		}
	}
	if err := write(indexPage, index(root, pages)); err != nil {
		fail(err)
	}
}

// collect files every documented command under the page it belongs on.
func collect(root *cobra.Command) map[string][]*cobra.Command {
	pages := map[string][]*cobra.Command{}
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Hidden || c.Name() == "help" {
			return
		}
		// An app that holds other commands has its guide for a page, and that is
		// written by hand.
		if c != root && !isGuide(root, c) {
			slug := kit.ReferencePage(c)
			pages[slug] = append(pages[slug], c)
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
	for slug := range pages {
		sort.Slice(pages[slug], func(i, j int) bool {
			return pages[slug][i].CommandPath() < pages[slug][j].CommandPath()
		})
	}
	return pages
}

// clean removes what a previous run wrote, so a page for a command that no
// longer exists goes with it. Only generated files are in scope: an app's guide
// is its README, and nothing else under an app's directory is written by hand.
func clean(root *cobra.Command) {
	docs := filepath.Join(moduleRoot(), "docs")
	for _, app := range visible(root) {
		if app.GroupID == kit.GroupSelf {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(docs, app.Name()))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || entry.Name() == "README.md" || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			if err := os.Remove(filepath.Join(docs, app.Name(), entry.Name())); err != nil {
				fail(err)
			}
		}
	}
	for _, slug := range []string{kit.SelfPage, indexPage} {
		if err := os.Remove(filepath.Join(docs, slug+".md")); err != nil && !os.IsNotExist(err) {
			fail(err)
		}
	}
}

// index is one row per command, for the reader who knows the word but not which
// app owns it, plus the flags that work everywhere.
func index(root *cobra.Command, pages map[string][]*cobra.Command) string {
	var b strings.Builder
	b.WriteString("# All commands\n\n")
	b.WriteString("Every command in one table, generated from the command tree. Search this page for a word, then follow the link for the arguments, flags and examples.\n\n")
	b.WriteString("```\n" + kit.Program + " <app> <collection> <verb> [TARGET...] [--flags]\n```\n\n")
	b.WriteString("Where a command shows `REF`, pass a full ID, the eight-character short ID a list printed, or something you already know: a subject, a name, a path, an email address. See [Naming what to act on](../using/naming.md).\n\n")

	b.WriteString("| Command | What it does |\n| --- | --- |\n")
	for _, slug := range sortedKeys(pages) {
		for _, c := range pages[slug] {
			if !c.Runnable() {
				continue
			}
			fmt.Fprintf(&b, "| [`%s`](%s) | %s |\n",
				kit.Program+" "+commandPath(c), link(indexPage, c), escape(c.Short))
		}
	}

	b.WriteString("\n## Flags that work on every command\n\n")
	b.WriteString("These are declared on the root, so any command takes them and they mean the same thing everywhere.\n\n")
	writeFlags(&b, root.LocalFlags())
	b.WriteString("\nSee [Settings, files and environment](../using/settings.md) for what each one changes, and [Output and exit codes](../using/output.md) for what a command answers with.\n")
	return b.String()
}

// page is one page of the reference: every command filed under it, in tree
// order, each with everything the tree knows about it.
//
// The command the page is named for has no entry of its own, because the page is
// its entry: its description is the lead, and whatever it holds is the list
// under it. Only its body - a synopsis, examples, flags - is written out, and
// only when it has one.
func page(root *cobra.Command, slug string, commands []*cobra.Command) string {
	var b strings.Builder

	if slug == kit.SelfPage {
		fmt.Fprintf(&b, "# %s itself\n\n", kit.Program)
		b.WriteString("Updating, uninstalling, shell completions, the skill an AI agent reads, and what a release changed.\n\n")
		b.WriteString("These act on this installation rather than on your account, so none of them needs you to be signed in.\n")
		for _, c := range commands {
			writeCommand(&b, c)
		}
		b.WriteString(footer(slug))
		return b.String()
	}

	// The title carries no backticks: it becomes frontmatter, which is read as a
	// string rather than as markdown, so a backtick there is a backtick on screen.
	scope := kit.ReferenceScope(commands[0])
	fmt.Fprintf(&b, "# %s %s\n\n%s\n\n", kit.Program, commandPath(scope), lead(scope))
	fmt.Fprintf(&b, "Every command under `%s %s`, with the arguments and flags it takes. For these commands in use, see [the %s guide](README.md).\n",
		kit.Program, commandPath(scope), app(root, scope).Name())
	if scope.Runnable() || scope.HasSubCommands() {
		writeBody(&b, scope)
	}

	for _, c := range commands {
		if c == scope {
			continue
		}
		writeCommand(&b, c)
	}

	b.WriteString(footer(slug))
	return b.String()
}

func footer(slug string) string {
	return "\n---\n\nEvery command also takes the [flags that work everywhere](" +
		relative(slug, indexPage) + "#flags-that-work-on-every-command).\n"
}

// writeCommand is one command's entry, and the shape never varies: what it is,
// what it holds, how it is invoked, what it takes, and it being used.
//
// A collection is a heading and everything under it is a level down, so the
// page's own contents list reads as the tree it documents rather than as one
// flat run. It stops at three, which is as deep as a contents list is read.
func writeCommand(b *strings.Builder, c *cobra.Command) {
	heading := kit.ReferenceHeading(c)
	level := "###"
	if !strings.Contains(heading, " ") {
		level = "##"
	}
	// A heading here is a command line, and reads as one. The backticks cost
	// nothing: both GitHub and the site drop them when they slugify, so the
	// anchor kit hands a help screen still lands on it.
	fmt.Fprintf(b, "\n%s `%s`\n\n%s\n", level, heading, lead(c))
	writeBody(b, c)
}

// writeBody is everything below a command's description.
func writeBody(b *strings.Builder, c *cobra.Command) {
	if subs := visible(c); len(subs) > 0 {
		names := make([]string, len(subs))
		for i, sub := range subs {
			names[i] = "`" + sub.Name() + "`"
		}
		fmt.Fprintf(b, "\nHolds %s.\n", list(names))
	}
	if !c.Runnable() {
		return
	}

	fmt.Fprintf(b, "\n```\n%s\n```\n", kit.Synopsis(c))

	if example := strings.TrimSpace(c.Example); example != "" {
		fmt.Fprintf(b, "\n```bash\n%s\n```\n", example)
	}

	if flags := c.LocalFlags(); hasOwnFlags(flags) {
		b.WriteString("\n")
		writeFlags(b, flags)
	}
}

// lead is how a command introduces itself: the long form where it has one, and
// its one-line summary where it has not.
func lead(c *cobra.Command) string {
	if long := strings.TrimSpace(c.Long); long != "" {
		return reflow(long)
	}
	return c.Short + "."
}

// writeFlags is the table a reader scans to find the one they want.
func writeFlags(b *strings.Builder, set *pflag.FlagSet) {
	b.WriteString("| Flag | Description |\n| --- | --- |\n")
	set.VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}
		name := "--" + f.Name
		if f.Shorthand != "" {
			name = "-" + f.Shorthand + ", " + name
		}
		if kind := f.Value.Type(); kind != "bool" {
			name += " " + kind
		}
		usage := f.Usage
		if f.DefValue != "" && f.DefValue != "false" && f.DefValue != "0" && f.DefValue != "[]" &&
			!strings.Contains(usage, "default") {
			usage += " (default `" + f.DefValue + "`)"
		}
		fmt.Fprintf(b, "| `%s` | %s |\n", name, escape(usage))
	})
}

// reflow joins a paragraph that was hard-wrapped for a terminal, because a
// markdown reader has a window of their own and wrapping it twice reads as a
// list of fragments.
func reflow(text string) string {
	paragraphs := strings.Split(text, "\n\n")
	for i, p := range paragraphs {
		lines := strings.Split(p, "\n")
		// An indented block is laid out on purpose; only prose is joined.
		if strings.HasPrefix(p, "  ") {
			continue
		}
		for j := range lines {
			lines[j] = strings.TrimSpace(lines[j])
		}
		paragraphs[i] = strings.Join(lines, " ")
	}
	return strings.Join(paragraphs, "\n\n")
}

// link is how one generated page points at a command documented on another.
func link(from string, c *cobra.Command) string {
	target := relative(from, kit.ReferencePage(c))
	if anchor := kit.ReferenceAnchor(c); anchor != "" {
		target += "#" + anchor
	}
	return target
}

// relative is the path from one page to another, as a reader on GitHub follows
// it: every page is a file in docs/, so the walk is from one to the other.
func relative(from, to string) string {
	up := strings.Repeat("../", strings.Count(from, "/"))
	return up + to + ".md"
}

func write(slug, body string) error {
	out := filepath.Join(moduleRoot(), "docs", slug+".md")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	return os.WriteFile(out, []byte(body), 0o644)
}

// moduleRoot is where the repository is, worked out from this file rather than
// from the working directory: a generator that writes wherever it was started
// from writes the reference into somebody's home directory.
func moduleRoot() string {
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		fail(fmt.Errorf("cannot locate the generator's own source"))
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(self)))
}

// isGuide reports whether a command's page is the hand-written guide beside this
// generated reference rather than a page of its own.
func isGuide(root, c *cobra.Command) bool {
	return c.Parent() == root && c.GroupID != kit.GroupSelf && c.HasSubCommands()
}

// app is the top-level command a scope belongs to.
func app(root *cobra.Command, scope *cobra.Command) *cobra.Command {
	for scope.Parent() != nil && scope.Parent() != root {
		scope = scope.Parent()
	}
	return scope
}

func commandPath(c *cobra.Command) string {
	return strings.TrimPrefix(c.CommandPath(), kit.Program+" ")
}

func visible(c *cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	for _, sub := range c.Commands() {
		if !sub.Hidden && sub.Name() != "help" {
			out = append(out, sub)
		}
	}
	return sorted(out)
}

func sorted(in []*cobra.Command) []*cobra.Command {
	out := append([]*cobra.Command(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

func sortedKeys(pages map[string][]*cobra.Command) []string {
	out := make([]string, 0, len(pages))
	for slug := range pages {
		out = append(out, slug)
	}
	sort.Strings(out)
	return out
}

func hasOwnFlags(set *pflag.FlagSet) bool {
	found := false
	set.VisitAll(func(f *pflag.Flag) {
		if !f.Hidden && f.Name != "help" {
			found = true
		}
	})
	return found
}

func list(items []string) string {
	switch len(items) {
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
	}
}

func escape(s string) string { return strings.ReplaceAll(s, "|", `\|`) }

func fail(err error) {
	fmt.Fprintln(os.Stderr, "Error:", err)
	os.Exit(1)
}
