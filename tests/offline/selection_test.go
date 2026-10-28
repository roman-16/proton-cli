package offline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestASelectionThatNamesNothingIsRefused(t *testing.T) {
	for _, args := range [][]string{
		{"mail", "messages", "trash"},
		{"mail", "messages", "delete"},
		{"mail", "conversations", "trash"},
		{"mail", "messages", "unschedule"},
		{"drive", "items", "delete"},
		{"pass", "items", "trash"},
		{"account", "sessions", "revoke"},
	} {
		refuses(t, 1, args, "Nothing selected")
	}
}

func TestACommandThatDecryptsStillJudgesItsArgumentsFirst(t *testing.T) {
	for _, tt := range []struct {
		args   []string
		phrase string
	}{
		{[]string{"pass", "items", "create"}, "An item needs a name"},
		{[]string{"pass", "aliases", "create"}, "An alias needs a prefix"},
		{[]string{"contacts", "create"}, "A contact needs at least a name or an email address"},
		{[]string{"calendar", "events", "create", "--calendar", "Work"}, "An event needs a title and a start"},
	} {
		refuses(t, 1, tt.args, tt.phrase)
	}
}

func TestAnAddressToAddHasToBeAnAddress(t *testing.T) {
	for _, bad := range []string{"work", "work@", "@example.com", "work@a@b"} {
		refuses(t, 1, []string{"mail", "settings", "addresses", "create", bad},
			"not an email address", "work@example.com")
	}
}

func TestADomainToAddHasToBeADomain(t *testing.T) {
	for _, bad := range []string{"example", "me@example.com", "https://example.com", "example.com/mail"} {
		refuses(t, 1, []string{"mail", "settings", "domains", "create", bad},
			"is not a domain name", "example.com")
		refuses(t, 1, []string{"pass", "settings", "domains", "create", bad},
			"is not a domain name", "example.com")
	}
}

func TestUpdatingADomainWithNothingToChangeIsRefused(t *testing.T) {
	refuses(t, 1, []string{"mail", "settings", "domains", "update", "example.com"},
		"Nothing to change", "--catch-all none")
	refuses(t, 1, []string{"pass", "settings", "domains", "update", "example.com"},
		"Nothing to change", "--catch-all", "--default")
}

func TestADomainsDisplayNameIsSetOrClearedButNotBoth(t *testing.T) {
	refuses(t, 1, []string{"pass", "settings", "domains", "update", "example.com", "--display-name", ""},
		"--display-name needs a name", "--clear-display-name")
	refuses(t, 1, []string{"pass", "settings", "domains", "update", "example.com",
		"--display-name", "Jane", "--clear-display-name"}, "contradict")
}

func TestAnAccessTokenIsDescribedWholeBeforeItIsMade(t *testing.T) {
	create := func(rest ...string) []string {
		return append([]string{"pass", "settings", "access-tokens", "create"}, rest...)
	}
	for _, tt := range []struct {
		args    []string
		phrases []string
	}{
		{create("--expires", "30d", "--vault", "Work"), []string{"A token needs a name", "--name"}},
		{create("--name", "ci", "--vault", "Work"), []string{"How long should the token work?", "--expires 30d"}},
		{create("--name", "ci", "--expires", "never", "--vault", "Work"), []string{"always expires", "1y"}},
		{create("--name", "ci", "--expires", "10m", "--vault", "Work"), []string{"between 1h and 1y"}},
		{create("--name", "ci", "--expires", "2y", "--vault", "Work"), []string{"between 1h and 1y"}},
		{create("--name", "ci", "--expires", "30d"), []string{"at least one vault", "--vault"}},
		{[]string{"pass", "settings", "access-tokens", "update", "ci"}, []string{"Nothing to change", "--vault"}},
	} {
		refuses(t, 1, tt.args, tt.phrases...)
	}
}

func TestSendingNeedsSomethingToSend(t *testing.T) {
	refuses(t, 1, []string{"mail", "messages", "send"}, "required")
	refuses(t, 1, []string{"mail", "messages", "send", "--subject", "x", "--body", "y"},
		"At least one recipient is required", "--to, --cc or --bcc")
}

