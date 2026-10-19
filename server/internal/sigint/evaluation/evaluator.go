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
// Every terminal sensor decision publishes either a reading or an outcome, so
// coverage can account for eligible events that have no answer.
type Evaluator struct {
	logger     *slog.Logger
	source     Source
	features   Features
	publisher  gcp.Publisher[*sigintv1.Reading]
	outcomes   gcp.Publisher[*sigintv1.Outcome]
	classifier classifier.Classifier
	failures   metric.Int64Counter
	skipped    metric.Int64Counter
	truncated  metric.Int64Counter
	slots      chan struct{}
}

// NewEvaluator injects a classifier configured with the platform's credentials.
// Runtime concurrency is bounded across all adapters sharing this instance.
func NewEvaluator(logger *slog.Logger, meters metric.MeterProvider, source Source, features Features, publisher gcp.Publisher[*sigintv1.Reading], outcomes gcp.Publisher[*sigintv1.Outcome], c classifier.Classifier) (*Evaluator, error) {
	logger = logger.With(attr.SlogComponent("sigint-evaluator"))
	meter := meters.Meter("github.com/speakeasy-api/gram/server/internal/sigint/evaluation")

	failures, err := meter.Int64Counter("gram.sigint.evaluation.failures", metric.WithDescription("Terminal sensor or event evaluations acknowledged without a reading"))
	if err != nil {
		return nil, fmt.Errorf("create sensor failure counter: %w", err)
	}

	skipped, err := meter.Int64Counter("gram.sigint.evaluation.skipped", metric.WithDescription("Intentionally skipped sensor configurations and events"))
	if err != nil {
		return nil, fmt.Errorf("create sensor skip counter: %w", err)
	}

	truncated, err := meter.Int64Counter("gram.sigint.evaluation.window_truncations", metric.WithDescription("Sensor evaluation attempts using less available history because of the content budget"))
	if err != nil {
		return nil, fmt.Errorf("create sensor window truncation counter: %w", err)
	}

	return &Evaluator{logger: logger, source: source, features: features, publisher: publisher, outcomes: outcomes, classifier: c, failures: failures, skipped: skipped, truncated: truncated, slots: make(chan struct{}, 4)}, nil
}

func (h *Evaluator) terminal(ctx context.Context, event Event, reason string, details ...slog.Attr) {
	h.failures.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
	attrs := []slog.Attr{attr.SlogSigintEventID(event.Subject.GetId()), attr.SlogSigintEventKind(event.Subject.GetKind()), attr.SlogProjectID(event.ProjectID), attr.SlogError(permanent(reason))}
	h.logger.LogAttrs(ctx, slog.LevelError, "sensor evaluation permanently failed", append(attrs, details...)...)
}

// Evaluate applies the source's eligible sensors to one normalized event.
// Nil means all readings and outcomes reached Pub/Sub and remaining work is terminal.
// Event-level failures before sensors load have no sensor to attribute an outcome to.
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
	if err := event.Evaluation.Validate(); err != nil {
		h.terminal(ctx, event, "invalid_evaluation_context")
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

	// One attempt ID covers the whole event evaluation, across window groups.
	attempt := uuid.NewString()
	var pending []gcp.PublishResult
	var compiled []compiledSensor
	maxSize := 1
	matchMessage := in.MatchingMessage()
	for _, sensor := range sensors {
		item, ready := compileSensor(sensor)
		ref := sensorRef{id: sensor.ID, slug: sensor.Slug, hash: ""}
		if ready {
			ref.hash = item.hash
		}
		decide := func(status sigintv1.Outcome_Status, reason string) {
			pending = append(pending, h.publishOutcome(ctx, outcome(event, ref, attempt, time.Now().UTC().Format(time.RFC3339Nano), status, reason)))
		}

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
			decide(sigintv1.Outcome_STATUS_FAILED, reason)
			continue
		}

		if !matched {
			h.skipped.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", "not_matched")))
			decide(sigintv1.Outcome_STATUS_NOT_APPLICABLE, "")
			continue
		}
		if !ready {
			h.skipped.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", "draft")))
			decide(sigintv1.Outcome_STATUS_DRAFT, "")
			continue
		}
		compiled = append(compiled, item)
		maxSize = max(maxSize, item.windowSize)
	}
	if len(compiled) == 0 {
		return awaitPublications(ctx, pending, "sensor outcome")
	}

	buffer := in.NewBuffer(maxSize)
	// Every group needs the target. Resolve it once before processing groups so
	// failed target reads are neither repeated nor counted once per window size.
	target, err := buffer.Resolve(ctx, 1)
	if err != nil {
		return errors.Join(h.evaluateGroup(ctx, event, attempt, compiled, target, err), awaitPublications(ctx, pending, "sensor outcome"))
	}

	// Ascending sizes let small windows succeed even when older content is invalid.
	// Larger windows reuse the newest contiguous context that fits the budget.
	var retry error
	for size := 1; size <= maxSize; size++ {
		var sensors []compiledSensor
		for _, sensor := range compiled {
			if sensor.windowSize == size {
				sensors = append(sensors, sensor)
			}
		}
		if len(sensors) == 0 {
			continue
		}
		var resolved ResolvedInput
		var err error
		if size == 1 {
			resolved = target
		} else {
			resolved, err = buffer.Resolve(ctx, size)
		}
		if err == nil && resolved.Truncated {
			h.truncated.Add(ctx, int64(len(sensors)), metric.WithAttributes(
				attribute.String("reason", "content_too_large"),
				attribute.Int("requested_window_size", size),
				attribute.Int("effective_window_size", resolved.MessageCount),
			))
			attrs := []slog.Attr{
				attr.SlogWideEvent(),
				attr.SlogOrganizationID(event.OrganizationID),
				attr.SlogProjectID(event.ProjectID),
				attr.SlogSigintEventID(event.Subject.GetId()),
				attr.SlogSigintEventKind(event.Subject.GetKind()),
				attr.SlogReason("content_too_large"),
				attr.SlogSigintRequestedWindowSize(size),
				attr.SlogSigintEffectiveWindowSize(resolved.MessageCount),
				attr.SlogSigintContentBudgetBytes(maxContentBytes),
			}
			if message := event.Subject.GetConversationMessage(); message != nil {
				attrs = append(attrs, attr.SlogChatID(message.GetConversationId()), attr.SlogMessageID(event.Subject.GetId()))
			}
			for _, sensor := range sensors {
				h.logger.LogAttrs(ctx, slog.LevelWarn, "sensor evaluation window truncated", append(attrs,
					attr.SlogSigintSensorID(sensor.id),
					attr.SlogSigintSensorMode(sensor.mode),
					attr.SlogSigintSensorDefinitionHash(sensor.hash),
				)...)
			}
		}
		retry = errors.Join(retry, h.evaluateGroup(ctx, event, attempt, sensors, resolved, err))
	}
	return errors.Join(retry, awaitPublications(ctx, pending, "sensor outcome"))
}

