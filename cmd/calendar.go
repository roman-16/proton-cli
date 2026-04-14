package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/client"
	"github.com/roman-16/proton-cli/internal/crypto"
	"github.com/spf13/cobra"
)

var calendarCmd = &cobra.Command{
	Use:   "calendar",
	Short: "Calendar operations (encrypted)",
}

var calendarListEventsCmd = &cobra.Command{
	Use:   "list-events",
	Short: "List calendar events (decrypted)",
	RunE:  runCalendarListEvents,
}

var calendarGetEventCmd = &cobra.Command{
	Use:   "get-event CALENDAR_ID EVENT_ID",
	Short: "Get a calendar event (decrypted)",
	Args:  cobra.ExactArgs(2),
	RunE:  runCalendarGetEvent,
}

var (
	calEventCalendar string
	calEventTitle    string
	calEventLocation string
	calEventStart    string
	calEventDuration string
	calEventAllDay   bool
)

var calendarCreateEventCmd = &cobra.Command{
	Use:   "create-event",
	Short: "Create a calendar event",
	RunE:  runCalendarCreateEvent,
}

var calendarUpdateEventCmd = &cobra.Command{
	Use:   "update-event CALENDAR_ID EVENT_ID",
	Short: "Update a calendar event",
	Args:  cobra.ExactArgs(2),
	RunE:  runCalendarUpdateEvent,
}

var (
	calUpdateTitle    string
	calUpdateLocation string
	calUpdateStart    string
	calUpdateDuration string
)

var (
	calListCalendar string
	calListStart    string
	calListEnd      string
)

func init() {
	calendarCreateEventCmd.Flags().StringVar(&calEventCalendar, "calendar", "", "Calendar ID (or name)")
	calendarCreateEventCmd.Flags().StringVar(&calEventTitle, "title", "", "Event title")
	calendarCreateEventCmd.Flags().StringVar(&calEventLocation, "location", "", "Event location")
	calendarCreateEventCmd.Flags().StringVar(&calEventStart, "start", "", "Start time (RFC3339 or YYYY-MM-DDTHH:MM)")
	calendarCreateEventCmd.Flags().StringVar(&calEventDuration, "duration", "1h", "Duration (e.g. 30m, 1h, 2h30m)")
	calendarCreateEventCmd.Flags().BoolVar(&calEventAllDay, "all-day", false, "All-day event")

	calendarListEventsCmd.Flags().StringVar(&calListCalendar, "calendar", "", "Calendar ID (or name)")
	calendarListEventsCmd.Flags().StringVar(&calListStart, "start", "", "Start date (YYYY-MM-DD)")
	calendarListEventsCmd.Flags().StringVar(&calListEnd, "end", "", "End date (YYYY-MM-DD)")

	calendarUpdateEventCmd.Flags().StringVar(&calUpdateTitle, "title", "", "New title")
	calendarUpdateEventCmd.Flags().StringVar(&calUpdateLocation, "location", "", "New location")
	calendarUpdateEventCmd.Flags().StringVar(&calUpdateStart, "start", "", "New start time")
	calendarUpdateEventCmd.Flags().StringVar(&calUpdateDuration, "duration", "", "New duration")

	calendarCmd.AddCommand(calendarCreateEventCmd, calendarListEventsCmd, calendarGetEventCmd, calendarUpdateEventCmd)
	rootCmd.AddCommand(calendarCmd)
}

