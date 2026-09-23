package kit

import (
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

func Exclusive(c *cobra.Command, names ...string) {
	c.MarkFlagsMutuallyExclusive(names...)
	registerCheck(c, names[0], nil, exclusive{cmd: c, names: names})
}

type exclusive struct {
	cmd   *cobra.Command
	names []string
}

func (e exclusive) validate() error {
	var given []string
	for _, name := range e.names {
		if e.cmd.Flags().Changed(name) {
			given = append(given, "--"+name)
		}
	}
	if len(given) < 2 {
		return nil
	}
	return Fail("%s contradict each other.", ui.Listing(given))
}
