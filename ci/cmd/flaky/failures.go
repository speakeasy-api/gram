package main

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
)

// modulePrefix is stripped from junit package names so junit reports and
// gotestsum's console output name a package the same way.
const modulePrefix = "github.com/speakeasy-api/gram/"

// testKey identifies a top-level Go test, e.g. {"server/internal/mcp", "TestFoo"}.
// Subtests fold into their parent: quarantine works on whole tests.
type testKey struct {
	Package string
	Test    string
}

func (k testKey) String() string {
	return k.Package + " " + k.Test
}

// newTestKey normalises a package path and a (possibly nested) test name.
func newTestKey(pkg, test string) testKey {
	top, _, _ := strings.Cut(test, "/")
	return testKey{Package: strings.TrimPrefix(pkg, modulePrefix), Test: top}
}

// packageLevel reports whether a failure belongs to the package rather than a
// single test: a build failure, a TestMain failure, or a crash outside a test.
// These are never flaky-test failures.
func (k testKey) packageLevel() bool {
	return k.Test == "" || k.Test == "TestMain"
}

type junitReport struct {
	Suites []struct {
		Name  string `xml:"name,attr"`
		Cases []struct {
			Classname string `xml:"classname,attr"`
			Name      string `xml:"name,attr"`
			Failure   *struct {
				Text string `xml:",chardata"`
			} `xml:"failure"`
			Error *struct {
				Text string `xml:",chardata"`
			} `xml:"error"`
		} `xml:"testcase"`
	} `xml:"testsuite"`
}

// parseJUnitFailures returns the distinct failing tests in a gotestsum junit
// report, sorted, and the packages whose test binary panicked. A panic ends the
// binary, so the package's remaining tests never ran and are absent from the
// report.
func parseJUnitFailures(r io.Reader) (failures []testKey, crashed []string, err error) {
	var report junitReport
	if err := xml.NewDecoder(r).Decode(&report); err != nil {
		return nil, nil, fmt.Errorf("decode junit report: %w", err)
	}

	var failed []failure
	for _, suite := range report.Suites {
		for _, c := range suite.Cases {
			if c.Failure == nil && c.Error == nil {
				continue
			}
			// A package-level failure (TestMain, a crash) has no classname.
			pkg := c.Classname
			if pkg == "" {
				pkg = suite.Name
			}
			k := newTestKey(pkg, c.Name)

			output := ""
			if c.Failure != nil {
				output += c.Failure.Text
			}
			if c.Error != nil {
				output += c.Error.Text
			}
			failed = append(failed, failure{Key: k, Output: output})
			if strings.HasPrefix(output, "panic: ") || strings.Contains(output, "\npanic: ") {
				crashed = append(crashed, k.Package)
			}
		}
	}

	slices.Sort(crashed)
	return attributeRaces(failed), slices.Compact(crashed), nil
}

// failure is one failing test and the output recorded for it.
type failure struct {
	Key    testKey
	Output string
}

var (
	// A race report sits between a "WARNING: DATA RACE" line and a line of
	// equals signs, and lists the stacks of the racing goroutines.
	raceReport = regexp.MustCompile(`(?s)WARNING: DATA RACE\n(.*?)\n==================`)
	// A stack frame in test code, e.g.
	// "github.com/speakeasy-api/gram/server/internal/mcp_test.TestFoo.func1()".
	raceTestFrame = regexp.MustCompile(`(github\.com/speakeasy-api/gram/\S+?)\.(Test\w+)[.(]`)
	// The line Go adds to every test that was running when a race was found.
	raceVerdict = regexp.MustCompile(`(?m)^\s*testing\.go:\d+: race detected during execution of test$`)
	// A failure a test reports itself, e.g. "    foo_test.go:12: boom".
	reportedLine = regexp.MustCompile(`(?m)^\s*\S+\.go:\d+: `)
)

// attributeRaces returns the distinct failing tests, sorted, with each data race
// blamed on the tests it names. When the race detector fires, Go fails every
// test running at that moment, so one racy test fails a whole parallel package.
// A failure whose only complaint is the race is dropped, and the tests whose
// code appears in the race report take its place. A race that names no test
// leaves its package's failures as they are.
func attributeRaces(failed []failure) []testKey {
	racers := map[string][]testKey{}
	for _, f := range failed {
		for _, report := range raceReport.FindAllStringSubmatch(f.Output, -1) {
			for _, m := range raceTestFrame.FindAllStringSubmatch(report[1], -1) {
				k := newTestKey(strings.TrimSuffix(m[1], "_test"), m[2])
				if !k.packageLevel() {
					racers[k.Package] = append(racers[k.Package], k)
				}
			}
		}
	}

	var keys []testKey
	for _, f := range failed {
		bystander := raceVerdict.MatchString(f.Output) &&
			!reportedLine.MatchString(raceVerdict.ReplaceAllString(f.Output, ""))
		if bystander && len(racers[f.Key.Package]) > 0 {
			continue
		}
		keys = append(keys, f.Key)
	}
	for _, ks := range racers {
		keys = append(keys, ks...)
	}
	return sortedUnique(keys)
}

var (
	ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	// gotestsum's summary prints one line per failure, e.g.
	// "=== FAIL: server/internal/mcp TestFoo/bar (1.05s)".
	failLine = regexp.MustCompile(`=== FAIL: (\S+) (\S+) \(`)
	// GitHub prefixes every job log line with its time.
	logTimestamp = regexp.MustCompile(`^\d{4}-\d\d-\d\dT[\d:.]+Z `)
)

// parseLogFailures returns the distinct failing tests named in a job log
// produced by gotestsum, sorted, with data races blamed as attributeRaces does.
// The lines after each failure line, up to the next, are that test's output.
// Package-level failures are dropped.
func parseLogFailures(r io.Reader) ([]testKey, error) {
	var failed []failure
	var output strings.Builder
	flush := func() {
		if n := len(failed); n > 0 {
			failed[n-1].Output = output.String()
		}
		output.Reset()
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := logTimestamp.ReplaceAllString(ansiEscape.ReplaceAllString(scanner.Text(), ""), "")
		m := failLine.FindStringSubmatch(line)
		if m == nil {
			output.WriteString(line + "\n")
			continue
		}
		flush()
		failed = append(failed, failure{Key: newTestKey(m[1], m[2])})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read job log: %w", err)
	}
	flush()

	keys := attributeRaces(failed)
	return slices.DeleteFunc(keys, testKey.packageLevel), nil
}

func sortedUnique(keys []testKey) []testKey {
	slices.SortFunc(keys, func(a, b testKey) int { return strings.Compare(a.String(), b.String()) })
	return slices.Compact(keys)
}