func runCalendarCreateEvent(cmd *cobra.Command, args []string) error {
	if calEventTitle == "" {
		return fmt.Errorf("--title is required")
	}
	if calEventStart == "" {
		return fmt.Errorf("--start is required")
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

	calendarID, err := resolveCalendarID(ctx, c, calEventCalendar)
	if err != nil {
		return err
	}

	calKeys, err := crypto.UnlockCalendar(ctx, c, calendarID, kr)
	if err != nil {
		return err
	}

	// Parse times
	startTime, err := parseTime(calEventStart)
	if err != nil {
		return fmt.Errorf("invalid --start: %w", err)
	}
	duration, err := time.ParseDuration(calEventDuration)
	if err != nil {
		return fmt.Errorf("invalid --duration: %w", err)
	}
	endTime := startTime.Add(duration)

	// Build VEVENTs — signed part (times) and encrypted part (title, location)
	signedVevent := buildSignedVEVENT(startTime, endTime, calEventAllDay)
	encryptedVevent := buildEncryptedVEVENT(calEventTitle, calEventLocation)

	// Encrypt
	signedCard, encryptedCard, sharedKeyPacket, err := crypto.EncryptEventCards(signedVevent, encryptedVevent, calKeys, "")
	if err != nil {
		return err
	}

	// Get member ID
	members, err := getCalendarMembersForSync(ctx, c, calendarID, kr)
	if err != nil {
		return err
	}

	// Build sync request
	syncData := map[string]interface{}{
		"MemberID": members.memberID,
		"Events": []map[string]interface{}{
			{
				"Overwrite": 0,
				"Event": map[string]interface{}{
					"Permissions":        63,
					"IsOrganizer":        1,
					"SharedKeyPacket":    sharedKeyPacket,
					"SharedEventContent": []interface{}{signedCard, encryptedCard},
					"Notifications":      nil,
					"Color":              nil,
				},
			},
		},
	}

	body, _ := json.Marshal(syncData)

	resp, statusCode, err := c.Do(ctx, "PUT", "/calendar/v1/"+calendarID+"/events/sync", nil, string(body), "", "")
	if err != nil {
		return err
	}

	if statusCode >= 400 {
		fmt.Fprintf(os.Stderr, "Error: %s\n", string(resp))
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Event created.\n")
	printJSON(resp)
	return nil
}

func runCalendarListEvents(cmd *cobra.Command, args []string) error {
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

	calendarID, err := resolveCalendarID(ctx, c, calListCalendar)
	if err != nil {
		return err
	}

	calKeys, err := crypto.UnlockCalendar(ctx, c, calendarID, kr)
	if err != nil {
		return err
	}

	// Default time range: today to 30 days from now
	start, end := defaultTimeRange()
	if calListStart != "" {
		t, err := time.Parse("2006-01-02", calListStart)
		if err != nil {
			return fmt.Errorf("invalid --start: %w", err)
		}
		start = t.Unix()
	}
	if calListEnd != "" {
		t, err := time.Parse("2006-01-02", calListEnd)
		if err != nil {
			return fmt.Errorf("invalid --end: %w", err)
		}
		end = t.Unix()
	}

	query := map[string]string{
		"Start":    fmt.Sprintf("%d", start),
		"End":      fmt.Sprintf("%d", end),
		"Timezone": "UTC",
		"Type":     "0",
	}

	resp, _, err := c.Do(ctx, "GET", "/calendar/v1/"+calendarID+"/events", query, "", "", "")
	if err != nil {
		return err
	}

	var eventsResp struct {
		Events []map[string]interface{}
	}
	if err := json.Unmarshal(resp, &eventsResp); err != nil {
		return err
	}

	// Decrypt each event's SharedEvents
	for i, event := range eventsResp.Events {
		sharedKeyPacket, _ := event["SharedKeyPacket"].(string)
		if shared, ok := event["SharedEvents"].([]interface{}); ok {
			var cards []map[string]interface{}
			for _, s := range shared {
				if m, ok := s.(map[string]interface{}); ok {
					cards = append(cards, m)
				}
			}
			decrypted, err := crypto.DecryptEventCards(cards, calKeys, sharedKeyPacket)
			if err == nil {
				eventsResp.Events[i]["DecryptedSharedEvents"] = decrypted
			}
		}
	}

	out, _ := json.MarshalIndent(eventsResp, "", "  ")
	os.Stdout.Write(out)
	fmt.Println()
	return nil
}

func runCalendarGetEvent(cmd *cobra.Command, args []string) error {
	calendarID := args[0]
	eventID := args[1]

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

	calKeys, err := crypto.UnlockCalendar(ctx, c, calendarID, kr)
	if err != nil {
		return err
	}

	resp, _, err := c.Do(ctx, "GET", "/calendar/v1/"+calendarID+"/events/"+eventID, nil, "", "", "")
	if err != nil {
		return err
	}

	var eventResp struct {
		Event map[string]interface{}
	}
	if err := json.Unmarshal(resp, &eventResp); err != nil {
		return err
	}

	event := eventResp.Event
	sharedKeyPacket, _ := event["SharedKeyPacket"].(string)
	if shared, ok := event["SharedEvents"].([]interface{}); ok {
		var cards []map[string]interface{}
		for _, s := range shared {
			if m, ok := s.(map[string]interface{}); ok {
				cards = append(cards, m)
			}
		}
		decrypted, err := crypto.DecryptEventCards(cards, calKeys, sharedKeyPacket)
		if err == nil {
			event["DecryptedSharedEvents"] = decrypted
		}
	}

	out, _ := json.MarshalIndent(eventResp, "", "  ")
	os.Stdout.Write(out)
	fmt.Println()
	return nil
}

// ── Helpers ──

type memberInfo struct {
	memberID string
}

func getCalendarMembersForSync(ctx context.Context, cl *client.Client, calendarID string, kr *crypto.KeyRings) (*memberInfo, error) {
	resp, _, err := cl.Do(ctx, "GET", "/calendar/v1/"+calendarID+"/members", nil, "", "", "")
	if err != nil {
		return nil, err
	}
	var res struct {
		Members []struct {
			ID        string
			AddressID string
		}
	}
	if err := json.Unmarshal(resp, &res); err != nil {
		return nil, err
	}
	for _, m := range res.Members {
		if _, ok := kr.AddrKRs[m.AddressID]; ok {
			return &memberInfo{memberID: m.ID}, nil
		}
	}
	return nil, fmt.Errorf("no matching member found")
}

func resolveCalendarID(ctx context.Context, cl *client.Client, nameOrID string) (string, error) {
	if nameOrID == "" {
		// Use first calendar
		resp, _, err := cl.Do(ctx, "GET", "/calendar/v1", nil, "", "", "")
		if err != nil {
			return "", err
		}
		var res struct {
			Calendars []struct {
				ID      string
				Members []struct{ Name string }
			}
		}
		if err := json.Unmarshal(resp, &res); err != nil {
			return "", err
		}
		if len(res.Calendars) == 0 {
			return "", fmt.Errorf("no calendars found")
		}
		return res.Calendars[0].ID, nil
	}

	// If it looks like an ID (long base64), use directly
	if len(nameOrID) > 20 {
		return nameOrID, nil
	}

	// Otherwise search by name
	resp, _, err := cl.Do(ctx, "GET", "/calendar/v1", nil, "", "", "")
	if err != nil {
		return "", err
	}
	var res struct {
		Calendars []struct {
			ID      string
			Members []struct{ Name string }
		}
	}
	if err := json.Unmarshal(resp, &res); err != nil {
		return "", err
	}
	for _, cal := range res.Calendars {
		for _, m := range cal.Members {
			if strings.EqualFold(m.Name, nameOrID) {
				return cal.ID, nil
			}
		}
	}
	return "", fmt.Errorf("calendar %q not found", nameOrID)
}

func parseTime(s string) (time.Time, error) {
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.ParseInLocation(f, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized time format: %s", s)
}

func defaultTimeRange() (int64, int64) {
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	end := start.AddDate(0, 0, 30)
	return start.Unix(), end.Unix()
}

// buildSignedVEVENT creates the cleartext-signed part (Type 2): times, UID, sequence.
func buildSignedVEVENT(start, end time.Time, allDay bool) string {
	uid := fmt.Sprintf("%d@proton-cli", time.Now().UnixNano())
	dtstamp := time.Now().UTC().Format("20060102T150405Z")

	var dtstart, dtend string
	if allDay {
		dtstart = fmt.Sprintf("DTSTART;VALUE=DATE:%s", start.Format("20060102"))
		dtend = fmt.Sprintf("DTEND;VALUE=DATE:%s", end.Format("20060102"))
	} else {
		dtstart = fmt.Sprintf("DTSTART:%s", start.UTC().Format("20060102T150405Z"))
		dtend = fmt.Sprintf("DTEND:%s", end.UTC().Format("20060102T150405Z"))
	}

	lines := []string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//proton-cli//EN",
		"BEGIN:VEVENT",
		fmt.Sprintf("UID:%s", uid),
		fmt.Sprintf("DTSTAMP:%s", dtstamp),
		dtstart,
		dtend,
		"SEQUENCE:0",
		"END:VEVENT",
		"END:VCALENDAR",
	}
	return strings.Join(lines, "\r\n")
}

// buildEncryptedVEVENT creates the encrypted part (Type 3): title, location, description.
func buildEncryptedVEVENT(title, location string) string {
	lines := []string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//proton-cli//EN",
		"BEGIN:VEVENT",
		fmt.Sprintf("SUMMARY:%s", title),
	}

	if location != "" {
		lines = append(lines, fmt.Sprintf("LOCATION:%s", location))
	}

	lines = append(lines, "END:VEVENT", "END:VCALENDAR")
	return strings.Join(lines, "\r\n")
}

// buildSignedVEVENTWithUID creates a signed part reusing an existing UID.
func buildSignedVEVENTWithUID(uid string, start, end time.Time, allDay bool, sequence int) string {
	dtstamp := time.Now().UTC().Format("20060102T150405Z")

	var dtstart, dtend string
	if allDay {
		dtstart = fmt.Sprintf("DTSTART;VALUE=DATE:%s", start.Format("20060102"))
		dtend = fmt.Sprintf("DTEND;VALUE=DATE:%s", end.Format("20060102"))
	} else {
		dtstart = fmt.Sprintf("DTSTART:%s", start.UTC().Format("20060102T150405Z"))
		dtend = fmt.Sprintf("DTEND:%s", end.UTC().Format("20060102T150405Z"))
	}

	lines := []string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//proton-cli//EN",
		"BEGIN:VEVENT",
		fmt.Sprintf("UID:%s", uid),
		fmt.Sprintf("DTSTAMP:%s", dtstamp),
		dtstart,
		dtend,
		fmt.Sprintf("SEQUENCE:%d", sequence),
		"END:VEVENT",
		"END:VCALENDAR",
	}
	return strings.Join(lines, "\r\n")
}

