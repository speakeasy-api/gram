package infra

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"cloud.google.com/go/compute/metadata"
	"cloud.google.com/go/pubsub/v2"
	"github.com/google/uuid"
	"github.com/sourcegraph/conc/pool"
	"github.com/speakeasy-api/gram/infra/gen"
	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/internal/attr"
	gcppub "github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/urfave/cli/v2"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func newPresidioSubmitCommand() *cli.Command {
	flags := []cli.Flag{
		&cli.StringFlag{
			Name:    "pubsub-emulator-host",
			Usage:   "Host to use for the PubSub emulator",
			EnvVars: []string{"PUBSUB_EMULATOR_HOST"},
		},
		&cli.StringSliceFlag{
			Name:  "entities",
			Usage: "Presidio entity types to detect (e.g. EMAIL_ADDRESS). Empty lets Presidio use its defaults.",
		},
		&cli.StringFlag{
			Name:  "reply-urn",
			Usage: "Optional reply URN echoed back on the request for future round-tripping",
		},
		&cli.IntFlag{
			Name:  "count",
			Value: 1,
			Usage: "Total number of messages to publish (each with a fresh id)",
		},
		&cli.IntFlag{
			Name:  "concurrency",
			Value: 1,
			Usage: "Number of concurrent publishers (load-test parallelism)",
		},
	}

	return &cli.Command{
		Name:      "presidio-submit",
		Usage:     "Publish a gram.risk.v1.PresidioRequest with the given contents to scan",
		ArgsUsage: "<content> [content...]",
		Flags:     flags,
		Action: func(c *cli.Context) error {
			ctx, cancel := context.WithCancel(c.Context)
			defer cancel()

			logger := PullLogger(c.Context)

			contents := c.Args().Slice()
			if len(contents) == 0 {
				return fmt.Errorf("at least one content argument is required")
			}

			var err error
			var projectID string
			opts := []option.ClientOption{option.WithLogger(logger)}
			switch {
			case c.String("pubsub-emulator-host") != "":
				opts = append(opts, option.WithEndpoint(c.String("pubsub-emulator-host")), option.WithoutAuthentication())
				projectID = "my-project-id"
			default:
				projectID, err = metadata.ProjectIDWithContext(ctx)
				if err != nil {
					return fmt.Errorf("failed to get google cloud project id: %w", err)
				}
			}

			client, err := pubsub.NewClient(ctx, projectID, opts...)
			if err != nil {
				return fmt.Errorf("failed to create pubsub client: %w", err)
			}

			broker := gcppub.NewEmulatedPubSub(logger, projectID, client, gen.Descriptors)

			pub, err := gcppub.PubSubPublisherForMessage(ctx, broker, &riskv1.PresidioRequest{})
			if err != nil {
				return fmt.Errorf("failed to get publisher: %w", err)
			}

			count := max(1, c.Int("count"))
			concurrency := max(1, c.Int("concurrency"))
			replyURN := ptrIfNotEmpty(c.String("reply-urn"))
			entities := c.StringSlice("entities")

			logger.InfoContext(ctx, "publishing presidio requests", attr.SlogValueAny(map[string]any{
				"count":       count,
				"concurrency": concurrency,
				"contents":    len(contents),
				"entities":    entities,
			}))

			var published, failed atomic.Int64
			start := time.Now()

			// One publisher reused across all goroutines; the pubsub client is
			// safe for concurrent use and batches publishes internally.
			p := pool.New().WithMaxGoroutines(concurrency)
			for range count {
				p.Go(func() {
					id := uuid.Must(uuid.NewV7()).String()
					msg := riskv1.PresidioRequest_builder{
						Id:        &id,
						CreatedAt: timestamppb.Now(),
						ReplyUrn:  replyURN,
						Contents:  contents,
						Entities:  entities,
					}.Build()

					if _, err := pub.Publish(ctx, msg).Get(ctx); err != nil {
						failed.Add(1)
						logger.ErrorContext(ctx, "publish failed", attr.SlogError(err), attr.SlogValueAny(map[string]any{"id": id}))
						return
					}
					published.Add(1)
				})
			}
			p.Wait()

			elapsed := time.Since(start)
			var rate float64
			if secs := elapsed.Seconds(); secs > 0 {
				rate = float64(published.Load()) / secs
			}

			logger.InfoContext(ctx, "done publishing presidio requests", attr.SlogValueAny(map[string]any{
				"published":    published.Load(),
				"failed":       failed.Load(),
				"elapsed":      elapsed.String(),
				"rate_per_sec": rate,
			}))

			if failed.Load() > 0 {
				return fmt.Errorf("%d of %d publishes failed", failed.Load(), count)
			}

			return nil
		},
	}
}

func ptrIfNotEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
