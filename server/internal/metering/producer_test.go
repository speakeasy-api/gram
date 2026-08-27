package metering_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestEnqueuePersistsDeterministicReadings(t *testing.T) {
	t.Parallel()
	conn, organizationID := newMeteringPostgres(t)
	ctx := t.Context()
	definition := metering.AgentSessionStorage()
	scope := metering.ProjectScope(organizationID, uuid.New())
	occurredAt := time.Date(2026, time.August, 25, 10, 11, 12, 123456789, time.UTC)
	producedAt := occurredAt.Add(time.Second)
	attributes := map[string]string{"diagnostic": "test"}

	ordinary, err := metering.NewUsage(metering.UsageInput{
		Meter:       definition,
		Scope:       scope,
		OperationID: "chat_message:" + uuid.NewString(),
		Value:       17,
		OccurredAt:  occurredAt,
		ProducedAt:  producedAt,
		Source:      "test",
		Attributes:  attributes,
	})
	require.NoError(t, err)
	attributes["diagnostic"] = "mutated"

	positiveAdjustment, err := metering.NewAdjustment(metering.AdjustmentInput{
		Meter:             definition,
		Scope:             scope,
		OperationID:       "adjustment:" + uuid.NewString(),
		Value:             3,
		OccurredAt:        occurredAt,
		ProducedAt:        producedAt,
		CorrectsReadingID: ordinary.ID(),
		Reason:            "source_reconciliation",
		Source:            "test",
		Attributes:        nil,
	})
	require.NoError(t, err)
	negativeAdjustment, err := metering.NewAdjustment(metering.AdjustmentInput{
		Meter:             definition,
		Scope:             scope,
		OperationID:       "adjustment:" + uuid.NewString(),
		Value:             -4,
		OccurredAt:        occurredAt,
		ProducedAt:        producedAt,
		CorrectsReadingID: ordinary.ID(),
		Reason:            "source_reconciliation",
		Source:            "test",
		Attributes:        nil,
	})
	require.NoError(t, err)

	tx, err := conn.Begin(ctx) //nolint:glint // transaction contains only package APIs and SQLc-generated queries
	require.NoError(t, err)
	require.NoError(t, metering.Enqueue(ctx, tx, []metering.Reading{ordinary, positiveAdjustment, negativeAdjustment}))
	require.NoError(t, tx.Commit(ctx))

	rows, err := testrepo.New(conn).ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 3)

	messages := make(map[string]*meteringv1.MeterReading, len(rows))
	for _, row := range rows {
		require.Equal(t, string(proto.MessageName(&meteringv1.MeterReading{})), row.Topic)
		message := &meteringv1.MeterReading{}
		require.NoError(t, proto.Unmarshal(row.Message, message))
		require.Equal(t, row.PublicID.String(), message.GetId())
		messages[message.GetOperationId()] = message
	}

	stored := messagesByID(messages, ordinary.ID().String())
	require.NotNil(t, stored)
	require.Equal(t, organizationID, stored.GetOrganizationId())
	projectID, ok := scope.ProjectID()
	require.True(t, ok)
	require.Equal(t, projectID.String(), stored.GetProjectId())
	require.Equal(t, string(metering.MeterAgentSessionStorage), stored.GetMeterId())
	require.Equal(t, uint32(1), stored.GetMeterVersion())
	require.Equal(t, string(metering.UnitSTokens), stored.GetUnit())
	require.Equal(t, string(metering.MeasurementTiktokenO200kBase), stored.GetMeasurementMethod())
	require.Equal(t, int64(17), stored.GetValue())
	require.Equal(t, occurredAt.Format(time.RFC3339Nano), stored.GetOccurredAt())
	require.Equal(t, producedAt.Format(time.RFC3339Nano), stored.GetProducedAt())
	require.Equal(t, "test", stored.GetAttributes()["diagnostic"])
	require.NotContains(t, stored.GetAttributes(), "codec")
	require.Equal(t, "test", stored.GetSource())
	require.Equal(t, meteringv1.MeterReading_KIND_USAGE, stored.GetKind())
	require.Empty(t, stored.GetCorrectsReadingId())
	require.Empty(t, stored.GetAdjustmentReason())

	positive := messagesByID(messages, positiveAdjustment.ID().String())
	negative := messagesByID(messages, negativeAdjustment.ID().String())
	require.NotNil(t, positive)
	require.NotNil(t, negative)
	require.Equal(t, ordinary.ID().String(), positive.GetCorrectsReadingId())
	require.Equal(t, ordinary.ID().String(), negative.GetCorrectsReadingId())
	require.Equal(t, int64(3), positive.GetValue())
	require.Equal(t, int64(-4), negative.GetValue())
	require.Equal(t, meteringv1.MeterReading_KIND_ADJUSTMENT, positive.GetKind())
	require.Equal(t, meteringv1.MeterReading_KIND_ADJUSTMENT, negative.GetKind())
	require.Equal(t, "source_reconciliation", positive.GetAdjustmentReason())
	require.Equal(t, "source_reconciliation", negative.GetAdjustmentReason())
}

func TestEnqueueRejectsMixedOrganizationBatchAtomically(t *testing.T) {
	t.Parallel()
	conn, organizationID := newMeteringPostgres(t)
	ctx := t.Context()
	definition := metering.AgentSessionStorage()
	now := time.Now().UTC()

	first, err := metering.NewUsage(metering.UsageInput{
		Meter:       definition,
		Scope:       metering.ProjectScope(organizationID, uuid.New()),
		OperationID: "chat_message:" + uuid.NewString(),
		Value:       1,
		OccurredAt:  now,
		ProducedAt:  now,
		Source:      "test",
		Attributes:  nil,
	})
	require.NoError(t, err)
	second, err := metering.NewUsage(metering.UsageInput{
		Meter:       definition,
		Scope:       metering.ProjectScope(organizationID+"_other", uuid.New()),
		OperationID: "chat_message:" + uuid.NewString(),
		Value:       1,
		OccurredAt:  now,
		ProducedAt:  now,
		Source:      "test",
		Attributes:  nil,
	})
	require.NoError(t, err)

	tx, err := conn.Begin(ctx) //nolint:glint // transaction contains only package APIs and SQLc-generated queries
	require.NoError(t, err)
	require.Error(t, metering.Enqueue(ctx, tx, []metering.Reading{first, second}))
	require.NoError(t, tx.Commit(ctx))

	rows, err := testrepo.New(conn).ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestEnqueueRollsBackWithCallerTransaction(t *testing.T) {
	t.Parallel()
	conn, organizationID := newMeteringPostgres(t)
	ctx := t.Context()
	now := time.Now().UTC()
	reading, err := metering.NewUsage(metering.UsageInput{
		Meter:       metering.AgentSessionStorage(),
		Scope:       metering.ProjectScope(organizationID, uuid.New()),
		OperationID: "chat_message:" + uuid.NewString(),
		Value:       9,
		OccurredAt:  now,
		ProducedAt:  now,
		Source:      "test",
		Attributes:  nil,
	})
	require.NoError(t, err)

	tx, err := conn.Begin(ctx) //nolint:glint // transaction contains only package APIs and SQLc-generated queries
	require.NoError(t, err)
	require.NoError(t, metering.Enqueue(ctx, tx, []metering.Reading{reading}))
	require.NoError(t, tx.Rollback(ctx))

	rows, err := testrepo.New(conn).ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func messagesByID(messages map[string]*meteringv1.MeterReading, id string) *meteringv1.MeterReading {
	for _, message := range messages {
		if message.GetId() == id {
			return message
		}
	}
	return nil
}