func runCalendarUpdateEvent(cmd *cobra.Command, args []string) error {
	calendarID := args[0]
	eventID := args[1]

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

	calKeys, err := crypto.UnlockCalendar(ctx, c, calendarID, kr)
	if err != nil {
		return err
	}

	// Fetch existing event to get UID, times, etc.
	resp, _, err := c.Do(ctx, "GET", "/calendar/v1/"+calendarID+"/events/"+eventID, nil, "", "", "")
	if err != nil {
		return err
	}

	var eventResp struct {
		Event struct {
			UID           string
			StartTime     int64
			EndTime       int64
			FullDay       int
			SharedEvents  []map[string]interface{}
			SharedKeyPacket string
		}
	}
	if err := json.Unmarshal(resp, &eventResp); err != nil {
		return err
	}

	ev := eventResp.Event

	// Decrypt existing encrypted part to get current title/location
	var currentTitle, currentLocation string
	var cards []map[string]interface{}
	for _, s := range ev.SharedEvents {
		cards = append(cards, s)
	}
	decrypted, _ := crypto.DecryptEventCards(cards, calKeys, ev.SharedKeyPacket)
	for _, d := range decrypted {
		for _, line := range strings.Split(d, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "SUMMARY:") {
				currentTitle = strings.TrimPrefix(line, "SUMMARY:")
			}
			if strings.HasPrefix(line, "LOCATION:") {
				currentLocation = strings.TrimPrefix(line, "LOCATION:")
			}
		}
	}

	// Apply updates
	title := currentTitle
	if calUpdateTitle != "" {
		title = calUpdateTitle
	}
	location := currentLocation
	if calUpdateLocation != "" {
		location = calUpdateLocation
	}

	startTime := time.Unix(ev.StartTime, 0)
	endTime := time.Unix(ev.EndTime, 0)
	if calUpdateStart != "" {
		startTime, err = parseTime(calUpdateStart)
		if err != nil {
			return fmt.Errorf("invalid --start: %w", err)
		}
		if calUpdateDuration != "" {
			d, err := time.ParseDuration(calUpdateDuration)
			if err != nil {
				return fmt.Errorf("invalid --duration: %w", err)
			}
			endTime = startTime.Add(d)
		} else {
			// Keep same duration
			origDuration := time.Unix(ev.EndTime, 0).Sub(time.Unix(ev.StartTime, 0))
			endTime = startTime.Add(origDuration)
		}
	} else if calUpdateDuration != "" {
		d, err := time.ParseDuration(calUpdateDuration)
		if err != nil {
			return fmt.Errorf("invalid --duration: %w", err)
		}
		endTime = startTime.Add(d)
	}

	allDay := ev.FullDay == 1
	signedVevent := buildSignedVEVENTWithUID(ev.UID, startTime, endTime, allDay, 1)
	encryptedVevent := buildEncryptedVEVENT(title, location)

	signedCard, encryptedCard, _, err := crypto.EncryptEventCards(signedVevent, encryptedVevent, calKeys, ev.SharedKeyPacket)
	if err != nil {
		return err
	}

	members, err := getCalendarMembersForSync(ctx, c, calendarID, kr)
	if err != nil {
		return err
	}

	syncData := map[string]interface{}{
		"MemberID": members.memberID,
		"Events": []map[string]interface{}{
			{
				"ID": eventID,
				"Event": map[string]interface{}{
					"Permissions":        63,
					"IsOrganizer":        1,
					"SharedEventContent": []interface{}{signedCard, encryptedCard},
					"Notifications":      nil,
					"Color":              nil,
				},
			},
		},
	}

	body, _ := json.Marshal(syncData)
	result, statusCode, err := c.Do(ctx, "PUT", "/calendar/v1/"+calendarID+"/events/sync", nil, string(body), "", "")
	if err != nil {
		return err
	}

	if statusCode >= 400 {
		fmt.Fprintf(os.Stderr, "Error: %s\n", string(result))
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Event updated.\n")
	printJSON(result)
	return nil
}
