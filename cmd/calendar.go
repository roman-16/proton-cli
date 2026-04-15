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
	Short: "Calendar operations",
}

var (
	calEventCalendar string
	calEventTitle    string
	calEventLocation string
	calEventStart    string
	calEventDuration string
	calEventAllDay   bool

	calUpdateTitle    string
	calUpdateLocation string
	calUpdateStart    string
	calUpdateDuration string

	calListCalendar string
	calListStart    string
	calListEnd      string

	calCreateName  string
	calCreateColor string
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

	calendarCreateCmd.Flags().StringVar(&calCreateName, "name", "", "Calendar name")
	calendarCreateCmd.Flags().StringVar(&calCreateColor, "color", "#8080FF", "Calendar color (hex)")

	calendarCmd.AddCommand(calendarCreateEventCmd, calendarListEventsCmd, calendarGetEventCmd, calendarUpdateEventCmd, calendarDeleteEventCmd, calendarListCalendarsCmd, calendarCreateCmd, calendarDeleteCmd)
	rootCmd.AddCommand(calendarCmd)
}

// ── Shared helpers ──

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

	if len(nameOrID) > 20 {
		return nameOrID, nil
	}

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
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//proton-cli//EN",
		"BEGIN:VEVENT",
		fmt.Sprintf("UID:%s", uid), fmt.Sprintf("DTSTAMP:%s", dtstamp),
		dtstart, dtend, "SEQUENCE:0",
		"END:VEVENT", "END:VCALENDAR",
	}
	return strings.Join(lines, "\r\n")
}

func buildEncryptedVEVENT(title, location string) string {
	lines := []string{
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//proton-cli//EN",
		"BEGIN:VEVENT",
		fmt.Sprintf("SUMMARY:%s", title),
	}
	if location != "" {
		lines = append(lines, fmt.Sprintf("LOCATION:%s", location))
	}
	lines = append(lines, "END:VEVENT", "END:VCALENDAR")
	return strings.Join(lines, "\r\n")
}

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
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//proton-cli//EN",
		"BEGIN:VEVENT",
		fmt.Sprintf("UID:%s", uid), fmt.Sprintf("DTSTAMP:%s", dtstamp),
		dtstart, dtend, fmt.Sprintf("SEQUENCE:%d", sequence),
		"END:VEVENT", "END:VCALENDAR",
	}
	return strings.Join(lines, "\r\n")
}

func searchEventByTitle(ctx context.Context, cl *client.Client, kr *crypto.KeyRings, search string) (string, string, error) {
	resp, _, err := cl.Do(ctx, "GET", "/calendar/v1", nil, "", "", "")
	if err != nil {
		return "", "", err
	}
	var calRes struct {
		Calendars []struct{ ID string }
	}
	if err := json.Unmarshal(resp, &calRes); err != nil {
		return "", "", err
	}

	start, end := defaultTimeRange()
	query := map[string]string{
		"Start": fmt.Sprintf("%d", start), "End": fmt.Sprintf("%d", end),
		"Timezone": "UTC", "Type": "0",
	}

	type match struct {
		calID, eventID, title string
		startTime             time.Time
	}
	var matches []match

	for _, cal := range calRes.Calendars {
		calKeys, err := crypto.UnlockCalendar(ctx, cl, cal.ID, kr)
		if err != nil {
			continue
		}
		evResp, _, err := cl.Do(ctx, "GET", "/calendar/v1/"+cal.ID+"/events", query, "", "", "")
		if err != nil {
			continue
		}
		var eventsRes struct {
			Events []map[string]interface{}
		}
		if err := json.Unmarshal(evResp, &eventsRes); err != nil {
			continue
		}

		for _, ev := range eventsRes.Events {
			sharedKP, _ := ev["SharedKeyPacket"].(string)
			shared, ok := ev["SharedEvents"].([]interface{})
			if !ok {
				continue
			}
			var cards []map[string]interface{}
			for _, s := range shared {
				if m, ok := s.(map[string]interface{}); ok {
					cards = append(cards, m)
				}
			}
			decrypted, err := crypto.DecryptEventCards(cards, calKeys, sharedKP)
			if err != nil {
				continue
			}
			for _, d := range decrypted {
				title := parseICalField(d, "SUMMARY")
				if title != "" && strings.Contains(strings.ToLower(title), strings.ToLower(search)) {
					startTS, _ := ev["StartTime"].(float64)
					matches = append(matches, match{
						calID: cal.ID, eventID: ev["ID"].(string),
						title: title, startTime: time.Unix(int64(startTS), 0),
					})
				}
			}
		}
	}

	if len(matches) == 0 {
		return "", "", fmt.Errorf("no event matching %q found in the next 30 days", search)
	}
	if len(matches) > 1 {
		fmt.Fprintf(os.Stderr, "Multiple matches for %q:\n", search)
		for _, m := range matches {
			fmt.Fprintf(os.Stderr, "  %s  %s  (calendar %s, event %s)\n",
				m.startTime.Local().Format("2006-01-02 15:04"), m.title, m.calID, m.eventID)
		}
		return "", "", fmt.Errorf("ambiguous: %d events match %q, use CALENDAR_ID EVENT_ID", len(matches), search)
	}
	return matches[0].calID, matches[0].eventID, nil
}
