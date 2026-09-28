package mailtext

import (
	"slices"
	"testing"
)

func TestCleanLinksRewritesOnlyTheTargetsOfTrackedLinks(t *testing.T) {
	body := `<p class=intro>Hi &amp; welcome</p>` +
		`<A class=cta HREF='https://trailhead.example/north?utm_source=newsletter&amp;utm_medium=email'>Read</A> ` +
		`<a href=https://trailhead.example/south?utm_campaign=april>South</a>` +
		`<a href="https://trailhead.example/east">East</a>` +
		`<a href="#top">Top</a>` +
		`<script>var s = '<a href="https://x.example/?utm_source=a">';</script>`
	got, links := CleanLinks(body, true)
	want := `<p class=intro>Hi &amp; welcome</p>` +
		`<A class=cta HREF='https://trailhead.example/north'>Read</A> ` +
		`<a href="https://trailhead.example/south">South</a>` +
		`<a href="https://trailhead.example/east">East</a>` +
		`<a href="#top">Top</a>` +
		`<script>var s = '<a href="https://x.example/?utm_source=a">';</script>`
	if got != want {
		t.Errorf("CleanLinks =\n%s\nwant\n%s", got, want)
	}
	if len(links) != 2 {
		t.Fatalf("cleaned %d links, want 2: %+v", len(links), links)
	}
	if links[0].Original != "https://trailhead.example/north?utm_source=newsletter&utm_medium=email" ||
		links[0].Cleaned != "https://trailhead.example/north" || len(links[0].Removed) != 2 {
		t.Errorf("first link = %+v", links[0])
	}
}

func TestCleanLinksLeavesAnUntrackedBodyAsItWas(t *testing.T) {
	body := "<!DOCTYPE html><html><body><p>Plain<br/>text\r\n</p><img src=\"cid:logo\"></body></html>"
	got, links := CleanLinks(body, true)
	if got != body || links != nil {
		t.Errorf("CleanLinks changed an untracked body: %q, %+v", got, links)
	}
}

func TestCleanLinksInPlainTextKeepsThePunctuationAround(t *testing.T) {
	body := "Read it (https://trailhead.example/north?utm_source=nl). Or https://trailhead.example/?fbclid=x, then reply."
	got, links := CleanLinks(body, false)
	want := "Read it (https://trailhead.example/north). Or https://trailhead.example/, then reply."
	if got != want {
		t.Errorf("CleanLinks = %q, want %q", got, want)
	}
	if len(links) != 2 {
		t.Errorf("cleaned %d links, want 2", len(links))
	}
}

func TestRemoteImagesNamesWhatABodyWouldLoad(t *testing.T) {
	body := `<img src="https://list-manage.example/track/open.php?u=1">` +
		`<img src="cid:logo@x"><img src="data:image/png;base64,AAAA">` +
		`<img src="https://list-manage.example/track/open.php?u=1">` +
		`<table background="https://cdn.example/bg.png"><tr><td style="background-image: url('https://cdn.example/cell.png')">x</td></tr></table>` +
		`<video poster="https://cdn.example/poster.jpg"></video>` +
		`<img srcset="https://cdn.example/only-srcset.png 2x">` +
		`<img src="https://cdn.example/split%0A.png">`
	want := []string{
		"https://list-manage.example/track/open.php?u=1",
		"https://cdn.example/bg.png",
		"https://cdn.example/cell.png",
		"https://cdn.example/poster.jpg",
		"https://cdn.example/split.png",
	}
	if got := RemoteImages(body); !slices.Equal(got, want) {
		t.Errorf("RemoteImages = %q, want %q", got, want)
	}
}
