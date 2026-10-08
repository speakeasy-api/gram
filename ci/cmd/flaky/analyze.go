package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// minDistinctChanges is how many different PRs (or main pushes) must hit the
// same test failure before it becomes a candidate. One PR failing a test is
// usually that PR's bug; the same test failing across unrelated changes is not.
const minDistinctChanges = 2

// scanConcurrency bounds how many runs the analyser fetches at once.
const scanConcurrency = 8

// failedRun is one failed workflow run, reduced to what the analyser needs.
type failedRun struct {
	ID           int64  `json:"id"`
	Event        string `json:"event"`
	HeadBranch   string `json:"head_branch"`
	HTMLURL      string `json:"html_url"`
	PullRequests []struct {
		Number int `json:"number"`
	} `json:"pull_requests"`
}

var queueBranch = regexp.MustCompile(`^gh-readonly-queue/[^/]+/pr-(\d+)-`)

// changeKey names the change a run tested, so a PR's own runs and its merge
// queue run count once. Every other run (a push to main) is its own change.
func (r failedRun) changeKey() string {
	if len(r.PullRequests) > 0 {
		return "pr-" + strconv.Itoa(r.PullRequests[0].Number)
	}
	if m := queueBranch.FindStringSubmatch(r.HeadBranch); m != nil {
		return "pr-" + m[1]
	}
	if r.Event == "pull_request" {
		return "branch-" + r.HeadBranch
	}
	return "run-" + strconv.FormatInt(r.ID, 10)
}

// evidence records, per failing test, the run URLs grouped by change.
type evidence map[testKey]map[string][]string

func (e evidence) add(k testKey, change, runURL string) {
	if e[k] == nil {
		e[k] = map[string][]string{}
	}
	if !slices.Contains(e[k][change], runURL) {
		e[k][change] = append(e[k][change], runURL)
	}
}

// candidates returns the tests that failed across at least minDistinctChanges
// changes, sorted.
func (e evidence) candidates() []testKey {
	var keys []testKey
	for k, changes := range e {
		if len(changes) >= minDistinctChanges {
			keys = append(keys, k)
		}
	}
	return sortedUnique(keys)
}

func (e evidence) describe(k testKey, window time.Duration) string {
	var b strings.Builder
	fmt.Fprintf(&b, "`%s` in `%s` failed in `server-test` across %d different changes in the last %d days, so CI treats it as a flaky-test candidate.\n\n",
		k.Test, k.Package, len(e[k]), int(window.Hours()/24))
	b.WriteString("If it fails again, CI quarantines it: the failing run is allowed to pass and this ticket is handed to the autopilot queue to fix.\n\n")
	b.WriteString("Failing runs:\n")

	changes := make([]string, 0, len(e[k]))
	for c := range e[k] {
		changes = append(changes, c)
	}
	slices.Sort(changes)
	for _, c := range changes {
		for _, u := range e[k][c] {
			fmt.Fprintf(&b, "- %s\n", u)
		}
	}
	return b.String()
}

// errNotFound marks a GitHub 404, e.g. a job log past its retention period.
var errNotFound = errors.New("not found")

type githubClient struct {
	token string
	repo  string
	http  *http.Client
}

func newGitHubClient(token, repo string) *githubClient {
	return &githubClient{token: token, repo: repo, http: &http.Client{Timeout: 2 * time.Minute}}
}

// get retries GitHub's intermittent 5xx responses, which a scan of hundreds of
// runs and logs reliably hits.
func (c *githubClient) get(ctx context.Context, path string) (*http.Response, error) {
	const attempts = 4

	for attempt := 1; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+c.repo+path, nil)
		if err != nil {
			return nil, fmt.Errorf("build github request: %w", err)
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("Authorization", "Bearer "+c.token)

		resp, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("call github %s: %w", path, err)
		}
		if resp.StatusCode == http.StatusOK {
			return resp, nil
		}
		_ = resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("github %s: %w", path, errNotFound)
		}
		if resp.StatusCode < 500 || attempt == attempts {
			return nil, fmt.Errorf("github %s returned status %d", path, resp.StatusCode)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt) * 2 * time.Second):
		}
	}
}

func (c *githubClient) getJSON(ctx context.Context, path string, out any) error {
	resp, err := c.get(ctx, path)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode github %s: %w", path, err)
	}
	return nil
}

