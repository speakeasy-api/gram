// Package chrepo stores sensor readings in ClickHouse.
package chrepo

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	sigintv1 "github.com/speakeasy-api/gram/infra/gen/gram/sigint/v1"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/sigint/readings"
)

const (
	// MaxAttributes bounds label fanout and allocation without restricting values' cardinality.
	MaxAttributes = 64

	// MaxAttributeBytes bounds a reading's combined label payload to 32 KiB.
	MaxAttributeBytes = 32 << 10

	// maxProbabilities bounds one reading's projection fanout, with ample room above exclusive sensors' 255 options.
	maxProbabilities = 1024

	// maxReadingBytes matches the producer's 9 MiB envelope beneath Pub/Sub's transport limit.
	maxReadingBytes = 9 << 20

	// firstTimestampYear excludes pre-epoch values unsupported by DateTime-based TTLs.
	firstTimestampYear = 1970

	// lastTimestampYear leaves two years of TTL headroom before DateTime's 2106 limit.
	lastTimestampYear = 2104
)

// Status values of sigint_readings_v2 rows.
const (
	// StatusAnswered marks a reading that carries a classifier result.
	StatusAnswered = "answered"

	// StatusNotApplicable marks a sensor whose match expression excluded the event.
	StatusNotApplicable = "not_applicable"

	// StatusDraft marks a sensor whose incomplete definition cannot be evaluated.
	StatusDraft = "draft"

	// StatusFailed marks a permanent evaluation failure with a failure reason.
	StatusFailed = "failed"
)

// maxFailureReasonBytes bounds an evaluator failure reason. Reasons are short
// snake_case codes; anything longer is not one.
const maxFailureReasonBytes = 64

// ErrInvalidReading identifies poison messages that cannot succeed on redelivery.
var ErrInvalidReading = errors.New("invalid sensor reading")

// ProbabilityRow preserves one member and its configured distribution position.
type ProbabilityRow struct {
	// SignalID is the stable signal UUID.
	SignalID uuid.UUID `ch:"signal_id"`

	// SignalSlug is the evaluation-time display slug.
	SignalSlug string `ch:"signal_slug"`

	// Probability is the classifier value, including valid zeroes.
	Probability float64 `ch:"probability"`
}

