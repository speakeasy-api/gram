// Command enforceload measures the enforcement reply path against local
// Redis and, optionally, the local Pub/Sub emulator.
package main

import (
	"context"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"cloud.google.com/go/pubsub/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/infra/gen"
	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/replyinbox"
	"github.com/speakeasy-api/gram/server/internal/scanners/gitleaks"
)

type config struct {
	mode           string
	concurrency    []int
	expected       int
	findings       int
	scanLatency    time.Duration
	timeout        time.Duration
	sampleInterval time.Duration
	redisAddr      string
	redisPassword  string
	redisPoolSize  int
	pubsubHost     string
	csvPath        string
	pauseBacklog   int
	pauseDuration  time.Duration
	mapProbe       bool
}

type sweepResult struct {
	mode                string
	concurrency         int
	expected            int
	findings            int
	payloadBytes        int
	registration        time.Duration
	duration            time.Duration
	throughput          float64
	p50                 time.Duration
	p90                 time.Duration
	p99                 time.Duration
	max                 time.Duration
	timeouts            int
	errors              int
	orphans             uint64
	drainBatches        uint64
	drainedReplies      uint64
	maxDrainBatch       uint64
	inboxDepthHighWater int64
	redisClientsBase    int64
	redisClientsPeak    int64
	sharedPoolPeak      int64
	drainerPoolPeak     int64
	redisMemoryDelta    int64
	ttlAtRelease        time.Duration
	backlogAtRelease    int64
	catchup             time.Duration
}

type awaitOutcome struct {
	duration time.Duration
	deadline bool
	err      error
}

type sampleStats struct {
	redisClientsBase int64
	redisMemoryBase  int64

	inboxDepth      atomic.Int64
	redisClients    atomic.Int64
	sharedPoolConns atomic.Int64
	drainerConns    atomic.Int64
	redisMemory     atomic.Int64
}

type sampler struct {
	cancel context.CancelFunc
	done   chan struct{}
	stats  *sampleStats
}

type mapProbeResult struct {
	mutexNSPerOp   float64
	syncMapNSPerOp float64
}

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "enforcement load test: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := parseConfig()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger := slog.New(slog.DiscardHandler)
	redisOptions := redis.Options{
		Addr:            cfg.redisAddr,
		Password:        cfg.redisPassword,
		Protocol:        2,
		PoolSize:        cfg.redisPoolSize,
		DisableIdentity: true,
	}
	redisClient := redis.NewClient(&redisOptions)
	defer func() { _ = redisClient.Close() }()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping local redis: %w", err)
	}

	if cfg.mapProbe {
		probe := probeWaiterMaps(5000, 5)
		fmt.Printf("waiter-map probe, 5000 goroutines: mutex %.0f ns/op, sync.Map %.0f ns/op, mutex/sync.Map %.2fx\n",
			probe.mutexNSPerOp, probe.syncMapNSPerOp, probe.mutexNSPerOp/probe.syncMapNSPerOp)
	}

	results := make([]sweepResult, 0, len(cfg.concurrency)+1)
	switch cfg.mode {
	case "reply":
		for _, concurrency := range cfg.concurrency {
			result, runErr := runReplyPoint(ctx, logger, redisClient, redisOptions, cfg, concurrency)
			if runErr != nil {
				return fmt.Errorf("reply-leg concurrency %d: %w", concurrency, runErr)
			}
			results = append(results, result)
			printPoint(result)
		}
		if cfg.pauseBacklog > 0 {
			result, runErr := runPauseProbe(ctx, logger, redisClient, redisOptions, cfg)
			if runErr != nil {
				return fmt.Errorf("drainer pause: %w", runErr)
			}
			results = append(results, result)
			printPoint(result)
		}
	case "full":
		loop, loopErr := newFullLoop(ctx, logger, redisClient, cfg.pubsubHost)
		if loopErr != nil {
			return loopErr
		}
		defer loop.close()
		for _, concurrency := range cfg.concurrency {
			result, runErr := runFullPoint(ctx, logger, loop, redisClient, redisOptions, cfg, concurrency)
			if runErr != nil {
				return fmt.Errorf("full-loop concurrency %d: %w", concurrency, runErr)
			}
			results = append(results, result)
			printPoint(result)
		}
	default:
		return fmt.Errorf("unsupported mode %q", cfg.mode)
	}

	if err := writeCSV(cfg.csvPath, results); err != nil {
		return err
	}
	printSummary(results)
	fmt.Printf("CSV: %s\n", cfg.csvPath)
	return nil
}

