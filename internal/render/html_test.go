package render

import "testing"

func TestHTMLToText(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{
			name: "br-to-newline with inline tags",
			in:   `<div dir="ltr">Hi<br><br>Here's the <b>invoice</b>.</div>`,
			want: "Hi\n\nHere's the invoice.",
		},
		{
			name: "paragraphs with link",
			in:   `<p>First paragraph.</p><p>Second paragraph with a <a href="https://example.com">link</a>.</p>`,
			want: "First paragraph.\n\nSecond paragraph with a link [https://example.com].",
		},
		{
			name: "heading and list; script/style/head dropped",
			in:   `<html><head><title>x</title><style>body{color:red}</style></head><body><h1>Heading</h1><ul><li>one</li><li>two</li></ul></body></html>`,
			want: "Heading\n\none\n\ntwo",
		},
		{
			name: "entity and script stripping",
			in:   `<script>alert('x')</script>Visible text&nbsp;here&amp;more.`,
			want: "Visible text here&more.",
		},
		{
			name: "plain text passthrough",
			in:   `plain text, no tags`,
			want: "plain text, no tags",
		},
		{
			name: "self-referential mailto link does not duplicate",
			in:   `<a href="mailto:foo@bar.com">foo@bar.com</a>`,
			want: "foo@bar.com",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := HTMLToText(tc.in)
			if got != tc.want {
				t.Errorf("HTMLToText()\nin:   %q\ngot:  %q\nwant: %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsHTML(t *testing.T) {
	for _, mt := range []string{"text/html", "text/html; charset=utf-8", "TEXT/HTML"} {
		if !IsHTML(mt) {
			t.Errorf("IsHTML(%q) = false, want true", mt)
		}
	}
	for _, mt := range []string{"text/plain", "application/octet-stream", ""} {
		if IsHTML(mt) {
			t.Errorf("IsHTML(%q) = true, want false", mt)
		}
	}
}
