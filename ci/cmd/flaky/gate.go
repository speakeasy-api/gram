package main

import (
	"context"
	"fmt"
	"io"
	"os"
)

// maxBypassedPackages caps how many packages one run may excuse flaky failures
// in. Flaky tests often fail as a cluster from one shared cause (a slow
// container, a shared fixture), so the cap counts packages, not tests. Past it,
// the run is more likely broken than unlucky, so it fails as usual.
const maxBypassedPackages = 3

// gateDecision is the outcome of checking a failed test run against the open
// flaky-test tickets.
type gateDecision struct {
	// Bypass holds the tickets of every failure when the run may pass.
	Bypass []flakyIssue
	// Blocking explains why the run must fail; empty when it may pass.
	Blocking string
}

// decide lets a failed run pass only when every failure is a whole test with an
// open flaky-test ticket and there are few enough of them.
func decide(failures []testKey, issues map[testKey]flakyIssue) gateDecision {
	if len(failures) == 0 {
		return gateDecision{Blocking: "the test step failed without a failing test in the junit report (build failure, timeout, or crash)"}
	}

	var bypass []flakyIssue
	var untracked []testKey
	for _, f := range failures {
		if f.packageLevel() {
			return gateDecision{Blocking: fmt.Sprintf("package %s failed outside a single test", f.Package)}
		}
		issue, ok := issues[f]
		if !ok {
			untracked = append(untracked, f)
			continue
		}
		bypass = append(bypass, issue)
	}

	if len(untracked) > 0 {
		return gateDecision{Blocking: fmt.Sprintf("%d failing test(s) are not tracked as flaky: %v", len(untracked), untracked)}
	}
	packages := map[string]bool{}
	for _, issue := range bypass {
		packages[issue.Key.Package] = true
	}
	if len(packages) > maxBypassedPackages {
		return gateDecision{Blocking: fmt.Sprintf("tracked flaky tests failed in %d packages at once (limit %d): treating the run as broken", len(packages), maxBypassedPackages)}
	}
	return gateDecision{Bypass: bypass}
}

type gateOptions struct {
	JUnitPath  string
	RunURL     string
	Quarantine quarantineConfig
}

// runGate returns nil when the failed run may pass. It promotes every failing
// candidate to quarantined and notes the run on each ticket.
func runGate(ctx context.Context, linear *linearClient, opts gateOptions, stdout io.Writer) error {
	f, err := os.Open(opts.JUnitPath)
	if err != nil {
		return fmt.Errorf("open junit report: %w", err)
	}
	defer func() { _ = f.Close() }()

	failures, err := parseJUnitFailures(f)
	if err != nil {
		return err
	}

	if linear == nil {
		return fmt.Errorf("tests failed and LINEAR_API_KEY is not set, so flaky tests cannot be checked: %v", failures)
	}

	issues, err := linear.openFlakyIssues(ctx)
	if err != nil {
		return err
	}

	decision := decide(failures, issues)
	if decision.Blocking != "" {
		return fmt.Errorf("not bypassing: %s", decision.Blocking)
	}

	for _, issue := range decision.Bypass {
		note := fmt.Sprintf("Failed again in %s. CI let the run pass because this test is tracked as flaky.", opts.RunURL)
		if !issue.Quarantined {
			if err := linear.quarantine(ctx, issue, opts.Quarantine); err != nil {
				return err
			}
			note = fmt.Sprintf("Quarantined: this candidate failed again in %s, so CI let the run pass. Fix the flakiness, then close this ticket to make the test blocking again.", opts.RunURL)
		}
		if err := linear.comment(ctx, issue, note); err != nil {
			return err
		}

		// A workflow annotation keeps the bypass visible on a green job.
		fmt.Fprintf(stdout, "::warning title=Flaky test bypassed::%s failed but is tracked as flaky in %s (%s)\n",
			issue.Key, issue.Identifier, issue.URL)
	}

	return nil
}
