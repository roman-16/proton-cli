package cli

import (
	"fmt"

	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/proton"
)

// cliHVResolver answers Proton's human-verification challenges by sending a
// person to Proton's own verification page.
//
// The page is what makes this work anywhere: opened in an ordinary browser it
// submits the solved CAPTCHA itself, leaving the proof standing on the challenge
// token - so what goes back to Proton is the challenge this run was given, and
// the browser can be on another machine entirely. Nothing has to be rendered
// here, which is why a server verifies as readily as a desktop.
//
// The address is printed whether or not a browser opened. Whether a machine can
// show a page is not a question worth asking - a forwarded display, a container
// with a launcher and no session, and a desktop all answer it differently and
// wrongly - and a run that claimed to have opened something it had not would
// leave nothing on screen to act on.
func cliHVResolver(a *app.App) proton.HVResolver {
	return func(hvErr *proton.HumanVerificationError) (string, string, error) {
		page, err := a.API.VerifyURL(hvErr)
		if err != nil {
			return "", "", proton.ErrHVUnavailable
		}

		// A verification solved before this run started is presented rather than
		// asked for again. It is the only way through for a run that cannot wait,
		// because signing in mints a challenge each time it is asked and a person
		// cannot answer one that has already been discarded.
		if token := a.Verified; token != "" {
			return token, proton.MethodCaptcha, nil
		}
		if !a.UI.CanPrompt() {
			return "", "", proton.ErrHVUnavailable
		}

		a.UI.Break()
		a.UI.Instruct("Proton wants to confirm you are human. Solve the CAPTCHA on this page -")
		if kit.ShowInBrowser(page) {
			a.UI.Instruct("it should have opened in your browser, and works on any device too:")
		} else {
			a.UI.Instruct("you can open it on any device:")
		}
		a.UI.Break()
		a.UI.Instruct("  " + page)
		a.UI.Break()
		if err := a.UI.Await("Press Enter once it says you are verified."); err != nil {
			return "", "", fmt.Errorf("human verification: %w", err)
		}
		return hvErr.Token, proton.MethodCaptcha, nil
	}
}
