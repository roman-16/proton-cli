// Package paid declares what may not be done to the paid account, and how to
// tell whether a run left it as it found it.
//
// Proton gates a good deal of what the web clients offer behind a subscription,
// and buying a second one to test with is not a reasonable thing to ask. So the
// tests that need a plan act on an account somebody actually uses, under one
// rule: a run has to be reversible.
//
// Two things enforce that rule, and they are both here because they are both
// about the account rather than about any test:
//
//   - Restrictions names the commands that are refused outright, because what
//     they change cannot be read first and put back. The runner asks before it
//     starts a process, so the one place a target account is chosen is the one
//     place the choice can be refused - a test cannot opt out by forgetting.
//   - Photograph is the account as it was. A run takes one before anything runs
//     and one after everything has, and Compare names whatever moved. This is
//     the half that catches a mistake nobody predicted; the restrictions are
//     promises, and this is what checks them.
package paid

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/roman-16/proton-cli/tests/argv"
	"github.com/roman-16/proton-cli/tests/fixture"
)

// A Restriction is one command the paid account refuses, and why.
//
// What earns a place: the command changes something the account has only one of
// and cannot be put back by making another; or it mints something that cannot
// be un-minted; or it acts across the whole account, on data no test made.
// Everything else is allowed, because a test that creates a thing can delete
// it - and a guard that refused everything would be safe and useless.
type Restriction struct {
	// Command is the command's own words, which have to name a real command.
	Command []string
	// Value narrows the restriction to one argument of that command, for the
	// commands whose target is a setting key rather than the verb. Empty means
	// the whole command is refused.
	Value string
	// OnlyOurs narrows the restriction to a target the run did not make, for the
	// command whose danger is not what it does but what it might be aimed at.
	// Deleting a domain the suite created is reversible; the same words pointed at
	// somebody's real domain take every address on it down, and only the reference
	// tells the two apart. So the command is allowed against something carrying
	// the suite's own prefix and refused against everything else - a reference
	// that resolved wrongly is refused, and so is one written by hand.
	OnlyOurs bool
	// Why is what somebody reads when a test they just wrote refuses to run.
	Why string
}

