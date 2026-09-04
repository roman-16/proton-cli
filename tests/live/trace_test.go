package live

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// What a run cost, recorded so it can be read rather than guessed at.
//
// The suite's wall clock is spent almost entirely inside invocations of the
// binary, and an invocation's cost is the depth of its request graph. Both are
// facts about a particular run, so both are written down: PROTON_CLI_TEST_TRACE
// names a file to append one line per invocation to, and scripts/testreport turns
// it into the tables worth arguing about.
//
// PROTON_CLI_TEST_TRACE_REQUESTS additionally asks each invocation for its debug
// log and harvests the requests it made, which is what makes the difference
// between "this command is slow" and "this command asks for eight things one
// after another". The log arrives on stderr, so those lines are taken back out
// before a test sees it: a run with tracing on must assert exactly what a run
// without it does.
const (
	traceVar         = "PROTON_CLI_TEST_TRACE"
	traceRequestsVar = "PROTON_CLI_TEST_TRACE_REQUESTS"
)

var (
	traceMu       sync.Mutex
	traceFile     *os.File
	traceRequests bool
)

// invocation is one run of the binary.
type invocation struct {
	Profile  string       `json:"profile"`
	Args     []string     `json:"args"`
	Ms       int64        `json:"ms"`
	Exit     int          `json:"exit"`
	Requests []apiRequest `json:"requests,omitempty"`
}

// apiRequest is one request that invocation made. FinishedAtMicros and Ms
// together place it on a timeline, which is how a chain of requests is told from
// a fan-out of them.
type apiRequest struct {
	Method           string `json:"method"`
	Path             string `json:"path"`
	Status           int    `json:"status"`
	Ms               int64  `json:"ms"`
	FinishedAtMicros int64  `json:"finished_at_micros"`
}

func openTrace() {
	path := os.Getenv(traceVar)
	if path == "" {
		return
	}
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open the trace file %s: %v\n", path, err)
		os.Exit(1)
	}
	traceFile = f
	traceRequests = os.Getenv(traceRequestsVar) != ""
}

func closeTrace() {
	traceMu.Lock()
	defer traceMu.Unlock()
	if traceFile != nil {
		_ = traceFile.Close()
		traceFile = nil
	}
}

func tracing() bool         { return traceFile != nil }
func tracingRequests() bool { return traceFile != nil && traceRequests }

// trace records one invocation and returns the stderr a test should see.
func trace(profile string, args []string, elapsed time.Duration, exitCode int, stderr string) string {
	if !tracing() {
		return stderr
	}
	var requests []apiRequest
	if traceRequests {
		requests, stderr = harvestRequests(stderr)
	}
	line, err := json.Marshal(invocation{
		Profile:  profile,
		Args:     args,
		Ms:       elapsed.Milliseconds(),
		Exit:     exitCode,
		Requests: requests,
	})
	if err != nil {
		return stderr
	}
	traceMu.Lock()
	defer traceMu.Unlock()
	if traceFile != nil {
		_, _ = traceFile.Write(append(line, '\n'))
	}
	return stderr
}

// harvestRequests takes the request log out of stderr and returns both halves.
func harvestRequests(stderr string) ([]apiRequest, string) {
	var (
		requests []apiRequest
		kept     []string
	)
	for line := range strings.SplitSeq(stderr, "\n") {
		fields, ok := logFields(line)
		if !ok {
			kept = append(kept, line)
			continue
		}
		if fields["msg"] != "api request" {
			continue
		}
		status, _ := strconv.Atoi(fields["status"])
		ms, _ := strconv.ParseInt(fields["duration_ms"], 10, 64)
		finished, err := time.Parse(time.RFC3339Nano, fields["time"])
		if err != nil {
			finished = time.Time{}
		}
		requests = append(requests, apiRequest{
			Method:           fields["method"],
			Path:             fields["path"],
			Status:           status,
			Ms:               ms,
			FinishedAtMicros: finished.UnixMicro(),
		})
	}
	return requests, strings.Join(kept, "\n")
}

// logFields reads one slog text line, or reports that the line is not one.
//
// Anything the binary logs at debug level is the harness's own doing, so every
// such line is removed from what a test sees rather than only the ones parsed
// into requests.
func logFields(line string) (map[string]string, bool) {
	if !strings.HasPrefix(line, "time=") || !strings.Contains(line, " level=DEBUG ") {
		return nil, false
	}
	fields := map[string]string{}
	for rest := line; rest != ""; {
		eq := strings.IndexByte(rest, '=')
		if eq < 0 {
			break
		}
		key := rest[:eq]
		rest = rest[eq+1:]
		var value string
		if strings.HasPrefix(rest, `"`) {
			quoted, err := strconv.QuotedPrefix(rest)
			if err != nil {
				break
			}
			value, _ = strconv.Unquote(quoted)
			rest = rest[len(quoted):]
		} else if sp := strings.IndexByte(rest, ' '); sp >= 0 {
			value, rest = rest[:sp], rest[sp:]
		} else {
			value, rest = rest, ""
		}
		fields[key] = value
		rest = strings.TrimPrefix(rest, " ")
	}
	return fields, true
}
