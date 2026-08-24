// Command enforceproto exercises the enforcement request-reply path against
// the local Pub/Sub emulator and Redis.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cloud.google.com/go/pubsub/v2"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"google.golang.org/api/option"

	"github.com/speakeasy-api/gram/infra/gen"
	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/replyinbox"
	"github.com/speakeasy-api/gram/server/internal/scanners/gitleaks"
)

func main() {
	if err := run(); err != nil {
		slog.Default().ErrorContext(context.Background(), "enforcement prototype failed", attr.SlogError(err))
		os.Exit(1)
	}
}

func run() error {
	pubsubHost := flag.String("pubsub-emulator-host", os.Getenv("PUBSUB_EMULATOR_HOST"), "Pub/Sub emulator host")
	redisAddr := flag.String("redis-addr", os.Getenv("GRAM_REDIS_CACHE_ADDR"), "Redis address")
	redisPassword := flag.String("redis-password", os.Getenv("GRAM_REDIS_CACHE_PASSWORD"), "Redis password")
	flag.Parse()
	if *pubsubHost == "" {
		return fmt.Errorf("pubsub emulator host is required")
	}
	if *redisAddr == "" {
		return fmt.Errorf("redis address is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	logger := slog.Default()

	redisOptions := redis.Options{Addr: *redisAddr, Password: *redisPassword, Protocol: 2}
	redisClient := redis.NewClient(&redisOptions)
	defer o11y.NoLogDefer(redisClient.Close)
	if err := redisClient.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping redis: %w", err)
	}

	const projectID = "gram-enforcement-prototype"
	pubsubClient, err := pubsub.NewClient(ctx, projectID, option.WithEndpoint(*pubsubHost), option.WithoutAuthentication())
	if err != nil {
		return fmt.Errorf("create pubsub client: %w", err)
	}
	defer o11y.NoLogDefer(pubsubClient.Close)
	broker := gcp.NewEmulatedPubSub(logger, projectID, pubsubClient, gen.Descriptors)

	inbox, err := replyinbox.New(ctx, logger, otel.GetTracerProvider(), otel.GetMeterProvider(), replyinbox.Config{
		RedisOptions: redisOptions,
		ReplicaID:    "",
		BlockTimeout: replyinbox.DefaultBlockTimeout,
		DrainGate:    nil,
	})
	if err != nil {
		return fmt.Errorf("create reply inbox: %w", err)
	}
	defer o11y.NoLogDefer(inbox.Close)

	fingerprinter, err := risk.ParsePepperKeyRing([]byte(os.Getenv("GRAM_RISK_FINGERPRINT_PEPPER_KEYRING")))
	if err != nil {
		return fmt.Errorf("parse risk fingerprint pepper keyring: %w", err)
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
		return fmt.Errorf("create gitleaks enforcement handler: %w", err)
	}
	subscriber, err := gcp.PubSubSubscriberForMessage(ctx, broker, &riskv1.GitleaksEnforcement{}, &riskv1.GitleaksEnforcer{})
	if err != nil {
		return fmt.Errorf("create enforcement subscriber: %w", err)
	}
	receiveCtx, stopReceive := context.WithCancel(ctx)
	receiveDone := make(chan error, 1)
	go func() {
		receiveDone <- subscriber.Receive(receiveCtx, handler.Handle)
	}()

	dispatcher, err := replyinbox.NewDispatcher(ctx, broker, inbox, replyinbox.DispatcherConfig{WaitTimeout: replyinbox.DefaultWaitTimeout})
	if err != nil {
		stopReceive()
		return fmt.Errorf("create enforcement dispatcher: %w", err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer closeCancel()
		_ = dispatcher.Close(closeCtx)
	}()

	const fakeSecret = "wJalrXUtnFEMIbKp7MDoRZfiCYqTvHgNsQ8xLcWd" //nolint:gosec // Synthetic gitleaks fixture.
	const fakeAccessKeyID = "ASIAZ2XY3WNBQR5TUVWX"
	lane := replyinbox.Lane{Scanner: riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_GITLEAKS, PolicyID: ""}
	outcome, err := dispatcher.Dispatch(ctx, replyinbox.DispatchRequest{
		OrganizationID: "prototype-org",
		ProjectID:      "prototype-project",
		Content:        "AccessKeyId: " + fakeAccessKeyID + ", SecretAccessKey: " + fakeSecret,
		Lanes:          []replyinbox.Lane{lane},
	})
	if err != nil {
		stopReceive()
		return fmt.Errorf("dispatch enforcement scan: %w", err)
	}
	reply := outcome.ByLane[lane]
	if !outcome.Complete || outcome.Deadline || reply == nil || reply.GetStatus() != riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK || len(reply.GetFindings()) == 0 {
		stopReceive()
		return fmt.Errorf("unexpected enforcement outcome: complete=%t deadline=%t replies=%d", outcome.Complete, outcome.Deadline, len(outcome.ByLane))
	}
	for _, finding := range reply.GetFindings() {
		if finding.GetMaskedPreview() == fakeSecret || finding.GetFingerprint() == "" {
			stopReceive()
			return fmt.Errorf("unsafe enforcement finding")
		}
	}

	stopReceive()
	if receiveErr := <-receiveDone; receiveErr != nil && !errors.Is(receiveErr, context.Canceled) {
		return fmt.Errorf("receive enforcement request: %w", receiveErr)
	}
	logger.InfoContext(ctx, "enforcement prototype passed", attr.SlogValueInt(len(reply.GetFindings())))
	return nil
}
