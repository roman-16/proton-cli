package passfile

import (
	"os"
	"regexp"
	"slices"
	"strings"
)

// Kaspersky exports a text file of sections separated by a line of three
// dashes, each section a run of "Label: value" lines.
//
// A section with a Login is a login, one with a Text is a note, and the lines of
// a note after the first belong to the note rather than being labels of their
// own.

// kasperskyLabels are the only labels a line may carry. Anything else on a line
// is part of the value above it, which is how a note keeps its shape.
var kasperskyLabels = []string{
	"Account name", "Application", "Comment", "Login", "Login name",
	"Name", "Password", "Text", "Website URL", "Website name",
}

var kasperskySeparator = regexp.MustCompile(`\n*---\n*`)

func readKaspersky(in source) (*Document, error) {
	raw, err := os.ReadFile(in.path)
	if err != nil {
		return nil, err
	}
	text := strings.ReplaceAll(string(stripBOM(raw)), "\r\n", "\n")
	sections := kasperskySeparator.Split(text, -1)

	doc := &Document{}
	var items []Entry
	var read bool
	for _, section := range sections {
		fields := kasperskySection(section)
		switch {
		case fields["Login"] != "":
			read = true
			in := item{
				Name: kasperskyName(fields),
				Note: fields["Comment"],
			}
			in.Email, in.Username = identifier(fields["Login"])
			in.Password = fields["Password"]
			if url := fields["Website URL"]; url != "" {
				in.URLs = []string{url}
			}
			items = append(items, in.login())
		case fields["Text"] != "":
			read = true
			items = append(items, item{Name: fields["Name"], Note: fields["Text"]}.note())
		case len(fields) > 0:
			doc.skip("", fields["Name"], "it holds neither a login nor a note")
		}
	}
	if !read {
		return nil, notThisFormat(in, "kaspersky")
	}
	doc.vault("", items)
	return doc, nil
}

// kasperskySection reads one item's labelled lines. Once a note has begun every
// further line is part of it, because a note may itself look like labels.
func kasperskySection(section string) map[string]string {
	fields := map[string]string{}
	var current string
	for _, line := range strings.Split(section, "\n") {
		label, value, found := strings.Cut(line, ":")
		label = strings.TrimSpace(label)
		isNote := fields["Text"] != "" || current == "Text"
		switch {
		case found && !isNote && slices.Contains(kasperskyLabels, label):
			current = label
			fields[label] = strings.TrimSpace(value)
		case current != "":
			fields[current] += "\n" + line
		}
	}
	for label, value := range fields {
		fields[label] = strings.TrimRight(value, "\n")
	}
	return fields
}

// kasperskyName is what the item is called, which depends on whether it is a
// website, an application or an account on its own.
func kasperskyName(fields map[string]string) string {
	var name string
	for _, label := range []string{"Login name", "Account name", "Website name"} {
		if fields[label] != "" {
			name = fields[label]
			break
		}
	}
	if app := fields["Application"]; app != "" {
		return strings.TrimSpace(app + " " + name)
	}
	return name
}
