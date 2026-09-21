package live

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/tests/account"
	"github.com/roman-16/proton-cli/tests/fixture"
	"github.com/roman-16/proton-cli/tests/paid"
)

// The paid account is photographed before the run and compared after it.
//
// The restrictions in tests/paid are promises - a command refused, a fixture
// never made, a test acting only on what it made. This is what checks them, and
// it is the one that catches a mistake nobody predicted. Anything left behind or
// missing fails the run and is named.

// before is the account as it was when the run started.
var before paid.Photograph

// runStarted is when the run began, so the sweep can tell mail this run caused
// from mail that was already there.
var runStarted int64

// noticesCaused counts the notices a run told Proton to send, so the sweep knows
// how many to wait for. runAs increments it.
var noticesCaused atomic.Int64

// collections are what the photograph counts, each read with a command that only
// reads. Between them they cover everything a test here could plausibly leave
// behind.
//
// A collection nobody can list is not covered, which is the honest limit of
// this: it says what it saw, and a test that changes something invisible to
// every listing has to be argued for on its own.
var collections = []struct {
	name string
	args []string
	id   string
	// fields are what a row is read by in the report, so what turned up is
	// readable rather than an identifier nobody can place - and, where a row
	// carries a state a run could change, what says the state came back too.
	fields []string
}{
	// The newest of the inbox, because a run can leave mail behind: sharing
	// something makes Proton write to you when the other side answers. Only the
	// newest are photographed - the rest is somebody's real mail and none of the
	// suite's business.
	{"inbox", []string{"mail", "messages", "list", "--folder", "inbox", "--limit", "25"}, "id", []string{"subject"}},
	{"calendars", []string{"calendar", "settings", "calendars", "list"}, "id", []string{"name"}},
	{"vaults", []string{"pass", "vaults", "list"}, "share_id", []string{"name"}},
	{"pass items", []string{"pass", "items", "list"}, "item_id", []string{"name"}},
	// The one listing whose rows carry a state a test can change, so the state is
	// compared beside the address: a run that left Proton no longer watching one
	// is exactly as much of a residue as one that left an address behind.
	{"watched addresses", []string{"pass", "breaches", "list"}, "address_id", []string{"email", "state"}},
	{"labels", []string{"mail", "settings", "labels", "list"}, "id", []string{"name"}},
	{"folders", []string{"mail", "settings", "folders", "list"}, "id", []string{"name"}},
	{"filters", []string{"mail", "settings", "filters", "list"}, "id", []string{"name"}},
	{"addresses", []string{"mail", "settings", "addresses", "list"}, "id", []string{"name"}},
	// The catch-all is compared beside the domain for the reason a watched
	// address's state is: a run that left stray mail arriving somewhere else has
	// changed where somebody's real mail goes, and the domain is still there.
	{"domains", []string{"mail", "settings", "domains", "list"}, "id", []string{"domain", "catch_all"}},
	{"forwardings", []string{"mail", "settings", "forwarding", "list"}, "id", []string{"to"}},
	{"smtp tokens", []string{"mail", "settings", "smtp-tokens", "list"}, "id", []string{"name"}},
	{"contacts", []string{"contacts", "list"}, "id", []string{"name"}},
	{"contact groups", []string{"contacts", "groups", "list"}, "id", []string{"name"}},
	{"alias mailboxes", []string{"pass", "settings", "mailboxes", "list"}, "id", []string{"email"}},
	// Which domain is the default is compared beside the domain for the reason a
	// watched address's state is: a run that left new aliases being made on
	// another domain has changed something real, and the domains are all still
	// there.
	{"alias domains", []string{"pass", "settings", "domains", "list"}, "domain", []string{"default", "status"}},
	{"alias contacts", []string{"pass", "aliases", "contacts", "list", fixture.PaidAlias}, "id", []string{"email"}},
	{"access tokens", []string{"pass", "settings", "access-tokens", "list"}, "id", []string{"name"}},
	{"secure links", []string{"pass", "links", "list"}, "link_id", []string{"item_id"}},
	{"drive root", []string{"drive", "items", "list", "/"}, "id", []string{"name"}},
}

// settingsPages are the values a run must leave exactly as it found them,
// compared whole so a single key changed anywhere shows up. The auto-reply is
// among them: a test may arm one, and this is what proves it was disarmed.
var settingsPages = [][]string{
	{"account", "settings", "get"},
	{"calendar", "settings", "get"},
	{"drive", "settings", "get"},
	{"mail", "settings", "autoreply", "get"},
	{"mail", "settings", "get"},
	{"pass", "settings", "extra-password", "get"},
}

// photographPaid records the account before any test runs.
func photographPaid() {
	runStarted = time.Now().Unix()
	before = takePhotograph()
	if before == nil {
		fmt.Fprintln(os.Stderr,
			"could not photograph the paid account, so a run could not be checked afterwards")
		os.Exit(1)
	}
}

// paidCameBack photographs it again and reports whether nothing moved.
func paidCameBack() bool {
	sweepNotices()
	after := takePhotograph()
	if after == nil {
		fmt.Fprintln(os.Stderr,
			"\ncould not photograph the paid account after the run; check it by hand")
		return false
	}
	problems := before.Compare(after)
	if len(problems) == 0 {
		return true
	}
	fmt.Fprintf(os.Stderr, `
╔══════════════════════════════════════════════════════════════╗
║  ⚠️  THE PAID ACCOUNT DID NOT COME BACK AS IT WAS            ║
╠══════════════════════════════════════════════════════════════╣
%s
╚══════════════════════════════════════════════════════════════╝
`, strings.Join(problems, "\n"))
	return false
}

