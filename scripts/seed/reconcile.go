package main

import (
	"fmt"
	"sync"

	"github.com/roman-16/proton-cli/tests/fixture"
)

// sweep removes what an interrupted suite run left behind.
//
// The suite clears up after itself; a run that was killed cannot, and what it
// leaves is indistinguishable from the account's own contents to everything
// except this prefix. It accumulates for as long as the accounts exist, it puts
// rows the fixture never declared in front of every list, and the README panel
// photographs whatever is there.
//
// A recurring event is listed once per occurrence and removed as a series, so a
// reference already swept is not swept again.
func (r *report) sweep(profile string, c fixture.Collection, list []map[string]any) {
	for _, name := range fixture.Sweep(seedRunner, profile, c, list) {
		r.note("-", profile, fmt.Sprintf("%s: %s", c.What, name))
	}
}

// reconcile brings one collection to the state the fixture declares.
//
// A row that is absent is made. A row that is present but disagrees with the
// fixture is removed and made again, because a half-right fixture is worse than
// a missing one: it passes a presence check and then fails an assertion
// somewhere far away. Rows the fixture says nothing about are left alone, unless
// they are the suite's own leftovers, which sweep takes.
func (r *report) reconcile(profile string, c fixture.Collection) {
	list, err := fixture.Rows(seedRunner, profile, c.List...)
	if err != nil {
		r.fail(c.What, err)
		return
	}
	r.sweep(profile, c, list)
	for _, p := range c.Pins {
		if _, err := fixture.Ensure(seedRunner, profile, c, p, list); err != nil {
			r.fail(fmt.Sprintf("%s: %s", c.What, p.ID), err)
			continue
		}
		if _, found := fixture.Find(list, c.Key, p.ID); !found {
			r.note("+", profile, fmt.Sprintf("%s: %s", c.What, p.ID))
		}
	}
}

// report is what the run did, so a seed that changed nothing can say so.
//
// The two accounts are seeded at the same time, so every line names the account
// it belongs to and the tally is guarded.
type report struct {
	mu       sync.Mutex
	made     int
	remade   int
	swept    int
	failures []string
}

func (r *report) note(mark, profile, what string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fmt.Printf("  %s %s: %s\n", mark, profile, what)
	switch mark {
	case "+":
		r.made++
	case "~":
		r.remade++
	case "-":
		r.swept++
	}
}

func (r *report) make(profile, what string, args []string) {
	if _, err := run(profile, args...); err != nil {
		r.fail(what, err)
		return
	}
	r.note("+", profile, what)
}

func (r *report) remake(profile, what string, args []string) {
	if _, err := run(profile, args...); err != nil {
		r.fail(what, err)
		return
	}
	r.note("~", profile, what)
}

func (r *report) fail(what string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fmt.Printf("  ! %s: %v\n", what, err)
	r.failures = append(r.failures, what)
}
