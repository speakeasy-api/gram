package metering

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"time"

	"github.com/google/uuid"
	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/metering/chrepo"
	meteringrepo "github.com/speakeasy-api/gram/server/internal/metering/repo"
	"github.com/speakeasy-api/gram/server/internal/streams"
)

// ReadingInserter persists validated workload readings.
type ReadingInserter interface {
	InsertReadings(context.Context, []chrepo.ReadingRow) error
}

// MeterReadingCHWriter validates Pub/Sub readings and writes them to ClickHouse.
type MeterReadingCHWriter struct {
	logger        *slog.Logger
	db            meteringrepo.DBTX
	inserter      ReadingInserter
	writesEnabled bool
}

// NewMeterReadingCHWriter creates a ClickHouse workload reading subscriber.
func NewMeterReadingCHWriter(logger *slog.Logger, db meteringrepo.DBTX, inserter ReadingInserter, writesEnabled bool) *MeterReadingCHWriter {
	return &MeterReadingCHWriter{
		logger:        logger.With(attr.SlogComponent("meter-reading-ch-writer")),
		db:            db,
		inserter:      inserter,
		writesEnabled: writesEnabled,
	}
}

var _ streams.BatchHandler[*meteringv1.MeterReading] = (*MeterReadingCHWriter)(nil)

// HandleBatch acknowledges messages without insertion when ClickHouse writes are disabled.
func (w *MeterReadingCHWriter) HandleBatch(ctx context.Context, messages []*meteringv1.MeterReading, _ []gcp.MessageMetadata) error {
	if !w.writesEnabled {
		return nil
	}

	insertedAt := time.Now().UTC()
	rows := make([]chrepo.ReadingRow, 0, len(messages))
	received := make(map[uuid.UUID]int, len(messages))

	for _, message := range messages {
		row, reason := meterReadingRow(message, insertedAt)
		if reason != "" {
			messageID := ""
			if message != nil {
				messageID = message.GetId()
			}
			w.logger.ErrorContext(ctx, "skipping unprocessable meter reading",
				attr.SlogReason(reason),
				attr.SlogMessageID(messageID),
			)
			continue
		}
		if priorIndex, duplicate := received[row.ID]; duplicate {
			if row.ProducedAt.Before(rows[priorIndex].ProducedAt) {
				continue
			}
			rows[priorIndex] = row
			continue
		}
		received[row.ID] = len(rows)
		rows = append(rows, row)
	}

	if err := w.enrichAgentSessionStorageRows(ctx, rows); err != nil {
		return err
	}
	if err := w.inserter.InsertReadings(ctx, rows); err != nil {
		return fmt.Errorf("insert meter readings: %w", err)
	}
	return nil
}

type agentSessionStorageLookupKey struct {
	organizationID string
	projectID      uuid.UUID
	chatID         uuid.UUID
	messageUserID  string
}

var consumerOwnedAgentSessionAttributes = [...]string{
	AttributeMessageUserAccountEmail,
	AttributeMessageUserDivisionName,
	AttributeMessageUserDepartmentName,
	AttributeMessageUserDirectoryMatch,
	AttributeMessageUserRBACRoles,
	AttributeChatOwnerUserID,
	AttributeChatOwnerExternalUserID,
	AttributeChatOwnerUserEmail,
	AttributeChatOwnerDivisionName,
	AttributeChatOwnerDepartmentName,
	AttributeChatOwnerDirectoryMatch,
	AttributeChatOwnerRBACRoles,
}

func setResolvedAttribute(attributes map[string]string, key string, value string) {
	if value != "" {
		attributes[key] = value
	}
}

