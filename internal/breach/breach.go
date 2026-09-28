package breach

import (
	"sort"
	"time"
)

type Breach struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Email        string   `json:"email"`
	Severity     string   `json:"severity"`
	Found        int64    `json:"found,omitempty"`
	Published    int64    `json:"published,omitempty"`
	Exposed      []string `json:"exposed"`
	Source       string   `json:"source,omitempty"`
	Size         int      `json:"size,omitempty"`
	PasswordTail string   `json:"password_tail,omitempty"`
	Actions      []string `json:"actions"`
	State        string   `json:"state"`
}

const (
	StateNew      = "new"
	StateOpen     = "open"
	StateResolved = "resolved"
)

const (
	alertUnread   = 1
	alertRead     = 2
	alertResolved = 3
)

func Code(state string) int {
	switch state {
	case StateResolved:
		return alertResolved
	case StateNew:
		return alertUnread
	}
	return alertRead
}

func (b Breach) Resolved() bool { return b.State == StateResolved }

type Record struct {
	ID                string
	Email             string
	ResolvedState     int
	Severity          float64
	Name              string
	CreatedAt         string
	PublishedAt       string
	Size              *int
	PasswordLastChars *string
	ExposedData       []struct{ Name string }
	Actions           []struct{ Name string }
	Source            struct {
		IsAggregated bool
		Domain       *string
	}
}

type Answer struct {
	IsEligible bool
	Count      int
	Breaches   []Record
	Samples    []Record
}

type Report struct {
	Breaches []Breach
	Withheld int
	Eligible bool
}

func (a Answer) Report() Report {
	records := a.Breaches
	if !a.IsEligible {
		records = a.Samples
	}
	report := Report{Breaches: make([]Breach, 0, len(records)), Eligible: a.IsEligible}
	for _, r := range records {
		report.Breaches = append(report.Breaches, r.Breach())
	}
	sort.SliceStable(report.Breaches, func(i, j int) bool {
		return when(report.Breaches[i]) > when(report.Breaches[j])
	})
	if a.Count > len(report.Breaches) {
		report.Withheld = a.Count - len(report.Breaches)
	}
	return report
}

func when(b Breach) int64 {
	if b.Published != 0 {
		return b.Published
	}
	return b.Found
}

func (r Record) Breach() Breach {
	b := Breach{
		ID: r.ID, Name: r.Name, Email: r.Email,
		Severity: severity(r.Severity),
		Found:    unix(r.CreatedAt), Published: unix(r.PublishedAt),
		Exposed: []string{},
		Actions: []string{},
		State:   state(r.ResolvedState),
	}
	for _, e := range r.ExposedData {
		b.Exposed = append(b.Exposed, e.Name)
	}
	for _, a := range r.Actions {
		b.Actions = append(b.Actions, a.Name)
	}
	switch {
	case r.Source.Domain != nil && *r.Source.Domain != "":
		b.Source = *r.Source.Domain
	case r.Source.IsAggregated:
		b.Source = "several sources"
	}
	if r.Size != nil {
		b.Size = *r.Size
	}
	if r.PasswordLastChars != nil {
		b.PasswordTail = *r.PasswordLastChars
	}
	return b
}

func severity(v float64) string {
	switch {
	case v < 0.33:
		return "low"
	case v < 0.67:
		return "medium"
	default:
		return "high"
	}
}

func state(code int) string {
	switch code {
	case alertUnread:
		return StateNew
	case alertResolved:
		return StateResolved
	}
	return StateOpen
}

func unix(stamp string) int64 {
	at, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return 0
	}
	return at.Unix()
}
