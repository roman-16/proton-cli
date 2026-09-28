package mail

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/internal/errs"
)

// An answer quotes the message it is answering, so a body that will not open
// stops the reply rather than quoting the armour.
//
// This is where the CLI parts company with the web client, which quotes the raw
// body on a decryption error. A send is the one place the writer never sees the
// quote before it leaves, and a page of ciphertext arriving under somebody's
// reply is worse than being told to answer without it.
func TestAnswerRefusesToQuoteABodyItCannotOpen(t *testing.T) {
	api := &recordingAPI{message: encryptedMessage(t, genMailKeyRing(t), "secret", mimeTypePlain)}
	s := New(api, testKeys(unlockedRings("addr-1", genMailKeyRing(t))))

	_, err := s.Answer(context.Background(), "m1", AnswerSpec{Action: ActionReply, Body: "Thanks."})
	if err == nil {
		t.Fatal("the reply was built around a body that never opened")
	}
	var problem *errs.Problem
	if !errors.As(err, &problem) {
		t.Fatalf("err = %v (%T), want a phrased problem", err, err)
	}
	if problem.ExitCode() != errs.ExitBug {
		t.Errorf("exit = %d, want %d: a message that will not decrypt is not the caller's mistake",
			problem.ExitCode(), errs.ExitBug)
	}
	if !strings.Contains(strings.Join(problem.Hints(), " "), "--no-quote") {
		t.Errorf("hints = %v, want the way to answer anyway", problem.Hints())
	}
}

func TestAnswerQuotesLinksWithoutTheirTrackingWhileTrackingIsBlocked(t *testing.T) {
	for imageProxy, want := range map[int]string{
		ProxyRemoteImages: `href="https://trailhead.example/north"`,
		0:                 `href="https://trailhead.example/north?utm_source=newsletter"`,
	} {
		kr := genMailKeyRing(t)
		message := encryptedMessage(t, kr,
			`<a href="https://trailhead.example/north?utm_source=newsletter">Read the trail report</a>`, "text/html")
		message["Sender"] = map[string]any{"Address": "news@trailhead.example", "Name": "Trailhead Weekly"}
		api := &recordingAPI{message: message, settings: map[string]any{"ImageProxy": imageProxy}}
		s := New(api, testKeys(unlockedRings("addr-1", kr)))

		content, err := s.Answer(context.Background(), "m1", AnswerSpec{
			Action: ActionForward, Body: "Worth a look.", To: []Recipient{{Address: "jane@example.com"}}, NoSignature: true,
		})
		if err != nil {
			t.Fatalf("ImageProxy %d: Answer: %v", imageProxy, err)
		}
		if !strings.Contains(content.Body, want) {
			t.Errorf("ImageProxy %d: the forward quotes\n%s\nwant it to carry %s", imageProxy, content.Body, want)
		}
	}
}
