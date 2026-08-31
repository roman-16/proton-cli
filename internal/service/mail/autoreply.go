package mail

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/proton"
)

// Proton stores the auto-responder as one settings object whose StartTime and
// EndTime mean different things per repeat mode: an absolute instant for a fixed
// window, seconds-into-day for a daily one, and a packed day+time offset for the
// weekly and monthly ones. This file is the only place that packing exists; the
// rest of the CLI speaks the wall-clock grammar of AutoReply.

// Repeat modes, matching Proton's AutoReplyDuration.
const (
	repeatFixed     = 0
	repeatDaily     = 1
	repeatWeekly    = 2
	repeatMonthly   = 3
	repeatPermanent = 4
)

// autoReplySubject is the subject Proton sends every auto-reply under. The web
// client hardcodes it and exposes no way to change it, so neither does the CLI.
const autoReplySubject = "Auto"

const daySeconds = 24 * 60 * 60

// weekdayNames indexes weekdays the way Proton does, with Sunday at 0.
var weekdayNames = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

var repeatNames = []struct {
	Name string
	N    int
}{
	{"fixed", repeatFixed},
	{"daily", repeatDaily},
	{"weekly", repeatWeekly},
	{"monthly", repeatMonthly},
	{"permanent", repeatPermanent},
}

// RepeatNames lists the accepted --repeat values, for help and validation.
func RepeatNames() []string {
	out := make([]string, 0, len(repeatNames))
	for _, r := range repeatNames {
		out = append(out, r.Name)
	}
	return out
}

func repeatFromName(s string) (int, error) {
	for _, r := range repeatNames {
		if strings.EqualFold(s, r.Name) {
			return r.N, nil
		}
	}
	return 0, fmt.Errorf("unknown repeat %q (use: %s)", s, strings.Join(RepeatNames(), ", "))
}

func repeatName(n int) string {
	for _, r := range repeatNames {
		if r.N == n {
			return r.Name
		}
	}
	return strconv.Itoa(n)
}

// AutoReply is the auto-responder in wall-clock terms. Start and End are read
// and written in the grammar the repeat mode dictates:
//
//	fixed      2026-07-01T09:00   (a date and time in Zone)
//	daily      09:00              (a time of day)
//	weekly     mon:09:00          (a weekday and time)
//	monthly    1:09:00            (a day of the month and time)
//	permanent  -                  (no bounds)
type AutoReply struct {
	Enabled bool     `json:"enabled"`
	Repeat  string   `json:"repeat"`
	Start   string   `json:"start,omitempty"`
	End     string   `json:"end,omitempty"`
	Days    []string `json:"days,omitempty"`
	Zone    string   `json:"zone,omitempty"`
	Message string   `json:"message"`
	Subject string   `json:"subject"`
}

// apiAutoResponder is the wire shape. Proton requires every field on write, so
// enable/disable is a read-modify-write of the whole object.
type apiAutoResponder struct {
	IsEnabled    bool   `json:"IsEnabled"`
	Message      string `json:"Message"`
	Repeat       int    `json:"Repeat"`
	DaysSelected []int  `json:"DaysSelected"`
	Zone         string `json:"Zone"`
	Subject      string `json:"Subject"`
	StartTime    int64  `json:"StartTime"`
	EndTime      int64  `json:"EndTime"`
}

// ScheduleSummary renders the schedule as one line, for status output.
func (a AutoReply) ScheduleSummary() string {
	switch a.Repeat {
	case "permanent":
		return "permanent"
	case "daily":
		s := fmt.Sprintf("daily %s-%s", a.Start, a.End)
		if len(a.Days) > 0 {
			s += " on " + strings.Join(a.Days, ",")
		}
		return s
	default:
		return fmt.Sprintf("%s %s to %s", a.Repeat, a.Start, a.End)
	}
}

// zoneOrLocal resolves the auto-reply's zone, falling back to the host zone.
func (a AutoReply) zoneOrLocal() (*time.Location, string, error) {
	name := a.Zone
	if name == "" {
		name = time.Now().Location().String()
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, "", fmt.Errorf("unknown time zone %q", name)
	}
	return loc, name, nil
}

