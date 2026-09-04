package live

import (
	"strings"
	"testing"
)

// Contact groups, which a plan gates.
//
// Proton groups addresses rather than people, so both ways of saying who is in
// one are covered: the whole contact, and one of their addresses.

func TestContactsGroups(t *testing.T) {
	gname := testID() + "-group"
	group := strings.TrimSpace(runOKPaid(t, "contacts", "groups", "create",
		"--name", gname, "--color", "#8080FF"))
	cleanupRunPaid(t, "Delete group: proton contacts groups delete "+group,
		"contacts", "groups", "delete", group)

	// A contact of this test's own, with two addresses, so the difference
	// between naming a person and naming an address is visible.
	work := testID() + "-work@example.com"
	home := testID() + "-home@example.com"
	cname := testID() + "-member"
	contact := strings.TrimSpace(runOKPaid(t, "contacts", "create", "--name", cname,
		"--email", "work:"+work, "--email", "home:"+home))
	cleanupRunPaid(t, "Delete contact: proton contacts delete "+contact,
		"contacts", "delete", contact)

	assertContains(t, runOKPaid(t, "contacts", "groups", "list"), gname)

	// Naming the contact takes all of their addresses in.
	runOKPaid(t, "contacts", "groups", "add", group, contact)
	if n := groupSize(t, group); n != 2 {
		t.Errorf("after adding the contact the group holds %d addresses, want 2", n)
	}
	runOKPaid(t, "contacts", "groups", "remove", group, contact)
	if n := groupSize(t, group); n != 0 {
		t.Errorf("after removing the contact the group holds %d addresses, want 0", n)
	}

	// --email narrows it to the ones named, which is the whole reason Proton
	// groups addresses rather than people.
	runOKPaid(t, "contacts", "groups", "add", group, contact, "--email", work)
	if n := groupSize(t, group); n != 1 {
		t.Errorf("after adding one address the group holds %d, want 1", n)
	}
	runOKPaid(t, "contacts", "groups", "remove", group, contact, "--email", work)
	if n := groupSize(t, group); n != 0 {
		t.Errorf("after removing that address the group holds %d, want 0", n)
	}

	// Renaming is the last thing a group does that nothing had tried.
	renamed := gname + "-renamed"
	runOKPaid(t, "contacts", "groups", "update", group, "--name", renamed)
	assertContains(t, runOKPaid(t, "contacts", "groups", "list"), renamed)
}

// groupSize is how many addresses a group holds. Membership lives on the
// address, so this is the group's own record rather than the listing.
func groupSize(t *testing.T, group string) int {
	t.Helper()
	shown := runJSONPaid(t, "contacts", "groups", "get", group)
	members, _ := shown["members"].([]interface{})
	return len(members)
}
