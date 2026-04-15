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

var mailSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search messages",
	RunE:  runMailSearch,
}

func runMailSearch(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	c, err := getAuthenticatedClient(ctx)
	if err != nil {
		return err
	}

	labelID, ok := mailboxLabelIDs[strings.ToLower(mailSearchFolder)]
	if !ok {
		labelID = mailSearchFolder
	}

	query := map[string]string{
		"LabelID":  labelID,
		"Sort":     "Time",
		"Desc":     "1",
		"PageSize": fmt.Sprintf("%d", mailSearchLimit),
	}
	if mailSearchKeyword != "" {
		query["Keyword"] = mailSearchKeyword
	}
	if mailSearchFrom != "" {
		query["From"] = mailSearchFrom
	}
	if mailSearchTo != "" {
		query["To"] = mailSearchTo
	}
	if mailSearchSubject != "" {
		query["Subject"] = mailSearchSubject
	}
	if mailSearchAfter != "" {
		t, err := time.Parse("2006-01-02", mailSearchAfter)
		if err != nil {
			return fmt.Errorf("invalid --after: %w", err)
		}
		query["Begin"] = fmt.Sprintf("%d", t.Unix())
	}
	if mailSearchBefore != "" {
		t, err := time.Parse("2006-01-02", mailSearchBefore)
		if err != nil {
			return fmt.Errorf("invalid --before: %w", err)
		}
		query["End"] = fmt.Sprintf("%d", t.Unix())
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
	fmt.Fprintf(os.Stderr, "\n%d results\n", res.Total)
	return nil
}
