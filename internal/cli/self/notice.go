package self

import (
	"context"
	"net/http"
	"time"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/selfmanage"
	"github.com/roman-16/proton-cli/internal/ui"
)

// lookup is all the time a courtesy may take. A machine that cannot answer in
// two seconds has nothing this run needs, and waiting longer would turn a
// remark into an interruption.
const lookup = 2 * time.Second

// Available says a release is there and what to do about it.
//
// There is one wording, because there is one thing being said. `update --check`
// asks the question outright and the notice volunteers the answer, and a reader
// who has seen either has learned the other.
func Available(u *ui.UI, current, latest string) {
	u.Notef("%s %s → %s available.", kit.Program, current, latest)
	u.Notef("Run `%s update` to install it, or `%s changelog %s` for what changed.",
		kit.Program, kit.Program, latest)
}

// Notice mentions a release once a day, to a reader who could install it.
//
// It runs after the command has written everything it had to write, so the wait
// falls where nobody is waiting: the answer is already on the screen and the
// only thing still to come is the shell prompt. Once a day is what makes that
// affordable - every other command that day costs nothing at all - and it is
// also how often the same news is worth repeating.
//
// The day is spent before the lookup rather than after it, so a machine that is
// offline, or one whose owner runs a hundred commands in a shell loop, makes one
// attempt between them rather than one each.
func Notice(ctx context.Context, u *ui.UI, version string, off bool) {
	if off || u.Quiet || !u.ErrIsTTY() {
		return
	}
	exe, err := resolveExe()
	if err != nil || !selfmanage.Watched(exe, version) {
		return
	}
	path := selfmanage.CheckPath()
	if !selfmanage.LoadCheck(path).Due(time.Now()) {
		return
	}
	if err := selfmanage.SaveCheck(path, selfmanage.Check{CheckedAt: time.Now()}); err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, lookup)
	defer cancel()
	latest, err := selfmanage.LatestVersion(ctx, &http.Client{Timeout: lookup})
	if err != nil || !selfmanage.IsNewer(latest, version) {
		return
	}
	u.Break()
	Available(u, version, latest)
}
