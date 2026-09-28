package mail

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/roman-16/proton-cli/internal/fetch"
	"github.com/roman-16/proton-cli/internal/mailtext"
	"github.com/roman-16/proton-cli/internal/proton"
)

// ErrTrackingNotBlocked is a tracker report asked of an account that has Block
// email tracking off, where nothing in a message is blocked or cleaned.
var ErrTrackingNotBlocked = errors.New("block email tracking is off")

// Tracker is one thing in a message that would have told its sender about the
// reader: an image a known tracker serves, or a link that carried tracking.
type Tracker struct {
	Kind    string `json:"kind"`
	Name    string `json:"tracker"`
	URL     string `json:"url"`
	Cleaned string `json:"cleaned,omitempty"`
}

const (
	TrackerImage = "image"
	TrackerLink  = "link"
)

// Trackers is the report Proton's web client shows beside a message: the images
// it blocked, grouped by who serves them, then the links it cleaned.
func (s *Service) Trackers(ctx context.Context, id string) ([]Tracker, error) {
	var raw *rawMessage
	var body string
	var set mailSettings
	err := fetch.Together(ctx,
		func(ctx context.Context) error {
			m, u, err := s.messageAndKeys(ctx, id)
			if err != nil {
				return err
			}
			opened, _, err := s.openBody(ctx, u, *m, false)
			if err != nil {
				return u.Explain(err, "message", sealed(m.Body))
			}
			raw, body = m, opened
			return nil
		},
		func(ctx context.Context) error {
			var err error
			set, err = s.settings(ctx)
			return err
		},
	)
	if err != nil {
		return nil, err
	}
	if !set.blocksTracking() {
		return nil, ErrTrackingNotBlocked
	}

	html := mailtext.IsHTML(raw.MIMEType)
	var images []Tracker
	for _, a := range raw.Attachments {
		if provider := a.header("x-pm-tracker-provider"); provider != "" {
			images = append(images, Tracker{Kind: TrackerImage, Name: provider, URL: embeddedLocation(a)})
		}
	}
	if set.proxiesImages() && html {
		remote, err := s.imageTrackers(ctx, mailtext.RemoteImages(body))
		if err != nil {
			return nil, err
		}
		images = append(images, remote...)
	}

	trackers := groupByProvider(images)
	_, links := mailtext.CleanLinks(body, html)
	for _, l := range links {
		trackers = append(trackers, Tracker{Kind: TrackerLink, Name: removedNames(l), URL: l.Original, Cleaned: l.Cleaned})
	}
	return trackers, nil
}

func embeddedLocation(a rawAttachment) string {
	location := strings.Trim(a.header("content-location"), `"'`)
	return strings.NewReplacer("\r", "", "\n", "").Replace(location)
}

func (s *Service) imageTrackers(ctx context.Context, images []string) ([]Tracker, error) {
	providers := make([]string, len(images))
	checks := make([]func(context.Context) error, len(images))
	for i, image := range images {
		checks[i] = func(ctx context.Context) error {
			var err error
			providers[i], err = s.trackerProvider(ctx, image)
			return err
		}
	}
	if err := fetch.Together(ctx, checks...); err != nil {
		return nil, err
	}
	var trackers []Tracker
	for i, provider := range providers {
		if provider != "" {
			trackers = append(trackers, Tracker{Kind: TrackerImage, Name: provider, URL: images[i]})
		}
	}
	return trackers, nil
}

// trackerProvider asks Proton's image proxy who serves an image, without having
// it load the image, and answers "" for an image no known tracker serves.
func (s *Service) trackerProvider(ctx context.Context, image string) (string, error) {
	resp, err := s.C.Do(ctx, proton.Request{
		Method: "GET", Path: "/core/v4/images",
		Query: url.Values{"Url": {encodeImageURI(image)}, "DryRun": {"1"}},
	})
	var apiErr *proton.APIError
	if errors.As(err, &apiErr) && apiErr.HTTPStatus < 500 && apiErr.HTTPStatus != 429 {
		// Recorded and not counted: an image Proton will not check has no
		// verdict, and the web client reports it as no tracker too.
		slog.DebugContext(ctx, "Proton declined to check an image for a tracker",
			"status", apiErr.HTTPStatus, "code", apiErr.Code)
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return resp.Header.Get("x-pm-tracker-provider"), nil
}

var doubleEncoded = regexp.MustCompile(`%25([0-9A-Fa-f]{2})`)

// encodeImageURI is the image address as the web client hands it to the proxy:
// the part before the query escaped the way encodeURI escapes it, without
// escaping an escape twice, and the first query segment as it was.
func encodeImageURI(image string) string {
	parts := strings.Split(strings.TrimSpace(image), "?")
	base := doubleEncoded.ReplaceAllString(encodeURI(parts[0]), "%$1")
	if len(parts) > 1 && parts[1] != "" {
		return base + "?" + parts[1]
	}
	return base
}

func encodeURI(s string) string {
	const unescaped = ";,/?:@&=+$-_.!~*'()#"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x80 && (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte(unescaped, c) >= 0) {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

func groupByProvider(images []Tracker) []Tracker {
	var order []string
	for _, t := range images {
		if !slices.Contains(order, t.Name) {
			order = append(order, t.Name)
		}
	}
	grouped := make([]Tracker, 0, len(images))
	for _, name := range order {
		for _, t := range images {
			if t.Name == name && !slices.ContainsFunc(grouped, func(g Tracker) bool { return g.Name == name && g.URL == t.URL }) {
				grouped = append(grouped, t)
			}
		}
	}
	return grouped
}

func removedNames(l mailtext.CleanedLink) string {
	if len(l.Removed) == 0 {
		return "redirect"
	}
	names := make([]string, len(l.Removed))
	for i, p := range l.Removed {
		names[i] = p.Key
	}
	return strings.Join(names, ", ")
}
