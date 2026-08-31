package external

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunnerCapturesSeparatedOutput(t *testing.T) {
	r := fixture(t, "printf 'answer\\n'; printf 'progress\\n' >&2")
	got, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Stdout != "answer\n" || got.Stderr != "progress\n" {
		t.Fatalf("Run = %#v", got)
	}
}

func TestRunnerCanAttachInputWithoutRelayingOutput(t *testing.T) {
	r := fixture(t, "read -r answer; printf 'got:%s\\n' \"$answer\"")
	got, err := r.RunWithInput(context.Background(), strings.NewReader("secret\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Stdout != "got:secret\n" {
		t.Fatalf("RunWithInput = %#v", got)
	}
}

func TestRunnerReportsMissingExecutable(t *testing.T) {
	r := Runner{Name: "missing", LookPath: func(string) (string, error) {
		return "", errors.New("missing")
	}}
	_, err := r.Run(context.Background())
	var missing *MissingError
	if !errors.As(err, &missing) || missing.Name != "missing" {
		t.Fatalf("Run error = %T %v", err, err)
	}
}

func TestRunnerNormalizesExitFailure(t *testing.T) {
	r := fixture(t, "printf '  authentication   failed\\n' >&2; exit 17")
	_, err := r.Run(context.Background())
	var failed *ExitError
	if !errors.As(err, &failed) || failed.Code != 17 || failed.Stderr != "authentication failed" {
		t.Fatalf("Run error = %#v", err)
	}
}

func TestRunnerTimesOut(t *testing.T) {
	r := fixture(t, "sleep 5")
	r.Timeout = 20 * time.Millisecond
	_, err := r.Run(context.Background())
	var timedOut *TimeoutError
	if !errors.As(err, &timedOut) {
		t.Fatalf("Run error = %T %v", err, err)
	}
}

func TestRunnerPreservesCallerCancellation(t *testing.T) {
	r := fixture(t, "sleep 5")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := r.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %T %v", err, err)
	}
}

func TestDecodeJSONAllowsAdditiveVersionChanges(t *testing.T) {
	type status struct {
		Connected bool `json:"connected"`
	}
	got, err := DecodeJSON[status](Result{Stdout: `{"connected":true,"added_later":"ok"}`})
	if err != nil || !got.Connected {
		t.Fatalf("DecodeJSON = (%#v, %v)", got, err)
	}
}

func TestDecodeJSONRejectsMalformedAndMultipleDocuments(t *testing.T) {
	for _, input := range []string{`{"connected":`, "{}\n{}"} {
		if _, err := DecodeJSON[map[string]any](Result{Stdout: input}); err == nil {
			t.Fatalf("DecodeJSON(%q) succeeded", input)
		}
	}
}

func fixture(t *testing.T, body string) Runner {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtures are Unix-only")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return Runner{Name: "fixture", LookPath: func(string) (string, error) { return path, nil }}
}
