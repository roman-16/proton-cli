package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const documented = `paths:
  /calendar/v1/{calendarID}/events:
    get:
      summary: List events
  /core/v4/addresses:
    get:
      summary: List addresses
  /core/v4/labels:
    get:
      summary: List labels
  /core/v4/users:
    get:
      summary: Read the user
  /drive/shares/{shareID}/files:
    get:
      summary: List files
    post:
      summary: Create a file
  /drive/volumes:
    get:
      summary: List volumes
  /mail/v4/conversations:
    get:
      summary: List conversations
  /mail/v4/messages:
    get:
      summary: List messages
  /pass/v1/share:
    get:
      summary: List shares
  /pass/v1/user/session:
    get:
      summary: Read the session
`

func write(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func readTestSpec(t *testing.T, content string) spec {
	t.Helper()
	parsed, err := readSpec(write(t, "spec.yaml", content))
	if err != nil {
		t.Fatalf("readSpec: %v", err)
	}
	return parsed
}

func TestReadSpec(t *testing.T) {
	parsed := readTestSpec(t, documented)

	if parsed.paths != 10 {
		t.Errorf("paths = %d, want 10", parsed.paths)
	}
	if parsed.lines != 33 {
		t.Errorf("lines = %d, want 33", parsed.lines)
	}
	for _, want := range []string{"GET /core/v4/addresses", "GET /drive/shares/{}/files", "POST /drive/shares/{}/files"} {
		if !parsed.operations[want] {
			t.Errorf("operations missing %q, have %v", want, parsed.operations)
		}
	}
	if len(parsed.operations) != 11 {
		t.Errorf("operations = %d, want 11", len(parsed.operations))
	}
}

func TestReadSent(t *testing.T) {
	sent, err := readSent(write(t, "sent.golden", "GET /core/v4/addresses\nPOST /drive/shares/{id}/files/{n}\n"))
	if err != nil {
		t.Fatalf("readSent: %v", err)
	}

	want := []request{
		{line: "GET /core/v4/addresses", key: "GET /core/v4/addresses"},
		{line: "POST /drive/shares/{id}/files/{n}", key: "POST /drive/shares/{}/files/{}"},
	}
	if len(sent) != len(want) {
		t.Fatalf("read %d requests, want %d", len(sent), len(want))
	}
	for i, r := range sent {
		if r != want[i] {
			t.Errorf("request %d = %+v, want %+v", i, r, want[i])
		}
	}
}

func TestCheck(t *testing.T) {
	tests := []struct {
		name     string
		previous string
		current  string
		sent     string
		want     string
	}{
		{
			name:     "a sync that documents the same requests passes",
			previous: documented,
			current:  documented,
			sent:     "GET /core/v4/addresses\nGET /drive/shares/{id}/files\n",
		},
		{
			name:     "a placeholder spelled differently is the same operation",
			previous: documented,
			current:  strings.ReplaceAll(documented, "{shareID}", "{shareId}"),
			sent:     "GET /drive/shares/{id}/files\n",
		},
		{
			name:     "a request the CLI sends that the sync dropped fails",
			previous: documented,
			current:  strings.ReplaceAll(documented, "  /core/v4/addresses:\n    get:\n      summary: List addresses\n", ""),
			sent:     "GET /core/v4/addresses\nGET /drive/shares/{id}/files\n",
			want:     "no longer documents 1 requests this CLI sends:\n  GET /core/v4/addresses",
		},
		{
			name:     "an endpoint the previous spec never documented is not a regression",
			previous: documented,
			current:  documented,
			sent:     "GET /calendar/v1/{id}/settings\n",
		},
		{
			name:     "a dropped endpoint the CLI never sends is not a regression",
			previous: documented,
			current:  strings.ReplaceAll(documented, "    post:\n      summary: Create a file\n", ""),
			sent:     "GET /core/v4/addresses\n",
		},
		{
			name:     "a spec that lost most of itself fails",
			previous: documented,
			current:  "paths:\n  /core/v4/addresses:\n    get:\n      summary: List addresses\n",
			sent:     "GET /core/v4/addresses\n",
			want:     "shrank by more than 10%",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sent, err := readSent(write(t, "sent.golden", test.sent))
			if err != nil {
				t.Fatalf("readSent: %v", err)
			}

			got := strings.Join(check(readTestSpec(t, test.previous), readTestSpec(t, test.current), sent), "\n")
			if test.want == "" && got != "" {
				t.Fatalf("check reported %q, want nothing", got)
			}
			if test.want != "" && !strings.Contains(got, test.want) {
				t.Errorf("check reported %q, want it to contain %q", got, test.want)
			}
		})
	}
}
