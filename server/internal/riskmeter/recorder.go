// Package riskmeter records detector workload and inference measurements.
package riskmeter

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/stokens"
)

const (
	// DetectorGitleaks identifies Gitleaks work.
	DetectorGitleaks = meteringv1.RiskEvaluation_DETECTOR_GITLEAKS
	// DetectorPresidio identifies Presidio work.
	DetectorPresidio = meteringv1.RiskEvaluation_DETECTOR_PRESIDIO
	// DetectorPromptInjection identifies prompt-injection classifier work.
	DetectorPromptInjection = meteringv1.RiskEvaluation_DETECTOR_PROMPT_INJECTION
	// DetectorPromptPolicy identifies prompt-policy judge work.
	DetectorPromptPolicy = meteringv1.RiskEvaluation_DETECTOR_PROMPT_POLICY
	// DetectorCustomRules identifies custom CEL rule work.
	DetectorCustomRules = meteringv1.RiskEvaluation_DETECTOR_CUSTOM_RULES

	// ModeRealtime identifies synchronous request-path work.
	ModeRealtime = meteringv1.RiskEvaluation_EXECUTION_MODE_REALTIME
	// ModeBatch identifies stored-message batch work.
	ModeBatch = meteringv1.RiskEvaluation_EXECUTION_MODE_BATCH
	// ModeShadow identifies asynchronous shadow work.
	ModeShadow = meteringv1.RiskEvaluation_EXECUTION_MODE_SHADOW

	// OutcomeCompleted identifies successful evaluation completion.
	OutcomeCompleted = meteringv1.RiskEvaluation_OUTCOME_COMPLETED
	// OutcomeFailed identifies failed evaluation completion.
	OutcomeFailed = meteringv1.RiskEvaluation_OUTCOME_FAILED
	// OutcomeCancelled identifies cancelled evaluation completion.
	OutcomeCancelled = meteringv1.RiskEvaluation_OUTCOME_CANCELLED
	// OutcomeSkipped identifies eligible work that could not be attempted.
	OutcomeSkipped = meteringv1.RiskEvaluation_OUTCOME_SKIPPED

	// RecordKindScan identifies detector scan completion.
	RecordKindScan = "scan"
	// RecordKindInference identifies one physical provider request.
	RecordKindInference = "inference"

	// MeasurementMethod identifies the canonical tokenizer used for scan volume.
	MeasurementMethod = "tiktoken_o200k_base"

	publishTimeout = 10 * time.Second
)

// Evaluation identifies one logical detector evaluation.
type Evaluation struct {
	// OrganizationID identifies the organization that owns the workload.
	OrganizationID string

	// ProjectID identifies the project that owns the workload.
	ProjectID string

	// OperationID is the caller's stable logical operation identity.
	OperationID string

	// Detector identifies the detector class.
	Detector meteringv1.RiskEvaluation_Detector

	// ExecutionMode distinguishes realtime, batch, and shadow work.
	ExecutionMode meteringv1.RiskEvaluation_ExecutionMode

	// PolicyID is diagnostic policy attribution.
	PolicyID string

	// PolicyVersion is the diagnostic policy version.
	PolicyVersion int64

	// OccurredAt is the UTC time when evaluation work started.
	OccurredAt time.Time
}

// Inference captures provider-reported accounting for one physical request.
type Inference struct {
	// Model identifies the provider model.
	Model string

	// ProviderRequestID is the provider's physical request identity.
	ProviderRequestID string

	// PromptTokens is nil when the provider did not report prompt usage.
	PromptTokens *int64

	// CompletionTokens is nil when the provider did not report completion usage.
	CompletionTokens *int64

	// CostUSD is nil when the provider did not report request cost.
	CostUSD *float64
}

// Recorder publishes terminal risk measurement events.
type Recorder struct {
	logger    *slog.Logger
	publisher gcp.Publisher[*meteringv1.RiskEvaluation]
	codec     *stokens.Codec
}

// NewRecorder creates a risk measurement recorder.
func NewRecorder(logger *slog.Logger, publisher gcp.Publisher[*meteringv1.RiskEvaluation]) *Recorder {
	return &Recorder{
		logger:    logger.With(attr.SlogComponent("risk-meter")),
		publisher: publisher,
		codec:     stokens.NewCodec(),
	}
}

