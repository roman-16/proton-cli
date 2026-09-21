package account

import (
	"context"
	"net/url"
	"strconv"

	"github.com/roman-16/proton-cli/internal/fetch"
	"github.com/roman-16/proton-cli/internal/proton"
)

// What Proton recorded about the account: every sign-in, and every change made
// to the credentials that allow one.
//
// The recording is a setting with three positions rather than two, because the
// second choice is how much of each event is kept - an IP address is the part
// that is only written down once somebody asks for it. Turning the recording off
// takes the events with it, which is the one thing about this collection that
// cannot be undone.

const (
	logsPath    = "/core/v4/logs/auth"
	logAuthPath = "/core/v4/settings/logauth"
)

// logsPageMax is how many events Proton returns for one request. Asking for
// more is answered with a 500 rather than with a shorter page.
const logsPageMax = 150

// What the log records, from nothing to an address per event.
const (
	LogOff      = "off"
	LogOn       = "on"
	LogDetailed = "detailed"
)

// Proton's own numbering of the same three (SETTINGS_LOG_AUTH_STATE,
// packages/shared/lib/interfaces/UserSettings.ts).
const (
	logAuthDisabled = 0
	logAuthBasic    = 1
	logAuthAdvanced = 2
)

// SecurityLog is what the account is recording and how much it has.
type SecurityLog struct {
	// Level is one of the three words above.
	Level string `json:"level"`
	// Events is how many the log holds, which is what says whether turning the
	// recording off would destroy anything.
	Events int `json:"events"`
}

// Event is one thing Proton wrote down.
//
// Everything after Status is what the account's protection level decides: an
// address arrives once detailed recording is on, and the four after it only for
// an account Proton Sentinel watches. What is off is absent rather than empty,
// so a listing shows the columns the account actually fills.
type Event struct {
	Time       int64  `json:"time"`
	Event      string `json:"event"`
	Status     string `json:"status"`
	App        string `json:"app,omitempty"`
	IP         string `json:"ip,omitempty"`
	Device     string `json:"device,omitempty"`
	Location   string `json:"location,omitempty"`
	ISP        string `json:"isp,omitempty"`
	Protection string `json:"protection,omitempty"`
}

// How an event ended, as Proton words it (AuthLogStatus).
const (
	EventSuccess = "success"
	EventAttempt = "attempt"
	EventFailure = "failure"
)

// rawEvent is one row as Proton writes it.
type rawEvent struct {
	Time             int64
	Status           string
	Description      string
	IP               string
	AppVersion       string
	Device           string
	Location         string
	InternetProvider string
	ProtectionDesc   string
}

// SecurityLog reads what the account records and how much there is to read.
//
// The setting and the count are asked for together: the count comes from the
// log's own first page, and neither answer is needed to ask for the other.
func (s *Service) SecurityLog(ctx context.Context) (*SecurityLog, error) {
	var (
		level string
		log   struct{ Total int }
	)
	if err := fetch.Together(ctx,
		func(ctx context.Context) error {
			var err error
			level, err = s.SecurityLogLevel(ctx)
			return err
		},
		func(ctx context.Context) error {
			return s.C.Decode(ctx, proton.Request{
				Method: "GET", Path: logsPath,
				Query: url.Values{"Page": {"0"}, "PageSize": {"1"}},
			}, &log)
		},
	); err != nil {
		return nil, err
	}
	return &SecurityLog{Level: level, Events: log.Total}, nil
}

// SecurityLogLevel reads what the account records, for the caller that already
// knows how much there is.
func (s *Service) SecurityLogLevel(ctx context.Context) (string, error) {
	var env struct {
		UserSettings struct{ LogAuth int }
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: settingsPath}, &env); err != nil {
		return "", err
	}
	return logLevel(env.UserSettings.LogAuth), nil
}

// SecurityLogEvents reads a page of events, newest first, and reports how many
// the log holds.
func (s *Service) SecurityLogEvents(ctx context.Context, page, size int) ([]Event, int, error) {
	return proton.Window(ctx, page, size, logsPageMax,
		func(ctx context.Context, page, size int) ([]Event, int, error) {
			var r struct {
				Total int
				Logs  []rawEvent
			}
			if err := s.C.Decode(ctx, proton.Request{
				Method: "GET", Path: logsPath,
				Query: url.Values{
					"Page":     {strconv.Itoa(page)},
					"PageSize": {strconv.Itoa(size)},
				},
			}, &r); err != nil {
				return nil, 0, err
			}
			out := make([]Event, 0, len(r.Logs))
			for _, l := range r.Logs {
				out = append(out, Event{
					Time:       l.Time,
					Event:      l.Description,
					Status:     l.Status,
					App:        l.AppVersion,
					IP:         l.IP,
					Device:     l.Device,
					Location:   l.Location,
					ISP:        l.InternetProvider,
					Protection: l.ProtectionDesc,
				})
			}
			return out, r.Total, nil
		})
}

// SecurityLogSet writes what the account records from now on.
func (s *Service) SecurityLogSet(ctx context.Context, level string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: logAuthPath,
		Body: map[string]any{"LogAuth": logAuthValue(level)},
	}, nil)
}

// SecurityLogDelete removes every event the log holds. What the account records
// from now on is unchanged.
func (s *Service) SecurityLogDelete(ctx context.Context) error {
	return s.C.Decode(ctx, proton.Request{Method: "DELETE", Path: logsPath}, nil)
}

// logLevel names what Proton numbers.
func logLevel(state int) string {
	switch state {
	case logAuthBasic:
		return LogOn
	case logAuthAdvanced:
		return LogDetailed
	}
	return LogOff
}

// logAuthValue is the number Proton keeps a level in.
func logAuthValue(level string) int {
	switch level {
	case LogOn:
		return logAuthBasic
	case LogDetailed:
		return logAuthAdvanced
	}
	return logAuthDisabled
}
