package tidyurl

import "testing"

func TestARedirectIsDecodedOnlyFromTheLinkItIsNamedFor(t *testing.T) {
	const favorite = "eyJuYW1lIjoidHdpdGNoX2Zhdm9yaXRlX3VwIiwiY2hhbm5lbCI6InMifQ=="
	for link, want := range map[string]string{
		"https://click.redditmail.com/CL0/https:%2F%2Fgood.example%2F/1/abc?a=1": "https://good.example/",
		"https://www.twitch.tv/r/e/" + favorite + "/x?utm_source=x":              "https://www.twitch.tv/s",
		"http://www.twitch.tv/r/e/" + favorite + "/x?utm_source=x":               "https://www.twitch.tv/s",

		"https://click.redditmail.com/CL1/x?u=https://click.redditmail.com/CL0/https:%2F%2Fevil.example%2F/1":          "",
		"https://click.redditmail.com/x/https://click.redditmail.com/CL0/https:%2F%2Fevil.example%2F/1?a=1":            "",
		"https://click.redditmail.com.evil.example/https://click.redditmail.com/CL0/https:%2F%2Fgood.example%2F/1?a=1": "",
		"https://evil.example/https://www.twitch.tv/r/e/" + favorite + "/x?a=1":                                        "",
	} {
		got, tracked := Clean(link)
		if want == "" {
			if tracked || got.URL != link {
				t.Errorf("Clean(%q) = %q, want the link as sent", link, got.URL)
			}
			continue
		}
		if !tracked || got.URL != want {
			t.Errorf("Clean(%q) = %q tracked=%v, want %q", link, got.URL, tracked, want)
		}
	}
}