func parseConfig() (config, error) {
	mode := flag.String("mode", "reply", "load mode: reply or full")
	concurrencyText := flag.String("concurrency", "10,50,100,500,1000,2500,5000", "comma-separated in-flight scans")
	expected := flag.Int("expected", 1, "replies expected per scan, from 1 to 5")
	findings := flag.Int("findings", 0, "synthetic findings per reply in reply mode")
	scanLatency := flag.Duration("scan-latency", 0, "simulated scanner latency in reply mode")
	timeout := flag.Duration("timeout", 15*time.Second, "deadline for each sweep point")
	sampleInterval := flag.Duration("sample-interval", 5*time.Millisecond, "Redis sampler interval")
	redisAddr := flag.String("redis-addr", os.Getenv("GRAM_REDIS_CACHE_ADDR"), "local Redis address")
	redisPassword := flag.String("redis-password", os.Getenv("GRAM_REDIS_CACHE_PASSWORD"), "local Redis password")
	redisPoolSize := flag.Int("redis-pool-size", 128, "shared reply-writer Redis pool size")
	pubsubHost := flag.String("pubsub-emulator-host", os.Getenv("PUBSUB_EMULATOR_HOST"), "local Pub/Sub emulator host")
	csvPath := flag.String("csv", "/tmp/enforcement-reply-load.csv", "CSV output path")
	pauseBacklog := flag.Int("pause-backlog", 0, "additional reply-mode scan count for the drainer pause probe")
	pauseDuration := flag.Duration("pause-duration", time.Second, "drainer pause after the backlog is written")
	mapProbe := flag.Bool("map-probe", true, "compare mutex map and sync.Map at 5000 concurrent operations")
	flag.Parse()

	concurrency, err := parseConcurrency(*concurrencyText)
	if err != nil {
		return config{}, err
	}
	if *mode != "reply" && *mode != "full" {
		return config{}, fmt.Errorf("mode must be reply or full")
	}
	if *expected < 1 || *expected > 5 {
		return config{}, fmt.Errorf("expected must be from 1 to 5")
	}
	if *mode == "full" && *expected != 1 {
		return config{}, fmt.Errorf("full mode supports exactly one distinct gitleaks lane")
	}
	if *findings < 0 {
		return config{}, fmt.Errorf("findings cannot be negative")
	}
	if *scanLatency < 0 {
		return config{}, fmt.Errorf("scan latency cannot be negative")
	}
	if *pauseBacklog < 0 {
		return config{}, fmt.Errorf("pause backlog cannot be negative")
	}
	if *pauseDuration <= 0 {
		return config{}, fmt.Errorf("pause duration must be positive")
	}
	if *timeout <= 0 || *sampleInterval <= 0 || *redisPoolSize <= 0 {
		return config{}, fmt.Errorf("timeout, sample interval, and Redis pool size must be positive")
	}
	if *redisAddr == "" {
		return config{}, fmt.Errorf("redis address is required")
	}
	if *mode == "full" && *pubsubHost == "" {
		return config{}, fmt.Errorf("pubsub emulator host is required in full mode")
	}
	return config{
		mode:           *mode,
		concurrency:    concurrency,
		expected:       *expected,
		findings:       *findings,
		scanLatency:    *scanLatency,
		timeout:        *timeout,
		sampleInterval: *sampleInterval,
		redisAddr:      *redisAddr,
		redisPassword:  *redisPassword,
		redisPoolSize:  *redisPoolSize,
		pubsubHost:     *pubsubHost,
		csvPath:        *csvPath,
		pauseBacklog:   *pauseBacklog,
		pauseDuration:  *pauseDuration,
		mapProbe:       *mapProbe,
	}, nil
}

func parseConcurrency(value string) ([]int, error) {
	parts := strings.Split(value, ",")
	result := make([]int, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || value <= 0 {
			return nil, fmt.Errorf("invalid concurrency %q", part)
		}
		result = append(result, value)
	}
	return result, nil
}

