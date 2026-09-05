package cli

import (
	_ "embed"
	"fmt"
	"strings"
	"text/template"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The instructions an agent reads before it drives this CLI.
//
// An agent arrives knowing shells and knowing nothing about this tool, and the
// expensive thing it does with that gap is guess: a flag that does not exist, a
// listing it reads one page of, an envelope field it invents. So what it is
// handed is what this build is - the shape of a command, how an answer is
// shaped, what every call should carry, where every command lives - and then it
// is sent to `--help` for the syntax.
//
// It says nothing about how to behave. Whether to ask before changing something,
// what may be done unattended, where a secret may be written: those belong to
// whoever runs the agent, they differ between deployments, and a tool that
// shipped its own answers would be arguing with its operator's. What this file
// owes is that the facts are complete, so any policy can be written on top of
// them.
//
// The prose is beside this file, with the same standing as a command's Long. The
// map is read off the tree, because that is the half that rots: a renamed
// collection would leave a hand-written list telling an agent to run something
// that is gone. And it is emitted rather than committed, so it describes the
// build that printed it and there is no second copy to keep in step.
//
// The format is Agent Skills (https://agentskills.io), which is what every agent
// that reads a skill at all reads. Where a given agent keeps its skills is that
// agent's business and changes; nothing here names one.

//go:embed skill.md
var contractSource string

var contract = template.Must(template.New("skill").Parse(contractSource))

// skillDescription is what an agent reads to decide whether this is the tool for
// what it was asked. It names the products, the words a person uses for them,
// and the ones this does not cover - because a skill that volunteers for a
// question about Proton VPN is worse than one that never triggers.
const skillDescription = "Work with a Proton account through the " + kit.Program +
	" command-line tool - read, search, send and organize Proton Mail; upload, download and share Drive files; " +
	"read and create Calendar events; read Pass logins, TOTP codes and aliases; look up Contacts. " +
	"Use when the user asks about their Proton mail, files, calendar, passwords or contacts, or mentions " +
	kit.Alias + " or the " + kit.Program + " command. " +
	"Not for Proton VPN, Wallet, Docs, Meet or Lumo, which it does not cover."

const skillCompatibility = "Requires the " + kit.Program + " binary on PATH and an account signed in with `" +
	kit.Program + " account login`."

// skill is the document, in the shape a machine format writes it: the
// frontmatter's fields, and the instructions under them.
type skill struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	Compatibility string `json:"compatibility"`
	License       string `json:"license"`
	Body          string `json:"body"`
}

// Text is the whole file, as an agent reads it off disk.
func (s skill) Text() string {
	return "---\n" +
		"name: " + s.Name + "\n" +
		"description: " + s.Description + "\n" +
		"compatibility: " + s.Compatibility + "\n" +
		"license: " + s.License + "\n" +
		"---\n\n" + s.Body
}

// newSkill writes the skill for the tree and the build that are running.
func newSkill(root *cobra.Command, version string) skill {
	var body strings.Builder
	err := contract.Execute(&body, map[string]string{
		"Commands": commandMap(root),
		"Docs":     kit.Docs,
		"Filters":  listingFilters(root),
		"Flags":    globalFlagTable(root),
		"Program":  kit.Program,
		"Version":  version,
	})
	// The template is compiled in and its data is a map of strings, so a failure
	// here is this file being wrong rather than anything a run could cause.
	if err != nil {
		panic(err)
	}
	return skill{
		// The name is also the directory the file is saved in, which the
		// specification requires it to match: the project's name, as every other
		// installed artifact carries it.
		Name:          kit.Alias,
		Description:   skillDescription,
		Compatibility: skillCompatibility,
		License:       "MIT",
		Body:          strings.TrimRight(body.String(), "\n"),
	}
}

func skillCmd(root *cobra.Command, version string) *cobra.Command {
	var bodyOnly bool
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Print the skill that teaches an AI agent to use " + kit.Program,
		Long: `Print the skill that teaches an AI agent to use ` + kit.Program + `.

A skill is a SKILL.md an agent reads before it acts (https://agentskills.io):
what ` + kit.Program + ` is for, how a command is shaped, what an answer looks like,
the flags every call takes, and where every command lives. It describes the
tool and nothing else - how the agent should behave with an account is yours to
say, in your own instructions. It is written from this build, so it names
exactly the commands this ` + kit.Program + ` has, and it tells the agent to print it
again when the installed ` + kit.Program + ` is a different one.

Save it as SKILL.md inside a directory named ` + kit.Alias + `, wherever your agent
reads skills. An agent that reads it as it runs rather than from a saved file
wants --body-only, which leaves the frontmatter out.`,
		Args: cobra.NoArgs,
		// No authentication: this is about the binary, not the account.
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			s := newSkill(root, version)
			text := s.Text()
			if bodyOnly {
				text = s.Body
			}
			return kit.Read(c, ui.DocumentSpec{
				BodyOnly: true,
				Parts:    []ui.Part{{Body: text}},
				Object:   s,
			})
		}),
	}
	cmd.Flags().BoolVar(&bodyOnly, "body-only", false, "Emit only the body, with no frontmatter")
	return cmd
}

