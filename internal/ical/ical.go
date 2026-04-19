// Package ical contains small helpers for building and parsing the iCal/VCard
// text used by Proton Calendar and Contacts. It is deliberately minimal — just
// enough for the fields the CLI reads and writes.
package ical

import (
	"fmt"
	"strings"
	"time"
)

// Field extracts a field value from iCal/vCard text. Handles both
// `FIELD:value` and `FIELD;PARAM=x:value` forms, plus `itemN.FIELD:…`.
func Field(text, name string) string {
	prefix := name + ":"
	prefixParam := name + ";"
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
		if strings.HasPrefix(line, prefixParam) {
			if i := strings.Index(line, ":"); i >= 0 {
				return line[i+1:]
			}
		}
		if strings.Contains(line, "."+name+";") || strings.Contains(line, "."+name+":") {
			if i := strings.Index(line, ":"); i >= 0 {
				return line[i+1:]
			}
		}
	}
	return ""
}

// EventUID returns a new locally-unique UID for a VEVENT.
func EventUID() string {
	return fmt.Sprintf("%d@proton-cli", time.Now().UnixNano())
}

// ContactUID returns a new locally-unique UID for a vCard.
func ContactUID() string {
	return fmt.Sprintf("proton-cli-%d", time.Now().UnixNano())
}

// SignedVEVENT builds the signed portion of a Proton calendar event
// (Card Type 2: UID + DTSTAMP + DTSTART + DTEND + SEQUENCE).
func SignedVEVENT(uid string, start, end time.Time, allDay bool, sequence int) string {
	dtstamp := time.Now().UTC().Format("20060102T150405Z")
	var dtstart, dtend string
	if allDay {
		dtstart = "DTSTART;VALUE=DATE:" + start.Format("20060102")
		dtend = "DTEND;VALUE=DATE:" + end.Format("20060102")
	} else {
		dtstart = "DTSTART:" + start.UTC().Format("20060102T150405Z")
		dtend = "DTEND:" + end.UTC().Format("20060102T150405Z")
	}
	return strings.Join([]string{
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//proton-cli//EN",
		"BEGIN:VEVENT",
		"UID:" + uid,
		"DTSTAMP:" + dtstamp,
		dtstart, dtend,
		fmt.Sprintf("SEQUENCE:%d", sequence),
		"END:VEVENT", "END:VCALENDAR",
	}, "\r\n")
}

// EncryptedVEVENT builds the encrypted portion of a Proton calendar event
// (Card Type 3: SUMMARY + optional LOCATION).
func EncryptedVEVENT(title, location string) string {
	lines := []string{
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//proton-cli//EN",
		"BEGIN:VEVENT",
		"SUMMARY:" + title,
	}
	if location != "" {
		lines = append(lines, "LOCATION:"+location)
	}
	lines = append(lines, "END:VEVENT", "END:VCALENDAR")
	return strings.Join(lines, "\r\n")
}

// SignedVCard builds the signed portion of a Proton contact
// (Type 2: FN + UID + optional EMAIL).
func SignedVCard(name, email, uid string) string {
	var b strings.Builder
	b.WriteString("BEGIN:VCARD\r\nVERSION:4.0\r\n")
	b.WriteString("FN:" + name + "\r\n")
	b.WriteString("UID:" + uid + "\r\n")
	if email != "" {
		b.WriteString("item1.EMAIL;PREF=1:" + email + "\r\n")
	}
	b.WriteString("END:VCARD")
	return b.String()
}

// EncryptedVCard builds the encrypted portion of a Proton contact
// (Type 3: TEL + NOTE + ORG).
func EncryptedVCard(phone, note, org string) string {
	var b strings.Builder
	b.WriteString("BEGIN:VCARD\r\nVERSION:4.0\r\n")
	if phone != "" {
		b.WriteString("TEL;PREF=1:" + phone + "\r\n")
	}
	if note != "" {
		b.WriteString("NOTE:" + note + "\r\n")
	}
	if org != "" {
		b.WriteString("ORG:" + org + "\r\n")
	}
	b.WriteString("END:VCARD")
	return b.String()
}

// ParseTime accepts a handful of common user-entered date/time formats,
// interpreting bare dates/times in the local timezone.
func ParseTime(s string) (time.Time, error) {
	for _, f := range []string{
		time.RFC3339,
		"2006-01-02T15:04",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	} {
		if t, err := time.ParseInLocation(f, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized time format: %s", s)
}
