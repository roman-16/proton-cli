package mailtext

import (
	"bytes"
	"errors"
	"io"
	"regexp"
	"strings"

	"github.com/roman-16/proton-cli/internal/tidyurl"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// CleanedLink is a link a body carried with tracking in it, and what it became.
type CleanedLink struct {
	Original string
	Cleaned  string
	Removed  []tidyurl.Param
}

// CleanLinks takes the tracking out of every link in a body. An HTML body keeps
// every byte apart from the targets of its links; a plain-text body has each
// web address it spells out replaced.
func CleanLinks(body string, isHTML bool) (string, []CleanedLink) {
	if isHTML {
		return cleanHTMLLinks(body)
	}
	return cleanPlainLinks(body)
}

func cleanHTMLLinks(body string) (string, []CleanedLink) {
	z := html.NewTokenizer(strings.NewReader(body))
	var out bytes.Buffer
	var cleaned []CleanedLink
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if !errors.Is(z.Err(), io.EOF) {
				return body, nil
			}
			break
		}
		raw := append([]byte(nil), z.Raw()...)
		if tt == html.StartTagToken || tt == html.SelfClosingTagToken {
			if token := z.Token(); token.DataAtom == atom.A {
				if rewritten, link, ok := cleanAnchor(raw, token); ok {
					raw = rewritten
					cleaned = append(cleaned, link)
				}
			}
		}
		out.Write(raw)
	}
	return out.String(), cleaned
}

func cleanAnchor(raw []byte, token html.Token) ([]byte, CleanedLink, bool) {
	var href string
	found := false
	for _, a := range token.Attr {
		if a.Namespace == "" && a.Key == "href" {
			href, found = a.Val, true
			break
		}
	}
	if !found || href == "" || strings.HasPrefix(href, "#") {
		return nil, CleanedLink{}, false
	}
	result, tracked := tidyurl.Clean(href)
	if !tracked {
		return nil, CleanedLink{}, false
	}
	start, end, quoted := hrefValueSpan(raw)
	if start < 0 {
		return nil, CleanedLink{}, false
	}
	value := html.EscapeString(result.URL)
	if !quoted {
		value = `"` + value + `"`
	}
	rewritten := append(append(append([]byte(nil), raw[:start]...), value...), raw[end:]...)
	return rewritten, CleanedLink{Original: href, Cleaned: result.URL, Removed: result.Removed}, true
}

// hrefValueSpan finds the first href attribute's value in a raw start tag, the
// way the tokenizer reads attributes, so that it alone can be replaced.
func hrefValueSpan(raw []byte) (start, end int, quoted bool) {
	i := 1
	for i < len(raw) && !isTagSpace(raw[i]) && raw[i] != '/' && raw[i] != '>' {
		i++
	}
	for i < len(raw) {
		for i < len(raw) && (isTagSpace(raw[i]) || raw[i] == '/') {
			i++
		}
		if i >= len(raw) || raw[i] == '>' {
			break
		}
		nameStart := i
		i++
		for i < len(raw) && !isTagSpace(raw[i]) && raw[i] != '/' && raw[i] != '>' && raw[i] != '=' {
			i++
		}
		name := strings.ToLower(string(raw[nameStart:i]))
		for i < len(raw) && isTagSpace(raw[i]) {
			i++
		}
		if i >= len(raw) || raw[i] != '=' {
			continue
		}
		i++
		for i < len(raw) && isTagSpace(raw[i]) {
			i++
		}
		var valueStart, valueEnd int
		isQuoted := false
		if i < len(raw) && (raw[i] == '"' || raw[i] == '\'') {
			quote := raw[i]
			valueStart = i + 1
			valueEnd = bytes.IndexByte(raw[valueStart:], quote)
			if valueEnd < 0 {
				return -1, -1, false
			}
			valueEnd += valueStart
			i = valueEnd + 1
			isQuoted = true
		} else {
			valueStart = i
			for i < len(raw) && !isTagSpace(raw[i]) && raw[i] != '>' {
				i++
			}
			valueEnd = i
		}
		if name == "href" {
			return valueStart, valueEnd, isQuoted
		}
	}
	return -1, -1, false
}

func isTagSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

var plainLink = regexp.MustCompile(`(?i)\bhttps?://[^\s<>"]+`)

func cleanPlainLinks(body string) (string, []CleanedLink) {
	var cleaned []CleanedLink
	out := plainLink.ReplaceAllStringFunc(body, func(match string) string {
		link, trailer := splitTrailer(match)
		result, tracked := tidyurl.Clean(link)
		if !tracked {
			return match
		}
		cleaned = append(cleaned, CleanedLink{Original: link, Cleaned: result.URL, Removed: result.Removed})
		return result.URL + trailer
	})
	return out, cleaned
}

// splitTrailer separates a web address written in running text from the
// punctuation that closes the sentence or the bracket around it.
func splitTrailer(match string) (string, string) {
	end := len(match)
	for end > 0 {
		c := match[end-1]
		switch {
		case strings.IndexByte(".,;:!?'", c) >= 0:
			end--
		case c == ')' && strings.Count(match[:end], "(") < strings.Count(match[:end], ")"):
			end--
		case c == ']' && strings.Count(match[:end], "[") < strings.Count(match[:end], "]"):
			end--
		case c == '}' && strings.Count(match[:end], "{") < strings.Count(match[:end], "}"):
			end--
		default:
			return match[:end], match[end:]
		}
	}
	return match[:end], match[end:]
}

// RemoteImages is every distinct image a body would load from the web, in the
// order it names them.
func RemoteImages(body string) []string {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var images []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if image := imageOf(n); image != "" && !seen[image] {
				seen[image] = true
				images = append(images, image)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return images
}

var styleImage = regexp.MustCompile(`(?i)url\((.*?)\)`)

var lineBreaks = regexp.MustCompile(`(?i)\r\n|\r|\n|%0A|%0D|%0C`)

// imageOf is what an element loads: the first of the attributes that name an
// image, or else the first url() of its style.
func imageOf(n *html.Node) string {
	attrs := map[string]string{}
	for _, a := range n.Attr {
		key := a.Key
		if a.Namespace != "" {
			key = a.Namespace + ":" + a.Key
		}
		if _, dup := attrs[key]; !dup {
			attrs[key] = a.Val
		}
	}
	var candidate string
	if _, hasHref := attrs["href"]; !hasHref && attrs["xlink:href"] != "" {
		candidate = attrs["xlink:href"]
	}
	for _, key := range []string{"src", "background", "poster"} {
		if candidate == "" {
			candidate = attrs[key]
		}
	}
	if candidate == "" {
		if _, hasSrc := attrs["src"]; !hasSrc {
			if found := styleImage.FindStringSubmatch(attrs["style"]); found != nil {
				candidate = strings.Trim(found[1], `'" `)
			}
		}
	}
	candidate = lineBreaks.ReplaceAllString(strings.TrimSpace(candidate), "")
	lower := strings.ToLower(candidate)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return ""
	}
	return candidate
}
