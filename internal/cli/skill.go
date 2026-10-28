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

//go:embed skill.md
var contractSource string

var contract = template.Must(template.New("skill").Parse(contractSource))

// skillDescription is what an agent reads to decide whether this is the tool for
// what it was asked. It names the products, the words a person uses for them,
// and the ones this does not cover - because a skill that volunteers for a
// question about Proton VPN is worse than one that never triggers.
const skillDescription = "Work with a Proton account through the " + kit.Program +
	" command-line tool - read, search, send and organize Proton Mail; upload, download and share Drive files; " +
	"read and create Calendar events and give them Proton Meet links; read Pass logins, TOTP codes and aliases; " +
	"manage Proton Meet meetings and rooms; look up Contacts. " +
	"Use when the user asks about their Proton mail, files, calendar, passwords, meetings or contacts, or mentions " +
	kit.Alias + " or the " + kit.Program + " command. " +
	"Not for Proton VPN, Wallet, Docs or Lumo, which it does not cover."

const skillCompatibility = "Requires the " + kit.Program + " binary on PATH and an account signed in with `" +
	kit.Program + " account login`."

type skill struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	Compatibility string `json:"compatibility"`
	License       string `json:"license"`
	Body          string `json:"body"`
}

func (s skill) Text() string {
	return "---\n" +
		"name: " + s.Name + "\n" +
		"description: " + s.Description + "\n" +
		"compatibility: " + s.Compatibility + "\n" +
		"license: " + s.License + "\n" +
		"---\n\n" + s.Body
}

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
	if err != nil {
		panic(err)
	}
	return skill{
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

func commandMap(root *cobra.Command) string {
	var b strings.Builder
	for _, app := range byGroup(root) {
		if !app.HasSubCommands() {
			continue
		}
		fmt.Fprintf(&b, "\n### %s - %s\n\n", app.Name(), app.Short)
		for _, holder := range holders(app) {
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

func verbsUnder(c *cobra.Command) []string {
	var out []string
	for _, sub := range visibleSubcommands(c) {
		if sub.Runnable() {
			out = append(out, sub.Name())
		}
	}
	return out
}

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
