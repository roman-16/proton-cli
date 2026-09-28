package tidyurl

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

type handlerArgs struct {
	decoded     string
	lastPath    string
	params      params
	fullPath    string
	originalURL string
}

type handlerResult struct {
	url string
	err error
}

type handler func(link string, a handlerArgs) handlerResult

var errHandler = errors.New("handler found no link")

var (
	redditmailTarget = regexp.MustCompile(`(?i)^https://click\.redditmail\.com/CL0/(.*?)/`)
	twitchTarget     = regexp.MustCompile(`^https?://www\.twitch\.tv/r/e/(.*?)/`)
)

var handlers = map[string]handler{
	"patchbot.io": func(_ string, a handlerArgs) handlerResult {
		parts := strings.Split(strings.ReplaceAll(a.decoded, "%3D", "="), "|")
		if len(parts) < 3 {
			return handlerResult{url: "undefined"}
		}
		target, err := decodeURIComponent(parts[2])
		if err != nil {
			return handlerResult{url: a.originalURL, err: err}
		}
		return handlerResult{url: target}
	},
	"urldefense.proofpoint.com": func(_ string, a handlerArgs) handlerResult {
		arg, ok := a.params.get("u")
		if !ok {
			return handlerResult{url: a.originalURL, err: errHandler}
		}
		target, err := decodeURIComponent(strings.ReplaceAll(arg, "-", "%"))
		if err != nil {
			return handlerResult{url: a.originalURL, err: err}
		}
		return handlerResult{url: strings.ReplaceAll(strings.ReplaceAll(target, "_", "/"), "%2F", "/")}
	},
	"stardockentertainment.info": func(link string, _ handlerArgs) handlerResult {
		target := decodeBase64(lastSegment(link))
		return handlerResult{url: strings.Replace(target, "watch>v=", "watch?v=", 1)}
	},
	"steam.gs": func(link string, _ handlerArgs) handlerResult {
		target, _, _ := strings.Cut(link, "%3Eutm_")
		return handlerResult{url: target}
	},
	"0yxjo.mjt.lu": func(link string, _ handlerArgs) handlerResult {
		return handlerResult{url: decodeBase64(lastSegment(link))}
	},
	"click.redditmail.com": func(link string, a handlerArgs) handlerResult {
		found := redditmailTarget.FindStringSubmatch(link)
		if found == nil {
			return handlerResult{url: a.originalURL, err: errHandler}
		}
		target, err := decodeURIComponent(found[1])
		if err != nil {
			return handlerResult{url: a.originalURL, err: err}
		}
		return handlerResult{url: target}
	},
	"deals.dominos.co.nz": func(link string, a handlerArgs) handlerResult {
		target := lastSegment(link)
		if target == "" {
			return handlerResult{url: a.originalURL, err: errHandler}
		}
		return handlerResult{url: decodeBase64(target)}
	},
	"redirectingat.com": func(link string, a handlerArgs) handlerResult {
		const host = "https://go.redirectingat.com/"
		before, after, found := strings.Cut(link, "?id")
		if before != host {
			return handlerResult{url: a.originalURL}
		}
		if !found {
			after = "undefined"
		} else if second := strings.Index(after, "?id"); second >= 0 {
			after = after[:second]
		}
		decoded, err := decodeURIComponent(after)
		if err != nil {
			return handlerResult{url: a.originalURL, err: err}
		}
		corrected, err := parse(host + "?id=" + decoded)
		if err != nil {
			return handlerResult{url: a.originalURL, err: err}
		}
		target, _ := corrected.params().get("url")
		if target == "" {
			return handlerResult{url: a.originalURL, err: errHandler}
		}
		if ok, err := validate(target); err != nil || !ok {
			return handlerResult{url: a.originalURL, err: errHandler}
		}
		return handlerResult{url: target}
	},
	"twitch.tv-email": func(link string, a handlerArgs) handlerResult {
		found := twitchTarget.FindStringSubmatch(link)
		if found == nil {
			return handlerResult{url: a.originalURL, err: errHandler}
		}
		parsed, ok := parseJSONValue(decodeBase64(found[1]))
		if !ok || parsed == nil {
			return handlerResult{url: a.originalURL, err: errHandler}
		}
		object, _ := parsed.(map[string]any)
		if object == nil || object["name"] != "twitch_favorite_up" {
			return handlerResult{url: ""}
		}
		channel, present := object["channel"]
		return handlerResult{url: "https://www.twitch.tv/" + jsString(channel, present)}
	},
}

func lastSegment(s string) string {
	return s[strings.LastIndexByte(s, '/')+1:]
}

func jsString(v any, present bool) string {
	if !present {
		return "undefined"
	}
	switch v := v.(type) {
	case nil:
		return "null"
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case map[string]any:
		return "[object Object]"
	}
	return ""
}
