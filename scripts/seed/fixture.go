package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/tests/fixture"
)

// The fixture. Both accounts hold the same of it, so a test can act as either
// and the README panel is a photograph of it.
//
// The names read like somebody's account rather than like fixtures for that
// second reason. Nothing here uses the `proton-cli-test-` prefix, which belongs
// to the artifacts the suite makes and clears up itself.

// mail brings the panel's three messages to the inbox.
//
// Mail is not a collection like the others: it is sent rather than created, it
// arrives a few seconds later, and what matters is which folder it lands in. A
// message the fixture wants but the inbox lacks is sent; one that is there is
// left where it is, read or not, because the suite marks messages as part of its
// own work.
func inbox() []string { return []string{"--folder", "inbox"} }

func (r *report) mail(profile, address, work string) {
	for _, m := range fixture.Panel() {
		r.deliver(profile, address, work, fixture.Mail{Subject: m.Subject, Body: m.Body, Attach: m.Attach})
	}
	// What the suite reads. Kept here rather than sent by the suite itself, so a
	// run spends its sending allowance on the send path it is testing.
	for _, m := range fixture.AllMail() {
		r.deliver(profile, address, work, m)
	}
}

// deliver sends one message unless the inbox already holds it.
func (r *report) deliver(profile, address, work string, m fixture.Mail) {
	what := "mail: " + m.Subject
	found, err := fixture.Rows(seedRunner, profile, append([]string{"mail", "messages", "list",
		"--subject", m.Subject, "--page-size", "1"}, inbox()...)...)
	if err != nil {
		r.fail(what, err)
		return
	}
	if len(found) > 0 {
		return
	}
	send := []string{"mail", "messages", "send", "--to", address, "--subject", m.Subject, "--body", m.Body}
	if m.HTML {
		send = append(send, "--html")
	}
	if m.Attach != "" {
		send = append(send, "--attach", filepath.Join(work, m.Attach))
	}
	if m.Inline != "" {
		send = append(send, "--attach-inline", filepath.Join(work, m.Inline))
	}
	r.make(profile, what, send)
}

// calendarName is what the suite addresses the account's calendar by.
const calendarName = "Default"

// calendars is the collection a leftover test calendar is swept from.
//
// It pins nothing - the account arrives with its own calendar and the fixture
// renames that one - but it has to be swept like everything else, and more
// urgently: a free plan allows three, so a couple of calendars an interrupted run
// left behind is the difference between the suite working and every test that
// makes one failing on a limit it has nothing to do with.
var calendars = fixture.Collection{
	What:   "calendar",
	List:   []string{"calendar", "settings", "calendars", "list"},
	Key:    "name",
	IDKeys: []string{"id"},
	Remove: []string{"calendar", "settings", "calendars", "delete"},
}

// calendar makes the account's calendar answer to that name.
//
// An account arrives with one whose name varies - "My calendar" on these - and a
// free plan allows few enough calendars that adding one would take a slot the
// suite needs to create and delete its own. Renaming the one already there costs
// nothing.
func (r *report) calendar(profile string) {
	list, err := fixture.Rows(seedRunner, profile, calendars.List...)
	if err != nil {
		r.fail("calendar: "+calendarName, err)
		return
	}
	// Swept first, so what is left to be named is the account's own calendar
	// rather than something a run left behind.
	r.sweep(profile, calendars, list)
	var kept []map[string]any
	for _, row := range list {
		if !strings.HasPrefix(fixture.Str(row["name"]), fixture.TestPrefix) {
			kept = append(kept, row)
		}
	}
	if _, ok := fixture.Find(kept, "name", calendarName); ok {
		return
	}
	if len(kept) == 0 {
		r.fail("calendar: "+calendarName, fmt.Errorf("the account has no calendar to name"))
		return
	}
	r.remake(profile, "calendar: "+calendarName, []string{"calendar", "settings", "calendars",
		"update", fixture.Str(kept[0]["id"]), "--name", calendarName})
}