// Record publishes one terminal scan attempt. A nil fragments slice records an
// unknown scan volume, while a non-nil empty slice records a known zero.
func (r *Recorder) Record(ctx context.Context, evaluation Evaluation, fragments []string, outcome meteringv1.RiskEvaluation_Outcome, inference *Inference) error {
	if r == nil {
		return nil
	}

	if inference != nil {
		err := fmt.Errorf("risk scan measurement must not include inference accounting")
		r.logger.ErrorContext(ctx, "build risk scan measurement", attr.SlogError(err))
		return err
	}
	message, err := r.newMessage(evaluation, RecordKindScan, outcome, nil)
	if err != nil {
		r.logger.ErrorContext(ctx, "build risk scan measurement", attr.SlogError(err))
		return err
	}
	if fragments != nil {
		measurementCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), publishTimeout)
		count, countErr := r.codec.Count(measurementCtx, fragments...)
		cancel()
		if countErr != nil {
			message.SetMeasurementError(true)
			r.logger.ErrorContext(measurementCtx, "measure risk scan volume", attr.SlogError(countErr))
		} else {
			message.SetStokens(int64(count))
		}
	}
	message.SetProducedAt(time.Now().UTC().Format(time.RFC3339Nano))

	publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), publishTimeout)
	defer cancel()
	return r.publish(publishCtx, message)
}

// RecordInference publishes provider accounting for one physical inference
// request. Inference records intentionally have no s-token volume.
func (r *Recorder) RecordInference(ctx context.Context, evaluation Evaluation, outcome meteringv1.RiskEvaluation_Outcome, inference *Inference) error {
	if r == nil {
		return nil
	}

	publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), publishTimeout)
	defer cancel()

	message, err := r.newMessage(evaluation, RecordKindInference, outcome, inference)
	if err != nil {
		r.logger.ErrorContext(ctx, "build risk inference measurement", attr.SlogError(err))
		return err
	}
	return r.publish(publishCtx, message)
}

func (r *Recorder) newMessage(evaluation Evaluation, recordKind string, outcome meteringv1.RiskEvaluation_Outcome, inference *Inference) (*meteringv1.RiskEvaluation, error) {
	if err := ValidateEvaluation(evaluation); err != nil {
		return nil, err
	}
	if err := ValidateOutcome(outcome); err != nil {
		return nil, err
	}
	if err := validateInference(inference); err != nil {
		return nil, err
	}

	physicalID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("create physical risk evaluation id: %w", err)
	}
	producedAt := time.Now().UTC()
	message := new(meteringv1.RiskEvaluation)
	message.SetId(physicalID.String())
	message.SetEvaluationId(EvaluationID(evaluation))
	message.SetOrganizationId(evaluation.OrganizationID)
	message.SetProjectId(evaluation.ProjectID)
	message.SetOperationId(evaluation.OperationID)
	message.SetDetector(evaluation.Detector)
	message.SetExecutionMode(evaluation.ExecutionMode)
	message.SetOutcome(outcome)
	message.SetOccurredAt(evaluation.OccurredAt.Format(time.RFC3339Nano))
	message.SetProducedAt(producedAt.Format(time.RFC3339Nano))
	message.SetMeasurementMethod(MeasurementMethod)
	message.SetPolicyId(evaluation.PolicyID)
	message.SetPolicyVersion(evaluation.PolicyVersion)
	message.SetRecordKind(recordKind)
	if inference != nil {
		message.SetModel(inference.Model)
		message.SetProviderRequestId(inference.ProviderRequestID)
		if inference.PromptTokens != nil {
			message.SetPromptTokens(*inference.PromptTokens)
		}
		if inference.CompletionTokens != nil {
			message.SetCompletionTokens(*inference.CompletionTokens)
		}
		if inference.CostUSD != nil {
			message.SetCostUsd(*inference.CostUSD)
		}
	}
	return message, nil
}

func (r *Recorder) publish(ctx context.Context, message *meteringv1.RiskEvaluation) error {
	if r.publisher == nil {
		err := fmt.Errorf("risk evaluation publisher is not configured")
		r.logger.ErrorContext(ctx, "publish risk measurement", attr.SlogError(err))
		return err
	}
	if _, err := r.publisher.Publish(ctx, message).Get(ctx); err != nil {
		err = fmt.Errorf("publish risk evaluation: %w", err)
		r.logger.ErrorContext(ctx, "publish risk measurement", attr.SlogError(err))
		return err
	}
	return nil
}

