package kit

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestFlagsThatContradictAreRefusedInOneSentence(t *testing.T) {
	for _, tc := range []struct {
		name  string
		given []string
		want  string
	}{
		{"one of them", []string{"computer"}, ""},
		{"two of them", []string{"computer", "shared"}, "--computer and --shared contradict each other."},
		{"all three, typed in another order", []string{"link", "shared", "computer"},
			"--computer, --shared and --link contradict each other."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "list"}
			for _, name := range []string{"computer", "shared", "link"} {
				cmd.Flags().String(name, "", "")
			}
			Exclusive(cmd, "computer", "shared", "link")
			for _, name := range tc.given {
				if err := cmd.Flags().Set(name, "x"); err != nil {
					t.Fatal(err)
				}
			}
			err := Judge(cmd)
			switch {
			case tc.want == "" && err != nil:
				t.Errorf("%v was refused: %v", tc.given, err)
			case tc.want != "" && (err == nil || err.Error() != tc.want):
				t.Errorf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestFlagsThatContradictStayOutOfCompletionOnceOneIsTyped(t *testing.T) {
	cmd := &cobra.Command{Use: "list"}
	cmd.Flags().Bool("read", false, "")
	cmd.Flags().Bool("unread", false, "")
	Exclusive(cmd, "read", "unread")
	for _, name := range []string{"read", "unread"} {
		if got := cmd.Flags().Lookup(name).Annotations; len(got) == 0 {
			t.Errorf("--%s carries no group for completion to read", name)
		}
	}
}