// takePhotograph reads every collection and settings page, or nil if it could
// not read one: a partial photograph would report a change that never happened.
func takePhotograph() paid.Photograph {
	out := paid.Photograph{}
	for _, c := range collections {
		rows, ok := readRows(c.args, c.id, c.fields)
		if !ok {
			fmt.Fprintf(os.Stderr, "could not read %s from the paid account\n", c.name)
			return nil
		}
		out[c.name] = rows
	}
	for _, page := range settingsPages {
		body, _, code, err := runAs(account.Paid, nil, asJSON(page)...)
		if err != nil || code != 0 {
			fmt.Fprintf(os.Stderr, "could not read %v from the paid account\n", page)
			return nil
		}
		out[strings.Join(page, " ")] = []string{strings.Join(strings.Fields(body), " ")}
	}
	return out
}

// readRows lists a collection and returns one line per row.
//
// A field is a word or a yes-or-no; a number is a count Proton keeps and not a
// state a run could have changed.
func readRows(args []string, idKey string, fields []string) ([]string, bool) {
	body, _, code, err := runAs(account.Paid, nil, asJSON(args)...)
	if err != nil || code != 0 {
		return nil, false
	}
	rows, ok := rowsOf(body)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		m, _ := row.(map[string]interface{})
		id, _ := m[idKey].(string)
		if id == "" {
			id, _ = m["id"].(string)
		}
		line := make([]string, 0, len(fields)+1)
		for _, f := range fields {
			switch v := m[f].(type) {
			case string:
				if v != "" {
					line = append(line, v)
				}
			case bool:
				line = append(line, f+"="+strconv.FormatBool(v))
			}
		}
		out = append(out, strings.TrimSpace(strings.Join(append(line, id), " ")))
	}
	// Sorted, because a listing's order is Proton's to choose and two runs have
	// to be comparable.
	sort.Strings(out)
	return out, true
}

// sweepNotices trashes the mail this run caused Proton to send.
//
// Sharing something makes Proton write to the owner when the other side answers,
// and accepting a forwarding does too. No setting on this end turns that off, so
// a run clears its own: mail from
// one of the declared senders carrying one of the declared subjects that arrived
// after the run began, moved to the trash rather than deleted, because it is
// real mail.
//
// It runs before the photograph, so what it clears is not then reported as
// something the run left behind - and anything it fails to clear stays, and the
// photograph names it.
//
// Proton writes a moment after the other side answers rather than as it answers,
// so this waits for as many notices as the run caused and stops as soon as it has
// them. A run that caused none sweeps once and returns.
func sweepNotices() {
	want := int(noticesCaused.Load())
	var swept int
	waitFor(90*time.Second, 5*time.Second, func() bool {
		swept += sweepOnce()
		return swept >= want
	})
	if swept < want {
		fmt.Fprintf(os.Stderr,
			"Proton was told to write %d notices and %d arrived in time to be cleared\n", want, swept)
	}
}

// sweepOnce trashes the notices that have arrived so far, and says how many.
func sweepOnce() int {
	var cleared int
	for _, sender := range paid.NoticeSenders() {
		cleared += sweepFrom(sender)
	}
	return cleared
}

func sweepFrom(sender string) int {
	body, _, code, err := runAs(account.Paid, nil, asJSON([]string{
		"mail", "messages", "list", "--folder", "all",
		"--from", sender, "--limit", "50",
	})...)
	if err != nil || code != 0 {
		return 0
	}
	rows, ok := rowsOf(body)
	if !ok {
		return 0
	}
	var cleared int
	for _, row := range rows {
		m, _ := row.(map[string]interface{})
		at, _ := m["time"].(float64)
		if int64(at) < runStarted {
			continue
		}
		subject, _ := m["subject"].(string)
		var ours bool
		for _, notice := range paid.Notices() {
			if strings.Contains(subject, notice) {
				ours = true
			}
		}
		if !ours {
			continue
		}
		id, _ := m["id"].(string)
		if id == "" {
			continue
		}
		if _, _, code, err := runAs(account.Paid, nil, "--yes", "mail", "messages", "trash", "--", id); err != nil || code != 0 {
			fmt.Fprintf(os.Stderr, "could not clear the notice %q Proton sent; it is in the inbox\n", subject)
			continue
		}
		cleared++
	}
	return cleared
}

// The canary has to have actually photographed something.
//
// A photograph that came back empty would compare equal to another empty one
// and pass every run while checking nothing, which is the failure mode of every
// guard that reads the world rather than the code.
func TestThePaidAccountWasPhotographed(t *testing.T) {
	if len(before) == 0 {
		t.Fatal("nothing was photographed before the run")
	}
	for _, c := range collections {
		if _, ok := before[c.name]; !ok {
			t.Errorf("%s was not photographed", c.name)
		}
	}
	for _, page := range settingsPages {
		key := strings.Join(page, " ")
		if v := before[key]; len(v) == 0 || v[0] == "" {
			t.Errorf("%s was not photographed", key)
		}
	}
}