// ReadingRow is the complete attempt snapshot inserted into sigint_readings_v2.
type ReadingRow struct {
	// ID is the logical reading identity shared by attempts.
	ID uuid.UUID `ch:"id"`

	// EvaluationAttemptID identifies this evaluation execution.
	EvaluationAttemptID uuid.UUID `ch:"evaluation_attempt_id"`

	// OrganizationID is authoritative tenant scope, independent of customer labels.
	OrganizationID string `ch:"organization_id"`

	// ProjectID owns the source event and configuration.
	ProjectID uuid.UUID `ch:"project_id"`

	// TriggerKind distinguishes streaming evaluations from manual runs.
	TriggerKind string `ch:"trigger_kind"`

	// RunID is zero for streaming and the stable request UUID for manual runs.
	RunID uuid.UUID `ch:"run_id"`

	// EvaluationRequestedAt is immutable across attempts and anchors partitioning and retention.
	EvaluationRequestedAt time.Time `ch:"evaluation_requested_at"`

	// SensorID is the stable configuration resource.
	SensorID uuid.UUID `ch:"sensor_id"`

	// SensorSlug is the evaluation-time slug.
	SensorSlug string `ch:"sensor_slug"`

	// EventKind namespaces the opaque event identity.
	EventKind string `ch:"event_kind"`

	// EventID is producer-assigned and need not be a UUID.
	EventID string `ch:"event_id"`

	// EventOccurredAt is immutable across attempts for the logical reading.
	EventOccurredAt time.Time `ch:"event_occurred_at"`

	// EvaluatedAt selects the newest completion independent of delivery order.
	EvaluatedAt time.Time `ch:"evaluated_at"`

	// InsertedAt measures ingestion lag, not measurement time.
	InsertedAt time.Time `ch:"inserted_at"`

	// Status is the evaluation outcome. Published readings are always answered.
	Status string `ch:"status"`

	// FailureReason is the bounded evaluator reason for failed outcomes.
	FailureReason string `ch:"failure_reason"`

	// DefinitionHash identifies the evaluated ordered configuration.
	DefinitionHash string `ch:"definition_hash"`

	// ConfiguredModel is the requested classifier model.
	ConfiguredModel string `ch:"configured_model"`

	// Models contains provider-reported models for the logical evaluation.
	Models []string `ch:"models"`

	// CompilerVersion identifies classification compilation behavior.
	CompilerVersion string `ch:"compiler_version"`

	// Provider identifies inference routing, not the source workload's provider.
	Provider string `ch:"provider"`

	// InputStrategy is the configured message context, empty when unrecorded.
	InputStrategy string `ch:"input_strategy"`

	// InputRequestedMessageCount is the configured count including the target.
	InputRequestedMessageCount uint8 `ch:"input_requested_message_count"`

	// InputMessageCount is the count actually supplied including the target.
	InputMessageCount uint8 `ch:"input_message_count"`

	// InputTruncated reports that the content budget excluded available history.
	InputTruncated bool `ch:"input_truncated"`

	// EventContextType identifies the optional source-context variant.
	EventContextType string `ch:"event_context_type"`

	// ConversationID is absent for independent event streams.
	ConversationID *uuid.UUID `ch:"conversation_id"`

	// ConversationRole preserves an extensible role vocabulary.
	ConversationRole string `ch:"conversation_role"`

	// ConversationGeneration is the captured transcript generation.
	ConversationGeneration int32 `ch:"conversation_generation"`

	// ConversationSequence is the captured message sequence.
	ConversationSequence int64 `ch:"conversation_sequence"`

	// ConversationCaptureKind distinguishes original and copied/generated history.
	ConversationCaptureKind string `ch:"conversation_capture_kind"`

	// ConversationOriginalMessageID retains an exact copy's original target.
	ConversationOriginalMessageID *uuid.UUID `ch:"conversation_original_message_id"`

	// ToolName is the producer's tool name when supplied.
	ToolName string `ch:"tool_name"`

	// ToolCallID is scoped to the source session or connection.
	ToolCallID string `ch:"tool_call_id"`

	// MCPServerID is present only for an associated MCP resource.
	MCPServerID *uuid.UUID `ch:"mcp_server_id"`

	// ActorUserID is source-observed human attribution, not billing allocation.
	ActorUserID string `ch:"actor_user_id"`

	// ActorExternalUserID is interpreted within tenant and source.
	ActorExternalUserID string `ch:"actor_external_user_id"`

	// ActorUserEmail is observed rather than inferred from a directory.
	ActorUserEmail string `ch:"actor_user_email"`

	// BillingUserID is explicit source allocation and never falls back to actor identity.
	BillingUserID string `ch:"billing_user_id"`

	// Source identifies the ingestion surface.
	Source string `ch:"source"`

	// UserAccountID identifies external AI credentials when resolved.
	UserAccountID *uuid.UUID `ch:"user_account_id"`

	// AccountType is the source account classification or empty when unknown.
	AccountType string `ch:"account_type"`

	// BillingMode is independent of the organization's product plan.
	BillingMode string `ch:"billing_mode"`

	// AssistantID is a workload resource, not an actor.
	AssistantID *uuid.UUID `ch:"assistant_id"`

	// Replayed distinguishes absent evidence from an explicit false value.
	Replayed *bool `ch:"replayed"`

	// Attributes contains opaque customer string labels.
	Attributes map[string]string `ch:"attributes"`

	// ResultType identifies multi_label, choice or score.
	ResultType string `ch:"result_type"`

	// Probabilities preserves every member in configured order.
	Probabilities []ProbabilityRow `ch:"probabilities"`

	// SelectedSignalID is applicable only to choice readings.
	SelectedSignalID *uuid.UUID `ch:"selected_signal_id"`

	// SelectedSignalSlug is the matching evaluation-time slug.
	SelectedSignalSlug string `ch:"selected_signal_slug"`

	// ExpectedIndex is applicable only to ordered-score readings.
	ExpectedIndex *float64 `ch:"expected_index"`

	// Confidence is optional and independent of option probabilities.
	Confidence *float64 `ch:"confidence"`
}

