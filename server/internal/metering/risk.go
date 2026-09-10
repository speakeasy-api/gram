package metering

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

// RiskProvenance snapshots attribution at the start of one scanner execution.
// Reusing a result retains its initiating policy rather than minting new usage.
type RiskProvenance struct {
	// OrganizationID owns the workload.
	OrganizationID string

	// ProjectID owns the scanned content.
	ProjectID uuid.UUID

	// RiskPolicyID identifies the policy that initiated this execution.
	RiskPolicyID uuid.UUID

	// RiskPolicyVersion is the initiating policy's version at execution time.
	RiskPolicyVersion int64

	// PolicyLinkReason identifies non-policy-triggered scans such as draft rule tests.
	// It is required only when both the policy ID and version are absent.
	PolicyLinkReason string

	// ChatID identifies a known persisted chat, never a derived external ID.
	ChatID uuid.UUID

	// ExternalConversationID is the raw conversation ID supplied by the external agent.
	ExternalConversationID string

	// ChatMessageID is the canonical persisted message, never a synthetic event ID.
	ChatMessageID uuid.UUID

	// ContentPartID identifies an independently scanned attachment or content part.
	ContentPartID uuid.UUID

	// MessageLinkReason explains why ChatMessageID is absent.
	MessageLinkReason string

	// OperationID is stable across delivery retries and independent of batching.
	OperationID string

	// ExecutionPath distinguishes realtime, inline batch and stream executions.
	ExecutionPath string

	// RequestID correlates an execution with its originating transport request.
	RequestID string

	// MessageType identifies the kind of scanned content.
	MessageType string

	// HookSource identifies the agent that supplied the content.
	HookSource string

	// UserID identifies the user associated with the content.
	UserID string

	// ToolCallID identifies the scanned invocation when known.
	ToolCallID string

	// ToolName identifies the scanned tool when applicable.
	ToolName string

	// Model identifies the scanner's model when applicable.
	Model string

	// Provider identifies the scanner's model provider when applicable.
	Provider string
}

// RiskRecorder publishes successful scanner work to the existing meter topic.
// Scanner completion, including fail-open and skipped states, remains the caller's decision.
type RiskRecorder struct {
	logger    *slog.Logger
	publisher gcp.Publisher[*meteringv1.MeterReading]
}

// NewRiskRecorder requires a publisher; use a gcp.NoopPublisher to discard readings.
func NewRiskRecorder(logger *slog.Logger, publisher gcp.Publisher[*meteringv1.MeterReading]) *RiskRecorder {
	return &RiskRecorder{logger: logger, publisher: publisher}
}

// Record publishes one completed scan. Zero-token inputs do not create usage.
// Callers on realtime paths supply a bounded cancellation-detached context.
func (r *RiskRecorder) Record(ctx context.Context, definition Definition, provenance RiskProvenance, stokens int64, occurredAt time.Time) error {
	message, err := PrepareRiskReading(definition, provenance, stokens, occurredAt)
	if err != nil {
		return err
	}
	if message == nil {
		return nil
	}
	if _, err := r.publisher.Publish(ctx, message).Get(ctx); err != nil {
		err = fmt.Errorf("publish risk meter reading: %w", err)
		r.logger.ErrorContext(ctx, "risk meter publication failed", attr.SlogError(err))
		return err
	}
	return nil
}

// PrepareRiskReading creates a validated, canonical envelope for a successful
// execution. Foreign-language scanners can carry this envelope without copying
// the meter registry or deterministic reading identity algorithm.
func PrepareRiskReading(definition Definition, provenance RiskProvenance, stokens int64, occurredAt time.Time) (*meteringv1.MeterReading, error) {
	if stokens == 0 {
		return nil, nil
	}
	switch definition {
	case RiskGitleaks(), RiskPresidio(), RiskPromptInjection(), RiskPromptPolicy(), RiskCustomRules(), RiskCLIDestructive():
	default:
		return nil, fmt.Errorf("meter is not a registered risk scanner")
	}
	if provenance.RiskPolicyID == uuid.Nil {
		if provenance.RiskPolicyVersion != 0 || strings.TrimSpace(provenance.PolicyLinkReason) == "" {
			return nil, fmt.Errorf("risk reading requires originating policy or explicit no-policy reason")
		}
	} else if provenance.RiskPolicyVersion <= 0 || provenance.PolicyLinkReason != "" {
		return nil, fmt.Errorf("policy-triggered risk reading requires a version and no unlinked policy reason")
	}
	if strings.TrimSpace(provenance.ExecutionPath) == "" {
		return nil, fmt.Errorf("risk reading requires execution path")
	}
	if provenance.ChatMessageID == uuid.Nil && strings.TrimSpace(provenance.MessageLinkReason) == "" {
		return nil, fmt.Errorf("risk reading requires a chat message id or explicit unlinked reason")
	}
	if provenance.ChatMessageID != uuid.Nil && provenance.MessageLinkReason != "" {
		return nil, fmt.Errorf("linked risk reading must not have an unlinked reason")
	}

	attributes := map[string]string{
		AttributeRiskPolicyLinkStatus: "unlinked",
		AttributeScanExecutionPath:    provenance.ExecutionPath,
		AttributeMessageLinkStatus:    "unlinked",
	}
	if provenance.RiskPolicyID != uuid.Nil {
		attributes[AttributeRiskPolicyID] = provenance.RiskPolicyID.String()
		attributes[AttributeRiskPolicyVersion] = strconv.FormatInt(provenance.RiskPolicyVersion, 10)
		attributes[AttributeRiskPolicyLinkStatus] = "linked"
	}
	if provenance.ChatMessageID != uuid.Nil {
		attributes[AttributeChatMessageID] = provenance.ChatMessageID.String()
		attributes[AttributeMessageLinkStatus] = "linked"
	}
	if provenance.ChatID != uuid.Nil {
		attributes[AttributeChatID] = provenance.ChatID.String()
	}
	if provenance.ContentPartID != uuid.Nil {
		attributes[AttributeContentPartID] = provenance.ContentPartID.String()
	}
	for _, attribute := range [...]struct{ key, value string }{
		{key: AttributeRiskPolicyLinkReason, value: provenance.PolicyLinkReason},
		{key: AttributeMessageLinkReason, value: provenance.MessageLinkReason},
		{key: AttributeScanRequestID, value: provenance.RequestID},
		{key: AttributeMessageType, value: provenance.MessageType},
		{key: AttributeHookSource, value: provenance.HookSource},
		{key: AttributeExternalConversationID, value: provenance.ExternalConversationID},
		{key: AttributeMessageUserID, value: provenance.UserID},
		{key: AttributeToolCallID, value: provenance.ToolCallID},
		{key: AttributeToolName, value: provenance.ToolName},
		{key: AttributeModel, value: provenance.Model},
		{key: AttributeProvider, value: provenance.Provider},
	} {
		if attribute.value != "" {
			attributes[attribute.key] = attribute.value
		}
	}
	reading, err := NewUsage(UsageInput{
		Meter:       definition,
		Scope:       ProjectScope(provenance.OrganizationID, provenance.ProjectID),
		OperationID: provenance.OperationID,
		Value:       stokens,
		OccurredAt:  occurredAt.UTC(),
		ProducedAt:  time.Now().UTC(),
		Source:      "risk_scanner",
		Attributes:  attributes,
	})
	if err != nil {
		return nil, fmt.Errorf("prepare risk meter reading: %w", err)
	}
	return toProto(reading, reading.ID()), nil
}
