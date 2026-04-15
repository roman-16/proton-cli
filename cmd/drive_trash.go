package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var driveTrashCmd = &cobra.Command{
	Use:   "trash",
	Short: "Manage drive trash",
}

var driveTrashListCmd = &cobra.Command{
	Use:   "list",
	Short: "List trashed items",
	RunE:  runDriveTrashList,
}

var driveTrashRestoreCmd = &cobra.Command{
	Use:   "restore LINK_ID...",
	Short: "Restore items from trash",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runDriveTrashRestore,
}

var driveTrashEmptyCmd = &cobra.Command{
	Use:   "empty",
	Short: "Empty the trash",
	RunE:  runDriveTrashEmpty,
}

func runDriveTrashList(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	c, err := getAuthenticatedClient(ctx)
	if err != nil {
		return err
	}

	volumeID, err := getDefaultVolumeID(ctx, c)
	if err != nil {
		return err
	}

	resp, _, err := c.Do(ctx, "GET",
		fmt.Sprintf("/drive/volumes/%s/trash", volumeID),
		map[string]string{"Page": "0", "PageSize": "150"}, "", "", "")
	if err != nil {
		return err
	}

	if flagJSON {
		printJSON(resp)
		return nil
	}

	// Trash endpoint returns ShareID + LinkIDs grouped by parent
	var res struct {
		Trash []struct {
			ShareID   string
			LinkIDs   []string
			ParentIDs []string
		}
	}
	if err := json.Unmarshal(resp, &res); err != nil {
		return err
	}

	// Collect all link IDs and their share
	type trashEntry struct {
		shareID, linkID string
	}
	var entries []trashEntry
	for _, t := range res.Trash {
		for _, id := range t.LinkIDs {
			entries = append(entries, trashEntry{shareID: t.ShareID, linkID: id})
		}
	}

	if len(entries) == 0 {
		fmt.Fprintln(os.Stderr, "(trash is empty)")
		return nil
	}

	// Fetch metadata for each link
	headers := []string{"LINK_ID", "TYPE", "SIZE", "TRASHED", "NAME"}
	var rows [][]string
	for _, e := range entries {
		linkBody, _, err := c.Do(ctx, "GET",
			fmt.Sprintf("/drive/shares/%s/links/%s", e.shareID, e.linkID),
			nil, "", "", "")
		if err != nil {
			rows = append(rows, []string{e.linkID, "?", "", "", "(fetch failed)"})
			continue
		}

		var linkRes struct {
			Link struct {
				LinkID  string
				Type    int
				Size    float64
				Trashed int64
				Name    string
			}
		}
		json.Unmarshal(linkBody, &linkRes)
		l := linkRes.Link

		typeStr := "FILE"
		if l.Type == 1 {
			typeStr = "DIR "
		}
		trashed := ""
		if l.Trashed > 0 {
			trashed = time.Unix(l.Trashed, 0).Local().Format("2006-01-02 15:04")
		}
		// Name is encrypted — show link ID as identifier
		rows = append(rows, []string{l.LinkID, typeStr, fmt.Sprintf("%.0f", l.Size), trashed, "(encrypted)"})
	}

	printTable(headers, rows)
	fmt.Fprintf(os.Stderr, "\n%d item(s) in trash\n", len(entries))
	return nil
}

func runDriveTrashRestore(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	c, err := getAuthenticatedClient(ctx)
	if err != nil {
		return err
	}

	volumeID, err := getDefaultVolumeID(ctx, c)
	if err != nil {
		return err
	}

	reqBody := map[string]interface{}{"LinkIDs": args}
	body, _ := json.Marshal(reqBody)
	resp, statusCode, err := c.Do(ctx, "PUT",
		fmt.Sprintf("/drive/v2/volumes/%s/trash/restore_multiple", volumeID),
		nil, string(body), "", "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return fmt.Errorf("restore failed: %s", string(resp))
	}

	fmt.Fprintf(os.Stderr, "Restored %d item(s) from trash.\n", len(args))
	return nil
}

func runDriveTrashEmpty(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	c, err := getAuthenticatedClient(ctx)
	if err != nil {
		return err
	}

	volumeID, err := getDefaultVolumeID(ctx, c)
	if err != nil {
		return err
	}

	resp, statusCode, err := c.Do(ctx, "DELETE",
		fmt.Sprintf("/drive/volumes/%s/trash", volumeID),
		nil, "", "", "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return fmt.Errorf("empty trash failed: %s", string(resp))
	}

	fmt.Fprintf(os.Stderr, "Trash emptied.\n")
	return nil
}