func (c *githubClient) failedRuns(ctx context.Context, workflow string, since time.Time) ([]failedRun, error) {
	var runs []failedRun
	for page := 1; ; page++ {
		q := url.Values{
			"status":   {"failure"},
			"created":  {">=" + since.Format("2006-01-02")},
			"per_page": {"100"},
			"page":     {strconv.Itoa(page)},
		}
		var out struct {
			WorkflowRuns []failedRun `json:"workflow_runs"`
		}
		if err := c.getJSON(ctx, "/actions/workflows/"+workflow+"/runs?"+q.Encode(), &out); err != nil {
			return nil, err
		}
		runs = append(runs, out.WorkflowRuns...)
		if len(out.WorkflowRuns) < 100 {
			return runs, nil
		}
	}
}

// failedJobLogs streams the log of every failed job in a run whose name starts
// with jobPrefix.
func (c *githubClient) failedJobLogs(ctx context.Context, runID int64, jobPrefix string, visit func(io.Reader) error) error {
	var out struct {
		Jobs []struct {
			ID         int64  `json:"id"`
			Name       string `json:"name"`
			Conclusion string `json:"conclusion"`
		} `json:"jobs"`
	}
	if err := c.getJSON(ctx, fmt.Sprintf("/actions/runs/%d/jobs?filter=latest&per_page=100", runID), &out); err != nil {
		return err
	}

	for _, job := range out.Jobs {
		if job.Conclusion != "failure" || !strings.HasPrefix(job.Name, jobPrefix) {
			continue
		}
		resp, err := c.get(ctx, fmt.Sprintf("/actions/jobs/%d/logs", job.ID))
		if errors.Is(err, errNotFound) {
			// Expired or deleted logs carry no evidence either way.
			continue
		}
		if err != nil {
			return err
		}
		err = visit(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

type analyzeOptions struct {
	Workflow  string
	Events    []string
	JobPrefix string
	Window    time.Duration
	DryRun    bool
}

// runAnalyze opens a candidate ticket for every test that failed across enough
// changes and is not already tracked.
func runAnalyze(ctx context.Context, gh *githubClient, linear *linearClient, opts analyzeOptions, stdout io.Writer) error {
	runs, err := gh.failedRuns(ctx, opts.Workflow, time.Now().Add(-opts.Window))
	if err != nil {
		return err
	}
	runs = slices.DeleteFunc(runs, func(r failedRun) bool { return !slices.Contains(opts.Events, r.Event) })

	// Most of the time goes to one GitHub round trip per run, so scan a few
	// runs at once.
	var (
		ev       = evidence{}
		mu       sync.Mutex
		firstErr error
		wg       sync.WaitGroup
		slots    = make(chan struct{}, scanConcurrency)
	)
	for _, run := range runs {
		slots <- struct{}{}
		wg.Go(func() {
			defer func() { <-slots }()
			err := gh.failedJobLogs(ctx, run.ID, opts.JobPrefix, func(log io.Reader) error {
				failures, err := parseLogFailures(log)
				if err != nil {
					return err
				}
				mu.Lock()
				defer mu.Unlock()
				for _, k := range failures {
					ev.add(k, run.changeKey(), run.HTMLURL)
				}
				return nil
			})
			if err != nil {
				mu.Lock()
				firstErr = cmp.Or(firstErr, fmt.Errorf("run %d: %w", run.ID, err))
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}

	tracked := map[testKey]flakyIssue{}
	if linear != nil {
		if tracked, err = linear.openFlakyIssues(ctx); err != nil {
			return err
		}
	}

	candidates := ev.candidates()
	fmt.Fprintf(stdout, "scanned %d failed runs: %d failing tests, %d candidates\n", len(runs), len(ev), len(candidates))

	for _, k := range candidates {
		if issue, ok := tracked[k]; ok {
			fmt.Fprintf(stdout, "tracked   %s (%s)\n", k, issue.Identifier)
			continue
		}
		if opts.DryRun || linear == nil {
			fmt.Fprintf(stdout, "candidate %s (%d changes)\n", k, len(ev[k]))
			continue
		}
		id, err := linear.createCandidate(ctx, k, ev.describe(k, opts.Window))
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "created   %s (%s)\n", k, id)
	}
	return nil
}
