package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// gotestsumJUnit is real gotestsum output: one package failing in TestMain and
// one with a failing subtest, its parent, and a passing test.
const gotestsumJUnit = `<?xml version="1.0" encoding="UTF-8"?>
<testsuites tests="3" failures="3" errors="0" time="0.733376">
	<testsuite tests="0" failures="0" time="0.490000" name="github.com/speakeasy-api/gram/server/internal/crash" timestamp="2026-10-08T14:42:30+01:00">
		<testcase classname="" name="TestMain" time="0.000000">
			<failure message="Failed" type="">exit status 1</failure>
		</testcase>
	</testsuite>
	<testsuite tests="3" failures="2" time="0.733000" name="github.com/speakeasy-api/gram/server/internal/mcp" timestamp="2026-10-08T14:42:30+01:00">
		<testcase classname="github.com/speakeasy-api/gram/server/internal/mcp" name="TestFlaky/sub" time="0.000000">
			<failure message="Failed" type="">boom</failure>
		</testcase>
		<testcase classname="github.com/speakeasy-api/gram/server/internal/mcp" name="TestFlaky" time="0.000000">
			<failure message="Failed" type="">boom</failure>
		</testcase>
		<testcase classname="github.com/speakeasy-api/gram/server/internal/mcp" name="TestPass" time="0.000000"></testcase>
	</testsuite>
</testsuites>`

func TestParseJUnitFailures(t *testing.T) {
	t.Parallel()

	got, crashed, err := parseJUnitFailures(strings.NewReader(gotestsumJUnit))
	require.NoError(t, err)
	require.Equal(t, []testKey{
		{Package: "server/internal/crash", Test: "TestMain"},
		{Package: "server/internal/mcp", Test: "TestFlaky"},
	}, got, "subtests fold into their parent; package-level failures keep their package")
	require.Empty(t, crashed)
}

func TestParseJUnitFailures_Panic(t *testing.T) {
	t.Parallel()

	// Real gotestsum output for a panicking test: only the panicking test is
	// recorded; the package's later tests never ran.
	const report = `<testsuites>
	<testsuite name="github.com/speakeasy-api/gram/server/internal/pan">
		<testcase classname="github.com/speakeasy-api/gram/server/internal/pan" name="TestBoom" time="0.000000">
			<failure message="Failed" type="">=== RUN   TestBoom&#xA;--- FAIL: TestBoom (0.00s)&#xA;panic: boom [recovered, repanicked]&#xA;</failure>
		</testcase>
		<testcase classname="github.com/speakeasy-api/gram/server/internal/pan" name="TestA" time="0.000000"></testcase>
	</testsuite>
</testsuites>`

	got, crashed, err := parseJUnitFailures(strings.NewReader(report))
	require.NoError(t, err)
	require.Equal(t, []testKey{{Package: "server/internal/pan", Test: "TestBoom"}}, got)
	require.Equal(t, []string{"server/internal/pan"}, crashed)
}

func TestParseLogFailures(t *testing.T) {
	t.Parallel()

	log := strings.Join([]string{
		"2026-10-01T20:43:17.6698818Z \x1b[31m✖\x1b[0m  server/internal/mcp (40.157s)",
		"2026-10-01T20:48:31.6253912Z === \x1b[31mFAIL\x1b[0m: server/internal/mcp TestFoo/case_one (1.05s)",
		"2026-10-01T20:48:31.6253912Z === \x1b[31mFAIL\x1b[0m: server/internal/mcp TestFoo (1.05s)",
		"2026-10-01T20:48:31.6253912Z === FAIL: server/internal/xmcp TestBar (0.10s)",
		"2026-10-01T20:48:31.6253912Z === FAIL: server/internal/crash TestMain (0.00s)",
	}, "\n")

	got, err := parseLogFailures(strings.NewReader(log))
	require.NoError(t, err)
	require.Equal(t, []testKey{
		{Package: "server/internal/mcp", Test: "TestFoo"},
		{Package: "server/internal/xmcp", Test: "TestBar"},
	}, got)
}

func TestKeyFromTitleRoundTrips(t *testing.T) {
	t.Parallel()

	k := testKey{Package: "server/internal/mcp", Test: "TestFoo"}
	got, ok := keyFromTitle(issueTitle(k))
	require.True(t, ok)
	require.Equal(t, k, got)

	for _, title := range []string{"bug: something else", "flaky test: server/internal/mcp", "flaky test: a b c"} {
		_, ok := keyFromTitle(title)
		require.False(t, ok, title)
	}
}

