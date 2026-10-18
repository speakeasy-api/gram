package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseTimings(t *testing.T) {
	t.Parallel()

	got, err := parseTimings(strings.NewReader(`# comment

example.com/mod/a 1500
  example.com/mod/b   0
`))
	require.NoError(t, err)
	require.Equal(t, map[string]int64{
		"example.com/mod/a": 1500,
		"example.com/mod/b": 0,
	}, got)
}

func TestParseTimings_Invalid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "missing duration", input: "example.com/mod/a\n", wantErr: "line 1: expected"},
		{name: "extra field", input: "example.com/mod/a 1 2\n", wantErr: "line 1: expected"},
		{name: "not a number", input: "# header\nexample.com/mod/a 1.5\n", wantErr: `line 2: "1.5" is not`},
		{name: "negative", input: "example.com/mod/a -1\n", wantErr: `line 1: "-1" is not`},
		{name: "duplicate", input: "example.com/mod/a 1\nexample.com/mod/a 2\n", wantErr: "line 2: example.com/mod/a is listed more than once"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseTimings(strings.NewReader(tc.input))
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestEmbeddedTimings_AreCanonical(t *testing.T) {
	t.Parallel()

	timings, err := loadTimings("")
	require.NoError(t, err)
	require.NotEmpty(t, timings)

	var buf bytes.Buffer
	require.NoError(t, formatTimings(&buf, timings))
	require.Equal(t, string(embeddedTimings), buf.String(),
		"timings.txt must be in the form 'shard timings' writes; regenerate it rather than editing by hand")
}

func TestWeigh_ScalesUnmeasuredPackagesToMeasuredTime(t *testing.T) {
	t.Parallel()

	pkgs := []pkg{
		{ImportPath: "example.com/mod/a", sourceBytes: 1000},
		{ImportPath: "example.com/mod/b", sourceBytes: 3000},
		{ImportPath: "example.com/mod/new", sourceBytes: 500},
		{ImportPath: "example.com/mod/instant", sourceBytes: 200},
	}
	timings := map[string]int64{
		"example.com/mod/a":       10_000,
		"example.com/mod/b":       30_000,
		"example.com/mod/instant": 0,
		"example.com/mod/gone":    99_000,
	}

	measured := weigh(pkgs, timings)
	require.Equal(t, 3, measured)

	weights := make(map[string]int64)
	for _, p := range pkgs {
		weights[p.ImportPath] = p.weight
	}

	// 40s over 4200 bytes of measured packages makes 500 bytes about 4.8s.
	require.Equal(t, map[string]int64{
		"example.com/mod/a":       10_000,
		"example.com/mod/b":       30_000,
		"example.com/mod/new":     4_761,
		"example.com/mod/instant": 1,
	}, weights)
}

func TestWeigh_WithoutMeasurementsUsesSourceBytes(t *testing.T) {
	t.Parallel()

	pkgs := []pkg{
		{ImportPath: "example.com/mod/a", sourceBytes: 1000},
		{ImportPath: "example.com/mod/b", sourceBytes: 0},
	}

	measured := weigh(pkgs, map[string]int64{"example.com/mod/other": 5})
	require.Zero(t, measured)
	require.Equal(t, int64(1000), pkgs[0].weight)
	require.Equal(t, int64(1), pkgs[1].weight, "weights must stay positive")
}

func TestMedian(t *testing.T) {
	t.Parallel()

	require.Equal(t, int64(7), median([]int64{7}))
	require.Equal(t, int64(5), median([]int64{9, 1, 5}))
	require.Equal(t, int64(4), median([]int64{8, 1, 3, 5}))
}

func TestRunTimings(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeReport(t, filepath.Join(dir, "run1", "shard-1", "junit-report.xml"), `<?xml version="1.0" encoding="UTF-8"?>
<testsuites tests="3" failures="1" errors="0" time="20.0">
	<testsuite tests="2" failures="0" time="10.000000" name="example.com/mod/a" timestamp="2026-10-16T10:00:00Z"></testsuite>
	<testsuite tests="1" failures="1" time="99.000000" name="example.com/mod/flaky" timestamp="2026-10-16T10:00:00Z"></testsuite>
	<testsuite tests="1" failures="0" time="0.000000" name="example.com/mod/cached" timestamp="2026-10-16T10:00:00Z"></testsuite>
</testsuites>`)
	writeReport(t, filepath.Join(dir, "run2", "shard-1", "junit-report.xml"), `<?xml version="1.0" encoding="UTF-8"?>
<testsuites>
	<testsuite tests="2" failures="0" time="14.000000" name="example.com/mod/a"></testsuite>
	<testsuite tests="1" failures="0" time="3.250000" name="example.com/mod/flaky"></testsuite>
</testsuites>`)
	writeReport(t, filepath.Join(dir, "run3", "shard-2", "junit-report.xml"), `<?xml version="1.0" encoding="UTF-8"?>
<testsuites>
	<testsuite tests="2" failures="0" time="11.000000" name="example.com/mod/a"></testsuite>
</testsuites>`)
	writeReport(t, filepath.Join(dir, "run3", "shard-2", "test-timings.json"), `{"not": "a junit report"}`)

	out := filepath.Join(t.TempDir(), "timings.txt")
	require.NoError(t, run([]string{"timings", "-o", out, dir}, io.Discard, io.Discard))

	data, err := os.ReadFile(out)
	require.NoError(t, err)

	got, err := parseTimings(bytes.NewReader(data))
	require.NoError(t, err)
	require.Equal(t, map[string]int64{
		"example.com/mod/a":     11_000,
		"example.com/mod/flaky": 3_250,
	}, got)
}

func TestRunTimings_NoReports(t *testing.T) {
	t.Parallel()

	err := run([]string{"timings", t.TempDir()}, io.Discard, io.Discard)
	require.ErrorContains(t, err, "no *.xml reports found")

	err = run([]string{"timings"}, io.Discard, io.Discard)
	require.ErrorContains(t, err, "name at least one JUnit report")
}

func writeReport(t *testing.T, path string, content string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}
