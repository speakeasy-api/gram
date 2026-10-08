// Command flaky keeps known-flaky Go tests from failing CI, and keeps the list
// of them in Linear so they get fixed.
//
// It has two subcommands:
//
//	flaky analyze   scan recent failed merge-queue and main runs; a test that
//	                failed for two or more different changes gets a
//	                "flaky-candidate" ticket.
//	flaky gate      run after a failed test step; pass the job when every
//	                failing test has an open flaky ticket. A failing candidate is
//	                quarantined: relabelled "flaky-quarantined" and handed to the
//	                autopilot assignee and delegate to fix.
//
// Closing a ticket makes its test blocking again. Both subcommands read
// LINEAR_API_KEY; analyze also reads GITHUB_TOKEN and GITHUB_REPOSITORY.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "flaky: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, argv []string, stdout io.Writer) error {
	if len(argv) == 0 {
		return errors.New("usage: flaky <analyze|gate> [flags]")
	}

	fs := flag.NewFlagSet("flaky "+argv[0], flag.ContinueOnError)
	team := fs.String("team", "AGE", "Linear team key that owns flaky-test tickets")

	var linear *linearClient
	setup := func() error {
		if err := fs.Parse(argv[1:]); err != nil {
			return err
		}
		if key := os.Getenv("LINEAR_API_KEY"); key != "" {
			linear = newLinearClient(key, *team)
		}
		return nil
	}

	switch argv[0] {
	case "analyze":
		workflow := fs.String("workflow", "pr.yaml", "workflow file whose failed runs are scanned")
		events := fs.String("events", "merge_group,push", "comma-separated run events to scan; these runs test code that already passed PR checks")
		jobPrefix := fs.String("job-prefix", "server-test", "only failed jobs whose name starts with this are scanned")
		days := fs.Int("days", 14, "how many days of runs to scan")
		dryRun := fs.Bool("dry-run", false, "print candidates instead of creating tickets")
		if err := setup(); err != nil {
			return err
		}

		token, repo := os.Getenv("GITHUB_TOKEN"), os.Getenv("GITHUB_REPOSITORY")
		if token == "" || repo == "" {
			return errors.New("GITHUB_TOKEN and GITHUB_REPOSITORY must be set")
		}
		if linear == nil && !*dryRun {
			return errors.New("LINEAR_API_KEY must be set unless -dry-run is given")
		}

		return runAnalyze(ctx, newGitHubClient(token, repo), linear, analyzeOptions{
			Workflow:  *workflow,
			Events:    strings.Split(*events, ","),
			JobPrefix: *jobPrefix,
			Window:    time.Duration(*days) * 24 * time.Hour,
			DryRun:    *dryRun,
		}, stdout)

	case "gate":
		junit := fs.String("junit", "junit-report.xml", "gotestsum junit report of the failed run")
		runURL := fs.String("run-url", "", "link to the CI run, noted on the ticket")
		assignee := fs.String("assignee", "Forge Bot", "Linear user who owns a quarantined ticket")
		delegate := fs.String("delegate", "Cursor", "Linear agent that works a quarantined ticket")
		labels := fs.String("labels", "autopilot,https://github.com/speakeasy-api/gram", "comma-separated labels added to a quarantined ticket")
		if err := setup(); err != nil {
			return err
		}

		return runGate(ctx, linear, gateOptions{
			JUnitPath: *junit,
			RunURL:    *runURL,
			Quarantine: quarantineConfig{
				Assignee: *assignee,
				Delegate: *delegate,
				Labels:   strings.Split(*labels, ","),
			},
		}, stdout)

	default:
		return fmt.Errorf("unknown subcommand %q: want analyze or gate", argv[0])
	}
}