func TestDecide(t *testing.T) {
	t.Parallel()

	flakyA := testKey{Package: "server/internal/mcp", Test: "TestA"}
	flakyB := testKey{Package: "server/internal/mcp", Test: "TestB"}
	issues := map[testKey]flakyIssue{
		flakyA: {Identifier: "AGE-1", Key: flakyA},
		flakyB: {Identifier: "AGE-2", Key: flakyB, Quarantined: true},
	}
	cluster := make([]testKey, 0, 6)
	for i := range 6 {
		k := testKey{Package: "server/internal/cluster", Test: "TestCluster" + string(rune('A'+i))}
		issues[k] = flakyIssue{Key: k}
		cluster = append(cluster, k)
	}
	spread := make([]testKey, 0, 4)
	for i := range 4 {
		k := testKey{Package: "server/internal/spread" + string(rune('a'+i)), Test: "TestSpread"}
		issues[k] = flakyIssue{Key: k}
		spread = append(spread, k)
	}

	t.Run("every failure tracked", func(t *testing.T) {
		t.Parallel()
		d := decide([]testKey{flakyA, flakyB}, nil, issues)
		require.Empty(t, d.Blocking)
		require.Len(t, d.Bypass, 2)
	})

	t.Run("an untracked failure blocks", func(t *testing.T) {
		t.Parallel()
		d := decide([]testKey{flakyA, {Package: "server/internal/mcp", Test: "TestReal"}}, nil, issues)
		require.Contains(t, d.Blocking, "not tracked as flaky")
		require.Empty(t, d.Bypass)
	})

	t.Run("a panic blocks even when the test is tracked", func(t *testing.T) {
		t.Parallel()
		d := decide([]testKey{flakyA}, []string{flakyA.Package}, issues)
		require.Contains(t, d.Blocking, "never ran")
	})

	t.Run("a package-level failure blocks", func(t *testing.T) {
		t.Parallel()
		d := decide([]testKey{flakyA, {Package: "server/internal/mcp", Test: "TestMain"}}, nil, issues)
		require.Contains(t, d.Blocking, "outside a single test")
	})

	t.Run("a failed step with no failing test blocks", func(t *testing.T) {
		t.Parallel()
		d := decide(nil, nil, issues)
		require.Contains(t, d.Blocking, "without a failing test")
	})

	t.Run("a cluster of tracked failures in one package passes", func(t *testing.T) {
		t.Parallel()
		d := decide(append(slices.Clone(cluster), flakyA), nil, issues)
		require.Empty(t, d.Blocking)
		require.Len(t, d.Bypass, 7)
	})

	t.Run("tracked failures across too many packages block", func(t *testing.T) {
		t.Parallel()
		d := decide(spread, nil, issues)
		require.Contains(t, d.Blocking, "failed in 4 packages at once")
	})
}

func TestChangeKey(t *testing.T) {
	t.Parallel()

	pr := failedRun{ID: 1, Event: "pull_request", HeadBranch: "feat/x"}
	pr.PullRequests = append(pr.PullRequests, struct {
		Number int `json:"number"`
	}{Number: 42})
	require.Equal(t, "pr-42", pr.changeKey())

	queue := failedRun{ID: 2, Event: "merge_group", HeadBranch: "gh-readonly-queue/main/pr-42-0123abcd"}
	require.Equal(t, "pr-42", queue.changeKey(), "a PR's queue run counts as the same change as the PR")

	push := failedRun{ID: 3, Event: "push", HeadBranch: "main"}
	require.Equal(t, "run-3", push.changeKey())
}

func TestEvidenceCandidates(t *testing.T) {
	t.Parallel()

	flaky := testKey{Package: "server/internal/mcp", Test: "TestFlaky"}
	regression := testKey{Package: "server/internal/mcp", Test: "TestBrokenByOnePR"}

	ev := evidence{}
	ev.add(flaky, "pr-1", "https://example.com/runs/1")
	ev.add(flaky, "pr-2", "https://example.com/runs/2")
	ev.add(regression, "pr-3", "https://example.com/runs/3")
	ev.add(regression, "pr-3", "https://example.com/runs/4")

	require.Equal(t, []testKey{flaky}, ev.candidates(),
		"a test failing repeatedly on one PR is that PR's bug, not a flake")
}

func TestAnnotateEscapesWorkflowCommands(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	annotate(&b, "Title: a, b", "line one\n::error::injected 100%")
	require.Equal(t, "::warning title=Title%3A a%2C b::line one%0A::error::injected 100%25\n", b.String())
}

func TestSplitList(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"merge_group", "push"}, splitList(" merge_group, push ,,"))
	require.Nil(t, splitList(""))
}