// Restrictions are the commands the paid account refuses.
func Restrictions() []Restriction {
	return []Restriction{{
		Command: []string{"account", "settings", "password", "set"},
		Why: "it would change the password somebody signs in with, and a run that stopped" +
			" halfway would leave them locked out of their own account",
	}, {
		Command: []string{"account", "settings", "recovery-email", "disable"},
		Why:     "it would take away the way back into somebody's real account",
	}, {
		Command: []string{"account", "settings", "recovery-email", "enable"},
		Why:     "which recovery an account allows is the owner's decision about their own account",
	}, {
		Command: []string{"account", "settings", "recovery-email", "set"},
		Why: "it would point the account's recovery at an address the owner did not choose," +
			" and mail Proton's verification to it",
	}, {
		Command: []string{"account", "settings", "recovery-email", "verify"},
		Why:     "it would mail somebody's real recovery address out of a test run",
	}, {
		Command: []string{"account", "settings", "recovery-phone", "disable"},
		Why:     "it would take away the way back into somebody's real account",
	}, {
		Command: []string{"account", "settings", "recovery-phone", "enable"},
		Why:     "which recovery an account allows is the owner's decision about their own account",
	}, {
		Command: []string{"account", "settings", "recovery-phone", "set"},
		Why:     "it would point the account's recovery at a number the owner did not choose",
	}, {
		Command: []string{"account", "settings", "recovery-phone", "verify"},
		Why:     "it would text somebody's real phone out of a test run",
	}, {
		Command: []string{"account", "settings", "recovery-phrase", "disable"},
		Why: "the words the owner wrote down would stop working, and nothing here can hand" +
			" them the new ones",
	}, {
		Command: []string{"account", "settings", "recovery-phrase", "set"},
		Why: "it replaces the phrase the owner wrote down with one printed into a test log," +
			" and the old one stops working",
	}, {
		Command: []string{"account", "settings", "second-password", "disable"},
		Why:     "which passwords an account keeps is the owner's decision about their own account",
	}, {
		Command: []string{"account", "settings", "second-password", "enable"},
		Why: "it would put a password only the run knows in front of somebody's keys, on" +
			" every device they have",
	}, {
		Command: []string{"account", "settings", "second-password", "set"},
		Why: "it would lock somebody's keys with a password only the run knows, on every" +
			" device they have",
	}, {
		Command: []string{"account", "settings", "two-factor", "disable"},
		Why:     "it would take a second factor off somebody's real account",
	}, {
		Command: []string{"account", "settings", "two-factor", "enable"},
		Why: "it would ask for a code only the run can compute at every sign-in the owner" +
			" makes",
	}, {
		Command: []string{"contacts", "merge"},
		Why:     "it folds real contacts together across the whole address book, and nothing separates them again",
	}, {
		Command: []string{"drive", "settings", "set"}, Value: "version-history",
		Why: "lowering it discards revisions Proton has already kept, and nothing puts them back",
	}, {
		Command: []string{"drive", "trash", "empty"},
		Why:     "it deletes everything in the trash, including whatever was there before the run",
	}, {
		Command: []string{"drive", "volumes", "delete"},
		Why: "it deletes a volume with every file on it, and Proton removes them within" +
			" 72 hours with nothing to bring them back",
	}, {
		Command: []string{"mail", "messages", "empty"},
		Why:     "it deletes a whole folder with no listing of what was in it",
	}, {
		Command: []string{"mail", "messages", "mark", "phishing"},
		Why: "it hands somebody's real mail, decrypted, to the people at Proton who read reports," +
			" and nothing withdraws one",
	}, {
		Command: []string{"mail", "settings", "addresses", "delete"},
		Why: "Proton allows one address deletion a year, and the address is gone for good" +
			" - nobody, including its owner, can have it again",
	}, {
		Command: []string{"mail", "settings", "autoreply", "disable"},
		Why:     "it would stop an auto-reply the account owner armed, and the mail that arrives meanwhile goes unanswered",
	}, {
		Command: []string{"mail", "settings", "autoreply", "enable"},
		Why:     "it would start answering real mail on somebody's behalf",
	}, {
		Command: []string{"mail", "settings", "autoreply", "set"},
		Why: "Proton keeps the last auto-reply message even while it is off and offers no way to clear it," +
			" so writing one cannot be undone - `set --message \"\"` is refused",
	}, {
		Command: []string{"mail", "settings", "domains", "delete"}, OnlyOurs: true,
		Why: "it takes down every address on the domain, and the account's own domain carries real mail" +
			" - name a domain the run made, carrying " + fixture.TestPrefix,
	}, {
		Command: []string{"mail", "settings", "filters", "apply"},
		Why:     "it runs a filter over the whole mailbox, and where real mail was filed from is not recorded",
	}, {
		Command: []string{"mail", "settings", "set"}, Value: "auto-delete-spam-trash",
		Why: "turning it on has Proton delete real mail that has been in spam or trash for thirty days",
	}, {
		Command: []string{"pass", "import"},
		Why:     "it adds a copy of everything an archive holds, and finding the copies again means a filter over real data",
	}, {
		Command: []string{"pass", "settings", "access-tokens", "delete"}, OnlyOurs: true,
		Why: "it cuts off whatever program holds the token, and the account's own tokens are somebody's" +
			" automation - name a token the run made, carrying " + fixture.TestPrefix,
	}, {
		Command: []string{"pass", "settings", "domains", "delete"}, OnlyOurs: true,
		Why: "it deletes every alias on the domain, and an alias address cannot be brought back" +
			" - name a domain the run made, carrying " + fixture.TestPrefix,
	}, {
		Command: []string{"pass", "settings", "extra-password", "disable"},
		Why: "it signs the owner out of Proton on every device they have, and only they can sign" +
			" back in",
	}, {
		Command: []string{"pass", "settings", "extra-password", "enable"},
		Why: "it puts a password only the run knows in front of somebody's Pass, and signs their" +
			" other devices out of it",
	}}
}

