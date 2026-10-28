package ical

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/roman-16/proton-cli/internal/contentline"
)

const (
	conferenceIDName  = "X-PM-CONFERENCE-ID"
	conferenceURLName = "X-PM-CONFERENCE-URL"
)

// The values of Proton's VIDEO_CONFERENCE_PROVIDER.
const (
	ProviderZoom = 1
	ProviderMeet = 2
)

type Conference struct {
	ID       string
	URL      string
	Provider int
	Host     string
	Password string
}

func (v *VEvent) conference() *Conference {
	if v.Conference == nil {
		v.Conference = &Conference{}
	}
	return v.Conference
}

func (c *Conference) readID(l contentline.Line) {
	c.ID = strings.TrimSpace(l.Value)
	if n, err := strconv.Atoi(strings.TrimSpace(l.Params.Get("X-PM-PROVIDER"))); err == nil {
		c.Provider = n
	}
}

// Proton's own clients read the host and password from either spelling of the
// parameter.
func (c *Conference) readURL(l contentline.Line) {
	c.URL = strings.TrimSpace(l.Value)
	c.Host = firstOf(l.Params, "X-PM-HOST", "HOST")
	c.Password = firstOf(l.Params, "X-PM-PASSWORD", "PASSWORD")
}

func firstOf(ps contentline.Params, names ...string) string {
	for _, n := range names {
		if v := ps.Get(n); v != "" {
			return v
		}
	}
	return ""
}

func (c Conference) idLine() contentline.Line {
	l := contentline.Line{Name: conferenceIDName, Value: c.ID}
	if c.Provider != 0 {
		l.Params = contentline.Params{{Name: "X-PM-PROVIDER", Value: strconv.Itoa(c.Provider)}}
	}
	return l
}

func (c Conference) urlLine() contentline.Line {
	l := contentline.Line{Name: conferenceURLName, Value: c.URL}
	if c.Password != "" {
		l.Params = append(l.Params, contentline.Param{Name: "X-PM-PASSWORD", Value: c.Password})
	}
	if c.Host != "" {
		l.Params = append(l.Params, contentline.Param{Name: "X-PM-HOST", Value: c.Host})
	}
	return l
}

func (c Conference) provider() int {
	switch {
	case c.Provider != 0:
		return c.Provider
	case meetLink.MatchString(c.URL):
		return ProviderMeet
	case strings.Contains(c.URL, "zoom.us"):
		return ProviderZoom
	}
	return 0
}

func (c Conference) complete() bool { return c.ID != "" && c.URL != "" && c.provider() != 0 }

// callSeparator is SEPARATOR_PROTON_EVENTS, which fences the join instructions
// Proton's web client writes into a description for clients that do not read
// the conference properties.
const callSeparator = "~-~-~-~-~-~-~%~!~%~!~%~!~%~!~%~!~%~!~%~!~%~!~%~!~%~!~%~!~%~!~%~!~-~-~-~-~-~-~"

var (
	callBlock = regexp.MustCompile(`\n?` + regexp.QuoteMeta(callSeparator) + `[\s\S]*?` + regexp.QuoteMeta(callSeparator))
	// PROTON_MEET_REGEX_LOCATION, which Proton's web client also finds a call by
	// when an event carries no conference properties.
	meetLink = regexp.MustCompile(`(https://)?meet(?:\.\w+)?\..+/join/id-(\w+)#pwd-([^\s#]+)`)
)

type Call struct {
	Provider int
	URL      string
}

func (v VEvent) Call() (Call, bool) {
	if c := v.Conference; c != nil && c.URL != "" {
		return Call{Provider: c.provider(), URL: c.URL}, true
	}
	m := meetLink.FindStringSubmatch(v.Description)
	if m == nil {
		return Call{}, false
	}
	url := m[0]
	if m[1] == "" {
		url = "https://" + url
	}
	return Call{Provider: ProviderMeet, URL: url}, true
}

func (v VEvent) ShownDescription() string {
	if _, ok := v.Call(); !ok {
		return v.Description
	}
	return strings.TrimSpace(withoutCallBlock(v.Description))
}

func (v VEvent) WithDescription(text string) VEvent {
	v.Description = described(text, v.Conference)
	return v
}

func (v VEvent) WithConference(c Conference) VEvent {
	v.Conference = &c
	v.Description = described(v.Description, &c)
	return v
}

func (v VEvent) WithoutCall() VEvent {
	v.Conference = nil
	v.Description = strings.TrimSpace(withoutCallBlock(v.Description))
	return v
}

func withoutCallBlock(text string) string { return callBlock.ReplaceAllString(text, "") }

// described is modelToDescriptionProperties in Proton's web client: the text
// without any join instructions, and the ones for the event's own call after it.
func described(text string, c *Conference) string {
	if c == nil || !c.complete() {
		return text
	}
	return strings.TrimSpace(withoutCallBlock(text)) + c.block()
}

// block is addVideoConfInfoToDescription in Proton's web client, word for word,
// since other clients show these lines to the attendees.
func (c Conference) block() string {
	if c.provider() == ProviderMeet {
		return "\n" + callSeparator + "\nJoin Proton Meet: " + c.URL + "\n" + callSeparator
	}
	passcode := ""
	if c.Password != "" {
		passcode = ", passcode: " + c.Password
	}
	host := ""
	if c.Host != "" {
		host = "Meeting host: " + c.Host
	}
	return "\n" + callSeparator + "\nJoin Zoom Meeting: " + c.URL + " (ID: " + c.ID + passcode + ")\n\n" +
		host + "\n" + callSeparator
}
