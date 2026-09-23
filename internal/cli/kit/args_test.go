package kit

import (
	"errors"
	"slices"
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

// An address is judged the same way on every command that takes one, before the
// command sees it, and the refusal shows the command's own way of writing one.
func TestAnEmailArgumentHasToBeABareAddress(t *testing.T) {
	add := &cobra.Command{Use: "add REF EMAIL", Example: "proton calendar settings calendars share add Work jane@proton.me"}
	root := &cobra.Command{Use: Program}
	root.AddCommand(add)
	InstallArguments(root)

	for _, good := range []string{"jane@proton.me", "jane.roe+team@example.com"} {
		if err := add.Args(add, []string{"Work", good}); err != nil {
			t.Errorf("%q was refused: %v", good, err)
		}
	}
	for _, bad := range []string{"jane", "jane@", "@proton.me", "Jane <jane@proton.me>", "jane roe@proton.me", "none"} {
		err := add.Args(add, []string{"Work", bad})
		var problem *errs.Problem
		if !errors.As(err, &problem) {
			t.Errorf("%q was accepted", bad)
			continue
		}
		if want := `"` + bad + `" is not an email address.`; problem.Error() != want {
			t.Errorf("said %q, want %q", problem.Error(), want)
		}
		if hints := problem.Hints(); len(hints) != 1 || hints[0] != add.Example {
			t.Errorf("offered %v, want the command's own example", hints)
		}
	}
}

// A sender is an address or a whole domain, and a domain is written the way an
// address ends.
func TestASenderIsAnAddressOrADomain(t *testing.T) {
	block := &cobra.Command{Use: "block SENDER...", Example: "proton mail settings senders block spammer@example.com"}
	root := &cobra.Command{Use: Program}
	root.AddCommand(block)
	InstallArguments(root)

	if err := block.Args(block, []string{"spammer@example.com", "@example.com"}); err != nil {
		t.Errorf("an address and a domain were refused: %v", err)
	}
	for _, bad := range []string{"example.com", "@", "@exa mple.com", "spammer"} {
		if err := block.Args(block, []string{bad}); err == nil {
			t.Errorf("%q was accepted as a sender", bad)
		}
	}
}

// The word that takes a value away is an argument only where the command says
// so, and its refusal shows that way of writing it too.
func TestNoneIsTakenWhereTheCommandSaysSo(t *testing.T) {
	set := &cobra.Command{
		Use:         "set EMAIL",
		Annotations: map[string]string{TakesNone: "EMAIL"},
		Example:     "proton account settings recovery-email set jane.roe@example.com\nproton account settings recovery-email set none",
	}
	root := &cobra.Command{Use: Program}
	root.AddCommand(set)
	InstallArguments(root)

	for _, none := range []string{"none", "NONE"} {
		if err := set.Args(set, []string{none}); err != nil {
			t.Errorf("%q was refused: %v", none, err)
		}
	}
	err := set.Args(set, []string{"jane"})
	var problem *errs.Problem
	if !errors.As(err, &problem) {
		t.Fatal("a value that is neither was accepted")
	}
	want := []string{
		"proton account settings recovery-email set jane.roe@example.com",
		"proton account settings recovery-email set none",
	}
	if !slices.Equal(problem.Hints(), want) {
		t.Errorf("offered %v, want %v", problem.Hints(), want)
	}
}
