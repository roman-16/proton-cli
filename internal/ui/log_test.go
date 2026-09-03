package ui

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// recorded runs one log call through the pair of loggers a UI builds and returns
// what each destination received.
func recorded(t *testing.T, level slog.Level, write func(log, trace *slog.Logger)) (screen string, file []map[string]any) {
	t.Helper()
	var errb, jsonl bytes.Buffer
	log, trace := newLoggers(&errb, level, []byte("a salt to test with"), &jsonl)
	write(log, trace)

	for line := range strings.SplitSeq(strings.TrimSpace(jsonl.String()), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("the log wrote a line that is not JSON: %q", line)
		}
		file = append(file, record)
	}
	return errb.String(), file
}

func TestTheFileGetsEverythingWhateverTheScreenIsSet(t *testing.T) {
	screen, file := recorded(t, slog.LevelWarn, func(log, _ *slog.Logger) {
		log.Debug("api request", "method", "GET", "path", "/core/v4/users", "status", 200)
	})
	if screen != "" {
		t.Errorf("a debug line reached a screen set to warnings: %q", screen)
	}
	if len(file) != 1 {
		t.Fatalf("the file got %d records, want 1", len(file))
	}
	if file[0]["path"] != "/core/v4/users" {
		t.Errorf("the record lost its path: %v", file[0])
	}
}

func TestTheRunsOwnRecordsNeverReachTheScreen(t *testing.T) {
	screen, file := recorded(t, slog.LevelDebug, func(_, trace *slog.Logger) {
		trace.Error("run failed", "run", "a91f", "exit", 7, "error", "something went wrong")
	})
	if screen != "" {
		t.Errorf("the run's own record was printed as well as recorded: %q", screen)
	}
	if len(file) != 1 || file[0]["msg"] != "run failed" {
		t.Errorf("the file did not get the run's outcome: %v", file)
	}
}

func TestAnIDIsRecordedAsAHandle(t *testing.T) {
	const id = "5bH2mQxKT9wLpN4vRs8kZc1yXd7fGh3jAe6bUi0oQm2nWr5tYv=="
	_, file := recorded(t, slog.LevelDebug, func(log, _ *slog.Logger) {
		log.Debug("not shown", "kind", "item", "ref", id)
	})
	if len(file) != 1 {
		t.Fatalf("the file got %d records, want 1", len(file))
	}
	ref, _ := file[0]["ref"].(string)
	if strings.Contains(ref, id[:8]) {
		t.Errorf("the ID was recorded as itself: %q", ref)
	}
	if !strings.HasPrefix(ref, "ref:") {
		t.Errorf("%q is not a handle", ref)
	}
	if file[0]["kind"] != "item" {
		t.Errorf("the kind was not kept: %v", file[0])
	}
}

func TestAnUndeclaredNameIsRefusedRatherThanWritten(t *testing.T) {
	_, file := recorded(t, slog.LevelDebug, func(log, _ *slog.Logger) {
		log.Debug("sending", "subject", "Invoice #2291 is ready")
	})
	if len(file) != 1 {
		t.Fatalf("the file got %d records, want 1", len(file))
	}
	if value, _ := file[0]["subject"].(string); strings.Contains(value, "Invoice") {
		t.Errorf("an undeclared attribute was written: %v", file[0])
	}
}

func TestBothDestinationsAgreeOnAHandle(t *testing.T) {
	const id = "5bH2mQxKT9wLpN4vRs8kZc1yXd7fGh3jAe6bUi0oQm2nWr5tYv=="
	screen, file := recorded(t, slog.LevelDebug, func(log, _ *slog.Logger) {
		log.Warn("not shown", "kind", "item", "ref", id)
	})
	ref, _ := file[0]["ref"].(string)
	if !strings.Contains(screen, ref) {
		t.Errorf("the screen and the file named one thing two ways:\n%s\n%v", screen, file[0])
	}
}

func TestWithNoFileTheScreenStillWorks(t *testing.T) {
	var errb bytes.Buffer
	log, trace := newLoggers(&errb, slog.LevelDebug, nil, nil)
	log.Warn("rate limited by Proton; waiting before trying again", "method", "GET", "wait_ms", 5000)
	trace.Error("run failed", "run", "a91f", "exit", 7)

	if !strings.Contains(errb.String(), "rate limited") {
		t.Errorf("the screen lost its warning: %q", errb.String())
	}
	if strings.Contains(errb.String(), "run failed") {
		t.Errorf("with nowhere to record it, the run's own record was printed instead: %q", errb.String())
	}
}

func TestAnErrorIsRecordedWithoutWhatItPickedUp(t *testing.T) {
	_, file := recorded(t, slog.LevelDebug, func(log, _ *slog.Logger) {
		log.Debug("api request failed", "method", "POST",
			"error", "encrypt for jane.roe@her-own-domain.at: no key")
	})
	value, _ := file[0]["error"].(string)
	for _, leaked := range []string{"jane.roe", "her-own-domain.at"} {
		if strings.Contains(value, leaked) {
			t.Errorf("%q survived into the log: %q", leaked, value)
		}
	}
	if !strings.Contains(value, "no key") {
		t.Errorf("the error lost what it was saying: %q", value)
	}
}