func runReplyPoint(
	ctx context.Context,
	logger *slog.Logger,
	redisClient *redis.Client,
	redisOptions redis.Options,
	cfg config,
	concurrency int,
) (sweepResult, error) {
	pointCtx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()
	inbox, err := replyinbox.New(pointCtx, logger, otel.GetTracerProvider(), otel.GetMeterProvider(), replyinbox.Config{
		RedisOptions: redisOptions,
		ReplicaID:    "load-" + uuid.NewString(),
		PollInterval: replyinbox.DefaultPollInterval,
		DrainGate:    nil,
	})
	if err != nil {
		return sweepResult{}, fmt.Errorf("create inbox: %w", err)
	}
	defer func() {
		_ = inbox.Close()
		_ = redisClient.Del(context.WithoutCancel(ctx), replyinbox.InboxKey(inbox.ReplicaID())).Err()
	}()

	monitor := startSampler(pointCtx, redisClient, inbox, cfg.sampleInterval)
	writer := replyinbox.NewWriter(redisClient)
	outcomes := make(chan awaitOutcome, concurrency)
	lanes := syntheticLanes(cfg.expected)
	var dispatchUnixNano atomic.Int64
	registrationStarted := time.Now()
	for scan := range concurrency {
		scanID := fmt.Sprintf("reply-%d-%d", concurrency, scan)
		go func() {
			outcome, awaitErr := inbox.Await(pointCtx, scanID, lanes)
			started := time.Unix(0, dispatchUnixNano.Load())
			outcomes <- awaitOutcome{duration: time.Since(started), deadline: outcome.Deadline, err: awaitErr}
		}()
	}
	if err := waitForWaiters(pointCtx, inbox, concurrency); err != nil {
		return sweepResult{}, err
	}
	registration := time.Since(registrationStarted)
	dispatchStarted := time.Now()
	dispatchUnixNano.Store(dispatchStarted.UnixNano())

	var writerErrors atomic.Int64
	var writtenReplies atomic.Uint64
	var scanners sync.WaitGroup
	scanners.Add(concurrency)
	for scan := range concurrency {
		scanID := fmt.Sprintf("reply-%d-%d", concurrency, scan)
		go func() {
			defer scanners.Done()
			if cfg.scanLatency > 0 {
				timer := time.NewTimer(cfg.scanLatency)
				select {
				case <-pointCtx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
			for replyIndex, lane := range lanes {
				reply := syntheticReply(scanID, lane, cfg.findings, replyIndex)
				if writeErr := writer.Write(pointCtx, inbox.URN(scanID), reply); writeErr != nil {
					writerErrors.Add(1)
					return
				}
				writtenReplies.Add(1)
			}
		}()
	}

	latencies := make([]time.Duration, 0, concurrency)
	timeouts := 0
	awaitErrors := 0
	for range concurrency {
		outcome := <-outcomes
		if outcome.deadline {
			timeouts++
		} else if outcome.err == nil {
			latencies = append(latencies, outcome.duration)
		} else {
			awaitErrors++
		}
	}
	scanners.Wait()
	duration := time.Since(dispatchStarted)
	waitForDrainAccounting(pointCtx, inbox, writtenReplies.Load())
	monitor.stop()
	snapshot := inbox.Snapshot()
	samples := monitor.stats
	payloadBytes := proto.Size(syntheticReply("payload-sample", lanes[0], cfg.findings, 0))
	result := buildResult("reply", concurrency, cfg.expected, cfg.findings, payloadBytes, registration, duration, latencies)
	result.timeouts = timeouts
	result.errors = awaitErrors + int(writerErrors.Load())
	populateRedisStats(&result, snapshot, samples)
	return result, nil
}

func runPauseProbe(
	ctx context.Context,
	logger *slog.Logger,
	redisClient *redis.Client,
	redisOptions redis.Options,
	cfg config,
) (sweepResult, error) {
	pointCtx, cancel := context.WithTimeout(ctx, cfg.timeout+cfg.pauseDuration)
	defer cancel()
	drainGate := make(chan struct{})
	inbox, err := replyinbox.New(pointCtx, logger, otel.GetTracerProvider(), otel.GetMeterProvider(), replyinbox.Config{
		RedisOptions: redisOptions,
		ReplicaID:    "load-pause-" + uuid.NewString(),
		PollInterval: replyinbox.DefaultPollInterval,
		DrainGate:    drainGate,
	})
	if err != nil {
		return sweepResult{}, fmt.Errorf("create inbox: %w", err)
	}
	defer func() {
		select {
		case <-drainGate:
		default:
			close(drainGate)
		}
		_ = inbox.Close()
		_ = redisClient.Del(context.WithoutCancel(ctx), replyinbox.InboxKey(inbox.ReplicaID())).Err()
	}()

	monitor := startSampler(pointCtx, redisClient, inbox, cfg.sampleInterval)
	writer := replyinbox.NewWriter(redisClient)
	outcomes := make(chan awaitOutcome, cfg.pauseBacklog)
	lane := syntheticLanes(1)[0]
	var dispatchUnixNano atomic.Int64
	registrationStarted := time.Now()
	for scan := range cfg.pauseBacklog {
		scanID := fmt.Sprintf("pause-%d", scan)
		go func() {
			outcome, awaitErr := inbox.Await(pointCtx, scanID, []replyinbox.Lane{lane})
			started := time.Unix(0, dispatchUnixNano.Load())
			outcomes <- awaitOutcome{duration: time.Since(started), deadline: outcome.Deadline, err: awaitErr}
		}()
	}
	if err := waitForWaiters(pointCtx, inbox, cfg.pauseBacklog); err != nil {
		return sweepResult{}, err
	}
	registration := time.Since(registrationStarted)
	dispatchStarted := time.Now()
	dispatchUnixNano.Store(dispatchStarted.UnixNano())
	var writers sync.WaitGroup
	var writerErrors atomic.Int64
	var writtenReplies atomic.Uint64
	writers.Add(cfg.pauseBacklog)
	for scan := range cfg.pauseBacklog {
		scanID := fmt.Sprintf("pause-%d", scan)
		go func() {
			defer writers.Done()
			if writeErr := writer.Write(pointCtx, inbox.URN(scanID), syntheticReply(scanID, lane, cfg.findings, 0)); writeErr != nil {
				writerErrors.Add(1)
				return
			}
			writtenReplies.Add(1)
		}()
	}
	writers.Wait()
	backlogAtRelease, backlogErr := redisClient.LLen(pointCtx, replyinbox.InboxKey(inbox.ReplicaID())).Result()
	if backlogErr != nil {
		return sweepResult{}, fmt.Errorf("read paused inbox depth: %w", backlogErr)
	}
	ttl, ttlErr := redisClient.TTL(pointCtx, replyinbox.InboxKey(inbox.ReplicaID())).Result()
	if ttlErr != nil {
		return sweepResult{}, fmt.Errorf("read paused inbox TTL: %w", ttlErr)
	}
	if cfg.pauseDuration > 0 {
		timer := time.NewTimer(cfg.pauseDuration)
		select {
		case <-pointCtx.Done():
			timer.Stop()
			return sweepResult{}, fmt.Errorf("hold drainer pause: %w", pointCtx.Err())
		case <-timer.C:
		}
	}
	close(drainGate)
	releasedAt := time.Now()

	latencies := make([]time.Duration, 0, cfg.pauseBacklog)
	timeouts := 0
	awaitErrors := 0
	for range cfg.pauseBacklog {
		outcome := <-outcomes
		if outcome.deadline {
			timeouts++
		} else if outcome.err == nil {
			latencies = append(latencies, outcome.duration)
		} else {
			awaitErrors++
		}
	}
	duration := time.Since(dispatchStarted)
	waitForDrainAccounting(pointCtx, inbox, writtenReplies.Load())
	monitor.stop()
	snapshot := inbox.Snapshot()
	result := buildResult("pause", cfg.pauseBacklog, 1, cfg.findings, proto.Size(syntheticReply("payload-sample", lane, cfg.findings, 0)), registration, duration, latencies)
	result.timeouts = timeouts
	result.errors = awaitErrors + int(writerErrors.Load())
	result.ttlAtRelease = ttl - cfg.pauseDuration
	result.backlogAtRelease = backlogAtRelease
	result.catchup = time.Since(releasedAt)
	populateRedisStats(&result, snapshot, monitor.stats)
	return result, nil
}

type fullLoop struct {
	broker      *gcp.EmulatedPubSubBroker
	receiveStop context.CancelFunc
	receiveDone chan error
	pubsub      *pubsub.Client
}

func newFullLoop(ctx context.Context, logger *slog.Logger, redisClient *redis.Client, emulatorHost string) (*fullLoop, error) {
	projectID := fmt.Sprintf("gram-enforcement-load-%d", time.Now().UnixNano())
	client, err := pubsub.NewClient(ctx, projectID, option.WithEndpoint(emulatorHost), option.WithoutAuthentication())
	if err != nil {
		return nil, fmt.Errorf("create Pub/Sub emulator client: %w", err)
	}
	broker := gcp.NewEmulatedPubSub(logger, projectID, client, gen.Descriptors)
	fingerprinter, err := risk.ParsePepperKeyRing([]byte(os.Getenv("GRAM_RISK_FINGERPRINT_PEPPER_KEYRING")))
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("parse risk fingerprint pepper keyring: %w", err)
	}
	handler, err := gitleaks.NewEnforceHandler(
		logger,
		otel.GetMeterProvider(),
		replyinbox.NewWriter(redisClient),
		func(tenantID string, message []byte) (string, error) {
			sum, _, fingerprintErr := fingerprinter.TenantedHS256(tenantID, message)
			return risk.EncodeFingerprint(sum), fingerprintErr
		},
		gitleaks.EnforceHandlerConfig{MaxRequestAge: gitleaks.DefaultMaxRequestAge},
	)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("create gitleaks enforcement handler: %w", err)
	}
	subscriber, err := gcp.PubSubSubscriberForMessage(ctx, broker, &riskv1.GitleaksEnforcement{}, &riskv1.GitleaksEnforcer{})
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("create gitleaks subscriber: %w", err)
	}
	receiveCtx, receiveStop := context.WithCancel(ctx)
	receiveDone := make(chan error, 1)
	go func() {
		receiveDone <- subscriber.Receive(receiveCtx, handler.Handle)
	}()
	return &fullLoop{broker: broker, receiveStop: receiveStop, receiveDone: receiveDone, pubsub: client}, nil
}

