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

	var keys []testKey
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
			keys = append(keys, k)

			output := ""
			if c.Failure != nil {
				output += c.Failure.Text
			}
			if c.Error != nil {
				output += c.Error.Text
			}
			if strings.HasPrefix(output, "panic: ") || strings.Contains(output, "\npanic: ") {
				crashed = append(crashed, k.Package)
			}
		}
	}

	slices.Sort(crashed)
	return sortedUnique(keys), slices.Compact(crashed), nil
}

var (
	ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	// gotestsum's summary prints one line per failure, e.g.
	// "=== FAIL: server/internal/mcp TestFoo/bar (1.05s)".
	failLine = regexp.MustCompile(`=== FAIL: (\S+) (\S+) \(`)
)

// parseLogFailures returns the distinct failing tests named in a job log
// produced by gotestsum, sorted. Package-level failures are dropped.
func parseLogFailures(r io.Reader) ([]testKey, error) {
	var keys []testKey

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		m := failLine.FindStringSubmatch(ansiEscape.ReplaceAllString(scanner.Text(), ""))
		if m == nil {
			continue
		}
		if k := newTestKey(m[1], m[2]); !k.packageLevel() {
			keys = append(keys, k)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read job log: %w", err)
	}

	return sortedUnique(keys), nil
}

func sortedUnique(keys []testKey) []testKey {
	slices.SortFunc(keys, func(a, b testKey) int { return strings.Compare(a.String(), b.String()) })
	return slices.Compact(keys)
}
