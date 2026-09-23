package live

import (
	"reflect"
	"testing"
)

type category struct {
	name          string
	shown, notify bool
}

func categories(t *testing.T) []category {
	t.Helper()
	var out []category
	for _, row := range runJSONArray(t, "mail", "settings", "categories", "list") {
		m := row.(map[string]interface{})
		name, _ := m["name"].(string)
		shown, _ := m["shown"].(bool)
		notify, _ := m["notify"].(bool)
		out = append(out, category{name: name, shown: shown, notify: notify})
	}
	return out
}

func requireCategories(t *testing.T) []category {
	t.Helper()
	all := categories(t)
	if len(all) == 0 {
		t.Skip("Proton has not given this account inbox categories, so there are none to change")
	}
	return all
}

func categoryNamed(t *testing.T, name string) category {
	t.Helper()
	for _, c := range categories(t) {
		if c.name == name {
			return c
		}
	}
	t.Fatalf("no category called %q", name)
	return category{}
}

func withCategoryView(t *testing.T, want string) {
	t.Helper()
	original, _ := runJSON(t, "mail", "settings", "get")["category_view"].(string)
	if original == want {
		return
	}
	cleanupRun(t, "Restore categories: proton mail settings set category-view "+original,
		"mail", "settings", "set", "category-view", original)
	runOK(t, "mail", "settings", "set", "category-view", want)
}

func showCategory(t *testing.T, c category, shown bool) {
	t.Helper()
	if c.shown == shown {
		return
	}
	verb, back := "enable", "disable"
	if !shown {
		verb, back = back, verb
	}
	cleanupRun(t, "Restore "+c.name+": proton mail settings categories "+back+" "+c.name,
		"mail", "settings", "categories", back, c.name)
	runOK(t, "mail", "settings", "categories", verb, c.name)
}

func TestMailCategoriesListTheSixInTheWebsOrder(t *testing.T) {
	var names []string
	for _, c := range requireCategories(t) {
		names = append(names, c.name)
	}
	want := []string{"primary", "social", "promotions", "newsletters", "transactions", "updates"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("categories = %q, want %q", names, want)
	}
	if !categoryNamed(t, "primary").shown {
		t.Error("primary is not shown")
	}
}

func TestMailCategoriesShowAndHideRoundTrip(t *testing.T) {
	all := requireCategories(t)
	withCategoryView(t, "on")
	target := all[1]
	for _, c := range all[1:] {
		if !c.shown {
			target = c
			break
		}
	}
	showCategory(t, target, !target.shown)
	if got := categoryNamed(t, target.name).shown; got == target.shown {
		t.Errorf("%s shown = %v after flipping it", target.name, got)
	}
}

func TestMailCategoriesNotifyRoundTrip(t *testing.T) {
	requireCategories(t)
	withCategoryView(t, "on")
	showCategory(t, categoryNamed(t, "social"), true)
	notify, restore := "--notify=true", "--notify=false"
	if categoryNamed(t, "social").notify {
		notify, restore = restore, notify
	}
	cleanupRun(t, "Restore social: proton mail settings categories update social "+restore,
		"mail", "settings", "categories", "update", "social", restore)
	runOK(t, "mail", "settings", "categories", "update", "social", notify)
	if got := categoryNamed(t, "social").notify; got != (notify == "--notify=true") {
		t.Errorf("social notify = %v after %s", got, notify)
	}
}

func TestMailCategoriesRefuseNotifyOnAHiddenOne(t *testing.T) {
	requireCategories(t)
	withCategoryView(t, "on")
	showCategory(t, categoryNamed(t, "promotions"), true)
	showCategory(t, categoryNamed(t, "updates"), false)
	_, stderr, code := run(t, "mail", "settings", "categories", "update", "updates", "--notify")
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	assertContains(t, stderr, "is hidden")
	assertContains(t, stderr, "categories enable updates")
}

func TestMailCategoriesRefuseHidingTheLastOneBesidesPrimary(t *testing.T) {
	all := requireCategories(t)
	withCategoryView(t, "on")
	showCategory(t, all[1], true)
	for _, c := range all[2:] {
		showCategory(t, c, false)
	}
	_, stderr, code := run(t, "mail", "settings", "categories", "disable", all[1].name)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	assertContains(t, stderr, "last one shown besides primary")
	assertContains(t, stderr, "set category-view off")
}

func TestMailCategoriesRefuseChangesWhileCategoriesAreOff(t *testing.T) {
	requireCategories(t)
	withCategoryView(t, "off")
	_, stderr, code := run(t, "mail", "settings", "categories", "enable", "updates")
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	assertContains(t, stderr, "Categories are off")
	_, stderr = runOKStderr(t, "mail", "settings", "categories", "list")
	assertContains(t, stderr, "set category-view on")
}

func TestMailCategoriesAnAccountWithoutAnyRefusesToChangeThem(t *testing.T) {
	if len(categories(t)) > 0 {
		t.Skip("the account has inbox categories, so there is nothing here to refuse for want of them")
	}
	_, stderr, code := run(t, "mail", "settings", "categories", "enable", "social")
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	assertContains(t, stderr, "This account has no inbox categories")
}
