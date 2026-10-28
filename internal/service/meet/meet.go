package meet

import (
	"fmt"
	"time"

	pgp "github.com/ProtonMail/gopenpgp/v3/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/proton"
)

type Service struct {
	C    proton.Doer
	keys keys.Get
}

func New(c proton.Doer, k keys.Get) *Service { return &Service{C: c, keys: k} }

// Type is Proton's MeetingType.
type Type int

const (
	Instant   Type = 0
	Personal  Type = 1
	Scheduled Type = 2
	Recurring Type = 3
	Room      Type = 4
)

func (t Type) String() string {
	switch t {
	case Instant:
		return "instant"
	case Personal:
		return "personal"
	case Scheduled:
		return "scheduled"
	case Recurring:
		return "recurring"
	case Room:
		return "room"
	}
	return fmt.Sprintf("type %d", int(t))
}

func (t Type) MarshalText() ([]byte, error) { return []byte(t.String()), nil }

func (t Type) Timed() bool { return t == Scheduled || t == Recurring }

func (t Type) Room() bool { return t == Room || t == Personal }

type NewMeeting struct {
	Title        string
	Type         Type
	AddressID    string
	Start, End   *time.Time
	RRule        string
	Timezone     string
	FromCalendar bool
}

type Meeting struct {
	ID              string     `json:"-"`
	Name            string     `json:"id"`
	Title           string     `json:"title"`
	URL             string     `json:"url"`
	Type            Type       `json:"type"`
	Start           *time.Time `json:"start,omitempty"`
	End             *time.Time `json:"end,omitempty"`
	Next            *time.Time `json:"next,omitempty"`
	Timezone        string     `json:"zone,omitempty"`
	RRule           string     `json:"rrule,omitempty"`
	CalendarID      string     `json:"calendar_id,omitempty"`
	CalendarEventID string     `json:"calendar_event_id,omitempty"`

	sessionKey     *pgp.SessionKey
	madeByCalendar bool
}

func (m *Meeting) SealTitle(title string) (string, error) { return sealTitle(m.sessionKey, title) }

func (m Meeting) FromCalendar() bool { return m.madeByCalendar || m.CalendarEventID != "" }