func (w *MeterReadingCHWriter) enrichAgentSessionStorageRows(ctx context.Context, rows []chrepo.ReadingRow) error {
	keys := make([]agentSessionStorageLookupKey, 0)
	seen := make(map[agentSessionStorageLookupKey]struct{})
	rowKeys := make(map[uuid.UUID]agentSessionStorageLookupKey)

	for i := range rows {
		row := &rows[i]
		if row.MeterID != string(MeterAgentSessionStorage) || row.CorrectsReadingID != nil {
			continue
		}
		rawChatID := row.Attributes[AttributeChatID]
		if rawChatID == "" {
			continue
		}
		for _, key := range consumerOwnedAgentSessionAttributes {
			delete(row.Attributes, key)
		}
		chatID, err := uuid.Parse(rawChatID)
		if err != nil || chatID == uuid.Nil {
			continue
		}
		lookupKey := agentSessionStorageLookupKey{
			organizationID: row.OrganizationID,
			projectID:      row.ProjectID,
			chatID:         chatID,
			messageUserID:  row.Attributes[AttributeMessageUserID],
		}
		rowKeys[row.ID] = lookupKey
		if _, duplicate := seen[lookupKey]; duplicate {
			continue
		}
		seen[lookupKey] = struct{}{}
		keys = append(keys, lookupKey)
	}
	if len(keys) == 0 {
		return nil
	}

	params := meteringrepo.ResolveAgentSessionStorageAttributesParams{
		ProjectIds:      make([]uuid.UUID, len(keys)),
		ChatIds:         make([]uuid.UUID, len(keys)),
		MessageUserIds:  make([]string, len(keys)),
		OrganizationIds: make([]string, len(keys)),
	}
	for i, key := range keys {
		params.OrganizationIds[i] = key.organizationID
		params.ProjectIds[i] = key.projectID
		params.ChatIds[i] = key.chatID
		params.MessageUserIds[i] = key.messageUserID
	}

	resolvedRows, err := meteringrepo.New(w.db).ResolveAgentSessionStorageAttributes(ctx, params)
	if err != nil {
		return fmt.Errorf("resolve agent session storage attributes: %w", err)
	}
	resolved := make(map[agentSessionStorageLookupKey]map[string]string, len(resolvedRows))
	for _, result := range resolvedRows {
		key := agentSessionStorageLookupKey{
			organizationID: result.OrganizationID,
			projectID:      result.ProjectID,
			chatID:         result.ChatID,
			messageUserID:  result.MessageUserID,
		}
		attributes := make(map[string]string)
		setResolvedAttribute(attributes, AttributeMessageUserAccountEmail, result.MessageUserAccountEmail)
		setResolvedAttribute(attributes, AttributeMessageUserDivisionName, result.MessageUserDivisionName)
		setResolvedAttribute(attributes, AttributeMessageUserDepartmentName, result.MessageUserDepartmentName)
		setResolvedAttribute(attributes, AttributeMessageUserDirectoryMatch, result.MessageUserDirectoryMatch)
		if len(result.MessageUserRoleSlugs) > 0 {
			rolesJSON, err := json.Marshal(result.MessageUserRoleSlugs)
			if err != nil {
				return fmt.Errorf("marshal message user roles: %w", err)
			}
			setResolvedAttribute(attributes, AttributeMessageUserRBACRoles, string(rolesJSON))
		}
		setResolvedAttribute(attributes, AttributeChatOwnerUserID, result.ChatOwnerUserID)
		setResolvedAttribute(attributes, AttributeChatOwnerExternalUserID, result.ChatOwnerExternalUserID)
		setResolvedAttribute(attributes, AttributeChatOwnerUserEmail, result.ChatOwnerUserEmail)
		setResolvedAttribute(attributes, AttributeChatOwnerDivisionName, result.ChatOwnerDivisionName)
		setResolvedAttribute(attributes, AttributeChatOwnerDepartmentName, result.ChatOwnerDepartmentName)
		setResolvedAttribute(attributes, AttributeChatOwnerDirectoryMatch, result.ChatOwnerDirectoryMatch)
		if len(result.ChatOwnerRoleSlugs) > 0 {
			rolesJSON, err := json.Marshal(result.ChatOwnerRoleSlugs)
			if err != nil {
				return fmt.Errorf("marshal chat owner roles: %w", err)
			}
			setResolvedAttribute(attributes, AttributeChatOwnerRBACRoles, string(rolesJSON))
		}
		resolved[key] = attributes
	}

	for i := range rows {
		key, ok := rowKeys[rows[i].ID]
		if !ok {
			continue
		}
		maps.Copy(rows[i].Attributes, resolved[key])
	}
	return nil
}

