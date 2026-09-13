package passfile

import "strings"

// RoboForm writes a CSV whose last columns are however many fields the item had,
// so a row is read by position rather than by name.
//
// A value that starts with @ or ' is written with a quote in front of it, which
// is the file's own escaping and not part of the value.

const (
	roboformName = iota
	roboformURL
	roboformMatchURL
	roboformLogin
	roboformPassword
	roboformNote
	roboformFolder
	roboformFields
)

func readRoboForm(in source) (*Document, error) {
	t, err := openCSV(in, "roboform", "Name", "Url", "MatchUrl", "Login", "Pwd", "Note", "Folder")
	if err != nil {
		return nil, err
	}
	doc := flatFile(t)
	vaults := newGrouped()
	for _, r := range t.rows {
		cells := r.cells
		at := func(i int) string {
			if i >= len(cells) {
				return ""
			}
			return cells[i]
		}
		in := item{
			Name: at(roboformName),
			Note: at(roboformNote),
		}
		url, match := at(roboformURL), at(roboformMatchURL)
		login, password := roboformValue(at(roboformLogin)), roboformValue(at(roboformPassword))
		vault := roboformVault(at(roboformFolder))
		switch {
		case url == "" && match == "" && login == "" && password == "":
			vaults.add(vault, in.note())
		case match == "" && login == "" && password == "":
			// A bookmark: an address somebody kept, with nothing to sign in
			// with.
			in.URLs = []string{url}
			vaults.add(vault, in.login())
		default:
			in.Email, in.Username = identifier(login)
			in.Password = password
			in.URLs = []string{match}
			in.TOTP = roboformTOTP(cells[min(roboformFields, len(cells)):])
			vaults.add(vault, in.login())
		}
	}
	vaults.into(doc)
	return doc, nil
}

// roboformVault is the folder an item sits in, innermost first.
func roboformVault(folder string) string {
	parts := strings.Split(folder, "/")
	return parts[len(parts)-1]
}

// roboformValue undoes the quote RoboForm puts in front of a value that starts
// with @ or a quote of its own.
func roboformValue(value string) string {
	if strings.HasPrefix(value, "'@") || strings.HasPrefix(value, "''") {
		return value[1:]
	}
	return value
}

// roboformTOTP finds the secret among the item's own fields, each of which is
// itself a comma-separated list ending in its value.
func roboformTOTP(fields []string) string {
	for _, f := range fields {
		if !strings.HasPrefix(f, "TOTP KEY$") {
			continue
		}
		parts := strings.Split(f, ",")
		return parts[len(parts)-1]
	}
	return ""
}
