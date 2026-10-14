package gram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/guardian"
)

const (
	smokeConcurrency = 4
	maxSmokeRuns     = 10000
)

type smokeProvider interface {
	codemode.Provider
	Shutdown(context.Context) error
}

// smokeHost answers tool callbacks without reaching any upstream.
type smokeHost struct{}

func (smokeHost) Servers(context.Context, codemode.PageArgs) (*codemode.ServerPage, error) {
	return &codemode.ServerPage{Items: []codemode.Server{{Slug: "smoke", Name: "Smoke"}}, NextCursor: ""}, nil
}

func (smokeHost) Search(context.Context, codemode.SearchArgs) (*codemode.SearchPage, error) {
	return &codemode.SearchPage{Items: []codemode.Candidate{}, NextCursor: "", Incomplete: false, FailedMembers: nil}, nil
}

func (smokeHost) Describe(context.Context, string) (*codemode.Description, error) {
	return nil, errors.New("smoke host has no tools to describe")
}

func (smokeHost) Call(_ context.Context, _ string, arguments json.RawMessage) (*codemode.ToolResult, error) {
	return &codemode.ToolResult{Outcome: "completed", Warnings: nil, OK: true, Data: arguments, Content: json.RawMessage(`[]`), Error: ""}, nil
}

type smokeCheck struct {
	name  string
	code  string
	check func(*codemode.ExecutionResult) error
}

var smokeChecks = []smokeCheck{
	{name: "evaluates an expression", code: "1 + 1", check: wantValue(`2`)},
	{name: "captures printed output", code: "print(\"hello\")\n\"ok\"", check: func(r *codemode.ExecutionResult) error {
		if r.Output != "hello\n" {
			return fmt.Errorf("output %q, want %q", r.Output, "hello\n")
		}
		return wantValue(`"ok"`)(r)
	}},
	{name: "round-trips a tool call", code: "page = await tools.servers()\nresult = await tools.call(page[\"items\"][0][\"slug\"] + \"--echo\", {\"n\": 7})\nresult[\"data\"][\"n\"]", check: func(r *codemode.ExecutionResult) error {
		if len(r.ToolCalls) != 1 || r.ToolCalls[0].Outcome != "completed" {
			return fmt.Errorf("tool calls %+v, want one completed call", r.ToolCalls)
		}
		return wantValue(`7`)(r)
	}},
	{name: "keeps no state between runs", code: "page", check: func(r *codemode.ExecutionResult) error {
		if r.Error == nil {
			return errors.New("a name from an earlier run resolved")
		}
		return nil
	}},
	{name: "denies OS calls", code: "import os\nos.getenv(\"HOME\")", check: func(r *codemode.ExecutionResult) error {
		if r.Error == nil || !strings.Contains(r.Error.Message, "'os.getenv' is not supported in this environment") {
			return fmt.Errorf("os.getenv was not denied by the runtime: %+v", r.Error)
		}
		return nil
	}},
}

func wantValue(want string) func(*codemode.ExecutionResult) error {
	return func(r *codemode.ExecutionResult) error {
		if r.Error != nil {
			return fmt.Errorf("%s: %s", r.Error.Code, r.Error.Message)
		}
		if string(r.Value) != want {
			return fmt.Errorf("value %s, want %s", r.Value, want)
		}
		return nil
	}
}

func newCodeRuntimeSmokeCommand() *cli.Command {
	flags := append([]cli.Flag{
		&cli.StringFlag{Name: "environment", Usage: "The current server environment", Required: true, EnvVars: []string{"GRAM_ENVIRONMENT"}},
		&cli.IntFlag{Name: "runs", Usage: "Warm runs to time after the checks (0 to 10000)", Value: 20},
		&cli.DurationFlag{Name: "hold", Usage: "Keep the runner claimed this long after the checks so its pod can be probed", Value: 0},
		&cli.BoolFlag{Name: "release-on-eof", Usage: "End the hold early when stdin closes", Value: false},
	}, codeRuntimeFlags()...)
	return &cli.Command{
		Name:  "code-runtime-smoke",
		Usage: "Claim a code runner with this server's configuration, run Python through it and release it",
		Flags: flags,
		Action: func(c *cli.Context) error {
			return runCodeRuntimeSmoke(c, c.App.Writer)
		},
	}
}

