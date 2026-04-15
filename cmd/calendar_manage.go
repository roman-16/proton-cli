package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/roman-16/proton-cli/internal/crypto"
	"github.com/spf13/cobra"
)

var calendarListCalendarsCmd = &cobra.Command{
	Use:   "list",
	Short: "List calendars",
	RunE:  runCalendarListCalendars,
}

var calendarCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a calendar",
	RunE:  runCalendarCreate,
}

var calendarDeleteCmd = &cobra.Command{
	Use:   "delete CALENDAR_ID",
	Short: "Delete a calendar (requires PROTON_PASSWORD)",
	Args:  cobra.ExactArgs(1),
	RunE:  runCalendarDelete,
}

func runCalendarListCalendars(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	c, err := getAuthenticatedClient(ctx)
	if err != nil {
		return err
	}

	resp, _, err := c.Do(ctx, "GET", "/calendar/v1", nil, "", "", "")
	if err != nil {
		return err
	}

	if flagJSON {
		printJSON(resp)
		return nil
	}

	var res struct {
		Calendars []struct {
			ID          string
			Name        string
			Description string
			Color       string
			Type        int
			Members     []struct {
				Name  string
				Email string
			}
		}
	}
	if err := json.Unmarshal(resp, &res); err != nil {
		return err
	}

	headers := []string{"ID", "NAME", "COLOR", "MEMBERS"}
	var rows [][]string
	for _, cal := range res.Calendars {
		name := cal.Name
		if name == "" && len(cal.Members) > 0 {
			name = cal.Members[0].Name
		}
		rows = append(rows, []string{cal.ID, name, cal.Color, fmt.Sprintf("%d", len(cal.Members))})
	}

	printTable(headers, rows)
	return nil
}

func runCalendarCreate(cmd *cobra.Command, args []string) error {
	if calCreateName == "" {
		return fmt.Errorf("--name is required")
	}

	ctx := context.Background()
	c, err := getAuthenticatedClient(ctx)
	if err != nil {
		return err
	}

	password := getFlag(flagPassword, "PROTON_PASSWORD")
	kr, err := crypto.UnlockKeys(ctx, c, password)
	if err != nil {
		return err
	}

	_, addrID, err := kr.PrimaryAddrKR()
	if err != nil {
		return err
	}

	reqBody := map[string]interface{}{
		"Name":      calCreateName,
		"Color":     calCreateColor,
		"Display":   1,
		"AddressID": addrID,
	}

	body, _ := json.Marshal(reqBody)
	resp, statusCode, err := c.Do(ctx, "POST", "/calendar/v1", nil, string(body), "", "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return fmt.Errorf("create calendar failed: %s", string(resp))
	}

	fmt.Fprintf(os.Stderr, "Calendar created.\n")
	printJSON(resp)
	return nil
}

func runCalendarDelete(cmd *cobra.Command, args []string) error {
	calendarID := args[0]

	ctx := context.Background()
	c, err := getAuthenticatedClient(ctx)
	if err != nil {
		return err
	}

	// Calendar delete requires the "locked" password scope.
	user := getFlag(flagUser, "PROTON_USER")
	password := getFlag(flagPassword, "PROTON_PASSWORD")
	if password == "" {
		return fmt.Errorf("password is required for calendar delete (set PROTON_PASSWORD or --password)")
	}

	fmt.Fprintf(os.Stderr, "Unlocking password scope...\n")
	if err := c.UnlockPasswordScope(ctx, user, []byte(password)); err != nil {
		return fmt.Errorf("failed to unlock password scope: %w", err)
	}

	resp, statusCode, err := c.Do(ctx, "DELETE", "/calendar/v1/"+calendarID, nil, "", "", "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return fmt.Errorf("delete calendar failed: %s", string(resp))
	}

	fmt.Fprintf(os.Stderr, "Calendar deleted.\n")
	return nil
}