// ValidateEvaluation verifies the metadata required to identify a logical evaluation.
func ValidateEvaluation(evaluation Evaluation) error {
	if strings.TrimSpace(evaluation.OrganizationID) == "" {
		return fmt.Errorf("risk evaluation organization id must not be empty")
	}
	projectID, err := uuid.Parse(evaluation.ProjectID)
	if err != nil {
		return fmt.Errorf("parse risk evaluation project id: %w", err)
	}
	if projectID == uuid.Nil {
		return fmt.Errorf("risk evaluation project id must not be zero")
	}
	if strings.TrimSpace(evaluation.OperationID) == "" {
		return fmt.Errorf("risk evaluation operation id must not be empty")
	}
	if _, ok := DetectorLabel(evaluation.Detector); !ok {
		return fmt.Errorf("invalid risk evaluation detector %q", evaluation.Detector)
	}
	if _, ok := ExecutionModeLabel(evaluation.ExecutionMode); !ok {
		return fmt.Errorf("invalid risk evaluation execution mode %q", evaluation.ExecutionMode)
	}
	if evaluation.PolicyVersion < 0 {
		return fmt.Errorf("risk evaluation policy version must not be negative")
	}
	if evaluation.OccurredAt.IsZero() || evaluation.OccurredAt.Location() != time.UTC {
		return fmt.Errorf("risk evaluation occurred at must be a nonzero UTC timestamp")
	}
	return nil
}

func validateInference(inference *Inference) error {
	if inference == nil {
		return nil
	}
	if inference.PromptTokens != nil && *inference.PromptTokens < 0 {
		return fmt.Errorf("risk inference prompt tokens must not be negative")
	}
	if inference.CompletionTokens != nil && *inference.CompletionTokens < 0 {
		return fmt.Errorf("risk inference completion tokens must not be negative")
	}
	if inference.CostUSD != nil && (math.IsNaN(*inference.CostUSD) || math.IsInf(*inference.CostUSD, 0) || *inference.CostUSD < 0) {
		return fmt.Errorf("risk inference cost must be finite and nonnegative")
	}
	return nil
}

// DetectorLabel returns the canonical persisted label for a known detector.
func DetectorLabel(detector meteringv1.RiskEvaluation_Detector) (string, bool) {
	switch detector {
	case DetectorGitleaks:
		return "gitleaks", true
	case DetectorPresidio:
		return "presidio", true
	case DetectorPromptInjection:
		return "prompt_injection", true
	case DetectorPromptPolicy:
		return "prompt_policy", true
	case DetectorCustomRules:
		return "custom_rules", true
	default:
		return "", false
	}
}

// ExecutionModeLabel returns the canonical persisted label for a known execution mode.
func ExecutionModeLabel(mode meteringv1.RiskEvaluation_ExecutionMode) (string, bool) {
	switch mode {
	case ModeRealtime:
		return "realtime", true
	case ModeBatch:
		return "batch", true
	case ModeShadow:
		return "shadow", true
	default:
		return "", false
	}
}

// OutcomeLabel returns the canonical persisted label for a known terminal outcome.
func OutcomeLabel(outcome meteringv1.RiskEvaluation_Outcome) (string, bool) {
	switch outcome {
	case OutcomeCompleted:
		return "completed", true
	case OutcomeFailed:
		return "failed", true
	case OutcomeCancelled:
		return "cancelled", true
	case OutcomeSkipped:
		return "skipped", true
	default:
		return "", false
	}
}

// ValidateOutcome verifies a terminal evaluation outcome.
func ValidateOutcome(outcome meteringv1.RiskEvaluation_Outcome) error {
	if _, ok := OutcomeLabel(outcome); !ok {
		return fmt.Errorf("invalid risk evaluation outcome %q", outcome)
	}
	return nil
}

type evaluationContextKey struct{}

// WithEvaluation adds logical evaluation metadata to a context.
func WithEvaluation(ctx context.Context, evaluation Evaluation) context.Context {
	return context.WithValue(ctx, evaluationContextKey{}, evaluation)
}

// EvaluationFromContext retrieves logical evaluation metadata from a context.
func EvaluationFromContext(ctx context.Context) (Evaluation, bool) {
	evaluation, ok := ctx.Value(evaluationContextKey{}).(Evaluation)
	return evaluation, ok
}

// EvaluationID returns the deterministic UUID for a logical evaluation.
func EvaluationID(evaluation Evaluation) string {
	executionMode, _ := ExecutionModeLabel(evaluation.ExecutionMode)
	detector, _ := DetectorLabel(evaluation.Detector)
	preimage := strings.Join([]string{
		"gram:risk:evaluation:v1",
		evaluation.OrganizationID,
		evaluation.ProjectID,
		executionMode,
		detector,
		evaluation.OperationID,
	}, "\x00")
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(preimage)).String()
}

// OperationID hashes length-framed identity parts into a bounded identifier.
func OperationID(parts ...string) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte("gram:risk:operation:v1\x00"))
	var length [8]byte
	for _, part := range parts {
		binary.BigEndian.PutUint64(length[:], uint64(len(part)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(part))
	}
	return "riskop:v1:" + hex.EncodeToString(hash.Sum(nil))
}