func (l *fullLoop) close() {
	l.receiveStop()
	if err := <-l.receiveDone; err != nil && !errors.Is(err, context.Canceled) {
		_, _ = fmt.Fprintf(os.Stderr, "Pub/Sub receiver shutdown: %v\n", err)
	}
	_ = l.pubsub.Close()
}

func runFullPoint(
	ctx context.Context,
	logger *slog.Logger,
	loop *fullLoop,
	redisClient *redis.Client,
	redisOptions redis.Options,
	cfg config,
	concurrency int,
) (sweepResult, error) {
	pointCtx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()
	inbox, err := replyinbox.New(pointCtx, logger, otel.GetTracerProvider(), otel.GetMeterProvider(), replyinbox.Config{
		RedisOptions: redisOptions,
		ReplicaID:    "load-full-" + uuid.NewString(),
		PollInterval: replyinbox.DefaultPollInterval,
		DrainGate:    nil,
	})
	if err != nil {
		return sweepResult{}, fmt.Errorf("create inbox: %w", err)
	}
	defer func() {
		_ = inbox.Close()
		_ = redisClient.Del(context.WithoutCancel(ctx), replyinbox.InboxKey(inbox.ReplicaID())).Err()
	}()
	dispatcher, err := replyinbox.NewDispatcher(pointCtx, loop.broker, inbox, replyinbox.DispatcherConfig{WaitTimeout: cfg.timeout})
	if err != nil {
		return sweepResult{}, fmt.Errorf("create dispatcher: %w", err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer closeCancel()
		_ = dispatcher.Close(closeCtx)
	}()

	monitor := startSampler(pointCtx, redisClient, inbox, cfg.sampleInterval)
	gate := make(chan struct{})
	ready := sync.WaitGroup{}
	ready.Add(concurrency)
	outcomes := make(chan awaitOutcome, concurrency)
	lane := replyinbox.Lane{Scanner: riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_GITLEAKS, PolicyID: ""}
	const fakeSecret = "wJalrXUtnFEMIbKp7MDoRZfiCYqTvHgNsQ8xLcWd" //nolint:gosec // Synthetic gitleaks fixture.
	const fakeAccessKeyID = "ASIAZ2XY3WNBQR5TUVWX"
	content := "AccessKeyId: " + fakeAccessKeyID + ", SecretAccessKey: " + fakeSecret
	for range concurrency {
		go func() {
			ready.Done()
			<-gate
			started := time.Now()
			outcome, dispatchErr := dispatcher.Dispatch(pointCtx, replyinbox.DispatchRequest{
				OrganizationID: "load-org",
				ProjectID:      "load-project",
				Content:        content,
				Lanes:          []replyinbox.Lane{lane},
			})
			reply := outcome.ByLane[lane]
			if dispatchErr == nil && !outcome.Deadline && (!outcome.Complete || reply == nil || reply.GetStatus() != riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK || len(reply.GetFindings()) == 0) {
				dispatchErr = errors.New("full-loop reply was not a successful finding-bearing result")
			}
			outcomes <- awaitOutcome{duration: time.Since(started), deadline: outcome.Deadline, err: dispatchErr}
		}()
	}
	ready.Wait()
	dispatchStarted := time.Now()
	close(gate)

	latencies := make([]time.Duration, 0, concurrency)
	timeouts := 0
	dispatchErrors := 0
	for range concurrency {
		outcome := <-outcomes
		if outcome.deadline {
			timeouts++
		} else if outcome.err == nil {
			latencies = append(latencies, outcome.duration)
		} else {
			dispatchErrors++
		}
	}
	duration := time.Since(dispatchStarted)
	waitForDrainAccounting(pointCtx, inbox, nonnegativeUint64(len(latencies))*nonnegativeUint64(cfg.expected))
	monitor.stop()
	snapshot := inbox.Snapshot()
	result := buildResult("full", concurrency, cfg.expected, -1, -1, 0, duration, latencies)
	result.timeouts = timeouts
	result.errors = dispatchErrors
	populateRedisStats(&result, snapshot, monitor.stats)
	return result, nil
}

func syntheticLanes(count int) []replyinbox.Lane {
	lanes := make([]replyinbox.Lane, 0, count)
	for index := range count {
		switch index {
		case 0:
			lanes = append(lanes, replyinbox.Lane{Scanner: riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_GITLEAKS, PolicyID: ""})
		case 1:
			lanes = append(lanes, replyinbox.Lane{Scanner: riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_PRESIDIO, PolicyID: ""})
		case 2:
			lanes = append(lanes, replyinbox.Lane{Scanner: riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_PROMPT_INJECTION, PolicyID: ""})
		default:
			lanes = append(lanes, replyinbox.Lane{Scanner: riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_JUDGE, PolicyID: fmt.Sprintf("synthetic-policy-%d", index-2)})
		}
	}
	return lanes
}

func syntheticReply(scanID string, lane replyinbox.Lane, findingCount, replyIndex int) *riskv1.EnforcementReply {
	findings := make([]*riskv1.EnforcementFinding, 0, findingCount)
	for findingIndex := range findingCount {
		findings = append(findings, riskv1.EnforcementFinding_builder{
			RuleId:        new(fmt.Sprintf("synthetic-rule-%d", findingIndex)),
			Category:      new("secret"),
			Score:         new(0.95),
			StartPos:      new(int32(findingIndex * 16)),
			EndPos:        new(int32(findingIndex*16 + 12)),
			Surface:       new("prompt"),
			Field:         new("content"),
			Path:          new(fmt.Sprintf("messages[%d].content", findingIndex)),
			ToolCallId:    new(""),
			MaskedPreview: new("synthetic_...masked"),
			Fingerprint:   new(fmt.Sprintf("%064d", findingIndex)),
		}.Build())
	}
	return riskv1.EnforcementReply_builder{
		ScanId:   new(scanID),
		Scanner:  new(lane.Scanner),
		Status:   new(riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK),
		Reason:   new(""),
		Findings: findings,
		PolicyId: new(lane.PolicyID),
		Diagnostics: riskv1.EnforcementDiagnostics_builder{
			ScanDurationMs:  new(int64(0)),
			ConsumerId:      new(fmt.Sprintf("synthetic-%d", replyIndex)),
			DeliveryAttempt: new(int32(1)),
		}.Build(),
	}.Build()
}

func waitForWaiters(ctx context.Context, inbox *replyinbox.Inbox, expected int) error {
	ticker := time.NewTicker(100 * time.Microsecond)
	defer ticker.Stop()
	for {
		if inbox.Snapshot().Waiters == expected {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("register %d waiters: %w", expected, ctx.Err())
		case <-ticker.C:
		}
	}
}

func waitForDrainAccounting(ctx context.Context, inbox *replyinbox.Inbox, expected uint64) {
	ticker := time.NewTicker(100 * time.Microsecond)
	defer ticker.Stop()
	for inbox.Snapshot().DrainedReplies < expected {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func startSampler(ctx context.Context, client *redis.Client, inbox *replyinbox.Inbox, interval time.Duration) *sampler {
	sampleCtx, cancel := context.WithCancel(ctx)
	stats := &sampleStats{
		redisClientsBase: 0,
		redisMemoryBase:  0,
		inboxDepth:       atomic.Int64{},
		redisClients:     atomic.Int64{},
		sharedPoolConns:  atomic.Int64{},
		drainerConns:     atomic.Int64{},
		redisMemory:      atomic.Int64{},
	}
	done := make(chan struct{})
	collectSample(sampleCtx, client, inbox, stats)
	stats.redisClientsBase = stats.redisClients.Load()
	stats.redisMemoryBase = stats.redisMemory.Load()
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-sampleCtx.Done():
				return
			case <-ticker.C:
				collectSample(sampleCtx, client, inbox, stats)
			}
		}
	}()
	return &sampler{cancel: cancel, done: done, stats: stats}
}

