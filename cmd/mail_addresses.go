package cmd

import (
	"context"
	"encoding/json"

	"github.com/spf13/cobra"
)

var mailAddressesCmd = &cobra.Command{
	Use:   "addresses",
	Short: "Manage email addresses",
}

var mailAddressesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List email addresses on the account",
	RunE:  runMailAddressesList,
}

func init() {
	mailAddressesCmd.AddCommand(mailAddressesListCmd)
}

func registerMailAddresses() {
	mailCmd.AddCommand(mailAddressesCmd)
}

func runMailAddressesList(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	c, err := getAuthenticatedClient(ctx)
	if err != nil {
		return err
	}

	body, _, err := c.Do(ctx, "GET", "/core/v4/addresses", nil, "", "", "")
	if err != nil {
		return err
	}

	if flagJSON {
		printJSON(body)
		return nil
	}

	var res struct {
		Addresses []struct {
			ID          string
			Email       string
			DisplayName string
			Type        int
			Status      int
		}
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return err
	}

	headers := []string{"ID", "EMAIL", "DISPLAY_NAME", "STATUS", "TYPE"}
	var rows [][]string
	for _, addr := range res.Addresses {
		status := "disabled"
		if addr.Status == 1 {
			status = "active"
		}
		addrType := "custom"
		switch addr.Type {
		case 1:
			addrType = "original"
		case 2:
			addrType = "alias"
		case 3:
			addrType = "custom"
		case 4:
			addrType = "premium"
		case 5:
			addrType = "external"
		}
		rows = append(rows, []string{addr.ID, addr.Email, addr.DisplayName, status, addrType})
	}

	printTable(headers, rows)
	return nil
}
