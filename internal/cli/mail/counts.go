package mail

import (
	"strconv"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

func countCmd(threads bool) *cobra.Command {
	short := "Count the messages in each folder and label"
	counted := "UNREAD is how many are unread, and TOTAL how many there are. Without\n" +
		"--folder every folder and label has a row, and while categories are on, so\n" +
		"does each category the inbox shows as a tab."
	if threads {
		short = "Count the threads in each folder and label"
		counted = "UNREAD is how many are unread, and TOTAL how many there are. A thread\n" +
			"counts as unread while any message in it is. Without --folder every folder\n" +
			"and label has a row, and while categories are on, so does each category the\n" +
			"inbox shows as a tab."
	}
	var folder string
	c := &cobra.Command{
		Use:   "count",
		Short: short,
		Long: short + ".\n\n" + counted + "\n\n" +
			"To count what a filter matches, the last line of `list` says how many.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			rows, err := c.App.Mail.Counts(c.Ctx, threads, folder)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[mailsvc.MailboxCount]{
				Noun: "folders or labels", Columns: countColumns(),
				Total: ui.Unknown, Page: ui.Unpaged,
			}, rows)
		}),
	}
	c.Flags().StringVar(&folder, "folder", "", "Folder or label to count")
	kit.Completes(c, "folder", mailsvc.SystemFolderNames())
	return c
}

func countColumns() []ui.Column[mailsvc.MailboxCount] {
	return []ui.Column[mailsvc.MailboxCount]{
		{Header: "NAME", Flex: true, Cell: func(m mailsvc.MailboxCount) string { return m.Name }},
		{Header: "UNREAD", Right: true, Cell: func(m mailsvc.MailboxCount) string { return strconv.Itoa(m.Unread) }},
		{Header: "TOTAL", Right: true, Cell: func(m mailsvc.MailboxCount) string { return strconv.Itoa(m.Total) }},
	}
}