func (s *sampler) stop() {
	s.cancel()
	<-s.done
}

func collectSample(ctx context.Context, client *redis.Client, inbox *replyinbox.Inbox, stats *sampleStats) {
	if depth, err := client.LLen(ctx, replyinbox.InboxKey(inbox.ReplicaID())).Result(); err == nil {
		storeMax(&stats.inboxDepth, depth)
	}
	if clients, err := client.ClientList(ctx).Result(); err == nil {
		count := int64(0)
		for _, line := range strings.Split(strings.TrimSpace(clients), "\n") {
			if line != "" {
				count++
			}
		}
		storeMax(&stats.redisClients, count)
	}
	storeMax(&stats.sharedPoolConns, int64(client.PoolStats().TotalConns))
	storeMax(&stats.drainerConns, int64(inbox.Snapshot().RedisPool.TotalConns))
	if memory, err := redisUsedMemory(ctx, client); err == nil {
		storeMax(&stats.redisMemory, memory)
	}
}

func redisUsedMemory(ctx context.Context, client *redis.Client) (int64, error) {
	info, err := client.Info(ctx, "memory").Result()
	if err != nil {
		return 0, fmt.Errorf("read Redis memory info: %w", err)
	}
	for _, line := range strings.Split(info, "\n") {
		value, found := strings.CutPrefix(strings.TrimSpace(line), "used_memory:")
		if !found {
			continue
		}
		memory, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse Redis used memory: %w", err)
		}
		return memory, nil
	}
	return 0, fmt.Errorf("used_memory missing from Redis INFO")
}

