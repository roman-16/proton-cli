package tidyurl

import (
	"errors"
	"net/netip"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/idna"
)

var errInvalidURL = errors.New("invalid URL")

// link is an http or https URL as the WHATWG URL standard parses it, which is
// how the web client that TidyURL runs in sees every link.
type link struct {
	scheme   string
	username string
	password string
	host     string
	port     string
	path     []string
	query    *string
	fragment *string
}

func parse(input string) (*link, error) {
	s := strings.TrimFunc(input, func(r rune) bool { return r <= ' ' })
	s = strings.NewReplacer("\t", "", "\n", "", "\r", "").Replace(s)

	colon := strings.IndexByte(s, ':')
	if colon < 1 || !isAlpha(s[0]) {
		return nil, errInvalidURL
	}
	for i := 1; i < colon; i++ {
		c := s[i]
		if !isAlpha(c) && !isDigit(c) && c != '+' && c != '-' && c != '.' {
			return nil, errInvalidURL
		}
	}
	u := &link{scheme: strings.ToLower(s[:colon])}
	if u.scheme != "http" && u.scheme != "https" {
		return nil, errInvalidURL
	}

	rest := strings.TrimLeft(s[colon+1:], `/\`)
	end := strings.IndexAny(rest, `/\?#`)
	if end < 0 {
		end = len(rest)
	}
	authority := rest[:end]
	rest = rest[end:]

	if at := strings.LastIndexByte(authority, '@'); at >= 0 {
		userinfo := authority[:at]
		authority = authority[at+1:]
		if authority == "" {
			return nil, errInvalidURL
		}
		name, secret, _ := strings.Cut(userinfo, ":")
		u.username = percentEncode(name, inUserinfoSet)
		u.password = percentEncode(secret, inUserinfoSet)
	}

	hostText, portText := splitPort(authority)
	if hostText == "" {
		return nil, errInvalidURL
	}
	host, err := parseHost(hostText)
	if err != nil {
		return nil, err
	}
	u.host = host
	if portText != nil {
		port, err := parsePort(*portText, u.scheme)
		if err != nil {
			return nil, err
		}
		u.port = port
	}

	pathText := rest
	if i := strings.IndexAny(rest, "?#"); i >= 0 {
		pathText = rest[:i]
		rest = rest[i:]
	} else {
		rest = ""
	}
	u.path = parsePath(pathText)

	if strings.HasPrefix(rest, "?") {
		query := rest[1:]
		if i := strings.IndexByte(query, '#'); i >= 0 {
			rest = query[i:]
			query = query[:i]
		} else {
			rest = ""
		}
		encoded := percentEncode(query, inSpecialQuerySet)
		u.query = &encoded
	}
	if strings.HasPrefix(rest, "#") {
		encoded := percentEncode(rest[1:], inFragmentSet)
		u.fragment = &encoded
	}
	return u, nil
}

func splitPort(authority string) (string, *string) {
	inBrackets := false
	for i := 0; i < len(authority); i++ {
		switch authority[i] {
		case '[':
			inBrackets = true
		case ']':
			inBrackets = false
		case ':':
			if !inBrackets {
				port := authority[i+1:]
				return authority[:i], &port
			}
		}
	}
	return authority, nil
}

func parsePort(text, scheme string) (string, error) {
	if text == "" {
		return "", nil
	}
	for i := 0; i < len(text); i++ {
		if !isDigit(text[i]) {
			return "", errInvalidURL
		}
	}
	trimmed := strings.TrimLeft(text, "0")
	if len(trimmed) > 5 {
		return "", errInvalidURL
	}
	port, _ := strconv.Atoi("0" + trimmed)
	if port > 65535 {
		return "", errInvalidURL
	}
	if (scheme == "http" && port == 80) || (scheme == "https" && port == 443) {
		return "", nil
	}
	return strconv.Itoa(port), nil
}

var hostProfile = idna.New(
	idna.MapForLookup(),
	idna.BidiRule(),
	idna.Transitional(false),
	idna.StrictDomainName(false),
	idna.CheckHyphens(false),
	idna.CheckJoiners(true),
	idna.VerifyDNSLength(false),
)

func parseHost(input string) (string, error) {
	if strings.HasPrefix(input, "[") {
		if !strings.HasSuffix(input, "]") {
			return "", errInvalidURL
		}
		addr, err := netip.ParseAddr(input[1 : len(input)-1])
		if err != nil || !addr.Is6() || addr.Zone() != "" {
			return "", errInvalidURL
		}
		return "[" + serializeIPv6(addr.As16()) + "]", nil
	}
	domain := percentDecode(input)
	ascii, err := domainToASCII(domain)
	if err != nil {
		return "", err
	}
	for _, r := range ascii {
		if isForbiddenDomainRune(r) {
			return "", errInvalidURL
		}
	}
	if endsInNumber(ascii) {
		return parseIPv4(ascii)
	}
	return ascii, nil
}

func serializeIPv6(address [16]byte) string {
	var pieces [8]uint16
	for i := range pieces {
		pieces[i] = uint16(address[2*i])<<8 | uint16(address[2*i+1])
	}
	zeroStart, zeroLen := -1, 1
	for i := 0; i < len(pieces); {
		if pieces[i] != 0 {
			i++
			continue
		}
		j := i
		for j < len(pieces) && pieces[j] == 0 {
			j++
		}
		if j-i > zeroLen {
			zeroStart, zeroLen = i, j-i
		}
		i = j
	}
	var b strings.Builder
	for i := 0; i < len(pieces); i++ {
		if i == zeroStart {
			if i == 0 {
				b.WriteByte(':')
			}
			b.WriteByte(':')
			i += zeroLen - 1
			continue
		}
		b.WriteString(strconv.FormatUint(uint64(pieces[i]), 16))
		if i < len(pieces)-1 {
			b.WriteByte(':')
		}
	}
	return b.String()
}

func domainToASCII(domain string) (string, error) {
	fast := true
	for i := 0; i < len(domain); i++ {
		if domain[i] >= utf8.RuneSelf {
			fast = false
			break
		}
	}
	if fast {
		for label := range strings.SplitSeq(domain, ".") {
			if len(label) >= 4 && strings.EqualFold(label[:4], "xn--") {
				fast = false
				break
			}
		}
	}
	if fast {
		return strings.ToLower(domain), nil
	}
	ascii, err := hostProfile.ToASCII(domain)
	if err != nil || ascii == "" {
		return "", errInvalidURL
	}
	return ascii, nil
}

func isForbiddenDomainRune(r rune) bool {
	if r <= 0x1F || r == 0x7F {
		return true
	}
	return strings.ContainsRune(" #%/:<>?@[\\]^|", r)
}

func endsInNumber(host string) bool {
	parts := strings.Split(host, ".")
	if parts[len(parts)-1] == "" {
		if len(parts) == 1 {
			return false
		}
		parts = parts[:len(parts)-1]
	}
	last := parts[len(parts)-1]
	if last != "" && strings.Trim(last, "0123456789") == "" {
		return true
	}
	_, ok := parseIPv4Number(last)
	return ok
}

func parseIPv4(host string) (string, error) {
	parts := strings.Split(host, ".")
	if parts[len(parts)-1] == "" && len(parts) > 1 {
		parts = parts[:len(parts)-1]
	}
	if len(parts) > 4 {
		return "", errInvalidURL
	}
	numbers := make([]uint64, len(parts))
	for i, part := range parts {
		n, ok := parseIPv4Number(part)
		if !ok {
			return "", errInvalidURL
		}
		numbers[i] = n
	}
	for _, n := range numbers[:len(numbers)-1] {
		if n > 255 {
			return "", errInvalidURL
		}
	}
	last := numbers[len(numbers)-1]
	if last >= 1<<(8*(5-len(numbers))) {
		return "", errInvalidURL
	}
	address := last
	for i, n := range numbers[:len(numbers)-1] {
		address += n << (8 * (3 - i))
	}
	return strconv.FormatUint(address>>24, 10) + "." + strconv.FormatUint(address>>16&0xFF, 10) + "." +
		strconv.FormatUint(address>>8&0xFF, 10) + "." + strconv.FormatUint(address&0xFF, 10), nil
}

func parseIPv4Number(s string) (uint64, bool) {
	if s == "" {
		return 0, false
	}
	base := 10
	switch {
	case len(s) >= 2 && (s[:2] == "0x" || s[:2] == "0X"):
		base, s = 16, s[2:]
	case len(s) >= 2 && s[0] == '0':
		base, s = 8, s[1:]
	}
	if s == "" {
		return 0, true
	}
	n, err := strconv.ParseUint(s, base, 64)
	if err != nil {
		var numErr *strconv.NumError
		if errors.As(err, &numErr) && errors.Is(numErr.Err, strconv.ErrRange) {
			return 1 << 40, true
		}
		return 0, false
	}
	return n, true
}

func parsePath(text string) []string {
	if text != "" && (text[0] == '/' || text[0] == '\\') {
		text = text[1:]
	}
	segments := splitKeepingEmpty(text)
	var path []string
	for i, segment := range segments {
		last := i == len(segments)-1
		switch {
		case isDoubleDot(segment):
			if len(path) > 0 {
				path = path[:len(path)-1]
			}
			if last {
				path = append(path, "")
			}
		case isSingleDot(segment):
			if last {
				path = append(path, "")
			}
		default:
			path = append(path, percentEncode(segment, inPathSet))
		}
	}
	return path
}

func splitKeepingEmpty(text string) []string {
	var segments []string
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '/' || text[i] == '\\' {
			segments = append(segments, text[start:i])
			start = i + 1
		}
	}
	return append(segments, text[start:])
}

