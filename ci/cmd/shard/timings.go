package main

import (
	"bufio"
	"bytes"
	_ "embed"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// embeddedTimings is the checked-in timings file that weights packages when
// -timings is not given. It is embedded so every caller of the tool, in CI and
// locally, weighs packages the same way without having to pass a path.
//
//go:embed timings.txt
var embeddedTimings []byte

const timingsHeader = `# Measured test durations used by ci/cmd/shard to balance CI shards.
# Each line is "<import path> <milliseconds>": the median elapsed time of the
# package across recent successful CI runs. Packages missing here are weighted
# by the size of their test sources instead.
#
# Regenerate from recent main-branch CI artifacts with:
#
#   mise run ci:shard-timings
`

const timingsUsage = `Usage: shard timings [-o file] <junit files or directories...>

Reads gotestsum JUnit reports and prints a timings file: the median elapsed time
of each package across the reports, in milliseconds. Directories are searched
recursively for *.xml files. Packages that failed or reported no elapsed time
(for example, a cached result) are left out of a report's observations.

Flags:
`

// parseTimings reads a timings file: one "<import path> <milliseconds>" pair
// per line. Blank lines and lines starting with "#" are ignored.
func parseTimings(r io.Reader) (map[string]int64, error) {
	timings := make(map[string]int64)

	scanner := bufio.NewScanner(r)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}

		fields := strings.Fields(text)
		if len(fields) != 2 {
			return nil, fmt.Errorf("timings line %d: expected \"<import path> <milliseconds>\", got %q", line, text)
		}

		ms, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || ms < 0 {
			return nil, fmt.Errorf("timings line %d: %q is not a non-negative number of milliseconds", line, fields[1])
		}

		if _, ok := timings[fields[0]]; ok {
			return nil, fmt.Errorf("timings line %d: %s is listed more than once", line, fields[0])
		}

		timings[fields[0]] = ms
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read timings: %w", err)
	}

	return timings, nil
}

// loadTimings reads the timings file at path, or the embedded one when path is
// empty.
func loadTimings(path string) (map[string]int64, error) {
	if path == "" {
		return parseTimings(bytes.NewReader(embeddedTimings))
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open timings: %w", err)
	}
	defer func() { _ = f.Close() }()

	return parseTimings(f)
}

// weigh sets each package's weight to its measured duration in milliseconds.
// A package without a measurement is weighted by its test-source bytes, scaled
// by the ratio of measured milliseconds to bytes across the packages that have
// both, so the two kinds of weight are comparable. With no measured package at
// all, every weight is plain bytes. It returns how many packages were measured.
//
// Every weight is at least 1, which assign relies on.
func weigh(pkgs []pkg, timings map[string]int64) int {
	var measured int
	var knownMs, knownBytes int64
	for _, p := range pkgs {
		if ms, ok := timings[p.ImportPath]; ok {
			measured++
			knownMs += ms
			knownBytes += p.sourceBytes
		}
	}

	for i := range pkgs {
		p := &pkgs[i]

		switch ms, ok := timings[p.ImportPath]; {
		case ok:
			p.weight = ms
		case knownMs > 0 && knownBytes > 0:
			p.weight = p.sourceBytes * knownMs / knownBytes
		default:
			p.weight = p.sourceBytes
		}

		p.weight = max(p.weight, 1)
	}

	return measured
}

// junitReport holds the parts of a gotestsum JUnit report that timings reads.
type junitReport struct {
	// Suites holds one entry per Go package.
	Suites []junitSuite `xml:"testsuite"`
}

// junitSuite is one package's results in a gotestsum JUnit report.
type junitSuite struct {
	// Name is the package's import path.
	Name string `xml:"name,attr"`

	// Time is the package's elapsed time in seconds, e.g. "12.345000".
	Time string `xml:"time,attr"`

	// Failures counts the package's failed tests.
	Failures int `xml:"failures,attr"`

	// Errors counts package-level errors, such as a build failure or a panic.
	Errors int `xml:"errors,attr"`
}