func storeMax(target *atomic.Int64, value int64) {
	for current := target.Load(); value > current; current = target.Load() {
		if target.CompareAndSwap(current, value) {
			return
		}
	}
}

func nonnegativeUint64(value int) uint64 {
	if value < 0 {
		return 0
	}
	return uint64(value)
}

func buildResult(
	mode string,
	concurrency int,
	expected int,
	findings int,
	payloadBytes int,
	registration time.Duration,
	duration time.Duration,
	latencies []time.Duration,
) sweepResult {
	p50, p90, p99, maximum := quantiles(latencies)
	throughput := 0.0
	if duration > 0 {
		throughput = float64(concurrency) / duration.Seconds()
	}
	return sweepResult{
		mode:                mode,
		concurrency:         concurrency,
		expected:            expected,
		findings:            findings,
		payloadBytes:        payloadBytes,
		registration:        registration,
		duration:            duration,
		throughput:          throughput,
		p50:                 p50,
		p90:                 p90,
		p99:                 p99,
		max:                 maximum,
		timeouts:            0,
		errors:              0,
		orphans:             0,
		drainBatches:        0,
		drainedReplies:      0,
		maxDrainBatch:       0,
		inboxDepthHighWater: 0,
		redisClientsBase:    0,
		redisClientsPeak:    0,
		sharedPoolPeak:      0,
		drainerPoolPeak:     0,
		redisMemoryDelta:    0,
		ttlAtRelease:        0,
		backlogAtRelease:    0,
		catchup:             0,
	}
}

