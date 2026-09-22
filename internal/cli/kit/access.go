package kit

import "github.com/spf13/cobra"

// Access is what somebody may do with a thing that is shared with them.
//
// One word across three apps, because it is one question: a Drive folder, a
// calendar and a Pass vault all hand somebody a key and then decide what that
// key lets them do. Drive and Calendar offer two answers and Pass offers three,
// so the domains differ - as --sort's and --format's do - but the name and the
// words in it do not, and `--access editor` reads the same wherever it is typed.
//
// It replaced a boolean. `--edit` could say only whether the second rung was
// reached, so revoking it was `--edit=false` and a third rung had nowhere to go.
const (
	// Viewer may read the thing and nothing else.
	Viewer = "viewer"
	// Editor may change it.
	Editor = "editor"
	// Limited sees only whether the calendar's owner is busy.
	Limited = "limited"
	// Full sees every detail of every event.
	Full = "full"
)

// AccessUsage is the one thing --access says, wherever it appears.
const AccessUsage = "What they may do with it"

// Access declares --access over the rungs a thing offers.
func AccessFlag(values ...string) *Enum {
	return &Enum{Name: "access", Usage: AccessUsage, Values: values, Default: Viewer}
}

// Viewing declares --access for a thing that is either read or changed, which
// is what Drive and Calendar offer.
func Viewing() *Enum { return AccessFlag(Viewer, Editor) }

// Watching declares --access for a published calendar, where nobody edits and
// the question is how much of it is shown. It opens on the narrower answer,
// which is the one that gives least away.
func Watching() *Enum {
	return &Enum{Name: "access", Usage: AccessUsage, Values: []string{Limited, Full}, Default: Limited}
}

// CanEdit reads the chosen rung as the boolean Drive and Calendar store.
func CanEdit(e *Enum) (bool, error) {
	v, err := e.Value()
	return v == Editor, err
}

// Access is the rung a stored boolean stands for, in the word every screen says
// it in.
func Access(canEdit bool) string {
	if canEdit {
		return Editor
	}
	return Viewer
}

// Changed reports whether the command line said anything about access, for an
// update that leaves what it is not told about alone.
func Changed(c *cobra.Command) bool { return c.Flags().Changed("access") }
