package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/crypto"
	"github.com/spf13/cobra"
)

var calendarListEventsCmd = &cobra.Command{
	Use:   "list-events",
	Short: "List calendar events (decrypted)",
	RunE:  runCalendarListEvents,
}

var calendarGetEventCmd = &cobra.Command{
	Use:   "get-event {CALENDAR_ID EVENT_ID | TITLE_SEARCH}",
	Short: "Get a calendar event (decrypted). Pass two IDs or a title to search.",
	Args:  cobra.RangeArgs(1, 2),
	RunE:  runCalendarGetEvent,
}

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

var calendarDeleteEventCmd = &cobra.Command{
	Use:   "delete-event {CALENDAR_ID EVENT_ID | TITLE_SEARCH}",
	Short: "Delete a calendar event",
	Args:  cobra.RangeArgs(1, 2),
	RunE:  runCalendarDeleteEvent,
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

	startTime, err := parseTime(calEventStart)
	if err != nil {
		return fmt.Errorf("invalid --start: %w", err)
	}
	duration, err := time.ParseDuration(calEventDuration)
	if err != nil {
		return fmt.Errorf("invalid --duration: %w", err)
	}
	endTime := startTime.Add(duration)

	signedVevent := buildSignedVEVENT(startTime, endTime, calEventAllDay)
	encryptedVevent := buildEncryptedVEVENT(calEventTitle, calEventLocation)

	signedCard, encryptedCard, sharedKeyPacket, err := crypto.EncryptEventCards(signedVevent, encryptedVevent, calKeys, "")
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
		"Start": fmt.Sprintf("%d", start), "End": fmt.Sprintf("%d", end),
		"Timezone": "UTC", "Type": "0",
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

	if flagJSON {
		out, _ := json.MarshalIndent(eventsResp, "", "  ")
		_, _ = os.Stdout.Write(out)
		fmt.Println()
		return nil
	}

	headers := []string{"DATE", "TIME", "DURATION", "TITLE", "LOCATION", "CALENDAR_ID", "EVENT_ID"}
	var rows [][]string
	for _, event := range eventsResp.Events {
		startTS, _ := event["StartTime"].(float64)
		endTS, _ := event["EndTime"].(float64)
		eventID, _ := event["ID"].(string)
		calID, _ := event["CalendarID"].(string)

		st := time.Unix(int64(startTS), 0).Local()
		et := time.Unix(int64(endTS), 0).Local()

		var title, location string
		if decrypted, ok := event["DecryptedSharedEvents"].([]string); ok {
			for _, d := range decrypted {
				if t := parseICalField(d, "SUMMARY"); t != "" {
					title = t
				}
				if l := parseICalField(d, "LOCATION"); l != "" {
					location = l
				}
			}
		}

		rows = append(rows, []string{
			st.Format("2006-01-02"), st.Format("15:04"),
			formatDuration(et.Sub(st)), title, location, calID, eventID,
		})
	}

	printTable(headers, rows)
	return nil
}

func runCalendarGetEvent(cmd *cobra.Command, args []string) error {
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

	var calendarID, eventID string
	if len(args) == 2 {
		calendarID = args[0]
		eventID = args[1]
	} else {
		calID, evID, err := searchEventByTitle(ctx, c, kr, args[0])
		if err != nil {
			return err
		}
		calendarID = calID
		eventID = evID
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

	if flagJSON {
		out, _ := json.MarshalIndent(eventResp, "", "  ")
		_, _ = os.Stdout.Write(out)
		fmt.Println()
		return nil
	}

	var title, location string
	if decrypted, ok := event["DecryptedSharedEvents"].([]string); ok {
		for _, d := range decrypted {
			if t := parseICalField(d, "SUMMARY"); t != "" {
				title = t
			}
			if l := parseICalField(d, "LOCATION"); l != "" {
				location = l
			}
		}
	}

	startTS, _ := event["StartTime"].(float64)
	endTS, _ := event["EndTime"].(float64)
	st := time.Unix(int64(startTS), 0).Local()
	et := time.Unix(int64(endTS), 0).Local()

	fmt.Printf("Event:    %s\n", title)
	fmt.Printf("Start:    %s\n", st.Format("2006-01-02 15:04"))
	fmt.Printf("End:      %s\n", et.Format("2006-01-02 15:04"))
	fmt.Printf("Duration: %s\n", formatDuration(et.Sub(st)))
	if location != "" {
		fmt.Printf("Location: %s\n", location)
	}
	fmt.Printf("ID:       %s\n", eventID)
	fmt.Printf("Calendar: %s\n", calendarID)
	return nil
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

	resp, _, err := c.Do(ctx, "GET", "/calendar/v1/"+calendarID+"/events/"+eventID, nil, "", "", "")
	if err != nil {
		return err
	}

	var eventResp struct {
		Event struct {
			UID             string
			StartTime       int64
			EndTime         int64
			FullDay         int
			SharedEvents    []map[string]interface{}
			SharedKeyPacket string
		}
	}
	if err := json.Unmarshal(resp, &eventResp); err != nil {
		return err
	}

	ev := eventResp.Event

	var currentTitle, currentLocation string
	cards := append([]map[string]interface{}{}, ev.SharedEvents...)
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

func runCalendarDeleteEvent(cmd *cobra.Command, args []string) error {
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

	var calendarID, eventID string
	if len(args) == 2 {
		calendarID = args[0]
		eventID = args[1]
	} else {
		calID, evID, err := searchEventByTitle(ctx, c, kr, args[0])
		if err != nil {
			return err
		}
		calendarID = calID
		eventID = evID
	}

	// Proton uses the sync endpoint for deletion: an event entry with only ID (no Event field)
	members, err := getCalendarMembersForSync(ctx, c, calendarID, kr)
	if err != nil {
		return err
	}

	syncData := map[string]interface{}{
		"MemberID": members.memberID,
		"Events": []map[string]interface{}{
			{"ID": eventID},
		},
	}

	body, _ := json.Marshal(syncData)
	resp, statusCode, err := c.Do(ctx, "PUT", "/calendar/v1/"+calendarID+"/events/sync", nil, string(body), "", "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return fmt.Errorf("delete event failed: %s", string(resp))
	}

	fmt.Fprintf(os.Stderr, "Event deleted.\n")
	return nil
}
