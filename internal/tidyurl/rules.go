package tidyurl

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

//go:embed rules.json
var rulesJSON []byte

type pattern struct {
	re     *regexp.Regexp
	global bool
}

func (p *pattern) UnmarshalJSON(data []byte) error {
	var raw struct{ Source, Flags string }
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var prefix string
	for _, flag := range raw.Flags {
		switch flag {
		case 'i', 'm', 's':
			prefix += string(flag)
		case 'g':
			p.global = true
		default:
			return fmt.Errorf("regular expression flag %q", flag)
		}
	}
	if prefix != "" {
		prefix = "(?" + prefix + ")"
	}
	re, err := regexp.Compile(prefix + raw.Source)
	if err != nil {
		return err
	}
	p.re = re
	return nil
}

func (p *pattern) removeFrom(s string) string {
	if p.global {
		return p.re.ReplaceAllLiteralString(s, "")
	}
	loc := p.re.FindStringIndex(s)
	if loc == nil {
		return s
	}
	return s[:loc[0]] + s[loc[1]:]
}

type decodeRule struct {
	Param      string `json:"param"`
	LookFor    string `json:"look_for"`
	Encoding   string `json:"encoding"`
	TargetPath bool   `json:"target_path"`
	Handler    string `json:"handler"`
}

type rule struct {
	Name      string      `json:"name"`
	Match     pattern     `json:"match"`
	MatchHref bool        `json:"match_href"`
	Rules     []string    `json:"rules"`
	Replace   []pattern   `json:"replace"`
	Exclude   []pattern   `json:"exclude"`
	Redirect  string      `json:"redirect"`
	Decode    *decodeRule `json:"decode"`
	Allow     []string    `json:"allow"`
	Rev       bool        `json:"rev"`
}

// allowed is every rule's allow list at once, whichever rules matched: TidyURL
// gathers them before it matches anything.
var rules, allowed = loadRules()

func loadRules() ([]rule, map[string]bool) {
	var loaded []rule
	if err := json.Unmarshal(rulesJSON, &loaded); err != nil {
		panic("tidyurl: rules.json: " + err.Error())
	}
	allow := map[string]bool{}
	for _, r := range loaded {
		if r.Decode != nil && r.Decode.Handler != "" && handlers[r.Decode.Handler] == nil {
			panic("tidyurl: rules.json names a handler this package does not have: " + r.Decode.Handler)
		}
		if r.Decode != nil && !knownEncodings[r.Decode.Encoding] {
			panic("tidyurl: rules.json names an encoding this package does not decode: " + r.Decode.Encoding)
		}
		for _, name := range r.Allow {
			allow[name] = true
		}
	}
	return loaded, allow
}

func removeEmptyValues(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '=' && (i+1 == len(s) || strings.IndexByte("&\n\r", s[i+1]) >= 0) {
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