func populateRedisStats(result *sweepResult, snapshot replyinbox.Stats, samples *sampleStats) {
	result.orphans = snapshot.OrphanedReplies
	result.drainBatches = snapshot.DrainBatches
	result.drainedReplies = snapshot.DrainedReplies
	result.maxDrainBatch = snapshot.MaxDrainBatch
	result.inboxDepthHighWater = samples.inboxDepth.Load()
	result.redisClientsBase = samples.redisClientsBase
	result.redisClientsPeak = samples.redisClients.Load()
	result.sharedPoolPeak = samples.sharedPoolConns.Load()
	result.drainerPoolPeak = samples.drainerConns.Load()
	result.redisMemoryDelta = max(0, samples.redisMemory.Load()-samples.redisMemoryBase)
}

func quantiles(values []time.Duration) (time.Duration, time.Duration, time.Duration, time.Duration) {
	if len(values) == 0 {
		return 0, 0, 0, 0
	}
	values = slices.Clone(values)
	slices.Sort(values)
	percentile := func(p float64) time.Duration {
		index := max(0, int(math.Ceil(float64(len(values))*p))-1)
		return values[index]
	}
	return percentile(0.50), percentile(0.90), percentile(0.99), values[len(values)-1]
}

func probeWaiterMaps(concurrency, rounds int) mapProbeResult {
	mutexSamples := make([]time.Duration, 0, rounds)
	syncMapSamples := make([]time.Duration, 0, rounds)
	for range rounds {
		var mu sync.Mutex
		waiters := make(map[int]struct{}, concurrency)
		mutexSamples = append(mutexSamples, timeConcurrent(concurrency, func(index int) {
			mu.Lock()
			waiters[index] = struct{}{}
			mu.Unlock()
			mu.Lock()
			delete(waiters, index)
			mu.Unlock()
		}))
		var waitersSync sync.Map
		syncMapSamples = append(syncMapSamples, timeConcurrent(concurrency, func(index int) {
			waitersSync.Store(index, struct{}{})
			waitersSync.Delete(index)
		}))
	}
	slices.Sort(mutexSamples)
	slices.Sort(syncMapSamples)
	operations := float64(concurrency * 2)
	return mapProbeResult{
		mutexNSPerOp:   float64(mutexSamples[len(mutexSamples)/2].Nanoseconds()) / operations,
		syncMapNSPerOp: float64(syncMapSamples[len(syncMapSamples)/2].Nanoseconds()) / operations,
	}
}

