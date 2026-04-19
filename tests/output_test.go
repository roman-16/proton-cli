package tests

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// ── --output json: field names use json tags (snake_case) ──

func TestOutputJSONMailMessages(t *testing.T) {
	skipIfNoCredentials(t)
	data := runJSON(t, "mail", "messages", "list", "--page-size", "1")
	if _, ok := data["messages"]; !ok {
		t.Fatal("expected 'messages' key (json tag) in output")
	}
	if _, ok := data["total"]; !ok {
		t.Fatal("expected 'total' key")
	}
}

func TestOutputJSONContacts(t *testing.T) {
	skipIfNoCredentials(t)
	contacts := runJSONArray(t, "contacts", "list")
	if len(contacts) == 0 {
		t.Skip("no contacts")
	}
	c := contacts[0].(map[string]interface{})
	// 'id' and 'name' are json-tagged, not 'ID'/'Name'
	if _, ok := c["id"]; !ok {
		t.Errorf("expected 'id' json key; got %v", keysOf(c))
	}
}

// ── --output yaml: respects json tags, uses snake_case ──

func TestOutputYAMLSnakeCase(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "mail", "messages", "list", "--page-size", "1", "--output", "yaml")
	// Expect snake_case keys
	for _, want := range []string{"from_address", "from_name", "num_attachments"} {
		if !strings.Contains(stdout, want+":") {
			t.Errorf("expected YAML key %q, got:\n%s", want, truncateOutput(stdout))
		}
	}
	// And NOT the Go-field lowercased alternatives
	for _, bad := range []string{"fromaddress:", "fromname:", "numattachments:"} {
		if strings.Contains(stdout, bad) {
			t.Errorf("unexpected YAML key %q (indicates yaml lib ignored json tags)", bad)
		}
	}
}

// ── --output yaml: raw api path keeps integers as integers ──

func TestOutputYAMLRawAPIKeepsIntegers(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "--output", "yaml", "api", "GET", "/core/v4/users")
	// Code: 1000 (int) rather than 1000.0
	intRe := regexp.MustCompile(`(?m)^Code:\s+\d+$`)
	floatRe := regexp.MustCompile(`(?m)^Code:\s+\d+\.\d+`)
	if !intRe.MatchString(stdout) {
		t.Errorf("expected integer Code in YAML output, got:\n%s", truncateOutput(stdout))
	}
	if floatRe.MatchString(stdout) {
		t.Error("Code rendered as float; json.Number conversion regressed")
	}
}

// ── --output text (default): human-readable ──

func TestOutputTextIsDefault(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "mail", "messages", "list", "--page-size", "1")
	// Table output has a separator line with ─ chars
	if !strings.Contains(stdout, "─") {
		t.Error("expected table output by default")
	}
	// And NOT a JSON brace
	if strings.HasPrefix(strings.TrimSpace(stdout), "{") {
		t.Error("default output looks like JSON")
	}
}

// ── invalid --output is rejected ──

func TestOutputUnknownFormat(t *testing.T) {
	skipIfNoCredentials(t)
	_, stderr, code := run(t, "--output", "xml", "mail", "messages", "list")
	if code == 0 {
		t.Error("expected non-zero exit for unknown --output")
	}
	_ = stderr
}

// ── JSON output parses as valid JSON across many commands ──

func TestOutputJSONParsesEverywhere(t *testing.T) {
	skipIfNoCredentials(t)
	cases := [][]string{
		{"mail", "messages", "list", "--page-size", "1"},
		{"mail", "labels", "list"},
		{"mail", "addresses", "list"},
		{"contacts", "list"},
		{"calendar", "calendars", "list"},
		{"pass", "vaults", "list"},
	}
	for _, args := range cases {
		stdout := runOK(t, append(args, "--output", "json")...)
		var v any
		if err := json.Unmarshal([]byte(stdout), &v); err != nil {
			t.Errorf("%v: not valid JSON: %v", args, err)
		}
	}
}
