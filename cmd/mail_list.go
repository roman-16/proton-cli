package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var mailListCmd = &cobra.Command{
	Use:   "list",
	Short: "List messages",
	RunE:  runMailList,
}

func runMailList(cmd *cobra.Command, args []string) error {
	labelID, ok := mailboxLabelIDs[strings.ToLower(mailListFolder)]
	if !ok {
		labelID = mailListFolder
	}

	ctx := context.Background()
	c, err := getAuthenticatedClient(ctx)
	if err != nil {
		return err
	}

	query := map[string]string{
		"LabelID":  labelID,
		"Page":     fmt.Sprintf("%d", mailListPage),
		"PageSize": fmt.Sprintf("%d", mailListPageSize),
		"Sort":     "Time",
		"Desc":     "1",
	}
	if mailListUnread {
		query["Unread"] = "1"
	}

	body, _, err := c.Do(ctx, "GET", "/mail/v4/messages", query, "", "", "")
	if err != nil {
		return err
	}

	var res struct {
		Total    int
		Messages []struct {
			ID      string
			Subject string
			Unread  int
			Time    int64
			Sender  struct {
				Name    string
				Address string
			}
			NumAttachments int
		}
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return err
	}

	if flagJSON {
		printJSON(body)
		return nil
	}

	headers := []string{"ID", "FROM", "SUBJECT", "DATE", "⚑"}
	var rows [][]string
	for _, msg := range res.Messages {
		from := msg.Sender.Address
		if msg.Sender.Name != "" {
			from = msg.Sender.Name
		}
		date := time.Unix(msg.Time, 0).Local().Format("2006-01-02 15:04")
		flags := ""
		if msg.Unread == 1 {
			flags += "●"
		}
		if msg.NumAttachments > 0 {
			flags += "📎"
		}
		rows = append(rows, []string{msg.ID, from, msg.Subject, date, flags})
	}

	printTable(headers, rows)
	fmt.Fprintf(os.Stderr, "\n%d messages total (page %d)\n", res.Total, mailListPage)
	return nil
}