// encode packs an AutoReply into the wire shape, validating that Start, End and
// Days match what the repeat mode allows.
func (a AutoReply) encode() (*apiAutoResponder, error) {
	repeat, err := repeatFromName(a.Repeat)
	if err != nil {
		return nil, err
	}
	loc, zone, err := a.zoneOrLocal()
	if err != nil {
		return nil, err
	}
	if repeat != repeatDaily && len(a.Days) > 0 {
		return nil, fmt.Errorf("--days applies to --repeat daily only")
	}
	out := &apiAutoResponder{
		IsEnabled: a.Enabled, Message: a.Message, Repeat: repeat,
		DaysSelected: []int{}, Zone: zone, Subject: autoReplySubject,
	}
	if repeat == repeatPermanent {
		if a.Start != "" || a.End != "" {
			return nil, fmt.Errorf("--repeat permanent takes no --start or --end")
		}
		return out, nil
	}
	if a.Start == "" || a.End == "" {
		return nil, fmt.Errorf("--repeat %s needs --start and --end (%s)", a.Repeat, startEndFormat(repeat))
	}
	if out.StartTime, err = encodeBound(a.Start, repeat, loc); err != nil {
		return nil, fmt.Errorf("invalid --start: %w", err)
	}
	if out.EndTime, err = encodeBound(a.End, repeat, loc); err != nil {
		return nil, fmt.Errorf("invalid --end: %w", err)
	}
	if repeat == repeatFixed && out.EndTime <= out.StartTime {
		return nil, fmt.Errorf("--end must be after --start")
	}
	if repeat == repeatDaily {
		days, err := encodeDays(a.Days)
		if err != nil {
			return nil, err
		}
		out.DaysSelected = days
	}
	return out, nil
}

// startEndFormat describes a mode's Start/End grammar, for error messages.
func startEndFormat(repeat int) string {
	switch repeat {
	case repeatFixed:
		return "e.g. 2026-07-01T09:00"
	case repeatDaily:
		return "e.g. 09:00"
	case repeatWeekly:
		return "e.g. mon:09:00"
	case repeatMonthly:
		return "e.g. 1:09:00"
	}
	return ""
}

func encodeBound(raw string, repeat int, loc *time.Location) (int64, error) {
	switch repeat {
	case repeatFixed:
		t, err := time.ParseInLocation("2006-01-02T15:04", raw, loc)
		if err != nil {
			if t, err = time.ParseInLocation("2006-01-02 15:04", raw, loc); err != nil {
				return 0, fmt.Errorf("%q is not a date and time (%s)", raw, startEndFormat(repeat))
			}
		}
		return t.Unix(), nil
	case repeatDaily:
		secs, err := parseClock(raw)
		if err != nil {
			return 0, err
		}
		return secs, nil
	case repeatWeekly:
		day, clock, err := splitDayClock(raw)
		if err != nil {
			return 0, err
		}
		n, err := parseWeekday(day)
		if err != nil {
			return 0, err
		}
		return int64(n)*daySeconds + clock, nil
	case repeatMonthly:
		day, clock, err := splitDayClock(raw)
		if err != nil {
			return 0, err
		}
		n, err := strconv.Atoi(day)
		if err != nil || n < 1 || n > 31 {
			return 0, fmt.Errorf("%q is not a day of the month (1-31)", day)
		}
		return int64(n-1)*daySeconds + clock, nil
	}
	return 0, nil
}

// splitDayClock splits "mon:09:00" into ("mon", secondsIntoDay).
func splitDayClock(raw string) (string, int64, error) {
	i := strings.Index(raw, ":")
	if i < 0 {
		return "", 0, fmt.Errorf("%q is missing a day (e.g. mon:09:00)", raw)
	}
	clock, err := parseClock(raw[i+1:])
	if err != nil {
		return "", 0, err
	}
	return raw[:i], clock, nil
}

// parseClock converts "09:00" into seconds into the day.
func parseClock(raw string) (int64, error) {
	t, err := time.Parse("15:04", raw)
	if err != nil {
		return 0, fmt.Errorf("%q is not a time of day (e.g. 09:00)", raw)
	}
	return int64(t.Hour()*60*60 + t.Minute()*60), nil
}

func parseWeekday(raw string) (int, error) {
	raw = strings.ToLower(raw)
	for i, n := range weekdayNames {
		if raw == n || raw == longWeekday(i) {
			return i, nil
		}
	}
	return 0, fmt.Errorf("%q is not a weekday (%s)", raw, strings.Join(weekdayNames, ", "))
}

func longWeekday(i int) string {
	return strings.ToLower(time.Weekday(i).String())
}