func timeConcurrent(concurrency int, operation func(int)) time.Duration {
	gate := make(chan struct{})
	ready := sync.WaitGroup{}
	done := sync.WaitGroup{}
	ready.Add(concurrency)
	done.Add(concurrency)
	for index := range concurrency {
		go func() {
			defer done.Done()
			ready.Done()
			<-gate
			operation(index)
		}()
	}
	ready.Wait()
	started := time.Now()
	close(gate)
	done.Wait()
	return time.Since(started)
}

func writeCSV(path string, results []sweepResult) error {
	file, err := os.Create(path) //nolint:gosec // The operator explicitly selects the local CSV destination.
	if err != nil {
		return fmt.Errorf("create CSV: %w", err)
	}
	w := csv.NewWriter(file)
	rows := [][]string{{
		"mode", "concurrency", "expected_replies", "findings_per_reply", "payload_bytes", "registration_ms", "duration_ms",
		"throughput_scans_s", "p50_ms", "p90_ms", "p99_ms", "max_ms", "timeouts", "errors", "orphans",
		"drain_batches", "drained_replies", "max_drain_batch", "inbox_depth_high_water", "redis_clients_base", "redis_clients_peak",
		"shared_pool_conns_peak", "drainer_pool_conns_peak", "redis_memory_delta_bytes", "backlog_at_release", "catchup_ms", "ttl_at_release_ms",
	}}
	for _, result := range results {
		rows = append(rows, []string{
			result.mode,
			strconv.Itoa(result.concurrency),
			strconv.Itoa(result.expected),
			strconv.Itoa(result.findings),
			strconv.Itoa(result.payloadBytes),
			formatMilliseconds(result.registration),
			formatMilliseconds(result.duration),
			fmt.Sprintf("%.2f", result.throughput),
			formatMilliseconds(result.p50),
			formatMilliseconds(result.p90),
			formatMilliseconds(result.p99),
			formatMilliseconds(result.max),
			strconv.Itoa(result.timeouts),
			strconv.Itoa(result.errors),
			strconv.FormatUint(result.orphans, 10),
			strconv.FormatUint(result.drainBatches, 10),
			strconv.FormatUint(result.drainedReplies, 10),
			strconv.FormatUint(result.maxDrainBatch, 10),
			strconv.FormatInt(result.inboxDepthHighWater, 10),
			strconv.FormatInt(result.redisClientsBase, 10),
			strconv.FormatInt(result.redisClientsPeak, 10),
			strconv.FormatInt(result.sharedPoolPeak, 10),
			strconv.FormatInt(result.drainerPoolPeak, 10),
			strconv.FormatInt(result.redisMemoryDelta, 10),
			strconv.FormatInt(result.backlogAtRelease, 10),
			formatMilliseconds(result.catchup),
			formatMilliseconds(result.ttlAtRelease),
		})
	}
	if err := w.WriteAll(rows); err != nil {
		_ = file.Close()
		return fmt.Errorf("write CSV: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close CSV: %w", err)
	}
	return nil
}

func formatMilliseconds(value time.Duration) string {
	return fmt.Sprintf("%.3f", float64(value)/float64(time.Millisecond))
}

func printPoint(result sweepResult) {
	fmt.Printf("%s c=%d r=%d: %.0f scans/s, p50=%s p99=%s max=%s, timeout=%d error=%d, depth=%d batch=%d, clients=%d, catchup=%s\n",
		result.mode, result.concurrency, result.expected, result.throughput, result.p50.Round(time.Microsecond),
		result.p99.Round(time.Microsecond), result.max.Round(time.Microsecond), result.timeouts, result.errors,
		result.inboxDepthHighWater, result.maxDrainBatch, result.redisClientsPeak, result.catchup.Round(time.Microsecond))
}

func printSummary(results []sweepResult) {
	fmt.Println("\nmode   conc replies scans/s p50_ms p90_ms p99_ms max_ms timeout orphan max_batch depth clients shared drain")
	for _, result := range results {
		fmt.Printf("%-6s %5d %7d %7.0f %6.2f %6.2f %6.2f %6.2f %7d %6d %9d %5d %7d %6d %5d\n",
			result.mode, result.concurrency, result.expected, result.throughput,
			result.p50.Seconds()*1000, result.p90.Seconds()*1000, result.p99.Seconds()*1000, result.max.Seconds()*1000,
			result.timeouts, result.orphans, result.maxDrainBatch, result.inboxDepthHighWater, result.redisClientsPeak,
			result.sharedPoolPeak, result.drainerPoolPeak)
	}
}