func TestOneDestinationForSeveralItemsIsRefused(t *testing.T) {
	for _, args := range [][]string{
		{"mail", "messages", "attachments", "download", "--dest", "-", "some-message"},
		{"mail", "messages", "attachments", "download", "--dest", "/tmp/one.bin", "some-message"},
	} {
		refuses(t, 1, args, "--dest-dir")
	}
}

func TestContradictoryFlagsAreRefused(t *testing.T) {
	refuses(t, 1, []string{"account", "sessions", "revoke", "--others", "some-uid"}, "--others")
}

func TestFlagsThatContradictAreRefusedInOneSentence(t *testing.T) {
	for _, tt := range []struct {
		args   []string
		phrase string
	}{
		{[]string{"mail", "messages", "list", "--read", "--unread"}, "--read and --unread"},
		{[]string{"mail", "conversations", "trash", "--unread", "--read"}, "--read and --unread"},
		{[]string{"drive", "items", "list", "--computer", "Laptop", "--shared", "Photos"},
			"--computer and --shared"},
		{[]string{"mail", "settings", "filters", "create", "--name", "Receipts",
			"--if", "subject contains receipt", "--sieve", "-"}, "--if and --sieve"},
		{[]string{"mail", "settings", "addresses", "update", "me@proton.me",
			"--signature", "Best, Jane", "--clear-signature"}, "--signature and --clear-signature"},
		{[]string{"calendar", "events", "update", "some-event", "--remind", "15m", "--no-remind"},
			"--remind and --no-remind"},
		{[]string{"calendar", "events", "update", "--meet", "--no-meet", "some-event"},
			"--meet and --no-meet"},
		{[]string{"calendar", "settings", "calendars", "update", "Work", "--remind", "15m", "--no-remind"},
			"--remind and --no-remind"},
		{[]string{"calendar", "settings", "calendars", "update", "Work", "--remind-all-day", "1d", "--no-remind"},
			"--remind-all-day and --no-remind"},
	} {
		refuses(t, 1, tt.args, tt.phrase+" contradict each other.")
	}
}

func TestAReorderAsksForOneOrderOrTheOther(t *testing.T) {
	for _, noun := range []string{"folders", "labels"} {
		refuses(t, 1, []string{"mail", "settings", noun, "reorder", "--alphabetical", "Work"},
			"not both")
		refuses(t, 1, []string{"mail", "settings", noun, "reorder"},
			"Nothing to reorder", "--alphabetical")
	}
}

func TestAPageThatCannotExistIsRefused(t *testing.T) {
	for _, listing := range [][]string{
		{"contacts", "list"},
		{"drive", "items", "list"},
		{"mail", "messages", "list"},
		{"mail", "conversations", "list"},
		{"mail", "drafts", "list"},
		{"pass", "items", "list"},
	} {
		refuses(t, 1, append(listing, "--page", "-1"), "--page counts from zero")
		refuses(t, 1, append(listing, "--limit", "-5"), "--limit is a count")
		refuses(t, 1, append(listing, "--page", "2", "--limit", "0"),
			"--page 2 asks for a page of a listing --limit 0 does not cut into")
	}
}

func TestACapThatCannotExistIsRefused(t *testing.T) {
	refuses(t, 1, []string{"mail", "messages", "trash", "--unread", "--limit", "-1"},
		"--limit is a count; 0 lifts the cap")
}

func TestTheRawAPICommandJudgesItsOwnArguments(t *testing.T) {
	refuses(t, 1, []string{"api", "POST", "/core/v4/labels", "--body", "{not json}"},
		"not valid JSON")
	refuses(t, 1, []string{"api", "GET", "/core/v4/users", "--query", "Noequalssign"},
		"key=value")
}

