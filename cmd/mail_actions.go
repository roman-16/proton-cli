package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var mailTrashCmd = &cobra.Command{
	Use:   "trash MESSAGE_ID...",
	Short: "Move messages to trash",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runMailTrash,
}

var mailDeleteCmd = &cobra.Command{
	Use:   "delete MESSAGE_ID...",
	Short: "Permanently delete messages",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runMailDelete,
}

var mailMoveCmd = &cobra.Command{
	Use:   "move MESSAGE_ID...",
	Short: "Move messages to a folder",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runMailMove,
}

var mailMarkCmd = &cobra.Command{
	Use:   "mark MESSAGE_ID...",
	Short: "Mark messages as read/unread/starred",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runMailMark,
}

func runMailTrash(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	c, err := getAuthenticatedClient(ctx)
	if err != nil {
		return err
	}

	reqBody := map[string]interface{}{"LabelID": "3", "IDs": args}
	body, _ := json.Marshal(reqBody)
	resp, statusCode, err := c.Do(ctx, "PUT", "/mail/v4/messages/label", nil, string(body), "", "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return fmt.Errorf("trash failed: %s", string(resp))
	}

	fmt.Fprintf(os.Stderr, "Moved %d message(s) to trash.\n", len(args))
	return nil
}

func runMailDelete(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	c, err := getAuthenticatedClient(ctx)
	if err != nil {
		return err
	}

	reqBody := map[string]interface{}{"IDs": args}
	body, _ := json.Marshal(reqBody)
	resp, statusCode, err := c.Do(ctx, "PUT", "/mail/v4/messages/delete", nil, string(body), "", "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return fmt.Errorf("delete failed: %s", string(resp))
	}

	fmt.Fprintf(os.Stderr, "Permanently deleted %d message(s).\n", len(args))
	return nil
}

func runMailMove(cmd *cobra.Command, args []string) error {
	labelID, ok := mailboxLabelIDs[strings.ToLower(mailMoveFolder)]
	if !ok {
		labelID = mailMoveFolder
	}

	ctx := context.Background()
	c, err := getAuthenticatedClient(ctx)
	if err != nil {
		return err
	}

	reqBody := map[string]interface{}{"LabelID": labelID, "IDs": args}
	body, _ := json.Marshal(reqBody)
	resp, statusCode, err := c.Do(ctx, "PUT", "/mail/v4/messages/label", nil, string(body), "", "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return fmt.Errorf("move failed: %s", string(resp))
	}

	fmt.Fprintf(os.Stderr, "Moved %d message(s) to %s.\n", len(args), mailMoveFolder)
	return nil
}

func runMailMark(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	c, err := getAuthenticatedClient(ctx)
	if err != nil {
		return err
	}

	idsBody := map[string]interface{}{"IDs": args}
	body, _ := json.Marshal(idsBody)

	if mailMarkRead {
		resp, statusCode, err := c.Do(ctx, "PUT", "/mail/v4/messages/read", nil, string(body), "", "")
		if err != nil {
			return err
		}
		if statusCode >= 400 {
			return fmt.Errorf("mark read failed: %s", string(resp))
		}
		fmt.Fprintf(os.Stderr, "Marked %d message(s) as read.\n", len(args))
	}

	if mailMarkUnread {
		resp, statusCode, err := c.Do(ctx, "PUT", "/mail/v4/messages/unread", nil, string(body), "", "")
		if err != nil {
			return err
		}
		if statusCode >= 400 {
			return fmt.Errorf("mark unread failed: %s", string(resp))
		}
		fmt.Fprintf(os.Stderr, "Marked %d message(s) as unread.\n", len(args))
	}

	if mailMarkStarred {
		starBody := map[string]interface{}{"LabelID": "10", "IDs": args}
		b, _ := json.Marshal(starBody)
		resp, statusCode, err := c.Do(ctx, "PUT", "/mail/v4/messages/label", nil, string(b), "", "")
		if err != nil {
			return err
		}
		if statusCode >= 400 {
			return fmt.Errorf("star failed: %s", string(resp))
		}
		fmt.Fprintf(os.Stderr, "Starred %d message(s).\n", len(args))
	}

	if mailMarkUnstar {
		unstarBody := map[string]interface{}{"LabelID": "10", "IDs": args}
		b, _ := json.Marshal(unstarBody)
		resp, statusCode, err := c.Do(ctx, "PUT", "/mail/v4/messages/unlabel", nil, string(b), "", "")
		if err != nil {
			return err
		}
		if statusCode >= 400 {
			return fmt.Errorf("unstar failed: %s", string(resp))
		}
		fmt.Fprintf(os.Stderr, "Unstarred %d message(s).\n", len(args))
	}

	return nil
}