func meterReadingRow(message *meteringv1.MeterReading, insertedAt time.Time) (chrepo.ReadingRow, string) {
	var zero chrepo.ReadingRow
	if message == nil {
		return zero, "nil_reading"
	}

	id, err := uuid.Parse(message.GetId())
	if err != nil || id == uuid.Nil {
		return zero, "invalid_id"
	}
	projectID, err := uuid.Parse(message.GetProjectId())
	if err != nil || projectID == uuid.Nil {
		return zero, "invalid_project_id"
	}
	definition, ok := LookupDefinition(MeterID(message.GetMeterId()), message.GetMeterVersion())
	if !ok {
		return zero, "unknown_meter_definition"
	}
	if message.GetUnit() != string(definition.unit) {
		return zero, "invalid_unit"
	}
	if message.GetMeasurementMethod() != string(definition.measurementMethod) {
		return zero, "invalid_measurement_method"
	}

	occurredAt, err := time.Parse(time.RFC3339Nano, message.GetOccurredAt())
	if err != nil {
		return zero, "invalid_occurred_at"
	}
	producedAt, err := time.Parse(time.RFC3339Nano, message.GetProducedAt())
	if err != nil {
		return zero, "invalid_produced_at"
	}

	scope := ProjectScope(message.GetOrganizationId(), projectID)
	var reading Reading
	var correctsReadingID *uuid.UUID
	switch message.GetKind() {
	case meteringv1.MeterReading_KIND_USAGE:
		if message.GetCorrectsReadingId() != "" || message.GetAdjustmentReason() != "" {
			return zero, "invalid_usage"
		}
		reading, err = NewUsage(UsageInput{
			Meter:       definition,
			Scope:       scope,
			OperationID: message.GetOperationId(),
			Value:       message.GetValue(),
			OccurredAt:  occurredAt,
			ProducedAt:  producedAt,
			Source:      message.GetSource(),
			Attributes:  message.GetAttributes(),
		})
		if err != nil {
			return zero, "invalid_usage"
		}
	case meteringv1.MeterReading_KIND_ADJUSTMENT:
		correctionID, parseErr := uuid.Parse(message.GetCorrectsReadingId())
		if parseErr != nil {
			return zero, "invalid_correction_id"
		}
		reading, err = NewAdjustment(AdjustmentInput{
			Meter:             definition,
			Scope:             scope,
			OperationID:       message.GetOperationId(),
			Value:             message.GetValue(),
			OccurredAt:        occurredAt,
			ProducedAt:        producedAt,
			CorrectsReadingID: correctionID,
			Reason:            message.GetAdjustmentReason(),
			Source:            message.GetSource(),
			Attributes:        message.GetAttributes(),
		})
		if err != nil {
			return zero, "invalid_adjustment"
		}
		correctsReadingID = &correctionID
	default:
		return zero, "invalid_kind"
	}
	if reading.ID() != id {
		return zero, "non_deterministic_id"
	}

	attributes := maps.Clone(message.GetAttributes())
	if attributes == nil {
		attributes = make(map[string]string, 1)
	}
	if definition.unit == UnitSTokens {
		attributes["codec"] = string(definition.measurementMethod)
	}

	return chrepo.ReadingRow{
		ID:                id,
		OrganizationID:    scope.organizationID,
		ProjectID:         scope.projectID,
		MeterID:           string(definition.id),
		OperationID:       message.GetOperationId(),
		Unit:              string(definition.unit),
		Value:             message.GetValue(),
		OccurredAt:        occurredAt,
		ProducedAt:        producedAt,
		InsertedAt:        insertedAt,
		CorrectsReadingID: correctsReadingID,
		Attributes:        attributes,
	}, ""
}