func runCodeRuntimeSmoke(c *cli.Context, out io.Writer) (err error) {
	if runs := c.Int("runs"); runs < 0 || runs > maxSmokeRuns {
		return fmt.Errorf("--runs must be between 0 and %d", maxSmokeRuns)
	}
	logger := PullLogger(c.Context)
	policy := guardian.NewDefaultPolicy(noop.NewTracerProvider())
	var provider smokeProvider
	switch c.String("code-runtime-provider") {
	case "gke":
		provider, err = newGKECodeProvider(c, logger, policy)
	case "local":
		policy, err = guardian.NewUnsafePolicy(noop.NewTracerProvider(), []string{})
		if err == nil {
			provider, err = newLocalCodeProvider(c, policy)
		}
	default:
		return fmt.Errorf("code runtime provider %q cannot be smoke tested", c.String("code-runtime-provider"))
	}
	if err != nil {
		return err
	}
	ctx := c.Context
	defer func() {
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if shutdownErr := provider.Shutdown(shutdown); shutdownErr != nil {
			err = errors.Join(err, fmt.Errorf("release runner: %w", shutdownErr))
		}
	}()

	project := uuid.New()
	scope := codemode.Scope{Organization: "code-runtime-smoke", Project: project, Gateway: uuid.New(), Caller: []string{"smoke"}, Session: "smoke", Request: json.RawMessage(`1`)}
	_, _ = fmt.Fprintf(out, "project %s\n", project)
	started := time.Now()
	lease, err := provider.Acquire(ctx, scope)
	if err != nil {
		return fmt.Errorf("claim runner: %w", err)
	}
	defer func() { _ = lease.Release(context.WithoutCancel(ctx)) }()
	_, _ = fmt.Fprintf(out, "claimed runner in %s\n", time.Since(started).Round(time.Millisecond))

	client := lease.Client()
	factory := func(context.Context) (codemode.Host, error) { return smokeHost{}, nil }
	failed := 0
	for _, check := range smokeChecks {
		begin := time.Now()
		err := check.check(client.Run(ctx, uuid.New(), check.code, factory))
		status := "ok  "
		if err != nil {
			status = "FAIL"
			failed++
		}
		_, _ = fmt.Fprintf(out, "%s %-28s %6s", status, check.name, time.Since(begin).Round(time.Millisecond))
		if err != nil {
			_, _ = fmt.Fprintf(out, "  %v", err)
		}
		_, _ = fmt.Fprintln(out)
	}

	if runs := c.Int("runs"); runs > 0 {
		sequential := make([]time.Duration, 0, runs)
		for range runs {
			begin := time.Now()
			if err := wantValue(`1`)(client.Run(ctx, uuid.New(), "1", factory)); err != nil {
				return fmt.Errorf("warm run: %w", err)
			}
			sequential = append(sequential, time.Since(begin))
		}
		_, _ = fmt.Fprintf(out, "warm run      %s\n", latencySummary(sequential))

		var mu sync.Mutex
		var wg sync.WaitGroup
		concurrent := make([]time.Duration, 0, smokeConcurrency)
		errs := make([]error, smokeConcurrency)
		for i := range smokeConcurrency {
			wg.Go(func() {
				begin := time.Now()
				errs[i] = wantValue(`1`)(client.Run(ctx, uuid.New(), "1", factory))
				mu.Lock()
				concurrent = append(concurrent, time.Since(begin))
				mu.Unlock()
			})
		}
		wg.Wait()
		if err := errors.Join(errs...); err != nil {
			return fmt.Errorf("concurrent run: %w", err)
		}
		_, _ = fmt.Fprintf(out, "%d at once     %s\n", smokeConcurrency, latencySummary(concurrent))
	}

	if hold := c.Duration("hold"); hold > 0 {
		eof := make(chan struct{})
		if c.Bool("release-on-eof") {
			go func() {
				_, _ = io.Copy(io.Discard, c.App.Reader)
				close(eof)
			}()
		}
		_, _ = fmt.Fprintf(out, "holding runner for %s\n", hold)
		select {
		case <-time.After(hold):
		case <-eof:
		case <-ctx.Done():
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d checks failed", failed, len(smokeChecks))
	}
	_, _ = fmt.Fprintln(out, "all checks passed")
	return nil
}

func latencySummary(samples []time.Duration) string {
	slices.Sort(samples)
	at := func(q float64) time.Duration {
		return samples[min(len(samples)-1, int(q*float64(len(samples))))].Round(time.Millisecond)
	}
	return fmt.Sprintf("p50 %s  p95 %s  max %s  (n=%d)", at(0.5), at(0.95), samples[len(samples)-1].Round(time.Millisecond), len(samples))
}
