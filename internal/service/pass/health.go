package pass

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/roman-16/proton-cli/internal/credcheck"
	"github.com/roman-16/proton-cli/internal/secret"
	"github.com/roman-16/proton-cli/internal/skip"
	"github.com/roman-16/proton-cli/internal/twofa"
)

// Password health: which of the logins in your vaults are worth going back to.
//
// Three of the four checks are answered on this machine out of what a listing
// already decrypts, so they cost no request at all. The fourth asks a public
// corpus whether a password has leaked, and asks it about a bucket rather than
// about a password - see internal/credcheck.
//
// What "weak" means is this CLI's own reading, stated in internal/secret and in
// the help of the command that reports it. The other three are facts.

// Risk is one thing that can be wrong with a stored login.
type Risk string

const (
	// RiskCompromised is a password the public corpus of leaked credentials
	// names.
	RiskCompromised Risk = "compromised"
	// RiskMissing2FA is a login for a site that offers a time-based code, with
	// neither a code nor a passkey stored against it.
	RiskMissing2FA Risk = "missing-2fa"
	// RiskReused is a password more than one login shares.
	RiskReused Risk = "reused"
	// RiskWeak is a password short enough or plain enough to guess.
	RiskWeak Risk = "weak"
)

// Risks is every Risk, in the order a list of values is offered in.
func Risks() []string {
	return []string{string(RiskCompromised), string(RiskMissing2FA), string(RiskReused), string(RiskWeak)}
}

// corpusAtOnce is how many buckets are asked for at the same time. The corpus is
// somebody else's host and a vault can hold hundreds of logins, so the fan-out
// is bounded rather than being however many passwords happen to be stored.
const corpusAtOnce = 8

// AtRisk lists the logins that fail one check, worst-named first.
//
// Only the logins Proton's own checks would cover are considered: what is in the
// trash is out, and so is anything excluded from the security checks, which is
// the same flag Pass sets from its own screen.
//
// corpus is the client the leaked-credential check talks to, and is used by that
// check alone.
func (s *Service) AtRisk(ctx context.Context, vaultFilter string, risk Risk, corpus *http.Client) ([]Item, error) {
	full, err := s.itemsFull(ctx, vaultFilter, browsed, false)
	if err != nil {
		return nil, err
	}
	logins := checkable(full)

	switch risk {
	case RiskCompromised:
		return s.compromised(ctx, logins, corpus)
	case RiskMissing2FA:
		return missing2FA(logins), nil
	case RiskReused:
		return reused(logins), nil
	case RiskWeak:
		return weak(logins), nil
	}
	return nil, nil
}

// checkable is the items a check covers: logins, and only the ones Proton's own
// checks would look at. An item excluded from the security checks is left out of
// every one of them, which is what the flag is for.
func checkable(items []FullItem) []FullItem {
	out := make([]FullItem, 0, len(items))
	for _, it := range items {
		if it.Type == "login" && !it.Excluded {
			out = append(out, it)
		}
	}
	return out
}

// reused groups the logins that share a password and numbers the groups.
//
// The number is what makes the answer readable: without it two pairs sharing two
// different passwords look exactly like one group of four. Groups are numbered
// by their first login's name, so the numbering is the same on every run and
// does not depend on the passwords themselves.
func reused(logins []FullItem) []Item {
	byPassword := map[string][]FullItem{}
	for _, it := range logins {
		if it.Password != "" {
			byPassword[it.Password] = append(byPassword[it.Password], it)
		}
	}

	groups := make([][]FullItem, 0, len(byPassword))
	for _, group := range byPassword {
		if len(group) < 2 {
			continue
		}
		slices.SortStableFunc(group, byName)
		groups = append(groups, group)
	}
	slices.SortStableFunc(groups, func(a, b []FullItem) int { return byName(a[0], b[0]) })

	var out []Item
	for i, group := range groups {
		for _, it := range group {
			out = append(out, flagged(it, RiskReused, i+1))
		}
	}
	return out
}

// weak is the logins whose password this CLI would not accept as one.
func weak(logins []FullItem) []Item {
	var out []Item
	for _, it := range logins {
		if it.Password != "" && secret.Weak(it.Password) {
			out = append(out, flagged(it, RiskWeak, 0))
		}
	}
	return out
}

// missing2FA is the logins for sites that offer a time-based code and have
// neither one stored nor a passkey that stands in for it.
func missing2FA(logins []FullItem) []Item {
	var out []Item
	for _, it := range logins {
		if hasSecondFactor(it) {
			continue
		}
		for _, u := range it.URLs {
			if twofa.Eligible(u) {
				out = append(out, flagged(it, RiskMissing2FA, 0))
				break
			}
		}
	}
	return out
}

// hasSecondFactor reports whether anything on the login is already a second
// factor: the code field, a custom field holding one, or a passkey.
func hasSecondFactor(it FullItem) bool {
	if it.TOTP != "" || len(it.Passkeys) > 0 {
		return true
	}
	for _, f := range it.Fields {
		if f.Type == "totp" && f.Value != "" {
			return true
		}
	}
	return false
}

// compromised is the logins whose password the public corpus names.
//
// One bucket is asked for per distinct password, so two logins sharing one cost
// one request. A bucket that could not be fetched is recorded as a skip: the
// listing is then short, and says so, rather than reporting the login as clean.
func (s *Service) compromised(ctx context.Context, logins []FullItem, corpus *http.Client) ([]Item, error) {
	passwords := map[string]bool{}
	for _, it := range logins {
		if it.Password != "" {
			passwords[it.Password] = false
		}
	}
	distinct := make([]string, 0, len(passwords))
	for pw := range passwords {
		distinct = append(distinct, pw)
	}
	slices.Sort(distinct)

	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		slots   = make(chan struct{}, corpusAtOnce)
		refused = map[string]bool{}
	)
	for _, pw := range distinct {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			found, err := credcheck.Compromised(ctx, corpus, pw)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				refused[pw] = true
				return
			}
			passwords[pw] = found
		}()
	}
	wg.Wait()

	var out []Item
	for _, it := range logins {
		switch {
		case it.Password == "":
		case refused[it.Password]:
			skip.Record(ctx, skip.KindItem, it.ItemID, skip.Unreadable, nil)
		case passwords[it.Password]:
			out = append(out, flagged(it, RiskCompromised, 0))
		}
	}
	return out, nil
}

// flagged is one login on its way into a listing, carrying what was found and
// nothing that was locked away.
func flagged(it FullItem, risk Risk, group int) Item {
	row := it.Item
	row.Risk = risk
	row.ReuseGroup = group
	return row
}

// byName orders two logins the way a listing does by default, so a group's
// numbering and a listing's order agree.
func byName(a, b FullItem) int {
	if n := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); n != 0 {
		return n
	}
	return strings.Compare(a.ItemID, b.ItemID)
}
