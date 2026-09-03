package kit

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestArgumentsReadsAUsageLine(t *testing.T) {
	for _, tc := range []struct {
		use  string
		want []Argument
	}{
		{"get REF", []Argument{{Name: "REF"}}},
		{"trash [REF...]", []Argument{{Name: "REF", Variadic: true}}},
		{"download REF [ATTACHMENT_REF]", []Argument{{Name: "REF"}, {Name: "ATTACHMENT_REF"}}},
		{"add REF CONTACT_REF...", []Argument{{Name: "REF"}, {Name: "CONTACT_REF", Variadic: true}}},
		{"list", nil},
		{"mark read", nil},
	} {
		got := Arguments(tc.use)
		if len(got) != len(tc.want) {
			t.Errorf("Arguments(%q) = %v, want %v", tc.use, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("Arguments(%q)[%d] = %v, want %v", tc.use, i, got[i], tc.want[i])
			}
		}
	}
}

// The argument that swallows the rest still stands at every position after it,
// so the tenth reference on a line is the same kind of thing as the first.
func TestAtFollowsAVariadicPastItsPosition(t *testing.T) {
	args := Arguments("add REF CONTACT_REF...")
	for i, want := range map[int]string{0: "REF", 1: "CONTACT_REF", 7: "CONTACT_REF"} {
		if got := At(args, i).Name; got != want {
			t.Errorf("At(%d) = %q, want %q", i, got, want)
		}
	}
	if got := At(Arguments("get REF"), 1).Name; got != "" {
		t.Errorf("At past a fixed argument = %q, want nothing", got)
	}
}

// A tree in the shape the CLI uses: a collection that lists wholly, a verb group
// under it that holds nothing of its own, and a sub-collection whose own listing
// has to be told what to look inside.
func testTree() *cobra.Command {
	root := &cobra.Command{Use: Program}
	mail := &cobra.Command{Use: "mail"}
	messages := &cobra.Command{Use: "messages"}
	messages.AddCommand(
		&cobra.Command{Use: "list"},
		&cobra.Command{Use: "get REF"},
	)
	mark := &cobra.Command{Use: "mark"}
	mark.AddCommand(&cobra.Command{Use: "read [REF...]"})
	attachments := &cobra.Command{Use: "attachments"}
	attachments.AddCommand(
		&cobra.Command{Use: "list REF"},
		&cobra.Command{Use: "download REF [ATTACHMENT_REF]"},
	)
	messages.AddCommand(mark, attachments)
	mail.AddCommand(messages)
	root.AddCommand(mail)
	return root
}

func find(t *testing.T, root *cobra.Command, path ...string) *cobra.Command {
	t.Helper()
	c, _, err := root.Find(path)
	if err != nil {
		t.Fatalf("find %v: %v", path, err)
	}
	return c
}

// A REF names the nearest collection above it that could have been listed
// without naming anything first, which is the only listing that could have put
// the reference on the screen in the first place.
func TestAddressedReachesPastWhatHoldsNothingOfItsOwn(t *testing.T) {
	root := testTree()
	for _, tc := range []struct {
		path []string
		want string
	}{
		{[]string{"mail", "messages", "get"}, "mail messages"},
		{[]string{"mail", "messages", "mark", "read"}, "mail messages"},
		{[]string{"mail", "messages", "attachments", "download"}, "mail messages"},
		{[]string{"mail", "messages", "attachments", "list"}, "mail messages"},
	} {
		if got := Addressed(find(t, root, tc.path...)); got != tc.want {
			t.Errorf("Addressed(%v) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// A second reference names the collection the command is part of: the
// attachments of the message it already addresses.
func TestASecondReferenceNamesTheCollectionItIsPartOf(t *testing.T) {
	download := find(t, testTree(), "mail", "messages", "attachments", "download")
	args := Arguments(download.Use)
	if got := Picks(download, At(args, 0)); got != "mail messages" {
		t.Errorf("first argument picks %q, want mail messages", got)
	}
	if got := Picks(download, At(args, 1)); got != "mail messages attachments" {
		t.Errorf("second argument picks %q, want mail messages attachments", got)
	}
}

// A command whose REF is not what the tree above it holds says so, and what it
// says wins.
func TestADeclaredCollectionOverridesTheTree(t *testing.T) {
	root := testTree()
	get := find(t, root, "mail", "messages", "get")
	get.Annotations = map[string]string{Addresses: "pass items"}
	if got := Addressed(get); got != "pass items" {
		t.Errorf("Addressed = %q, want pass items", got)
	}
}

// An argument naming something this CLI does not hold is left to the shell,
// which knows about files and needs no telling.
func TestAnArgumentThatNamesNothingPicksNothing(t *testing.T) {
	get := find(t, testTree(), "mail", "messages", "get")
	for _, name := range []string{"PATH", "SRC", "EMAIL", "KEY"} {
		if got := Picks(get, Argument{Name: name}); got != "" {
			t.Errorf("%s picks %q, want nothing", name, got)
		}
	}
}

// A command that completes its own arguments keeps doing so, and one that takes
// no reference is left alone entirely.
func TestCompleteReferencesLeavesTheOthersAlone(t *testing.T) {
	root := testTree()
	list := find(t, root, "mail", "messages", "list")
	own := &cobra.Command{
		Use: "set KEY VALUE",
		ValidArgsFunction: func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			return []string{"mine"}, cobra.ShellCompDirectiveNoFileComp
		},
	}
	find(t, root, "mail", "messages").AddCommand(own)

	CompleteReferences(root)

	if list.ValidArgsFunction != nil {
		t.Error("a command taking no reference was given a completion")
	}
	if find(t, root, "mail", "messages", "get").ValidArgsFunction == nil {
		t.Error("a command taking a reference was not given a completion")
	}
	got, _ := own.ValidArgsFunction(own, nil, "")
	if len(got) != 1 || got[0] != "mine" {
		t.Errorf("a command's own completion was replaced: %v", got)
	}
}
