package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const (
	labelTypeMessageLabel  = 1
	labelTypeMessageFolder = 3
)

var mailLabelsCmd = &cobra.Command{
	Use:   "labels",
	Short: "Manage mail labels and folders",
}

var mailLabelsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List labels and folders",
	RunE:  runMailLabelsList,
}

var (
	labelCreateName   string
	labelCreateColor  string
	labelCreateFolder bool
)

var mailLabelsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a label or folder",
	RunE:  runMailLabelsCreate,
}

var mailLabelsDeleteCmd = &cobra.Command{
	Use:   "delete LABEL_ID...",
	Short: "Delete labels or folders",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runMailLabelsDelete,
}

func init() {
	mailLabelsCreateCmd.Flags().StringVar(&labelCreateName, "name", "", "Label name")
	mailLabelsCreateCmd.Flags().StringVar(&labelCreateColor, "color", "#7272a7", "Label color (hex)")
	mailLabelsCreateCmd.Flags().BoolVar(&labelCreateFolder, "folder", false, "Create a folder instead of a label")

	mailLabelsCmd.AddCommand(mailLabelsListCmd, mailLabelsCreateCmd, mailLabelsDeleteCmd)
}

func registerMailLabels() {
	mailCmd.AddCommand(mailLabelsCmd)
}

func runMailLabelsList(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	c, err := getAuthenticatedClient(ctx)
	if err != nil {
		return err
	}

	labelsBody, _, err := c.Do(ctx, "GET", "/core/v4/labels",
		map[string]string{"Type": fmt.Sprintf("%d", labelTypeMessageLabel)}, "", "", "")
	if err != nil {
		return err
	}

	foldersBody, _, err := c.Do(ctx, "GET", "/core/v4/labels",
		map[string]string{"Type": fmt.Sprintf("%d", labelTypeMessageFolder)}, "", "", "")
	if err != nil {
		return err
	}

	type labelEntry struct {
		ID    string
		Name  string
		Color string
		Path  string
	}

	var labelsRes struct{ Labels []labelEntry }
	var foldersRes struct{ Labels []labelEntry }
	_ = json.Unmarshal(labelsBody, &labelsRes)
	_ = json.Unmarshal(foldersBody, &foldersRes)

	if flagJSON {
		combined := map[string]interface{}{"Labels": labelsRes.Labels, "Folders": foldersRes.Labels}
		out, _ := json.MarshalIndent(combined, "", "  ")
		_, _ = os.Stdout.Write(out)
		fmt.Println()
		return nil
	}

	headers := []string{"ID", "TYPE", "NAME", "COLOR", "PATH"}
	var rows [][]string
	for _, l := range foldersRes.Labels {
		rows = append(rows, []string{l.ID, "FOLDER", l.Name, l.Color, l.Path})
	}
	for _, l := range labelsRes.Labels {
		rows = append(rows, []string{l.ID, "LABEL", l.Name, l.Color, ""})
	}

	printTable(headers, rows)
	return nil
}

func runMailLabelsCreate(cmd *cobra.Command, args []string) error {
	if labelCreateName == "" {
		return fmt.Errorf("--name is required")
	}

	labelType := labelTypeMessageLabel
	if labelCreateFolder {
		labelType = labelTypeMessageFolder
	}

	ctx := context.Background()
	c, err := getAuthenticatedClient(ctx)
	if err != nil {
		return err
	}

	reqBody := map[string]interface{}{"Name": labelCreateName, "Color": labelCreateColor, "Type": labelType}
	body, _ := json.Marshal(reqBody)
	resp, statusCode, err := c.Do(ctx, "POST", "/core/v4/labels", nil, string(body), "", "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return fmt.Errorf("create failed: %s", string(resp))
	}

	kind := "Label"
	if labelCreateFolder {
		kind = "Folder"
	}
	fmt.Fprintf(os.Stderr, "%s created.\n", kind)
	printJSON(resp)
	return nil
}

func runMailLabelsDelete(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	c, err := getAuthenticatedClient(ctx)
	if err != nil {
		return err
	}

	reqBody := map[string]interface{}{"LabelIDs": args}
	body, _ := json.Marshal(reqBody)
	resp, statusCode, err := c.Do(ctx, "DELETE", "/core/v4/labels", nil, string(body), "", "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return fmt.Errorf("delete failed: %s", string(resp))
	}

	fmt.Fprintf(os.Stderr, "Deleted %d label(s)/folder(s).\n", len(args))
	return nil
}
