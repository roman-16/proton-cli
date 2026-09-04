// Package fixture declares what the test accounts hold for the suite to read.
//
// Most tests need *a* real, delivered, encrypted thing of a particular shape
// rather than a freshly made one. Making those shapes on every run costs two
// things: a delivery to wait for, and - for mail - a message from a sending
// allowance that a free plan caps at fifty an hour. So they live on the account
// instead, and the accessor a test calls finds one, makes it if the account has
// not got it, and remembers it for the rest of the run.
//
// Declaring them here rather than in either place that acts on them is what
// keeps the two in step: `scripts/seed` fills an account by hand and the suite
// fills it as it goes, and both read this.
//
// A test that MUTATES its fixture, or that exercises the making of one, must not
// read a shared one - it makes its own.
package fixture

// Mail is one message the suite expects to find in the inbox.
type Mail struct {
	// Subject identifies it. It reads like somebody's mail rather than like a
	// fixture, for the same reason the rest of the seed does, and it never
	// carries the TestPrefix, which belongs to what the suite makes and clears
	// up itself.
	Subject string
	// Body is sent verbatim.
	Body string
	// HTML sends the body as HTML, which an inline image needs.
	HTML bool
	// Attach and Inline name files in Files.
	Attach string
	Inline string
}

// Plain is a message with nothing special about it: no attachments, and a body
// carrying no quote markers, so stripping quotes from it must change nothing.
//
// Its body contains its subject, which is what lets a test tell a body it can
// read from a body it only thinks it can read.
var Plain = Mail{
	Subject: "Notes from the reading group",
	Body: "Notes from the reading group are below.\n\n" +
		"We finished the second chapter and agreed to meet on Thursday.\n",
}

// Quoted carries the canonical reply block, which is the thing --strip-quotes
// removes. The wording is the fixture's whole purpose, so it is spelled out
// rather than generated.
var Quoted = Mail{
	Subject: "Re: the second chapter",
	Body: "My new note for the reading group.\n\n" +
		"On Tue, 24 Sep 2024, Sender <a@b.com> wrote:\n\n" +
		"> ancient quoted text\n" +
		"> that should disappear\n",
}

// Attachments carries one regular attachment and one inline image, so both the
// attachment tests and the ones about telling the two dispositions apart have a
// real message to read. One message answers both, because a mail with an inline
// image and an attachment is one shape rather than two.
var Attachments = Mail{
	Subject: "Trail photos and the packing list",
	Body:    "<p>Both are attached.</p>",
	HTML:    true,
	Attach:  "packing-list.txt",
	Inline:  "inline-image.png",
}

// Mutable are messages a test may change and change back: marked unread,
// starred, moved, trashed and restored. The change is the subject of those
// tests, not the sending, so they take one of these instead of sending their
// own.
//
// There are several because each test needs a message no other test is
// changing; they are handed out one at a time. They read like ordinary mail for
// the same reason the rest does.
var Mutable = []Mail{
	{Subject: "Library books are due on Friday", Body: "Two of them can be renewed online.\n"},
	{Subject: "Your parcel is on its way", Body: "It should arrive between nine and noon.\n"},
	{Subject: "Team lunch next Tuesday", Body: "The place on the corner, half past twelve.\n"},
	{Subject: "Water meter reading", Body: "The reading for this quarter is recorded.\n"},
}

// AllMail is every message the suite expects, for the seed to reconcile.
func AllMail() []Mail { return append([]Mail{Plain, Quoted, Attachments}, Mutable...) }

// AliasName is the Pass alias on the free accounts that the suite reads rather
// than makes.
//
// Making an alias is what Proton meters hardest here - a handful an hour,
// against several tests that each want one - so only the test about making an
// alias makes its own. The address behind this is Proton's to choose and differs
// per account, which is why the name is what identifies it.
const AliasName = "Newsletters"

// PaidAlias is the Pass alias on the paid account: made once, kept for good.
//
// Alias contacts need a subscription, so the only account that can test them is
// somebody's real one - and an alias address cannot be un-minted, so a test that
// made its own would spend one of theirs on every run. This is made by the first
// run that needs it and then never removed, so the cost is one address for the
// life of the account rather than one an hour. Every run hangs contacts off it
// and takes them away again; contacts are reversible, the address is not.
//
// The name is deliberately outside TestPrefix, because Sweep deletes everything
// carrying that prefix and this is the one fixture that must survive - and it
// says what it is, because it is sitting in somebody's real password manager.
const PaidAlias = "proton-cli fixture"

// PaidAliasPrefix is the front of the address Proton mints for it. Proton
// appends a word of its own, so the address is not predictable and the prefix is
// only ever used once.
const PaidAliasPrefix = "protoncli"

// TestPrefix is the namespace the suite makes its own artifacts under.
//
// The suite clears up after itself; a run that was killed cannot, and what it
// leaves is indistinguishable from the account's own contents to everything
// except this prefix.
const TestPrefix = "proton-cli-test-"