func TestAnAutoReplyScheduleThatContradictsItselfIsRefused(t *testing.T) {
	for _, tt := range []struct {
		args    []string
		phrases []string
	}{
		{[]string{"--repeat", "permanent", "--start", "09:00", "--body", "x"}, []string{"takes no --start"}},
		{[]string{"--repeat", "daily", "--start", "09:00", "--end", "17:00", "--body", "x"}, []string{"needs --days"}},
		{[]string{"--repeat", "weekly", "--start", "mon:09:00", "--end", "fri:17:00", "--days", "mon", "--body", "x"},
			[]string{"--days applies to --repeat daily"}},
		{[]string{"--repeat", "hourly", "--body", "x"},
			[]string{"--repeat accepts: fixed, daily, weekly, monthly, permanent"}},
		{[]string{"--repeat", "permanent"}, []string{"A message is required"}},
	} {
		refuses(t, 1, append([]string{"mail", "settings", "autoreply", "set"}, tt.args...), tt.phrases...)
	}
}

func TestEmptyingAFolderNeedsToKnowWhichOne(t *testing.T) {
	refuses(t, 1, []string{"mail", "messages", "empty"}, "Which folder", "--folder trash")
}

func TestUpdatingAMessageNeedsAnInstruction(t *testing.T) {
	refuses(t, 1, []string{"mail", "messages", "update", "any-ref"},
		"Nothing to change", "--expires 7d")
	refuses(t, 1, []string{"mail", "conversations", "update", "any-ref"},
		"Nothing to change", "--expires 7d")
}

func TestASectionNeedsAnItemTypeThatHasOne(t *testing.T) {
	refuses(t, 1, []string{"pass", "items", "create", "--name", "x", "--field", "Recovery/Code=1"},
		"has no sections", "--type custom")
	refuses(t, 1, []string{"pass", "items", "create", "--type", "custom", "--name", "x", "--field", "bad"},
		"NAME=VALUE")
	refuses(t, 1, []string{"pass", "items", "update", "ref", "--field", "Section/=x"},
		"has no name")
}

func TestASecureLinkNeedsAnExpiry(t *testing.T) {
	refuses(t, 1, []string{"pass", "links", "create", "anything"}, "How long", "--expires 7d")
	refuses(t, 1, []string{"pass", "links", "create", "anything", "--expires", "next tuesday"},
		"--expires")
}

// A public link is a URL, and one that is not a Drive link is wrong before
// anybody looks it up. The whole answer is on the command line: no session, no
// request, and nothing for Proton to be asked about.
func TestALinkThatIsNotADriveLinkIsRefused(t *testing.T) {
	for _, link := range []string{
		"https://example.com/somewhere",
		"7X2K9M3N1P",
		"not a url at all",
		"https://drive.proton.me/urls/",
	} {
		refuses(t, 1, []string{"drive", "items", "list", "/", "--link", link},
			"That is not a Proton Drive link")
	}
}

func TestOpeningALinkIsNotRefusedForWantOfAnAccount(t *testing.T) {
	args := []string{"drive", "items", "list", "/",
		"--link", "https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL"}
	stderr, exited := runBriefly(t, args...)
	if strings.Contains(stderr, "not signed in") {
		t.Errorf("opening a public link asked for an account\nstderr: %s", truncate(stderr))
	}
	if exited {
		t.Errorf("the run was over at once rather than reaching for the link\nstderr: %s", truncate(stderr))
	}
}

func TestUploadingIntoALinkIsNotRefusedForWantOfAnAccount(t *testing.T) {
	src := filepath.Join(t.TempDir(), "photo.jpg")
	if err := os.WriteFile(src, []byte("the bytes"), 0o600); err != nil {
		t.Fatalf("write the file to upload: %v", err)
	}
	args := []string{"drive", "items", "upload", src, "/",
		"--link", "https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL"}
	stderr, exited := runBriefly(t, args...)
	if strings.Contains(stderr, "not signed in") {
		t.Errorf("uploading into a public link asked for an account\nstderr: %s", truncate(stderr))
	}
	if exited {
		t.Errorf("the run was over at once rather than reaching for the link\nstderr: %s", truncate(stderr))
	}
}