// Queries owns ClickHouse access for sensor readings.
type Queries struct {
	conn clickhouse.Conn
}

// New binds the repository to a ClickHouse connection.
func New(conn clickhouse.Conn) *Queries {
	return &Queries{conn: conn}
}

func invalid(reason string) (ReadingRow, error) {
	return ReadingRow{}, fmt.Errorf("%w: %s", ErrInvalidReading, reason)
}

// ParseReading validates an answered attempt without changing its values or
// inferring attribution.
func ParseReading(m *sigintv1.Reading, insertedAt time.Time) (ReadingRow, error) {
	row, err := parseAttempt(m, insertedAt, true)
	if err != nil {
		return row, err
	}
	row.Status = StatusAnswered
	var probabilities []*sigintv1.Reading_Probability
	switch {
	case m.HasMultiLabel():
		row.ResultType = "multi_label"
		probabilities = m.GetMultiLabel().GetSignals()
	case m.HasChoice():
		row.ResultType = "choice"
		choice := m.GetChoice()
		probabilities = choice.GetDistribution()
		row.SelectedSignalID, err = optionalUUID(choice.GetSelectedSignalId())
		if err != nil || row.SelectedSignalID == nil {
			return invalid("invalid selected signal")
		}
		row.SelectedSignalSlug = choice.GetSelectedSignalSlug()
		if choice.HasConfidence() {
			row.Confidence = new(choice.GetConfidence())
		}
	case m.HasScore():
		row.ResultType = "score"
		score := m.GetScore()
		probabilities = score.GetDistribution()
		if !score.HasExpectedIndex() || len(probabilities) < 2 {
			return invalid("missing ordered score")
		}
		row.ExpectedIndex = new(score.GetExpectedIndex())
		if !finiteInRange(*row.ExpectedIndex, float64(len(probabilities)-1)) {
			return invalid("invalid expected index")
		}
		if score.HasConfidence() {
			row.Confidence = new(score.GetConfidence())
		}
	default:
		return invalid("missing result")
	}
	if row.Confidence != nil && !finiteInRange(*row.Confidence, 1) {
		return invalid("invalid confidence")
	}
	if len(probabilities) == 0 || len(probabilities) > maxProbabilities {
		return invalid("invalid distribution size")
	}
	row.Probabilities = make([]ProbabilityRow, 0, len(probabilities))
	seen := make(map[uuid.UUID]struct{}, len(probabilities))
	selectedFound := row.SelectedSignalID == nil
	for _, p := range probabilities {
		if p == nil || !p.HasProbability() || !finiteInRange(p.GetProbability(), 1) || p.GetSignalSlug() == "" {
			return invalid("invalid probability")
		}
		id, parseErr := uuid.Parse(p.GetSignalId())
		if parseErr != nil || id == uuid.Nil {
			return invalid("invalid signal UUID")
		}
		if _, duplicate := seen[id]; duplicate {
			return invalid("duplicate signal")
		}
		seen[id] = struct{}{}
		if row.SelectedSignalID != nil && id == *row.SelectedSignalID {
			if row.SelectedSignalSlug != p.GetSignalSlug() {
				return invalid("selected slug differs from distribution")
			}
			selectedFound = true
		}
		row.Probabilities = append(row.Probabilities, ProbabilityRow{SignalID: id, SignalSlug: p.GetSignalSlug(), Probability: p.GetProbability()})
	}
	if !selectedFound {
		return invalid("selected signal missing from distribution")
	}
	return row, nil
}