// FixtureOnly are the commands only the fixture may run on the paid account.
//
// These are not refused - the fixture has to be able to bring itself about - so
// they are checked where the rule can actually be evaluated: the subject is a
// test rather than a command line, and what a test contains is a property of the
// source. tests/rules reads the suite and fails on one that reaches for these.
//
// A fixture is made once and kept, which is what makes the difference: minting
// one address for the life of the account is a cost worth paying for a feature
// that is otherwise untestable, and minting one per run is not.
func FixtureOnly() []Restriction {
	return []Restriction{{
		Command: []string{"pass", "aliases", "create"},
		Why: "an alias address cannot be un-minted, so a test that made its own would spend" +
			" one of somebody's on every run. fixture.PaidAlias is made once and kept",
	}, {
		Command: []string{"mail", "settings", "addresses", "create"},
		Why: "an address cannot be given back - Proton allows one deletion a year - so a test" +
			" that made its own would spend that allowance. fixture.PaidForwarder is made" +
			" once and kept",
	}}
}

// OffLimits says why a command may not be run as the paid account, or "".
//
// A restriction matches the command wherever it appears in the arguments, so
// the flags a caller puts around it are not a way around it.
func OffLimits(args []string) string {
	for _, r := range Restrictions() {
		at := argv.At(args, r.Command...)
		if at < 0 {
			continue
		}
		if r.Value != "" && !slices.Contains(args, r.Value) {
			continue
		}
		if r.OnlyOurs && aimedAtOurs(args[at+len(r.Command):]) {
			continue
		}
		return r.Why
	}
	return ""
}

// aimedAtOurs reports whether everything the command was pointed at is
// something the suite made.
//
// A flag is not a target, so it is passed over; everything else has to carry the
// prefix, and a command pointed at nothing at all is pointed at whatever the
// reference happens to match. The reading is deliberately crude: a flag's value
// counts as a target and would be refused, which is the direction to be wrong
// in.
func aimedAtOurs(rest []string) bool {
	targets := 0
	for _, arg := range rest {
		if arg == "--" || strings.HasPrefix(arg, "-") {
			continue
		}
		if !strings.Contains(arg, fixture.TestPrefix) {
			return false
		}
		targets++
	}
	return targets > 0
}

// Notices are the subjects Proton writes to the account about, unprompted, when
// a test shares something and the other side answers.
//
// Nothing on this end turns them off, so a run sweeps its own: mail from one of
// NoticeSenders carrying one of these subjects that arrived after the run began
// is moved to the trash, never deleted, because it is real mail. Adding a test
// that makes Proton write to the account means adding its subject here.
func Notices() []string {
	return []string{
		"has accepted your invitation",
		"has declined your invitation",
		"shared a vault with you",
		"shared a calendar with you",
		"Forwarding active",
		"Forwarding request declined",
	}
}

// NoticeSenders are the addresses Proton writes those from.
//
// There is more than one: sharing writes from the account's own no-reply
// address, and forwarding writes from Mail's. A sweep that knew only the first
// left the second sitting in somebody's inbox, which the photograph then
// reported as something the run had left behind.
func NoticeSenders() []string {
	return []string{"no-reply@proton.me", "no-reply@mail.proton.me"}
}

// A Photograph is what the account held, as one line per thing, by collection.
type Photograph map[string][]string

// Compare names everything that moved between two photographs, worst-read
// first: what was left behind, then what went missing.
//
// An empty result is the whole point. A non-empty one is a run that did not
// come back, and every line of it is something on somebody's real account.
func (before Photograph) Compare(after Photograph) []string {
	var problems []string
	for _, key := range keys(before, after) {
		gone, added := Difference(before[key], after[key])
		for _, v := range added {
			problems = append(problems, fmt.Sprintf("  %s: left behind  %s", key, v))
		}
		for _, v := range gone {
			problems = append(problems, fmt.Sprintf("  %s: missing      %s", key, v))
		}
	}
	sort.Strings(problems)
	return problems
}

// Difference reports what is in before and not after, and the other way round.
//
// It counts rather than sets, so two of a thing where there was one is noticed.
func Difference(before, after []string) (gone, added []string) {
	had := map[string]int{}
	for _, v := range before {
		had[v]++
	}
	for _, v := range after {
		if had[v] > 0 {
			had[v]--
			continue
		}
		added = append(added, v)
	}
	for v, n := range had {
		for range n {
			gone = append(gone, v)
		}
	}
	sort.Strings(gone)
	sort.Strings(added)
	return gone, added
}

func keys(a, b Photograph) []string {
	seen := map[string]bool{}
	for k := range a {
		seen[k] = true
	}
	for k := range b {
		seen[k] = true
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