func TestANewRevisionInALinkIsRefused(t *testing.T) {
	src := filepath.Join(t.TempDir(), "photo.jpg")
	if err := os.WriteFile(src, []byte("the bytes"), 0o600); err != nil {
		t.Fatalf("write the file to upload: %v", err)
	}
	refuses(t, 1, []string{"drive", "items", "upload", src, "/", "--if-exists", "replace",
		"--link", "https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL"},
		"cannot take a new revision", "--if-exists rename")
}

func TestNamingTwoDriveTreesAtOnceIsRefused(t *testing.T) {
	refuses(t, 1, []string{"drive", "items", "list", "/", "--shared", "Project",
		"--link", "https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL"},
		"--shared and --link contradict each other.")
}

func TestALinkPasswordIsNotAFlagValue(t *testing.T) {
	refuses(t, 1, []string{"drive", "items", "list", "/", "--link",
		"https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL", "--link-password", "hunter2"},
		"Unknown flag: --link-password")
}

func TestAFolderTheWebNeverEmptiesIsRefused(t *testing.T) {
	for _, folder := range []string{"inbox", "drafts", "sent", "starred", "archive", "all", "scheduled", "social"} {
		refuses(t, 1, []string{"mail", "messages", "empty", "--folder", folder}, "cannot be emptied")
	}
}

func TestAMoveIntoAFolderThatTakesNoneIsRefused(t *testing.T) {
	for _, folder := range []string{"drafts", "sent", "all", "scheduled", "snoozed"} {
		refuses(t, 1, []string{"mail", "messages", "move", "--into", folder, "--unread"}, "cannot be moved into")
	}
	refuses(t, 3, []string{"mail", "messages", "move", "--into", "starred", "--unread"}, "is a label, not a folder")
}

func TestAContactsDetailsAreJudgedBeforeSigningIn(t *testing.T) {
	notes := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(notes, []byte("not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		args   []string
		phrase string
	}{
		{[]string{"contacts", "create", "--name", "Jane Roe", "--photo", notes}, "is not an image proton can read"},
		{[]string{"contacts", "update", "jane", "--photo", "https://"}, "is not a web address a photo can be loaded from"},
		{[]string{"contacts", "update", "jane", "--photo", notes, "--clear-photo"}, "contradict each other"},
		{[]string{"contacts", "emails", "update", "jane", "--sign", "maybe"}, "--sign accepts: on, off, default"},
		{[]string{"contacts", "emails", "update", "jane"}, "Nothing to change."},
	} {
		refuses(t, 1, tt.args, tt.phrase)
	}
}

func TestPassFoldersAndURLRulesAreJudgedFirst(t *testing.T) {
	for _, tt := range []struct {
		args   []string
		phrase string
	}{
		{[]string{"pass", "folders", "create"}, "A folder needs a name"},
		{[]string{"pass", "folders", "create", "--name", "Banking/Cards"}, "cannot contain /"},
		{[]string{"pass", "folders", "update", "Personal/Banking", "--name", "a/b"}, "cannot contain /"},
		{[]string{"pass", "items", "list", "--recursive"}, "no folder was named"},
		{[]string{"pass", "items", "trash", "--type", "note", "--recursive"}, "no folder was named"},
		{[]string{"pass", "items", "create", "--name", "GitHub", "--url", "regex:^https://"}, "regular expression"},
		{[]string{"pass", "items", "create", "--name", "GitHub", "--url", "never:"}, "nothing after the rule"},
		{[]string{"pass", "items", "create", "--name", "GitHub", "--url", "exact-url:not an address"}, "is not an address"},
		{[]string{"pass", "items", "create", "--name", "GitHub", "--url", "github.com", "--url", "github.com"}, "given twice"},
		{[]string{"pass", "items", "update", "GitHub", "--url", "regex:^https://"}, "regular expression"},
	} {
		refuses(t, 1, tt.args, tt.phrase)
	}
}
