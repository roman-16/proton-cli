package export

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/roman-16/proton-cli/internal/progress"
	"github.com/roman-16/proton-cli/internal/proton"
)

type App string

const (
	AppCalendar App = "calendar"
	AppContacts App = "contacts"
	AppDrive    App = "drive"
	AppMail     App = "mail"
	AppPass     App = "pass"
)

var Apps = []App{AppPass, AppContacts, AppCalendar, AppMail, AppDrive}

func Known(name string) (App, bool) {
	for _, app := range Apps {
		if string(app) == name {
			return app, true
		}
	}
	return "", false
}

func Names(apps []App) []string {
	out := make([]string, 0, len(apps))
	for _, app := range apps {
		out = append(out, string(app))
	}
	return out
}

type Part interface {
	App() App
	Noun() string
	Holds(ctx context.Context) ([]Held, error)
	Run(ctx context.Context, c *Copy, sink progress.Sink) (Result, error)
}

type Held struct {
	Count int
	Noun  string
}

const Uncounted = -1

func Version(of ...any) string {
	sum := sha256.New()
	enc := json.NewEncoder(sum)
	for _, v := range of {
		_ = enc.Encode(v)
	}
	return hex.EncodeToString(sum.Sum(nil)[:12])
}

func Stops(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return true
	}
	var network *proton.NetworkError
	var api *proton.APIError
	var verify *proton.HumanVerificationError
	return errors.As(err, &network) || (errors.As(err, &api) && !proton.DoesNotExist(err)) ||
		errors.As(err, &verify) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

type Result struct {
	Exported     int
	Owed         int
	Unreadable   int
	Documents    int
	Spreadsheets int
	Shared       map[string]int
	Unlocked     bool
}

func Total(rs ...Result) Result {
	out := Result{Shared: map[string]int{}}
	for _, r := range rs {
		out.Exported += r.Exported
		out.Owed += r.Owed
		out.Unreadable += r.Unreadable
		out.Documents += r.Documents
		out.Spreadsheets += r.Spreadsheets
		out.Unlocked = out.Unlocked || r.Unlocked
		for noun, n := range r.Shared {
			out.Shared[noun] += n
		}
	}
	return out
}

func Gone(err error) bool { return proton.DoesNotExist(err) }
