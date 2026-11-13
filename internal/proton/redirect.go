package proton

import (
	"fmt"
	"net/http"
	"strings"
)

const maxRedirects = 10

// SameOrigin follows a redirect only within the scheme and host it started at.
// net/http drops Authorization and Cookie on a redirect to another host but
// forwards every other header, x-pm-uid and pm-storage-token among them, and a
// 307 or 308 sends the body again.
func SameOrigin(req *http.Request, via []*http.Request) error {
	if len(via) == 0 {
		return nil
	}
	origin := via[0].URL
	if !strings.EqualFold(origin.Scheme, req.URL.Scheme) || !strings.EqualFold(origin.Host, req.URL.Host) {
		return http.ErrUseLastResponse
	}
	if len(via) >= maxRedirects {
		return fmt.Errorf("stopped after %d redirects", maxRedirects)
	}
	return nil
}