func encodeDays(names []string) ([]int, error) {
	out := make([]int, 0, len(names))
	seen := map[int]bool{}
	for _, raw := range names {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			n, err := parseWeekday(part)
			if err != nil {
				return nil, fmt.Errorf("invalid --days: %w", err)
			}
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--repeat daily needs --days (e.g. mon,tue,wed,thu,fri)")
	}
	return out, nil
}

// decode unpacks the wire shape back into wall-clock terms, so what `get`
// prints can be handed straight back to `set`.
func (r apiAutoResponder) decode() AutoReply {
	out := AutoReply{
		Enabled: r.IsEnabled, Repeat: repeatName(r.Repeat),
		Zone: r.Zone, Message: r.Message, Subject: r.Subject,
	}
	loc := time.Local
	if r.Zone != "" {
		if l, err := time.LoadLocation(r.Zone); err == nil {
			loc = l
		}
	}
	if r.Repeat != repeatPermanent {
		out.Start = decodeBound(r.StartTime, r.Repeat, loc)
		out.End = decodeBound(r.EndTime, r.Repeat, loc)
	}
	if r.Repeat == repeatDaily {
		for _, d := range r.DaysSelected {
			if d >= 0 && d < len(weekdayNames) {
				out.Days = append(out.Days, weekdayNames[d])
			}
		}
	}
	return out
}

func decodeBound(v int64, repeat int, loc *time.Location) string {
	switch repeat {
	case repeatFixed:
		return time.Unix(v, 0).In(loc).Format("2006-01-02T15:04")
	case repeatDaily:
		return formatClock(v)
	case repeatWeekly:
		day := int(v / daySeconds)
		if day < 0 || day >= len(weekdayNames) {
			day = 0
		}
		return weekdayNames[day] + ":" + formatClock(v%daySeconds)
	case repeatMonthly:
		return strconv.Itoa(int(v/daySeconds)+1) + ":" + formatClock(v%daySeconds)
	}
	return ""
}

func formatClock(secs int64) string {
	return fmt.Sprintf("%02d:%02d", secs/(60*60), (secs%(60*60))/60)
}

// DecodeAutoReply parses a raw MailSettings.AutoResponder object. It exists so
// the settings renderer can summarise the auto-reply without a second request.
func DecodeAutoReply(raw []byte) (AutoReply, error) {
	var r apiAutoResponder
	if err := json.Unmarshal(raw, &r); err != nil {
		return AutoReply{}, err
	}
	return r.decode(), nil
}

func (s *Service) fetchAutoResponder(ctx context.Context) (*apiAutoResponder, error) {
	var resp struct {
		MailSettings struct{ AutoResponder apiAutoResponder }
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/mail/v4/settings"}, &resp); err != nil {
		return nil, err
	}
	return &resp.MailSettings.AutoResponder, nil
}

// AutoReplyGet reads the current auto-responder.
func (s *Service) AutoReplyGet(ctx context.Context) (*AutoReply, error) {
	raw, err := s.fetchAutoResponder(ctx)
	if err != nil {
		return nil, err
	}
	ar := raw.decode()
	return &ar, nil
}

// AutoReplySet writes a whole auto-responder and enables it, mirroring the web
// client's form: saving a schedule is what turns the auto-reply on.
func (s *Service) AutoReplySet(ctx context.Context, ar AutoReply) error {
	ar.Enabled = true
	body, err := ar.encode()
	if err != nil {
		return err
	}
	return s.putAutoResponder(ctx, body)
}

// ValidateAutoReply checks the complete schedule without sending a request.
// Commands call it before asking for consent, so a malformed local value is
// rejected before the user is asked to approve anything.
func ValidateAutoReply(ar AutoReply) error {
	ar.Enabled = true
	_, err := ar.encode()
	return err
}

// AutoReplyToggle flips IsEnabled while preserving the stored schedule.
func (s *Service) AutoReplyToggle(ctx context.Context, enabled bool) error {
	raw, err := s.fetchAutoResponder(ctx)
	if err != nil {
		return err
	}
	if raw.Subject == "" {
		raw.Subject = autoReplySubject
	}
	if raw.DaysSelected == nil {
		raw.DaysSelected = []int{}
	}
	raw.IsEnabled = enabled
	return s.putAutoResponder(ctx, raw)
}

func (s *Service) putAutoResponder(ctx context.Context, body *apiAutoResponder) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/mail/v4/settings/autoresponder",
		Body: map[string]any{"AutoResponder": body},
	}, nil)
}
