package mail

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestABuiltInFolderResolvesByNameWithoutARequest(t *testing.T) {
	s := New(answers{}, testKeys(nil))
	for name, want := range map[string]string{
		"inbox": labelInbox, "INBOX": labelInbox, "trash": labelTrash, "Sent": labelSent,
		"scheduled": labelScheduled, "starred": labelStarred, "social": labelSocial,
	} {
		box, err := s.ResolveMailbox(context.Background(), name)
		if err != nil {
			t.Errorf("ResolveMailbox(%q): %v", name, err)
			continue
		}
		if box.ID != want || !box.System {
			t.Errorf("ResolveMailbox(%q) = %+v, want the built-in %s", name, box, want)
		}
	}
}

func TestAllFollowsAlmostAllMail(t *testing.T) {
	for setting, want := range map[string]string{"1": labelAlmostAllMail, "0": labelAllMail} {
		s := New(answers{
			"GET /mail/v4/settings": `{"MailSettings":{"AlmostAllMail":` + setting + `}}`,
		}, testKeys(nil))
		box, err := s.ResolveMailbox(context.Background(), "all")
		if err != nil {
			t.Fatalf("ResolveMailbox(all): %v", err)
		}
		if box.ID != want {
			t.Errorf("almost-all-mail %s: all is %q, want %q", setting, box.ID, want)
		}
	}
}

func TestStarredIsALabel(t *testing.T) {
	s := New(answers{}, testKeys(nil))
	if _, err := s.ResolveLabelTarget(context.Background(), "starred"); err != nil {
		t.Errorf("starred as a label: %v", err)
	}
	_, err := s.ResolveFolderTarget(context.Background(), "starred")
	if err == nil || !strings.Contains(err.Error(), "is a label, not a folder") {
		t.Errorf("starred as a move target: %v, want the label refusal", err)
	}
}

func TestAMoveIntoAFolderThatTakesNoneIsRefusedBeforeAnyRequest(t *testing.T) {
	s := New(answers{}, testKeys(nil))
	for _, name := range []string{"drafts", "sent", "all", "scheduled", "snoozed"} {
		_, err := s.ResolveFolderTarget(context.Background(), name)
		if err == nil || !strings.Contains(err.Error(), "cannot be moved into - only inbox, archive, spam, trash, a tab") {
			t.Errorf("move --into %s: %v, want the refusal", name, err)
		}
	}
	for _, name := range []string{"inbox", "archive", "spam", "trash", "primary"} {
		if _, err := s.ResolveFolderTarget(context.Background(), name); err != nil {
			t.Errorf("move --into %s: %v", name, err)
		}
	}
}

func TestOnlyTrashSpamSnoozedAndYourOwnCanBeEmptied(t *testing.T) {
	for name, want := range map[string]bool{
		"trash": true, "Spam": true, "snoozed": true, "Receipts": true,
		"inbox": false, "social": false, "all": false, "starred": false, "scheduled": false,
	} {
		if got := Emptiable(name); got != want {
			t.Errorf("Emptiable(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestATabHoldsOnlyTheInboxsMail(t *testing.T) {
	for _, tc := range []struct {
		labels []string
		place  string
		want   bool
	}{
		{[]string{labelInbox, labelSocial}, labelSocial, true},
		{[]string{labelArchive, labelSocial}, labelSocial, false},
		{[]string{labelArchive}, labelArchive, true},
		{[]string{labelArchive}, labelInbox, false},
		{[]string{labelTrash}, labelAllMail, true},
	} {
		if got := inPlace(tc.labels, tc.place); got != tc.want {
			t.Errorf("inPlace(%v, %s) = %v, want %v", tc.labels, tc.place, got, tc.want)
		}
	}
}

const ownLabels = `{"Labels":[{"ID":"l1","Name":"Work","Type":1}]}`

const ownFolders = `{"Labels":[` +
	`{"ID":"f1","Name":"Projects","Type":3,"Order":1},` +
	`{"ID":"f2","Name":"Clients","Type":3,"Order":1,"ParentID":"f1"},` +
	`{"ID":"f3","Name":"Receipts","Type":3,"Order":2}]}`

func mailboxNames(t *testing.T, categoryView string) []string {
	t.Helper()
	s := New(answers{
		"GET /mail/v4/settings":      `{"MailSettings":{"MailCategoryView":` + categoryView + `,"AlmostAllMail":0}}`,
		"GET /core/v4/labels?Type=1": ownLabels,
		"GET /core/v4/labels?Type=3": ownFolders,
		"GET /core/v4/labels?Type=4": sixCategories,
	}, testKeys(nil))
	boxes, err := s.Mailboxes(context.Background())
	if err != nil {
		t.Fatalf("Mailboxes: %v", err)
	}
	names := make([]string, 0, len(boxes))
	for _, b := range boxes {
		names = append(names, b.Name)
	}
	return names
}

func TestMailboxesComeInTheSidebarsOrder(t *testing.T) {
	sidebar := []string{"drafts", "scheduled", "snoozed", "sent", "starred", "archive", "spam", "trash", "all",
		"Projects", "Clients", "Receipts", "Work"}
	if got, want := mailboxNames(t, "false"), append([]string{"inbox"}, sidebar...); !reflect.DeepEqual(got, want) {
		t.Errorf("categories off: %q, want %q", got, want)
	}
	tabs := []string{"inbox", "primary", "social", "promotions", "newsletters"}
	if got, want := mailboxNames(t, "true"), append(tabs, sidebar...); !reflect.DeepEqual(got, want) {
		t.Errorf("categories on: %q, want %q", got, want)
	}
}