// failGroup permanently fails every sensor sharing an input that cannot be
// classified, counting the failure once for the group.
func (h *Evaluator) failGroup(ctx context.Context, event Event, attempt string, compiled []compiledSensor, reason string, input *ResolvedInput) error {
	h.terminal(ctx, event, reason)
	at := time.Now().UTC().Format(time.RFC3339Nano)
	pending := make([]gcp.PublishResult, 0, len(compiled))
	for _, sensor := range compiled {
		o := outcome(event, sensor.ref(), attempt, at, sigintv1.Outcome_STATUS_FAILED, reason)
		if input != nil {
			withInput(o, sensor, *input)
		}
		pending = append(pending, h.publishOutcome(ctx, o))
	}
	return awaitPublications(ctx, pending, "sensor outcome")
}

func (h *Evaluator) evaluateGroup(ctx context.Context, event Event, attempt string, compiled []compiledSensor, input ResolvedInput, err error) error {
	if err != nil {
		if terminal, ok := errors.AsType[*permanentError](err); ok {
			return h.failGroup(ctx, event, attempt, compiled, terminal.reason, nil)
		}

		if errors.Is(err, ErrInvalidInput) {
			return h.failGroup(ctx, event, attempt, compiled, "invalid_content", nil)
		}
		return fmt.Errorf("resolve evaluation input: %w", err)
	}

	state := input.State
	data, err := json.Marshal(state)
	if err != nil || string(data) == "null" {
		return h.failGroup(ctx, event, attempt, compiled, "invalid_content", &input)
	}

	if len(data) > maxContentBytes {
		return h.failGroup(ctx, event, attempt, compiled, "content_too_large", &input)
	}
	req := classifier.NewRequest(state)
	for _, sensor := range compiled {
		for _, question := range sensor.questions {
			req.Ask(question)
		}
	}
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

	var pending, decided []gcp.PublishResult
	fail := func(sensor compiledSensor, reason string) {
		o := withClassification(withInput(outcome(event, sensor.ref(), attempt, at, sigintv1.Outcome_STATUS_FAILED, reason), sensor, input), result)
		decided = append(decided, h.publishOutcome(ctx, o))
	}
	for _, sensor := range compiled {
		failed, transient := false, false
		var failures []error
		for _, q := range sensor.questions {
			outcome, exists := outcomes[q.Key]
			if !exists {
				failed = true
				transient = transient || !permanentOperation
				failures = append(failures, fmt.Errorf("question %s: missing outcome", q.Key))
				continue
			}
			if outcome.Failure != nil {
				failed = true
				transient = transient || outcome.Failure.Retryable
				failures = append(failures, fmt.Errorf("question %s: %s: %s", q.Key, outcome.Failure.Code, outcome.Failure.Message))
			}
		}
		if failed {
			if transient {
				retry = errors.Join(retry, fmt.Errorf("retry incomplete sensor evaluation"))
			} else {
				h.terminal(ctx, event, "classifier_rejected",
					attr.SlogSigintSensorID(sensor.id),
					attr.SlogSigintSensorMode(sensor.mode),
					attr.SlogClassifierError(errors.Join(append(failures, operationErr)...)),
				)
				fail(sensor, "classifier_rejected")
			}
			continue
		}

		r, err := reading(event, sensor, outcomes, attempt, at, result)
		if err != nil {
			h.terminal(ctx, event, "invalid_answer")
			fail(sensor, "invalid_answer")
			continue
		}
		r.SetInput(readingInput(sensor, input))

		if proto.Size(r) > maxReadingBytes {
			h.terminal(ctx, event, "reading_too_large")
			fail(sensor, "reading_too_large")
			continue
		}
		pending = append(pending, h.publisher.Publish(ctx, r))
	}
	return errors.Join(retry, awaitPublications(ctx, pending, "sensor reading"), awaitPublications(ctx, decided, "sensor outcome"))
}
