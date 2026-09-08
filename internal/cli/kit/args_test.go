package kit

import (
	"errors"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/spf13/cobra"
)

func TestArityIsReadOffTheUsageLine(t *testing.T) {
	for _, tc := range []struct {
		use         string
		least, most int
	}{
		{"list", 0, 0},
		{"get REF", 1, 1},
		{"set KEY VALUE", 2, 2},
		{"upload SRC [DEST]", 1, 2},
		{"trash [REF...]", 0, unbounded},
		{"reorder REF REF...", 2, unbounded},
		{"add REF CONTACT_REF...", 2, unbounded},
	} {
		least, most := arity(Arguments(tc.use))
		if least != tc.least || most != tc.most {
			t.Errorf("%q takes %d..%d, want %d..%d", tc.use, least, most, tc.least, tc.most)
		}
	}
}

// The complaint is written from the same names the help screen shows, so what a
// reader is told is missing is what they were shown to type.
func TestTheWrongNumberIsRefusedInTheCommandsOwnWords(t *testing.T) {
	get := &cobra.Command{Use: "get REF", Example: "proton mail messages get 'Invoice #2291'"}
	list := &cobra.Command{Use: "list"}
	upload := &cobra.Command{Use: "upload SRC [DEST]"}
	root := &cobra.Command{Use: Program}
	root.AddCommand(get, list, upload)
	InstallArguments(root)

	for _, tc := range []struct {
		name  string
		cmd   *cobra.Command
		args  []string
		says  string
		tries []string
	}{
		{
			name:  "nothing where one is needed",
			cmd:   get,
			says:  "`proton get` needs REF: a full ID, a short ID, or a human handle.",
			tries: []string{"proton mail messages get 'Invoice #2291'", "proton get --help"},
		},
		{
			name:  "the second of two",
			cmd:   upload,
			says:  "`proton upload` needs SRC: a local file or directory to read.",
			tries: []string{"proton upload --help"},
		},
		{
			name:  "more than there are places for",
			cmd:   get,
			args:  []string{"a", "b"},
			says:  "`proton get` takes REF, but 2 arguments were given.",
			tries: []string{"proton get --help"},
		},
		{
			name:  "one where none are taken",
			cmd:   list,
			args:  []string{"a"},
			says:  "`proton list` takes no arguments, but 1 argument was given.",
			tries: []string{"proton list --help"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cmd.Args(tc.cmd, tc.args)
			if err == nil {
				t.Fatalf("%v was accepted", tc.args)
			}
			if err.Error() != tc.says {
				t.Errorf("said %q, want %q", err.Error(), tc.says)
			}
			var hinter errs.Hinter
			if !errors.As(err, &hinter) {
				t.Fatal("the refusal offers nothing to do about it")
			}
			if strings.Join(hinter.Hints(), "\n") != strings.Join(tc.tries, "\n") {
				t.Errorf("offered %v, want %v", hinter.Hints(), tc.tries)
			}
		})
	}
}

// A command that knows what its arguments may be says so itself, once the count
// has established that there are the right number of them to look at.
func TestACommandsOwnCheckRunsOnTheRightNumberOfArguments(t *testing.T) {
	looked := 0
	set := &cobra.Command{
		Use: "set KEY VALUE",
		Args: func(_ *cobra.Command, args []string) error {
			looked++
			if len(args) != 2 {
				t.Fatalf("the command was handed %d arguments to judge", len(args))
			}
			return errs.Problemf("no setting called %q", args[0])
		},
	}
	root := &cobra.Command{Use: Program}
	root.AddCommand(set)
	InstallArguments(root)

	if err := set.Args(set, []string{"one"}); err == nil {
		t.Error("a short command line reached the command's own check")
	}
	if err := set.Args(set, []string{"a", "b"}); err == nil {
		t.Error("the command's own check did not run")
	}
	if looked != 1 {
		t.Errorf("the command's own check ran %d times, want 1", looked)
	}
}

// The values a command offers are the values it accepts: one declaration, so a
// shell cannot suggest something the command then refuses.
func TestAnArgumentFromAFixedListIsHeldToIt(t *testing.T) {
	shell := &cobra.Command{Use: "completion SHELL", ValidArgs: []string{"bash", "zsh\tthe z shell"}}
	root := &cobra.Command{Use: Program}
	root.AddCommand(shell)
	InstallArguments(root)

	if err := shell.Args(shell, []string{"zsh"}); err != nil {
		t.Errorf("a value it offers was refused: %v", err)
	}
	err := shell.Args(shell, []string{"bosh"})
	if err == nil {
		t.Fatal("a value it never offers was accepted")
	}
	if want := "`proton completion` accepts: bash, zsh."; err.Error() != want {
		t.Errorf("said %q, want %q", err.Error(), want)
	}
}
