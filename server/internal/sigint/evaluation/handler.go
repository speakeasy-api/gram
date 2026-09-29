package evaluation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/proto"

	conversationv1 "github.com/speakeasy-api/gram/infra/gen/gram/conversation/v1"
	sigintv1 "github.com/speakeasy-api/gram/infra/gen/gram/sigint/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/streams"
)

// Leave headroom below Pub/Sub's 10 MiB limit for attributes and wire overhead.
const maxReadingBytes = 9 * 1024 * 1024

// Features checks current organization entitlement before any inference.
type Features interface {
	IsFeatureEnabled(context.Context, string, productfeatures.Feature) (bool, error)
}

// Handler evaluates messages without durable progress state. Redelivery repeats
// inference and publication under stable reading IDs, with new attempt IDs.
type Handler struct {
	logger     *slog.Logger
	source     Source
	features   Features
	blobs      BlobReader
	publisher  gcp.Publisher[*sigintv1.Reading]
	classifier classifier.Classifier
	failures   metric.Int64Counter
	skipped    metric.Int64Counter
	slots      chan struct{}
}

// NewHandler injects a classifier configured with the platform's credentials.
// Runtime concurrency is bounded across batch calls.
func NewHandler(logger *slog.Logger, meters metric.MeterProvider, source Source, features Features, blobs BlobReader, publisher gcp.Publisher[*sigintv1.Reading], c classifier.Classifier) (*Handler, error) {
	meter := meters.Meter("github.com/speakeasy-api/gram/server/internal/sigint/evaluation")
	failures, err := meter.Int64Counter("gram.sigint.evaluation.failures", metric.WithDescription("Terminal sensor or message evaluations acknowledged without a reading"))
	if err != nil {
		return nil, fmt.Errorf("create sensor failure counter: %w", err)
	}
	skipped, err := meter.Int64Counter("gram.sigint.evaluation.skipped", metric.WithDescription("Intentionally skipped sensor configurations and messages"))
	if err != nil {
		return nil, fmt.Errorf("create sensor skip counter: %w", err)
	}
	return &Handler{logger: logger, source: source, features: features, blobs: blobs, publisher: publisher, classifier: c, failures: failures, skipped: skipped, slots: make(chan struct{}, 4)}, nil
}

// HandleBatchWithResult nacks only transiently failed messages in the batch.
func (h *Handler) HandleBatchWithResult(ctx context.Context, messages []streams.BatchMessage[*conversationv1.Message]) error {
	var group errgroup.Group
	group.SetLimit(4)
	for _, message := range messages {
		group.Go(func() error {
			if err := h.Handle(ctx, message.Message, message.Metadata); err != nil {
				message.Fail(err)
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return fmt.Errorf("evaluate message batch: %w", err)
	}
	return nil
}

func (h *Handler) terminal(ctx context.Context, m *conversationv1.Message, reason string) {
	h.failures.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
	h.logger.ErrorContext(ctx, "sensor evaluation permanently failed", attr.SlogMessageID(m.GetId()), attr.SlogError(permanent(reason)))
}

// Handle evaluates only user/assistant roles, including historical replays.
// Nil means all successful readings reached Pub/Sub and remaining work is terminal.
func (h *Handler) Handle(ctx context.Context, m *conversationv1.Message, _ gcp.MessageMetadata) error {
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	case <-ctx.Done():
		return fmt.Errorf("wait for sensor capacity: %w", ctx.Err())
	}
	if m == nil {
		h.terminal(ctx, m, "invalid_message")
		return nil
	}
	if m.GetRole() != conversationv1.Message_ROLE_USER && m.GetRole() != conversationv1.Message_ROLE_ASSISTANT {
		return nil
	}
	project, err := uuid.Parse(m.GetProjectId())
	if err != nil || project == uuid.Nil || m.GetOrganizationId() == "" {
		h.terminal(ctx, m, "invalid_identity")
		return nil
	}
	for _, id := range []string{m.GetId(), m.GetConversationId()} {
		parsed, err := uuid.Parse(id)
		if err != nil || parsed == uuid.Nil {
			h.terminal(ctx, m, "invalid_identity")
			return nil
		}
	}
	if _, err := time.Parse(time.RFC3339Nano, m.GetCreatedAt()); err != nil {
		h.terminal(ctx, m, "invalid_timestamp")
		return nil
	}
	enabled, err := h.features.IsFeatureEnabled(ctx, m.GetOrganizationId(), productfeatures.FeatureSignalsIntelligence)
	if err != nil {
		return fmt.Errorf("check evaluation entitlement: %w", err)
	}
	if !enabled {
		return nil
	}
	sensors, err := h.source.Load(ctx, m.GetOrganizationId(), project)
	if err != nil {
		return fmt.Errorf("load sensor definitions: %w", err)
	}
	var compiled []compiledSensor
	for _, sensor := range sensors {
		item, ready := compileSensor(sensor)
		if !ready {
			h.skipped.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", "draft")))
			continue
		}
		compiled = append(compiled, item)
	}
	if len(compiled) == 0 {
		return nil
	}
	state, err := input(ctx, h.blobs, m)
	if err != nil {
		if terminal, ok := errors.AsType[*permanentError](err); ok {
			h.terminal(ctx, m, terminal.reason)
			return nil
		}
		return fmt.Errorf("resolve evaluation input: %w", err)
	}
	req := classifier.NewRequest(state)
	for _, sensor := range compiled {
		for _, question := range sensor.questions {
			req.Ask(question)
		}
	}
	attempt := uuid.NewString()
	result := h.classifier.Classify(ctx, req)
	at := time.Now().UTC().Format(time.RFC3339Nano)
	outcomes := make(map[classifier.QuestionKey]classifier.QuestionOutcome, len(result.Outcomes))
	for outcome := range result.Iter() {
		outcomes[outcome.Key] = outcome
	}
	var retry error
	if err := result.Err(); err != nil && !errors.Is(err, classifier.ErrRequestTooLarge) {
		retry = fmt.Errorf("evaluate sensors: %w", err)
	}
	var pending []gcp.PublishResult
	for _, sensor := range compiled {
		failed, transient := false, false
		for _, q := range sensor.questions {
			outcome, exists := outcomes[q.Key]
			if !exists {
				failed, transient = true, true
				continue
			}
			if outcome.Failure != nil {
				failed = true
				transient = transient || outcome.Failure.Retryable
			}
		}
		if failed {
			if transient {
				retry = errors.Join(retry, fmt.Errorf("retry incomplete sensor evaluation"))
			} else {
				h.terminal(ctx, m, "classifier_rejected")
			}
			continue
		}
		r, err := reading(m, sensor, outcomes, attempt, at, result)
		if err != nil {
			h.terminal(ctx, m, "invalid_answer")
			continue
		}
		if proto.Size(r) > maxReadingBytes {
			h.terminal(ctx, m, "reading_too_large")
			continue
		}
		pending = append(pending, h.publisher.Publish(ctx, r))
	}
	for _, publication := range pending {
		if _, err := publication.Get(ctx); err != nil {
			retry = errors.Join(retry, fmt.Errorf("publish sensor reading: %w", err))
		}
	}
	return retry
}
