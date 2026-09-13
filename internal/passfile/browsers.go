package passfile

import "os"

// The browsers, which all export a flat list of logins and nothing else.
//
// Chrome, Edge and Brave write the same file, Firefox writes its own with the
// dates on it, and Safari's is shared with Apple Passwords.

func readChromium(in source) (*Document, error) {
	t, err := openCSV(in, "chrome", "name", "url", "username", "password")
	if err != nil {
		return nil, err
	}
	doc := flatFile(t)
	var items []Entry
	for _, r := range t.rows {
		email, username := identifier(r.get("username"))
		items = append(items, item{
			Name:     r.get("name"),
			Note:     r.get("note"),
			Email:    email,
			Username: username,
			Password: r.get("password"),
			URLs:     []string{r.get("url")},
		}.login())
	}
	doc.vault("", items)
	return doc, nil
}

func readFirefox(in source) (*Document, error) {
	t, err := openCSV(in, "firefox", "url", "username", "password", "timeCreated", "timePasswordChanged")
	if err != nil {
		return nil, err
	}
	doc := flatFile(t)
	var items []Entry
	for _, r := range t.rows {
		// The browser keeps the account you sign in to Firefox with among the
		// passwords it saved, and it is not one of them.
		if r.get("url") == "chrome://FirefoxAccounts" {
			continue
		}
		email, username := identifier(r.get("username"))
		items = append(items, item{
			Email:      email,
			Username:   username,
			Password:   r.get("password"),
			URLs:       []string{r.get("url")},
			CreateTime: r.number("timeCreated") / 1000,
			ModifyTime: r.number("timePasswordChanged") / 1000,
		}.login())
	}
	doc.vault("", items)
	return doc, nil
}

// readSafari also reads Apple Passwords, which writes the same file. Safari
// before Sonoma wrote only the first four columns, so the two it gained are
// wanted rather than required.
func readSafari(in source) (*Document, error) {
	t, err := openCSV(in, "safari", "Title", "Username", "Password")
	if err != nil {
		return nil, err
	}
	doc := flatFile(t)
	var items []Entry
	for _, r := range t.rows {
		email, username := identifier(r.get("Username"))
		items = append(items, item{
			Name:     r.get("Title"),
			Note:     r.get("Notes"),
			Email:    email,
			Username: username,
			Password: r.get("Password"),
			URLs:     []string{r.get("URL")},
			TOTP:     r.get("OTPAuth"),
		}.login())
	}
	doc.vault("", items)
	return doc, nil
}

// openCSV reads a file that has to be a CSV with certain columns, and refuses
// one that is not before anything is made of it.
func openCSV(in source, format string, columns ...string) (*table, error) {
	raw, err := os.ReadFile(in.path)
	if err != nil {
		return nil, err
	}
	t, err := readCSV(raw)
	if err != nil || !t.has(columns...) {
		return nil, notThisFormat(in, format)
	}
	return t, nil
}

// flatFile is a document for a file that holds one list and no vaults, so
// everything in it lands wherever the import is pointed.
func flatFile(t *table) *Document {
	doc := &Document{}
	if w := t.warning(); w != "" {
		doc.Warnings = append(doc.Warnings, w)
	}
	return doc
}