// parseAttempt validates the identity, metadata and attribution shared by
// readings and outcomes. Outcomes of incomplete sensors need no definition.
func parseAttempt(m *sigintv1.Reading, insertedAt time.Time, requireDefinition bool) (ReadingRow, error) {
	var row ReadingRow
	if m == nil || !m.HasEvent() {
		return invalid("missing event")
	}
	if proto.Size(m) > maxReadingBytes {
		return invalid("reading exceeds transport budget")
	}
	evaluation, err := readings.ParseContext(m.GetTriggerKind(), m.GetRunId(), m.GetEvaluationRequestedAt())
	if err != nil {
		return invalid("invalid evaluation context")
	}
	row.TriggerKind = string(evaluation.TriggerKind)
	row.RunID = evaluation.RunID
	row.EvaluationRequestedAt = evaluation.RequestedAt
	for _, identity := range []struct {
		value  string
		target *uuid.UUID
	}{
		{value: m.GetId(), target: &row.ID},
		{value: m.GetEvaluationAttemptId(), target: &row.EvaluationAttemptID},
		{value: m.GetProjectId(), target: &row.ProjectID},
		{value: m.GetSensorId(), target: &row.SensorID},
	} {
		id, err := uuid.Parse(identity.value)
		if err != nil || id == uuid.Nil {
			return invalid("invalid required UUID")
		}
		*identity.target = id
	}
	row.OrganizationID = m.GetOrganizationId()
	row.SensorSlug = m.GetSensorSlug()
	row.EventKind = m.GetEvent().GetKind()
	row.EventID = m.GetEvent().GetId()
	row.DefinitionHash = m.GetDefinitionHash()
	required := []string{row.OrganizationID, row.EventKind, row.EventID}
	if requireDefinition {
		required = append(required, row.SensorSlug, row.DefinitionHash)
	}
	for _, value := range required {
		if strings.TrimSpace(value) == "" {
			return invalid("missing required metadata")
		}
	}
	row.EventOccurredAt, err = parseTimestamp(m.GetEvent().GetOccurredAt())
	if err != nil {
		return invalid("invalid occurrence time")
	}
	row.EvaluatedAt, err = parseTimestamp(m.GetEvaluatedAt())
	if err != nil {
		return invalid("invalid evaluation time")
	}
	row.InsertedAt = insertedAt.UTC()
	row.ConfiguredModel = m.GetConfiguredModel()
	row.Models = slices.Clone(m.GetModels())
	row.CompilerVersion = m.GetCompilerVersion()
	row.Provider = m.GetProvider()
	if m.HasInput() {
		input := m.GetInput()
		requested, requestedClamped := conv.ClampedIntToUint8(int(input.GetRequestedMessageCount()))
		supplied, suppliedClamped := conv.ClampedIntToUint8(int(input.GetMessageCount()))
		if input.GetStrategy() == "" || requestedClamped || suppliedClamped || supplied < 1 || supplied > requested {
			return invalid("invalid input context")
		}
		row.InputStrategy = input.GetStrategy()
		row.InputRequestedMessageCount = requested
		row.InputMessageCount = supplied
		row.InputTruncated = input.GetTruncated()
	}
	row.Source = m.GetSource()
	row.BillingUserID = m.GetBillingUserId()
	row.ActorUserID = m.GetActor().GetUserId()
	row.ActorExternalUserID = m.GetActor().GetExternalUserId()
	row.ActorUserEmail = m.GetActor().GetUserEmail()
	row.AccountType = m.GetAccount().GetAccountType()
	row.BillingMode = m.GetAccount().GetBillingMode()
	row.AssistantID, err = optionalUUID(m.GetAssistantId())
	if err != nil {
		return invalid("invalid assistant UUID")
	}
	row.UserAccountID, err = optionalUUID(m.GetAccount().GetUserAccountId())
	if err != nil {
		return invalid("invalid account UUID")
	}
	if m.HasReplayed() {
		row.Replayed = new(m.GetReplayed())
	}
	if context := m.GetEvent().GetConversationMessage(); context != nil {
		row.EventContextType = "conversation_message"
		row.ConversationID, err = optionalUUID(context.GetConversationId())
		if err != nil {
			return invalid("invalid conversation UUID")
		}
		row.ConversationRole = strings.ToLower(strings.TrimPrefix(context.GetRole().String(), "ROLE_"))
		row.ConversationGeneration = context.GetGeneration()
		row.ConversationSequence = context.GetSequence()
		row.ConversationCaptureKind = context.GetCaptureKind()
		row.ConversationOriginalMessageID, err = optionalUUID(context.GetOriginalMessageId())
		if err != nil || row.ConversationGeneration < 0 || row.ConversationSequence < 0 {
			return invalid("invalid conversation lineage")
		}
		switch row.ConversationCaptureKind {
		case "", "original", "reobserved", "synthetic", "unknown":
		default:
			return invalid("invalid conversation capture kind")
		}
	}
	if context := m.GetEvent().GetToolCall(); context != nil {
		row.EventContextType = "tool_call"
		row.ToolName = context.GetToolName()
		row.ToolCallID = context.GetToolCallId()
		row.MCPServerID, err = optionalUUID(context.GetMcpServerId())
		if err != nil {
			return invalid("invalid MCP server UUID")
		}
	}
	if !AttributesWithinLimits(m.GetAttributes()) {
		return invalid("invalid attribute payload")
	}
	row.Attributes = maps.Clone(m.GetAttributes())
	return row, nil
}

