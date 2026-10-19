// Command testlogs turns the logs of a `bazel test` run of Go tests into the
// `go test -json` event stream that gotestsum reads, so the JUnit report CI
// produces keeps the shape the flaky-test gate (ci/cmd/flaky) and the shard
// timings (ci/cmd/shard) were written against: one suite per package, named by
// its import path, timed by how long the package took.
//
// Usage:
//
//	go run ./ci/cmd/testlogs -bep bep.json -o events.json [-rerun-report rerun-report.txt] [-coverprofile cover.out]
//
// bep.json is the file `bazel test --build_event_json_file` writes. Tests must
// run with GO_TEST_WRAP_TESTV=1 in their environment, which makes the rules_go
// test wrapper log every test with test2json framing rather than only the
// failures. Each log goes through `go tool test2json`, and the package's final
// pass or fail event gets the duration Bazel measured, since test2json can only
// time its own conversion.
//
// With -coverprofile, the Go coverage profiles that `bazel coverage` wrote
// next to each test log (cover_format=go_cover, see .bazelrc) are merged into
// one, without generated code under gen/.
//
// A target that failed and then passed a retry (--flaky_test_attempts) is
// reported by its last attempt, so it passes; -rerun-report lists the tests
// that failed on an earlier attempt.
package main

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// modulePath prefixes a Bazel package to make the Go import path. Every Go
// package in the module lives in the Bazel package of the same directory.
const modulePath = "github.com/speakeasy-api/gram"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "testlogs: %v\n", err)
		os.Exit(1)
	}
}

func run(argv []string) error {
	fs := flag.NewFlagSet("testlogs", flag.ContinueOnError)
	bepPath := fs.String("bep", "", "build event JSON file from bazel test --build_event_json_file")
	outPath := fs.String("o", "", "file to write go test -json events to (default stdout)")
	rerunPath := fs.String("rerun-report", "", "file to list tests that only passed on a retry")
	coverPath := fs.String("coverprofile", "", "file to merge the tests' Go coverage profiles into")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *bepPath == "" {
		return errors.New("-bep is required")
	}

	attempts, err := readAttempts(*bepPath)
	if err != nil {
		return err
	}

	out := io.Writer(os.Stdout)
	if *outPath != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			return fmt.Errorf("create %s: %w", *outPath, err)
		}
		defer func() { _ = f.Close() }()
		out = f
	}

	var reruns []string
	var profiles []string
	for _, label := range sortedKeys(attempts) {
		runs := attempts[label]
		pkg, err := importPath(label)
		if err != nil {
			return err
		}

		last := runs[len(runs)-1]
		profiles = append(profiles, filepath.Join(filepath.Dir(last.LogPath), "coverage.dat"))
		events, err := convert(last, pkg)
		if err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
		if _, err := out.Write(events); err != nil {
			return fmt.Errorf("write events: %w", err)
		}

		if last.passed() {
			for _, earlier := range runs[:len(runs)-1] {
				if earlier.passed() {
					continue
				}
				failed, err := failedTests(earlier, pkg)
				if err != nil {
					return fmt.Errorf("%s attempt %d: %w", label, earlier.Attempt, err)
				}
				for _, name := range failed {
					reruns = append(reruns, fmt.Sprintf("%s.%s: failed attempt %d of %d", pkg, name, earlier.Attempt, len(runs)))
				}
			}
		}
	}

	if *rerunPath != "" {
		body := strings.Join(reruns, "\n")
		if body != "" {
			body += "\n"
		}
		if err := os.WriteFile(*rerunPath, []byte(body), 0o600); err != nil {
			return fmt.Errorf("write %s: %w", *rerunPath, err)
		}
	}

	if *coverPath != "" {
		if err := mergeCoverage(*coverPath, profiles); err != nil {
			return err
		}
	}
	return nil
}

// mergeCoverage concatenates Go coverage profiles under a single mode line,
// dropping generated code. Every profile of one run shares the mode Bazel
// instrumented with. A test that wrote no profile is skipped.
func mergeCoverage(outPath string, profiles []string) error {
	var mode string
	var body bytes.Buffer
	for _, path := range profiles {
		raw, err := os.ReadFile(path) // #nosec G304 -- reads profiles next to test logs named by the build events.
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read coverage profile: %w", err)
		}
		for line := range strings.Lines(string(raw)) {
			switch {
			case strings.HasPrefix(line, "mode: "):
				mode = line
			case strings.Contains(line, "/gen/"):
			default:
				body.WriteString(line)
			}
		}
	}
	if mode == "" {
		return errors.New("no coverage profiles found; run with bazel coverage")
	}
	if err := os.WriteFile(outPath, append([]byte(mode), body.Bytes()...), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", outPath, err)
	}
	return nil
}

// attempt is one execution of a test target, as reported in the build events.
type attempt struct {
	Label    string
	Attempt  int
	Status   string
	LogPath  string
	Duration time.Duration
}

func (a attempt) passed() bool { return a.Status == "PASSED" }

// buildEvent holds the parts of a build event that describe a test result.
type buildEvent struct {
	ID struct {
		TestResult *struct {
			Label   string `json:"label"`
			Run     int    `json:"run"`
			Shard   int    `json:"shard"`
			Attempt int    `json:"attempt"`
		} `json:"testResult"`
	} `json:"id"`
	TestResult *struct {
		Status           string `json:"status"`
		TestAttemptMs    string `json:"testAttemptDurationMillis"`
		TestAttemptDur   string `json:"testAttemptDuration"`
		TestActionOutput []struct {
			Name string `json:"name"`
			URI  string `json:"uri"`
		} `json:"testActionOutput"`
	} `json:"testResult"`
}

