package mail

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/ui"
)

// An answer from the index says what it did not cover, and the four ways it
// can be short are four different things to do about it.
//
// Every one of them looks the same on the screen without the caveat - an empty
// listing, or a short one - so what is at stake is whether somebody reads "no
// such message" off an answer that never looked.
func TestAnIndexedSearchSaysWhatItDidNotCover(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cover mailsvc.Coverage
		opts  mailsvc.ListOptions
		want  string
	}{
		{
			name:  "the mailbox is still being read",
			cover: mailsvc.Coverage{Indexed: true, Partial: true, Have: 12400, Total: 48213},
			opts:  mailsvc.ListOptions{Keyword: "permit"},
			want:  "Only 12400 of 48213 messages are indexed",
		},
		{
			name:  "a keyword with nothing indexed",
			cover: mailsvc.Coverage{},
			opts:  mailsvc.ListOptions{Keyword: "permit"},
			want:  "There is no mail index on this machine",
		},
		{
			name:  "the bodies are still arriving",
			cover: mailsvc.Coverage{Indexed: true, Have: 48213, Total: 48213, Bodies: 2830},
			opts:  mailsvc.ListOptions{Keyword: "permit"},
			want:  "Only 2830 of 48213 message bodies are indexed",
		},
		{
			name:  "a filter that is not about what a message says",
			cover: mailsvc.Coverage{Indexed: true, Have: 48213, Total: 48213, Bodies: 2830},
			opts:  mailsvc.ListOptions{Unread: true},
			want:  "",
		},
		{
			name:  "Proton could not say what changed",
			cover: mailsvc.Coverage{Indexed: true, Have: 48213, Total: 48213, Bodies: 48213, Stale: true},
			opts:  mailsvc.ListOptions{Unread: true},
			want:  "The mail index has fallen behind Proton",
		},
		{
			name:  "Proton answered a question it can answer",
			cover: mailsvc.Coverage{},
			opts:  mailsvc.ListOptions{Unread: true},
			want:  "",
		},
		{
			name:  "the whole mailbox answered",
			cover: mailsvc.Coverage{Indexed: true, Have: 48213, Total: 48213, Bodies: 48213},
			opts:  mailsvc.ListOptions{Keyword: "permit"},
			want:  "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var errb bytes.Buffer
			c := &kit.Invocation{
				Ctx: context.Background(),
				App: &app.App{UI: ui.New(ui.Options{Format: ui.FormatText, Out: &bytes.Buffer{}, Err: &errb})},
			}
			shortIndex(c, tc.cover, tc.opts)
			got := errb.String()
			switch {
			case tc.want == "" && got != "":
				t.Errorf("said %q, want nothing", got)
			case tc.want != "" && !strings.Contains(got, tc.want):
				t.Errorf("said %q, want it to carry %q", got, tc.want)
			}
		})
	}
}