// AttributesWithinLimits reports whether labels fit the storage budget and
// every key is non-empty.
func AttributesWithinLimits(attributes map[string]string) bool {
	if len(attributes) > MaxAttributes {
		return false
	}
	attributeBytes := 0
	for key, value := range attributes {
		attributeBytes += len(key) + len(value)
		if key == "" || attributeBytes > MaxAttributeBytes {
			return false
		}
	}
	return true
}

func finiteInRange(value, maximum float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= maximum
}

func optionalUUID(value string) (*uuid.UUID, error) {
	if value == "" {
		return nil, nil
	}
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil {
		return nil, fmt.Errorf("%w: invalid optional UUID", ErrInvalidReading)
	}
	return &id, nil
}

func parseTimestamp(value string) (time.Time, error) {
	timestamp, err := time.Parse(time.RFC3339Nano, value)
	// DateTime-based TTLs and DateTime64(9) must both represent the timestamp.
	if err != nil || timestamp.Year() < firstTimestampYear || timestamp.Year() >= lastTimestampYear {
		return time.Time{}, fmt.Errorf("%w: unsupported timestamp", ErrInvalidReading)
	}
	return timestamp.UTC(), nil
}

// InsertReadings preserves every valid physical attempt, including duplicates:
// replacement on evaluated_at converges them. Binary batches avoid
// interpolating arbitrary label keys into SQL.
func (q *Queries) InsertReadings(ctx context.Context, rows []ReadingRow) error {
	if len(rows) == 0 {
		return nil
	}
	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{"async_insert": 0}))
	batch, err := q.conn.PrepareBatch(ctx, `INSERT INTO sigint_readings_v2 (
		id, evaluation_attempt_id, organization_id, project_id, sensor_id, sensor_slug,
		trigger_kind, run_id, evaluation_requested_at,
		event_kind, event_id, event_occurred_at, evaluated_at, inserted_at, definition_hash,
		status, failure_reason, configured_model, models, compiler_version, provider,
		input_strategy, input_requested_message_count, input_message_count, input_truncated,
		event_context_type,
		conversation_id, conversation_role, conversation_generation, conversation_sequence,
		conversation_capture_kind, conversation_original_message_id, tool_name, tool_call_id, mcp_server_id,
		actor_user_id, actor_external_user_id, actor_user_email, billing_user_id, source,
		user_account_id, account_type, billing_mode, assistant_id, replayed, attributes,
		result_type, probabilities, selected_signal_id, selected_signal_slug,
		expected_index, confidence
	)`)
	if err != nil {
		return fmt.Errorf("prepare sensor reading batch: %w", err)
	}
	defer o11y.NoLogDefer(batch.Abort)
	for i := range rows {
		if err := batch.AppendStruct(&rows[i]); err != nil {
			return fmt.Errorf("append sensor reading: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send sensor reading batch: %w", err)
	}
	return nil
}
