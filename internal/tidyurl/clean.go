// Package tidyurl takes the tracking out of links the way Proton Mail's web
// client does: it is TidyURL, the library the web client cleans links with,
// carried over rule for rule, with the rules it ships.
package tidyurl

import (
	"math"
	"strings"
	"unicode/utf16"
)

// Param is one query parameter taken out of a link.
type Param struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Result is a link with the tracking taken out of it.
type Result struct {
	URL     string
	Removed []Param
}

// Clean reports false for a link that carried no tracking, or that is not an
// http or https URL, and Result.URL is then the link as it was.
func Clean(original string) (Result, bool) {
	unchanged := Result{URL: original}
	if _, err := parse(original); err != nil {
		return unchanged, false
	}
	cleaned, removed, err := clean(original, true)
	if err != nil || strings.EqualFold(original, cleaned) {
		return unchanged, false
	}
	return Result{URL: cleaned, Removed: removed}, true
}

func validate(s string) (bool, error) {
	if _, err := parse(s); err == nil {
		return true, nil
	}
	if s != "undefined" && s != "null" && s != "" {
		return false, errInvalidURL
	}
	return false, nil
}

func clean(original string, reclean bool) (string, []Param, error) {
	valid, err := validate(original)
	if err != nil {
		return "", nil, err
	}
	if !valid {
		return original, nil, nil
	}
	parsed, _ := parse(original)
	if len(parsed.params()) == 0 {
		return original, nil, nil
	}
	rebuilt := parsed.protocol() + "//" + parsed.hostname() + parsed.pathname() + parsed.search() + parsed.hash()
	u, err := parse(rebuilt)
	if err != nil {
		return "", nil, err
	}
	query := u.params()
	lower := query.lowercased()
	pathname := u.pathname()

	var toRemove []string
	var replace []*pattern
	var matched []*rule
	for i := range rules {
		r := &rules[i]
		target := u.hostname()
		if r.MatchHref {
			target = u.href()
		}
		if !r.Match.re.MatchString(target) {
			continue
		}
		toRemove = append(toRemove, r.Rules...)
		for j := range r.Replace {
			replace = append(replace, &r.Replace[j])
		}
		matched = append(matched, r)
	}
	for _, r := range matched {
		for _, exclude := range r.Exclude {
			if exclude.re.MatchString(rebuilt) {
				return original, nil, nil
			}
		}
	}

	var removed []Param
	for _, key := range toRemove {
		if allowed[key] {
			continue
		}
		if value, ok := query.get(key); ok {
			removed = append(removed, Param{Key: key, Value: value})
			query = query.without(key)
		}
	}
	for _, p := range replace {
		pathname = p.removeFrom(pathname)
	}
	if len(removed) > 0 {
		u.setParams(query)
	}
	cleaned := u.protocol() + "//" + u.hostname() + pathname + u.search() + u.hash()

	for _, r := range matched {
		if r.Redirect == "" {
			continue
		}
		value, ok := lower.get(r.Redirect)
		if !ok {
			continue
		}
		if decoded, err := decodeURIComponent(value); err == nil && decoded != value {
			valid, err := validate(decoded)
			if err != nil {
				return "", nil, err
			}
			if valid {
				value = decoded
			}
		}
		valid, err := validate(value)
		if err != nil {
			return "", nil, err
		}
		if !valid {
			continue
		}
		cleaned = value + u.hash()
		if reclean {
			if cleaned, _, err = clean(cleaned, false); err != nil {
				return "", nil, err
			}
		}
	}

	for _, r := range matched {
		if r.Decode == nil {
			continue
		}
		if target, ok := decodeTarget(r.Decode, query, pathname, cleaned, reclean); ok {
			cleaned = target + u.hash()
		}
	}

	if strings.HasSuffix(original, "#") {
		cleaned += "#"
		rebuilt += "#"
	}
	for _, r := range matched {
		if r.Rev {
			cleaned = removeEmptyValues(cleaned)
		}
	}

	if _, err := parse(cleaned); err != nil {
		return "", nil, err
	}
	before, after := utf16Length(rebuilt), utf16Length(cleaned)
	reduction := math.Round((100-float64(after)/float64(before)*100)*100) / 100
	if reduction < 0 || (before == after && reduction == 0) {
		cleaned = original
	}
	return cleaned, removed, nil
}

// decodeTarget is where a rule that decodes a link says it leads, and false when
// it leads nowhere. TidyURL catches whatever goes wrong in one of these and
// moves on to the next rule, so no failure here stops the clean.
func decodeTarget(d *decodeRule, query params, pathname, current string, reclean bool) (string, bool) {
	value, has := query.get(d.Param)
	has = has && d.Param != ""
	if !has && !d.TargetPath {
		return "", false
	}
	lastPath := lastSegment(pathname)
	encoded := lastPath
	if has {
		if value == "" {
			return "", false
		}
		encoded = value
	}
	decoded := decodeAs(encoded, d.Encoding)

	var target string
	if v, ok := parseJSONValue(decoded); ok && isObject(v) {
		object, _ := v.(map[string]any)
		found, isString := object[d.LookFor].(string)
		if !isString {
			return "", false
		}
		target = found
	} else if d.Handler != "" {
		currentURL, err := parse(current)
		if err != nil {
			return "", false
		}
		result := handlers[d.Handler](current, handlerArgs{
			decoded: decoded, lastPath: lastPath, params: currentURL.params(),
			fullPath: pathname, originalURL: current,
		})
		if result.err == nil {
			if _, err := validate(result.url); err != nil {
				return "", false
			}
		}
		target = result.url
	} else {
		target = decoded
	}

	if reclean {
		cleaned, _, err := clean(target, false)
		if err != nil {
			return "", false
		}
		target = cleaned
	}
	if target == "" {
		return "", false
	}
	if valid, err := validate(target); err != nil || !valid {
		return "", false
	}
	return target, true
}

func utf16Length(s string) int { return len(utf16.Encode([]rune(s))) }