func isSingleDot(s string) bool {
	return s == "." || strings.EqualFold(s, "%2e")
}

func isDoubleDot(s string) bool {
	switch strings.ToLower(s) {
	case "..", ".%2e", "%2e.", "%2e%2e":
		return true
	}
	return false
}

func (u *link) protocol() string { return u.scheme + ":" }

func (u *link) hostname() string {
	if u.port == "" {
		return u.host
	}
	return u.host + ":" + u.port
}

func (u *link) pathname() string { return "/" + strings.Join(u.path, "/") }

func (u *link) search() string {
	if u.query == nil || *u.query == "" {
		return ""
	}
	return "?" + *u.query
}

func (u *link) hash() string {
	if u.fragment == nil || *u.fragment == "" {
		return ""
	}
	return "#" + *u.fragment
}

func (u *link) href() string {
	var b strings.Builder
	b.WriteString(u.scheme + "://")
	if u.username != "" || u.password != "" {
		b.WriteString(u.username)
		if u.password != "" {
			b.WriteString(":" + u.password)
		}
		b.WriteByte('@')
	}
	b.WriteString(u.hostname())
	b.WriteString(u.pathname())
	if u.query != nil {
		b.WriteString("?" + *u.query)
	}
	if u.fragment != nil {
		b.WriteString("#" + *u.fragment)
	}
	return b.String()
}

