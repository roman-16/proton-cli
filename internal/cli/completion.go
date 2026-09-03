package cli

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/spf13/cobra"
)

// completionCmd exposes cobra's generator under the tool's own group.
//
// It stays visible: completion is one of the two things that make a tree this
// large usable at all, the other being that every leaf documents itself.
//
// One script serves both names. Cobra writes a script for whatever the root is
// called, and the binary answers to two names, so the generated script is
// extended to register the second one before it is written out.
func completionCmd(root *cobra.Command) *cobra.Command {
	c := &cobra.Command{
		Use:   "completion",
		Short: "Generate a shell completion script",
		Long: `Generate a shell completion script.

Completion knows the whole command tree, every flag, and the values each
enumerated flag accepts - so it offers folder names, item types, output formats
and setting keys as you type them.

Where a command takes a reference it offers back what your listings showed: the
short ID, and the subject, name or address beside it. It reads what this machine
remembers rather than asking Proton, so a collection you have not listed yet
offers nothing and says which listing would fill it.

One script covers both ` + kit.Program + ` and ` + kit.Alias + `.

  bash        ` + kit.Program + ` completion bash > /etc/bash_completion.d/` + kit.Program + `
  zsh         ` + kit.Program + ` completion zsh > "${fpath[1]}/_` + kit.Program + `"
  fish        ` + kit.Program + ` completion fish > ~/.config/fish/completions/` + kit.Program + `.fish
  powershell  ` + kit.Program + ` completion powershell | Out-String | Invoke-Expression`,
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			var buf bytes.Buffer
			var err error
			switch args[0] {
			case "bash":
				err = root.GenBashCompletionV2(&buf, true)
			case "zsh":
				err = root.GenZshCompletion(&buf)
			case "fish":
				err = root.GenFishCompletion(&buf, true)
			default:
				err = root.GenPowerShellCompletionWithDesc(&buf)
			}
			if err != nil {
				return err
			}
			_, err = io.WriteString(cmd.OutOrStdout(), alsoCompleteAlias(args[0], buf.String()))
			return err
		},
	}
	return c
}

// alsoCompleteAlias returns script with kit.Alias registered against the same
// completion function kit.Program uses.
//
// Where a shell can be told about the second name at the end, it is appended.
// Zsh is the exception: compinit reads the `#compdef` line to decide which
// commands a file is for, and a file it never loads cannot register anything,
// so the alias has to be named in the header.
func alsoCompleteAlias(shell, script string) string {
	switch shell {
	case "bash":
		// The two branches mirror cobra's own, which picks -o nospace by the
		// bash version rather than by the command.
		return script + fmt.Sprintf(`
if [[ $(type -t compopt) = "builtin" ]]; then
    complete -o default -F __start_%[1]s %[2]s
else
    complete -o default -o nospace -F __start_%[1]s %[2]s
fi
`, kit.Program, kit.Alias)
	case "zsh":
		script = strings.Replace(script,
			"#compdef "+kit.Program+"\n",
			"#compdef "+kit.Program+" "+kit.Alias+"\n", 1)
		return script + fmt.Sprintf("\ncompdef _%s %s\n", kit.Program, kit.Alias)
	case "fish":
		return script + fmt.Sprintf("\ncomplete -c %s -w %s\n", kit.Alias, kit.Program)
	default:
		return script + fmt.Sprintf(
			"\nRegister-ArgumentCompleter -CommandName '%s' -ScriptBlock ${__%sCompleterBlock}\n",
			kit.Alias, strings.ReplaceAll(kit.Program, "-", "_"))
	}
}
