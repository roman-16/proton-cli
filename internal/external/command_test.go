package external

import (
	"bytes"
	"context"
	"errors"
	"io"
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

func TestRunnerRelaysInteractivePromptsAsTheyArriveAndStillCapturesThem(t *testing.T) {
	r := fixture(t, `
printf 'Password: ' >&2
read -r password
printf 'Two-factor code: '
read -r code
[ "$password" = secret ] && [ "$code" = 123456 ]
`)
	inputReader, inputWriter := io.Pipe()
	terminalReader, terminalWriter := io.Pipe()
	t.Cleanup(func() {
		_ = inputReader.Close()
		_ = inputWriter.Close()
		_ = terminalReader.Close()
		_ = terminalWriter.Close()
	})

	type outcome struct {
		result Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := r.RunInteractive(context.Background(), inputReader, terminalWriter)
		done <- outcome{result: result, err: err}
	}()

	readPrompt := func(want string) {
		t.Helper()
		got := make([]byte, len(want))
		read := make(chan error, 1)
		go func() {
			_, err := io.ReadFull(terminalReader, got)
			read <- err
		}()
		select {
		case err := <-read:
			if err != nil {
				t.Fatalf("read relayed prompt: %v", err)
			}
			if string(got) != want {
				t.Fatalf("relayed prompt = %q, want %q", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("prompt %q was not relayed before the child waited for input", want)
		}
	}

	readPrompt("Password: ")
	if _, err := io.WriteString(inputWriter, "secret\n"); err != nil {
		t.Fatal(err)
	}
	readPrompt("Two-factor code: ")
	if _, err := io.WriteString(inputWriter, "123456\n"); err != nil {
		t.Fatal(err)
	}
	if err := inputWriter.Close(); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.result.Stderr != "Password: " || got.result.Stdout != "Two-factor code: " {
			t.Fatalf("RunInteractive = %#v", got.result)
		}
	case <-time.After(time.Second):
		t.Fatal("interactive child did not finish")
	}
}

func TestRunnerInteractiveRetainsNormalizedFailuresAndTimeouts(t *testing.T) {
	t.Run("exit", func(t *testing.T) {
		r := fixture(t, "printf 'question' >&2; exit 17")
		var terminal bytes.Buffer
		result, err := r.RunInteractive(context.Background(), strings.NewReader(""), &terminal)
		var failed *ExitError
		if !errors.As(err, &failed) || failed.Code != 17 || failed.Stderr != "question" {
			t.Fatalf("RunInteractive error = %#v", err)
		}
		if result.Stderr != "question" || terminal.String() != "question" {
			t.Fatalf("RunInteractive output = (%#v, %q)", result, terminal.String())
		}
	})

	t.Run("timeout", func(t *testing.T) {
		r := fixture(t, "printf 'question' >&2; sleep 5")
		r.Timeout = 20 * time.Millisecond
		var terminal bytes.Buffer
		result, err := r.RunInteractive(context.Background(), strings.NewReader(""), &terminal)
		var timedOut *TimeoutError
		if !errors.As(err, &timedOut) {
			t.Fatalf("RunInteractive error = %T %v", err, err)
		}
		if result.Stderr != "question" || terminal.String() != "question" {
			t.Fatalf("RunInteractive output = (%#v, %q)", result, terminal.String())
		}
	})
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
