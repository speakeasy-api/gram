package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"google.golang.org/protobuf/proto"

	sigintv1 "github.com/speakeasy-api/gram/infra/gen/gram/sigint/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/celeval"
	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/sigint/matching"
)

// Leave headroom below Pub/Sub's 10 MiB limit for attributes and wire overhead.
const maxReadingBytes = 9 * 1024 * 1024

// Features checks current organization entitlement before any inference.
type Features interface {
	IsFeatureEnabled(context.Context, string, productfeatures.Feature) (bool, error)
}

// Evaluator evaluates source events without durable progress state. Redelivery repeats
// inference and publication under stable reading IDs, with new attempt IDs.
type Evaluator struct {
	logger     *slog.Logger
	source     Source
	features   Features
	publisher  gcp.Publisher[*sigintv1.Reading]
	classifier classifier.Classifier
	failures   metric.Int64Counter
	skipped    metric.Int64Counter
	slots      chan struct{}
}

// NewEvaluator injects a classifier configured with the platform's credentials.
// Runtime concurrency is bounded across all adapters sharing this instance.
func NewEvaluator(logger *slog.Logger, meters metric.MeterProvider, source Source, features Features, publisher gcp.Publisher[*sigintv1.Reading], c classifier.Classifier) (*Evaluator, error) {
	meter := meters.Meter("github.com/speakeasy-api/gram/server/internal/sigint/evaluation")
	failures, err := meter.Int64Counter("gram.sigint.evaluation.failures", metric.WithDescription("Terminal sensor or event evaluations acknowledged without a reading"))
	if err != nil {
		return nil, fmt.Errorf("create sensor failure counter: %w", err)
	}
	skipped, err := meter.Int64Counter("gram.sigint.evaluation.skipped", metric.WithDescription("Intentionally skipped sensor configurations and events"))
	if err != nil {
		return nil, fmt.Errorf("create sensor skip counter: %w", err)
	}
	return &Evaluator{logger: logger, source: source, features: features, publisher: publisher, classifier: c, failures: failures, skipped: skipped, slots: make(chan struct{}, 4)}, nil
}

func (h *Evaluator) terminal(ctx context.Context, event Event, reason string) {
	h.failures.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
	h.logger.ErrorContext(ctx, "sensor evaluation permanently failed", attr.SlogSigintEventID(event.Subject.GetId()), attr.SlogSigintEventKind(event.Subject.GetKind()), attr.SlogProjectID(event.ProjectID), attr.SlogError(permanent(reason)))
}

// Evaluate applies the source's eligible sensors to one normalized event.
// Nil means all successful readings reached Pub/Sub and remaining work is terminal.
func (h *Evaluator) Evaluate(ctx context.Context, in Input) error {
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	case <-ctx.Done():
		return fmt.Errorf("wait for sensor capacity: %w", ctx.Err())
	}
	var event Event
	if in == nil {
		h.terminal(ctx, event, "invalid_event")
		return nil
	}
	event = in.Event()
	project, err := uuid.Parse(event.ProjectID)
	if err != nil || project == uuid.Nil || strings.TrimSpace(event.OrganizationID) == "" || strings.TrimSpace(event.Subject.GetId()) == "" || strings.TrimSpace(event.Subject.GetKind()) == "" {
		h.terminal(ctx, event, "invalid_identity")
		return nil
	}
	if _, err := time.Parse(time.RFC3339Nano, event.Subject.GetOccurredAt()); err != nil {
		h.terminal(ctx, event, "invalid_timestamp")
		return nil
	}
	enabled, err := h.features.IsFeatureEnabled(ctx, event.OrganizationID, productfeatures.FeatureSignalsIntelligence)
	if err != nil {
		return fmt.Errorf("check evaluation entitlement: %w", err)
	}
	if !enabled {
		return nil
	}
	sensors, err := h.source.Load(ctx, event.OrganizationID, project, event.Subject.GetKind())
	if err != nil {
		return fmt.Errorf("load sensor definitions: %w", err)
	}
	var compiled []compiledSensor
	matchMessage := in.MatchingMessage()
	for _, sensor := range sensors {
		matched, err := matching.Match(ctx, sensor.MatchExpression, matchMessage)
		if err != nil {
			if errors.Is(err, celeval.ErrCanceled) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				return fmt.Errorf("match sensor: %w", err)
			}
			reason := "match_evaluation"
			switch {
			case errors.Is(err, celeval.ErrExpressionSize):
				reason = "match_expression_size"
			case errors.Is(err, celeval.ErrResultType):
				reason = "match_result_type"
			case errors.Is(err, celeval.ErrCompile):
				reason = "match_compile"
			case errors.Is(err, celeval.ErrCostLimit):
				reason = "match_cost_limit"
			}
			h.failures.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
			h.logger.ErrorContext(ctx, "sensor matching failed", attr.SlogSigintSensorID(sensor.ID), attr.SlogProjectID(event.ProjectID), attr.SlogError(err))
			continue
		}
		if !matched {
			h.skipped.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", "not_matched")))
			continue
		}
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
	state, err := in.Resolve(ctx)
	if err != nil {
		if terminal, ok := errors.AsType[*permanentError](err); ok {
			h.terminal(ctx, event, terminal.reason)
			return nil
		}
		if errors.Is(err, ErrInvalidInput) {
			h.terminal(ctx, event, "invalid_content")
			return nil
		}
		return fmt.Errorf("resolve evaluation input: %w", err)
	}
	data, err := json.Marshal(state)
	if err != nil || string(data) == "null" {
		h.terminal(ctx, event, "invalid_content")
		return nil
	}
	if len(data) > maxContentBytes {
		h.terminal(ctx, event, "content_too_large")
		return nil
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
	operationErr := result.Err()
	permanentOperation := errors.Is(operationErr, classifier.ErrDisabled) || errors.Is(operationErr, classifier.ErrInvalidRequest)
	if err := operationErr; err != nil && !permanentOperation && !errors.Is(err, classifier.ErrRequestTooLarge) {
		retry = fmt.Errorf("evaluate sensors: %w", err)
	}
	var pending []gcp.PublishResult
	for _, sensor := range compiled {
		failed, transient := false, false
		for _, q := range sensor.questions {
			outcome, exists := outcomes[q.Key]
			if !exists {
				failed = true
				transient = transient || !permanentOperation
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
				h.terminal(ctx, event, "classifier_rejected")
			}
			continue
		}
		r, err := reading(event, sensor, outcomes, attempt, at, result)
		if err != nil {
			h.terminal(ctx, event, "invalid_answer")
			continue
		}
		if proto.Size(r) > maxReadingBytes {
			h.terminal(ctx, event, "reading_too_large")
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