// photoCount is how many photos the library has to hold.
const photoCount = 3

// photos tops the library up.
//
// A photo has no name - a row is a link ID, a capture time and two hashes - so
// the fixture pins how many there are rather than which they are. That is also
// what the tests want: they upload their own and diff the library around it,
// and a library with nothing in it is the one shape that tells them nothing.
func (r *report) photos(profile, work string) {
	list, err := fixture.Rows(seedRunner, profile, "drive", "photos", "list")
	if err != nil {
		r.fail("photo", err)
		return
	}
	for i := len(list); i < photoCount; i++ {
		r.make(profile, fmt.Sprintf("photo: %d of %d", i+1, photoCount),
			[]string{"drive", "photos", "upload", filepath.Join(work, fmt.Sprintf("photo-%d.png", i+1))})
	}
}

// empty clears the three trashes.
//
// It runs last, so that what repair removed goes with it: a wrong label is
// deleted, a wrong file is trashed, and the panel's mail is re-sent over the
// top of trashed copies. Left alone all of that accumulates for as long as the
// accounts exist.
//
// Nothing here is recoverable afterwards, which is the point of a trash being
// empty, and is why it is the accounts kept for this that it runs against.
func (r *report) empty(profile string) {
	for _, e := range []struct {
		what string
		args []string
	}{
		{"drive trash", []string{"drive", "trash", "empty"}},
		{"pass trash", []string{"pass", "trash", "empty"}},
		{"mail trash", []string{"mail", "messages", "delete", "--folder", "trash", "--all"}},
	} {
		if _, err := run(profile, append([]string{"--yes"}, e.args...)...); err != nil {
			r.fail("empty "+e.what, err)
		}
	}
}

// stage makes the panel's three messages the only unread mail in the inbox.
//
// The panel opens on `mail messages list --unread`, and an inbox holds whatever
// Proton has sent it - share notifications the suite provoked, and Proton's own
// marketing. Neither can be kept out, so the recording is made deterministic
// from the other side: everything is marked read, the three are sent again, and
// they are the only unread mail there is.
func (r *report) stage(profile, address, work string) {
	if _, err := run(profile, append([]string{"--yes", "mail", "messages", "mark", "read", "--all"}, inbox()...)...); err != nil {
		r.fail("stage: mark the inbox read", err)
		return
	}
	for _, m := range fixture.Panel() {
		if _, err := run(profile, "--yes", "mail", "messages", "trash", "--subject", m.Subject, "--limit", "20"); err != nil {
			r.fail("stage: clear "+m.Subject, err)
			return
		}
	}
	// Sent oldest first, because an inbox lists newest first and the panel should
	// read in the order the fixture declares.
	panel := fixture.Panel()
	for i := len(panel) - 1; i >= 0; i-- {
		m := panel[i]
		send := []string{"mail", "messages", "send", "--to", address, "--subject", m.Subject, "--body", m.Body}
		if m.Attach != "" {
			send = append(send, "--attach", filepath.Join(work, m.Attach))
		}
		if _, err := run(profile, send...); err != nil {
			r.fail("stage: send "+m.Subject, err)
			return
		}
		r.note("+", profile, "mail: "+m.Subject)
	}
	r.await(profile)
}

// await waits for the staged mail to arrive. Delivery runs a few seconds behind
// the send, and a recording follows.
func (r *report) await(profile string) {
	deadline := time.Now().Add(2 * time.Minute)
	for _, m := range fixture.Panel() {
		for {
			found, err := fixture.Rows(seedRunner, profile, append([]string{"mail", "messages", "list",
				"--subject", m.Subject, "--page-size", "1"}, inbox()...)...)
			if err == nil && len(found) > 0 {
				break
			}
			if time.Now().After(deadline) {
				r.fail("stage: waiting for "+m.Subject, fmt.Errorf("did not arrive"))
				break
			}
			time.Sleep(2 * time.Second)
		}
	}
}
