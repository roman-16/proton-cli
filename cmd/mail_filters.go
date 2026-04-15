package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var mailFiltersCmd = &cobra.Command{
	Use:   "filters",
	Short: "Manage mail filters",
}

var mailFiltersListCmd = &cobra.Command{
	Use:   "list",
	Short: "List mail filters",
	RunE:  runMailFiltersList,
}

var (
	filterName   string
	filterSieve  string
	filterStatus int
)

var mailFiltersCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a sieve filter",
	RunE:  runMailFiltersCreate,
}

var mailFiltersDeleteCmd = &cobra.Command{
	Use:   "delete FILTER_ID",
	Short: "Delete a filter",
	Args:  cobra.ExactArgs(1),
	RunE:  runMailFiltersDelete,
}

var mailFiltersEnableCmd = &cobra.Command{
	Use:   "enable FILTER_ID",
	Short: "Enable a filter",
	Args:  cobra.ExactArgs(1),
	RunE:  runMailFiltersEnable,
}

var mailFiltersDisableCmd = &cobra.Command{
	Use:   "disable FILTER_ID",
	Short: "Disable a filter",
	Args:  cobra.ExactArgs(1),
	RunE:  runMailFiltersDisable,
}

func init() {
	mailFiltersCreateCmd.Flags().StringVar(&filterName, "name", "", "Filter name")
	mailFiltersCreateCmd.Flags().StringVar(&filterSieve, "sieve", "", "Sieve script")
	mailFiltersCreateCmd.Flags().IntVar(&filterStatus, "status", 1, "Status (1=enabled, 0=disabled)")

	mailFiltersCmd.AddCommand(mailFiltersListCmd, mailFiltersCreateCmd, mailFiltersDeleteCmd, mailFiltersEnableCmd, mailFiltersDisableCmd)
}

// registerMailFilters is called from mail.go init to add filters as a subcommand.
func registerMailFilters() {
	mailCmd.AddCommand(mailFiltersCmd)
}

func runMailFiltersList(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	c, err := getAuthenticatedClient(ctx)
	if err != nil {
		return err
	}

	body, _, err := c.Do(ctx, "GET", "/mail/v4/filters", nil, "", "", "")
	if err != nil {
		return err
	}

	if flagJSON {
		printJSON(body)
		return nil
	}

	var res struct {
		Filters []struct {
			ID      string
			Name    string
			Status  int
			Version int
			Sieve   string
		}
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return err
	}

	headers := []string{"ID", "STATUS", "NAME", "VERSION"}
	var rows [][]string
	for _, f := range res.Filters {
		status := "disabled"
		if f.Status == 1 {
			status = "enabled"
		}
		rows = append(rows, []string{f.ID, status, f.Name, fmt.Sprintf("%d", f.Version)})
	}

	printTable(headers, rows)
	return nil
}

func runMailFiltersCreate(cmd *cobra.Command, args []string) error {
	if filterName == "" {
		return fmt.Errorf("--name is required")
	}
	if filterSieve == "" {
		return fmt.Errorf("--sieve is required")
	}

	ctx := context.Background()
	c, err := getAuthenticatedClient(ctx)
	if err != nil {
		return err
	}

	reqBody := map[string]interface{}{
		"Name":    filterName,
		"Sieve":   filterSieve,
		"Version": 2,
		"Status":  filterStatus,
	}

	body, _ := json.Marshal(reqBody)
	resp, statusCode, err := c.Do(ctx, "POST", "/mail/v4/filters", nil, string(body), "", "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return fmt.Errorf("create filter failed: %s", string(resp))
	}

	fmt.Fprintf(os.Stderr, "Filter created.\n")
	printJSON(resp)
	return nil
}

func runMailFiltersDelete(cmd *cobra.Command, args []string) error {
	filterID := args[0]

	ctx := context.Background()
	c, err := getAuthenticatedClient(ctx)
	if err != nil {
		return err
	}

	resp, statusCode, err := c.Do(ctx, "DELETE", "/mail/v4/filters/"+filterID, nil, "", "", "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return fmt.Errorf("delete filter failed: %s", string(resp))
	}

	fmt.Fprintf(os.Stderr, "Filter deleted.\n")
	return nil
}

func runMailFiltersEnable(cmd *cobra.Command, args []string) error {
	filterID := args[0]

	ctx := context.Background()
	c, err := getAuthenticatedClient(ctx)
	if err != nil {
		return err
	}

	resp, statusCode, err := c.Do(ctx, "PUT", "/mail/v4/filters/"+filterID+"/enable", nil, "", "", "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return fmt.Errorf("enable filter failed: %s", string(resp))
	}

	fmt.Fprintf(os.Stderr, "Filter enabled.\n")
	return nil
}

func runMailFiltersDisable(cmd *cobra.Command, args []string) error {
	filterID := args[0]

	ctx := context.Background()
	c, err := getAuthenticatedClient(ctx)
	if err != nil {
		return err
	}

	resp, statusCode, err := c.Do(ctx, "PUT", "/mail/v4/filters/"+filterID+"/disable", nil, "", "", "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return fmt.Errorf("disable filter failed: %s", string(resp))
	}

	fmt.Fprintf(os.Stderr, "Filter disabled.\n")
	return nil
}