func (u *link) params() params {
	if u.query == nil {
		return nil
	}
	return parseParams(*u.query)
}

func (u *link) setParams(p params) {
	serialized := p.String()
	if serialized == "" {
		u.query = nil
		return
	}
	u.query = &serialized
}

func inC0ControlSet(b byte) bool { return b < 0x20 || b > 0x7E }

func inFragmentSet(b byte) bool {
	return inC0ControlSet(b) || strings.IndexByte(" \"<>`", b) >= 0
}

func inQuerySet(b byte) bool {
	return inC0ControlSet(b) || strings.IndexByte(" \"#<>", b) >= 0
}

func inSpecialQuerySet(b byte) bool { return inQuerySet(b) || b == '\'' }

func inPathSet(b byte) bool {
	return inQuerySet(b) || strings.IndexByte("?^`{}", b) >= 0
}

func inUserinfoSet(b byte) bool {
	return inPathSet(b) || strings.IndexByte("/:;=@[\\]|", b) >= 0
}

func percentEncode(s string, encode func(byte) bool) string {
	var b strings.Builder
	for _, r := range s {
		var buf [utf8.UTFMax]byte
		n := utf8.EncodeRune(buf[:], r)
		for _, c := range buf[:n] {
			if encode(c) {
				b.WriteByte('%')
				b.WriteByte(upperHex[c>>4])
				b.WriteByte(upperHex[c&0xF])
			} else {
				b.WriteByte(c)
			}
		}
	}
	return b.String()
}

const upperHex = "0123456789ABCDEF"

func percentDecode(s string) string {
	var out []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			out = append(out, unhex(s[i+1])<<4|unhex(s[i+2]))
			i += 2
			continue
		}
		out = append(out, s[i])
	}
	return strings.ToValidUTF8(string(out), "\uFFFD")
}

func isAlpha(c byte) bool { return c|0x20 >= 'a' && c|0x20 <= 'z' }

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isHex(c byte) bool { return isDigit(c) || (c|0x20 >= 'a' && c|0x20 <= 'f') }

func unhex(c byte) byte {
	if isDigit(c) {
		return c - '0'
	}
	return c | 0x20 - 'a' + 10
}
