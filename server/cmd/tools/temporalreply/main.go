// Command temporalreply benchmarks the experimental Temporal request-reply
// brokers against a local Temporal development server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.temporal.io/sdk/client"
	tlog "go.temporal.io/sdk/log"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/requestreply"
	"github.com/speakeasy-api/gram/server/internal/temporalreply"
)

const (
	taskQueue    = "temporal-requestreply-tool"
	urnNamespace = "benchmark:reply"
)

type loopbackPublisher struct {
	replier requestreply.ReplyBroker[*riskv1.EnforcementReply]
}

func (p *loopbackPublisher) Publish(ctx context.Context, request *riskv1.GitleaksEnforcement) gcp.PublishResult {
	correlationID, err := temporalreply.ParseReplyURN(urnNamespace, request.GetReplyUrn())
	if err != nil {
		return gcp.NewErrPublishResult(err)
	}
	reply := &riskv1.EnforcementReply{}
	reply.SetCorrelationId(correlationID)
	reply.SetScanner(riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_GITLEAKS)
	reply.SetStatus(riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK)
	if err := p.replier.Reply(ctx, request.GetReplyUrn(), reply); err != nil {
		return gcp.NewErrPublishResult(err)
	}
	return gcp.NewSuccessPublishResult()
}

func (p *loopbackPublisher) Stop(context.Context) error {
	return nil
}

type point struct {
	concurrency int
	wall        time.Duration
	roundTrips  []time.Duration
}

func parseConcurrency(value string) ([]int, error) {
	parts := strings.Split(value, ",")
	values := make([]int, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("parse concurrency %q: %w", part, err)
		}
		if value <= 0 {
			return nil, errors.New("concurrency values must be positive")
		}
		values = append(values, value)
	}
	return values, nil
}

func runPoint(
	ctx context.Context,
	broker requestreply.RequestBroker[*riskv1.GitleaksEnforcement, *riskv1.EnforcementReply],
	concurrency int,
	timeout time.Duration,
) (point, error) {
	roundTrips := make([]time.Duration, concurrency)
	errs := make([]error, concurrency)
	start := make(chan struct{})
	var group sync.WaitGroup
	for index := range concurrency {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			requestCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			request := &riskv1.GitleaksEnforcement{}
			request.SetContent("benchmark")
			started := time.Now()
			reply, err := broker.Request(requestCtx, request)
			roundTrips[index] = time.Since(started)
			if err != nil {
				errs[index] = err
				return
			}
			workflowID, err := temporalreply.ParseReplyURN(urnNamespace, request.GetReplyUrn())
			if err != nil {
				errs[index] = err
				return
			}
			if reply.GetCorrelationId() != workflowID {
				errs[index] = fmt.Errorf("reply correlation id %q does not match workflow id %q", reply.GetCorrelationId(), workflowID)
			}
		}()
	}

	started := time.Now()
	close(start)
	group.Wait()
	wall := time.Since(started)
	for index, err := range errs {
		if err != nil {
			return point{}, fmt.Errorf("request %d: %w", index, err)
		}
	}
	return point{concurrency: concurrency, wall: wall, roundTrips: roundTrips}, nil
}

func percentile(sorted []time.Duration, percentile float64) time.Duration {
	return sorted[int(percentile*float64(len(sorted)-1))]
}

func printPoint(result point) {
	sorted := append([]time.Duration(nil), result.roundTrips...)
	sort.Slice(sorted, func(left, right int) bool { return sorted[left] < sorted[right] })
	fmt.Printf(
		"concurrency=%d p50=%s p99=%s max=%s requests/s=%.0f\n",
		result.concurrency,
		percentile(sorted, 0.50),
		percentile(sorted, 0.99),
		sorted[len(sorted)-1],
		float64(result.concurrency)/result.wall.Seconds(),
	)
}

func run() error {
	concurrencyFlag := flag.String("concurrency", "1,10,100", "comma-separated concurrent request counts")
	timeout := flag.Duration("timeout", 5*time.Second, "timeout for each request")
	flag.Parse()
	concurrencies, err := parseConcurrency(*concurrencyFlag)
	if err != nil {
		return err
	}
	if *timeout <= 0 {
		return errors.New("timeout must be positive")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	devServerOptions := testsuite.DevServerOptions{
		ExistingPath: "",
		ClientOptions: &client.Options{
			Namespace: "default",
			Logger:    tlog.NewStructuredLogger(slog.New(slog.DiscardHandler)),
		},
		LogLevel: "error",
		ExtraArgs: []string{
			"--dynamic-config-value", "frontend.rps=100000",
			"--dynamic-config-value", "frontend.namespaceRPS=100000",
			"--dynamic-config-value", "history.rps=100000",
			"--dynamic-config-value", "matching.rps=100000",
		},
	}
	if path, lookupErr := exec.LookPath("temporal"); lookupErr == nil {
		devServerOptions.ExistingPath = path
	}
	devServer, err := testsuite.StartDevServer(ctx, devServerOptions)
	if err != nil {
		return fmt.Errorf("start Temporal development server: %w", err)
	}
	defer o11y.NoLogDefer(devServer.Stop)

	temporalWorker := worker.New(devServer.Client(), taskQueue, worker.Options{
		MaxConcurrentWorkflowTaskPollers: 8,
	})
	temporalWorker.RegisterWorkflowWithOptions(temporalreply.Workflow, workflow.RegisterOptions{
		Name:                          temporalreply.WorkflowName,
		DisableAlreadyRegisteredCheck: false,
	})
	if err := temporalWorker.Start(); err != nil {
		return fmt.Errorf("start Temporal worker: %w", err)
	}
	defer temporalWorker.Stop()

	replier, err := temporalreply.NewReplyBroker[*riskv1.EnforcementReply](devServer.Client(), urnNamespace)
	if err != nil {
		return fmt.Errorf("create reply broker: %w", err)
	}
	publisher := &loopbackPublisher{replier: replier}
	requester, err := temporalreply.NewRequestBroker(
		devServer.Client(),
		publisher,
		&riskv1.EnforcementReply{},
		temporalreply.Config{TaskQueue: taskQueue, URNNamespace: urnNamespace},
	)
	if err != nil {
		return fmt.Errorf("create request broker: %w", err)
	}

	if _, err := runPoint(ctx, requester, 1, *timeout); err != nil {
		return fmt.Errorf("warm up request-reply path: %w", err)
	}
	for _, concurrency := range concurrencies {
		result, err := runPoint(ctx, requester, concurrency, *timeout)
		if err != nil {
			return fmt.Errorf("benchmark concurrency %d: %w", concurrency, err)
		}
		printPoint(result)
	}
	if err := requester.Close(ctx); err != nil {
		return fmt.Errorf("close request broker: %w", err)
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "temporal reply benchmark: %v\n", err)
		os.Exit(1)
	}
}