// readAttempts returns the attempts of each test target in the build events,
// oldest first.
func readAttempts(path string) (map[string][]attempt, error) {
	f, err := os.Open(path) // #nosec G304 -- reads the build event file named on the command line.
	if err != nil {
		return nil, fmt.Errorf("open build events: %w", err)
	}
	defer func() { _ = f.Close() }()

	attempts := make(map[string][]attempt)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for scanner.Scan() {
		var ev buildEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			return nil, fmt.Errorf("decode build event: %w", err)
		}
		id := ev.ID.TestResult
		if id == nil || ev.TestResult == nil {
			continue
		}
		if id.Run > 1 || id.Shard > 1 {
			return nil, fmt.Errorf("%s: runs_per_test and shard_count are not supported", id.Label)
		}

		a := attempt{Label: id.Label, Attempt: id.Attempt, Status: ev.TestResult.Status}
		for _, output := range ev.TestResult.TestActionOutput {
			if output.Name != "test.log" {
				continue
			}
			u, err := url.Parse(output.URI)
			if err != nil || u.Scheme != "file" {
				return nil, fmt.Errorf("%s: test log is not a local file: %s", id.Label, output.URI)
			}
			a.LogPath = u.Path
		}
		if a.LogPath == "" {
			return nil, fmt.Errorf("%s: no test log in build events", id.Label)
		}
		a.Duration, err = attemptDuration(ev.TestResult.TestAttemptDur, ev.TestResult.TestAttemptMs)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", id.Label, err)
		}
		attempts[id.Label] = append(attempts[id.Label], a)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read build events: %w", err)
	}

	for _, runs := range attempts {
		slices.SortFunc(runs, func(a, b attempt) int { return cmp.Compare(a.Attempt, b.Attempt) })
	}
	return attempts, nil
}

// attemptDuration reads whichever of the two duration fields Bazel filled in:
// the protobuf Duration ("1.5s") or the older milliseconds count.
func attemptDuration(duration string, millis string) (time.Duration, error) {
	if duration != "" {
		d, err := time.ParseDuration(duration)
		if err != nil {
			return 0, fmt.Errorf("parse test duration %q: %w", duration, err)
		}
		return d, nil
	}
	if millis != "" {
		ms, err := strconv.ParseInt(millis, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse test duration %q: %w", millis, err)
		}
		return time.Duration(ms) * time.Millisecond, nil
	}
	return 0, nil
}

// importPath maps a test label such as //server/internal/oops:oops_test to the
// import path of the package it tests.
func importPath(label string) (string, error) {
	pkg, _, ok := strings.Cut(strings.TrimPrefix(label, "//"), ":")
	if !ok || strings.HasPrefix(label, "@") {
		return "", fmt.Errorf("unexpected test label %q", label)
	}
	if pkg == "" {
		return modulePath, nil
	}
	return modulePath + "/" + pkg, nil
}

// event holds the test2json fields failedTests reads.
type event struct {
	Action string
	Test   string
}

// convert runs an attempt's log through test2json and stamps the package's
// final event with the attempt's duration.
func convert(a attempt, pkg string) ([]byte, error) {
	raw, err := test2json(a.LogPath, pkg)
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	lines := bytes.SplitSeq(bytes.TrimRight(raw, "\n"), []byte("\n"))
	for line := range lines {
		if len(line) == 0 {
			continue
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(line, &fields); err != nil {
			return nil, fmt.Errorf("decode test2json event: %w", err)
		}
		_, hasTest := fields["Test"]
		var action string
		_ = json.Unmarshal(fields["Action"], &action)
		if !hasTest && (action == "pass" || action == "fail" || action == "skip") {
			elapsed, err := json.Marshal(a.Duration.Seconds())
			if err != nil {
				return nil, fmt.Errorf("encode elapsed: %w", err)
			}
			fields["Elapsed"] = elapsed
			line, err = json.Marshal(fields)
			if err != nil {
				return nil, fmt.Errorf("encode test2json event: %w", err)
			}
		}
		out.Write(line)
		out.WriteByte('\n')
	}
	return out.Bytes(), nil
}

// failedTests returns the top-level tests that failed in an attempt.
func failedTests(a attempt, pkg string) ([]string, error) {
	raw, err := test2json(a.LogPath, pkg)
	if err != nil {
		return nil, err
	}
	var failed []string
	for line := range bytes.SplitSeq(raw, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var ev event
		if err := json.Unmarshal(line, &ev); err != nil {
			return nil, fmt.Errorf("decode test2json event: %w", err)
		}
		if ev.Action == "fail" && ev.Test != "" && !strings.Contains(ev.Test, "/") {
			failed = append(failed, ev.Test)
		}
	}
	return failed, nil
}

func test2json(logPath string, pkg string) ([]byte, error) {
	log, err := os.Open(logPath) // #nosec G304 -- reads a test log named by the build events.
	if err != nil {
		return nil, fmt.Errorf("open test log: %w", err)
	}
	defer func() { _ = log.Close() }()

	var stdout, stderr bytes.Buffer
	cmd := exec.Command("go", "tool", "test2json", "-t", "-p", pkg)
	cmd.Stdin = log
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go tool test2json: %w: %s", err, stderr.String())
	}
	return stdout.Bytes(), nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