func runTimings(argv []string, stdout, stderr io.Writer) error {
	fset := flag.NewFlagSet("shard timings", flag.ContinueOnError)
	fset.SetOutput(io.Discard)

	out := fset.String("o", "", "file to write the timings to (default: stdout)")

	if err := fset.Parse(argv); err != nil {
		fset.SetOutput(stderr)
		fmt.Fprint(stderr, timingsUsage)
		fset.PrintDefaults()

		if errors.Is(err, flag.ErrHelp) {
			return nil
		}

		return err
	}

	if fset.NArg() == 0 {
		return errors.New("timings: name at least one JUnit report or directory")
	}

	reports, err := findReports(fset.Args())
	if err != nil {
		return err
	}

	if len(reports) == 0 {
		return errors.New("timings: no *.xml reports found")
	}

	observations := make(map[string][]int64)
	for _, path := range reports {
		if err := readReport(path, observations); err != nil {
			return err
		}
	}

	timings := make(map[string]int64, len(observations))
	for name, values := range observations {
		timings[name] = median(values)
	}

	var buf bytes.Buffer
	if err := formatTimings(&buf, timings); err != nil {
		return err
	}

	fmt.Fprintf(stderr, "timings: %d packages from %d reports\n", len(timings), len(reports))

	if *out == "" {
		if _, err := stdout.Write(buf.Bytes()); err != nil {
			return fmt.Errorf("write timings: %w", err)
		}

		return nil
	}

	if err := os.WriteFile(*out, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write timings: %w", err)
	}

	return nil
}

// findReports expands args into a sorted list of report files: files are kept
// as given and directories are searched recursively for *.xml files.
func findReports(args []string) ([]string, error) {
	var reports []string

	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			return nil, fmt.Errorf("stat report path: %w", err)
		}

		if !info.IsDir() {
			reports = append(reports, arg)
			continue
		}

		err = filepath.WalkDir(arg, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if !d.IsDir() && filepath.Ext(path) == ".xml" {
				reports = append(reports, path)
			}

			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("search %s for reports: %w", arg, err)
		}
	}

	slices.Sort(reports)

	return slices.Compact(reports), nil
}

// readReport adds the elapsed milliseconds of each passing package in the JUnit
// report at path to observations.
func readReport(path string, observations map[string][]int64) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read report: %w", err)
	}

	var report junitReport
	if err := xml.Unmarshal(data, &report); err != nil {
		return fmt.Errorf("parse report %s: %w", path, err)
	}

	for _, s := range report.Suites {
		if s.Name == "" || s.Failures > 0 || s.Errors > 0 {
			continue
		}

		seconds, err := strconv.ParseFloat(s.Time, 64)
		if err != nil {
			return fmt.Errorf("report %s: package %s has time %q: %w", path, s.Name, s.Time, err)
		}

		ms := int64(math.Round(seconds * 1000))
		if ms <= 0 {
			continue
		}

		observations[s.Name] = append(observations[s.Name], ms)
	}

	return nil
}

// median returns the middle of values, or the mean of the two middle values
// when there is an even number of them. values must not be empty.
func median(values []int64) int64 {
	sorted := slices.Clone(values)
	slices.Sort(sorted)

	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}

	return (sorted[mid-1] + sorted[mid]) / 2
}

// formatTimings writes timings as a timings file, sorted by import path so a
// refresh produces a readable diff.
func formatTimings(w io.Writer, timings map[string]int64) error {
	names := make([]string, 0, len(timings))
	for name := range timings {
		names = append(names, name)
	}
	slices.Sort(names)

	var buf bytes.Buffer
	buf.WriteString(timingsHeader)
	buf.WriteString("\n")
	for _, name := range names {
		fmt.Fprintf(&buf, "%s %d\n", name, timings[name])
	}

	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("write timings: %w", err)
	}

	return nil
}