// commandMap is where everything lives: a section per app, a line per group that
// holds verbs, and one closing section for the commands that stand on their own
// at the root.
//
// A line per group rather than per command is what keeps this a routing table
// instead of a copy of the reference. It names the collection, says what it
// holds, and lists its verbs - which is enough to land on the right `--help` in
// one step instead of three.
//
// The apps come in the order the help screen puts them, because an agent reading
// both should meet one map of the product rather than two.
func commandMap(root *cobra.Command) string {
	var b strings.Builder
	for _, app := range byGroup(root) {
		if !app.HasSubCommands() {
			continue
		}
		fmt.Fprintf(&b, "\n### %s - %s\n\n", app.Name(), app.Short)
		for _, holder := range holders(app) {
			// The app's own verbs need no description: the heading above them is it.
			described := ""
			if holder != app {
				described = holder.Short + ": "
			}
			fmt.Fprintf(&b, "- `%s` - %s%s\n",
				holder.CommandPath(), described, strings.Join(verbsUnder(holder), ", "))
		}
	}

	fmt.Fprintf(&b, "\n### %s - the tool itself, and the raw API\n\n", kit.Program)
	for _, c := range byGroup(root) {
		if c.HasSubCommands() {
			continue
		}
		fmt.Fprintf(&b, "- `%s` - %s\n", c.CommandPath(), c.Short)
	}
	return b.String()
}

// byGroup is the root's commands in the order the help screen lists them: the
// apps, then the account, then the tool itself.
func byGroup(root *cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	for _, group := range root.Groups() {
		for _, c := range visibleSubcommands(root) {
			if c.GroupID == group.ID {
				out = append(out, c)
			}
		}
	}
	return out
}

// holders is every command in a subtree that has verbs of its own, in tree
// order, starting with the subtree itself.
func holders(c *cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	if len(verbsUnder(c)) > 0 {
		out = append(out, c)
	}
	for _, sub := range visibleSubcommands(c) {
		if sub.HasSubCommands() {
			out = append(out, holders(sub)...)
		}
	}
	return out
}

// verbsUnder is what a group can be asked to do.
func verbsUnder(c *cobra.Command) []string {
	var out []string
	for _, sub := range visibleSubcommands(c) {
		if sub.Runnable() {
			out = append(out, sub.Name())
		}
	}
	return out
}

// listingFilters is what each listing can be narrowed by.
//
// It is the one place per-command flags are worth their room. Not knowing that a
// listing takes a filter is the mistake that answers wrongly and exits zero -
// an agent that has never heard of --folder reports no mail from somebody who
// writes weekly - while every other flag it has not heard of fails loudly and
// sends it to --help. A collection's `list` answers for its bulk verbs too,
// because the interface guarantees a listing takes whatever narrows them.
//
// The value is named by its type rather than described, which says the one thing
// the name does not - whether the flag takes anything at all - and leaves what
// to put there to --help, where the defaults and the accepted values are.
func listingFilters(root *cobra.Command) string {
	var b strings.Builder
	var walk func([]*cobra.Command)
	walk = func(cmds []*cobra.Command) {
		for _, c := range cmds {
			if c.Name() == "list" {
				if taken := flagSignature(c); taken != "" {
					fmt.Fprintf(&b, "- `%s %s`\n", kit.Synopsis(c), taken)
				}
			}
			walk(visibleSubcommands(c))
		}
	}
	walk(byGroup(root))
	return b.String()
}

// flagSignature is a command's own flags, as they would be typed.
func flagSignature(c *cobra.Command) string {
	var out []string
	c.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}
		name := "--" + f.Name
		if kind := f.Value.Type(); kind != "bool" {
			name += " " + kind
		}
		out = append(out, name)
	})
	return strings.Join(out, " ")
}

// globalFlagTable is the flags every command takes, which is where most of an
// agent's calls differ from a person's.
func globalFlagTable(root *cobra.Command) string {
	var b strings.Builder
	b.WriteString("| Flag | What it does |\n| --- | --- |\n")
	root.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		name := "--" + f.Name
		if f.Shorthand != "" {
			name = "-" + f.Shorthand + ", " + name
		}
		if kind := f.Value.Type(); kind != "bool" {
			name += " " + kind
		}
		fmt.Fprintf(&b, "| `%s` | %s |\n", name, strings.ReplaceAll(f.Usage, "|", `\|`))
	})
	return b.String()
}
