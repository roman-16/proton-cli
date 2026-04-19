package render

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// HTMLToText renders an HTML fragment as plain text:
//
//   - script/style/head/title content is dropped
//   - <br> emits a newline
//   - block-level elements (p, div, li, tr, h1-h6, pre, blockquote) are
//     separated by newlines
//   - <a href="X">text</a> becomes "text [X]" when href differs from text
//   - repeated whitespace is collapsed to a single space; blank-line runs
//     to a single blank line
//
// Returns the input unchanged on parse failure.
func HTMLToText(s string) string {
	doc, err := html.Parse(strings.NewReader(s))
	if err != nil {
		return s
	}
	var b strings.Builder
	walk(&b, doc)
	return collapseWhitespace(b.String())
}

var blockElems = map[atom.Atom]bool{
	atom.P:          true,
	atom.Div:        true,
	atom.Li:         true,
	atom.Tr:         true,
	atom.H1:         true,
	atom.H2:         true,
	atom.H3:         true,
	atom.H4:         true,
	atom.H5:         true,
	atom.H6:         true,
	atom.Pre:        true,
	atom.Blockquote: true,
	atom.Ul:         true,
	atom.Ol:         true,
	atom.Table:      true,
	atom.Hr:         true,
	atom.Article:    true,
	atom.Section:    true,
}

func walk(b *strings.Builder, n *html.Node) {
	if n.Type == html.ElementNode {
		switch n.DataAtom {
		case atom.Script, atom.Style, atom.Head, atom.Title, atom.Noscript:
			return
		case atom.Br:
			b.WriteByte('\n')
			return
		case atom.Hr:
			b.WriteString("\n\n")
			return
		case atom.A:
			var inner strings.Builder
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(&inner, c)
			}
			text := inner.String()
			b.WriteString(text)
			var href string
			for _, a := range n.Attr {
				if a.Key == "href" {
					href = a.Val
					break
				}
			}
			href = strings.TrimSpace(href)
			trimmed := strings.TrimSpace(text)
			if href != "" && href != trimmed && !strings.HasPrefix(href, "mailto:"+trimmed) {
				b.WriteString(" [")
				b.WriteString(href)
				b.WriteByte(']')
			}
			return
		}
	}

	isBlock := n.Type == html.ElementNode && blockElems[n.DataAtom]
	if isBlock {
		b.WriteByte('\n')
	}
	if n.Type == html.TextNode {
		b.WriteString(n.Data)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(b, c)
	}
	if isBlock {
		b.WriteByte('\n')
	}
}

func collapseWhitespace(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blanks := 0
	for _, line := range lines {
		clean := strings.Join(strings.Fields(line), " ")
		if clean == "" {
			blanks++
			if blanks <= 1 {
				out = append(out, "")
			}
			continue
		}
		blanks = 0
		out = append(out, clean)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// IsHTML is a heuristic for whether a MIME type string represents HTML.
func IsHTML(mimeType string) bool {
	return strings.Contains(strings.ToLower(mimeType), "html")
}
