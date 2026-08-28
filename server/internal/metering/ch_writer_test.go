package metering_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/metering/chrepo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type captureReadingInserter struct {
	rows []chrepo.ReadingRow
	err  error
}

func (c *captureReadingInserter) InsertReadings(_ context.Context, rows []chrepo.ReadingRow) error {
	c.rows = append(c.rows, rows...)
	return c.err
}

func usageMessage(t *testing.T, input metering.UsageInput) (*meteringv1.MeterReading, metering.Reading) {
	t.Helper()
	reading, err := metering.NewUsage(input)
	require.NoError(t, err)
	message := commonMessage(reading.ID(), input.Scope, input.OperationID, input.Value, input.OccurredAt, input.ProducedAt, input.Source, input.Attributes)
	message.SetKind(meteringv1.MeterReading_KIND_USAGE)
	return message, reading
}

func adjustmentMessage(t *testing.T, input metering.AdjustmentInput) (*meteringv1.MeterReading, metering.Reading) {
	t.Helper()
	reading, err := metering.NewAdjustment(input)
	require.NoError(t, err)
	message := commonMessage(reading.ID(), input.Scope, input.OperationID, input.Value, input.OccurredAt, input.ProducedAt, input.Source, input.Attributes)
	message.SetKind(meteringv1.MeterReading_KIND_ADJUSTMENT)
	message.SetCorrectsReadingId(input.CorrectsReadingID.String())
	message.SetAdjustmentReason(input.Reason)
	return message, reading
}

func commonMessage(
	id uuid.UUID,
	scope metering.Scope,
	operationID string,
	value int64,
	occurredAt time.Time,
	producedAt time.Time,
	source string,
	attributes map[string]string,
) *meteringv1.MeterReading {
	message := &meteringv1.MeterReading{}
	message.SetId(id.String())
	message.SetOrganizationId(scope.OrganizationID())
	if projectID, ok := scope.ProjectID(); ok {
		message.SetProjectId(projectID.String())
	}
	message.SetMeterId(string(metering.MeterAgentSessionStorage))
	message.SetOperationId(operationID)
	message.SetUnit(string(metering.UnitSTokens))
	message.SetValue(value)
	message.SetOccurredAt(occurredAt.Format(time.RFC3339Nano))
	message.SetAttributes(attributes)
	message.SetMeterVersion(1)
	message.SetProducedAt(producedAt.Format(time.RFC3339Nano))
	message.SetMeasurementMethod(string(metering.MeasurementTiktokenO200kBase))
	message.SetSource(source)
	return message
}

func TestMeterReadingCHWriterSkipsPoisonAndDeduplicatesBatch(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	input := metering.UsageInput{
		Meter:       metering.AgentSessionStorage(),
		Scope:       metering.ProjectScope("org-"+uuid.NewString(), uuid.New()),
		OperationID: "chat_message:" + uuid.NewString(),
		Value:       7,
		OccurredAt:  now,
		ProducedAt:  now,
		Source:      "test",
		Attributes:  nil,
	}
	valid, reading := usageMessage(t, input)
	cloned := proto.Clone(valid)
	invalid, ok := cloned.(*meteringv1.MeterReading)
	require.True(t, ok)
	invalid.SetId("not-a-uuid")
	capture := &captureReadingInserter{rows: nil, err: nil}
	writer := metering.NewMeterReadingCHWriter(testenv.NewLogger(t), capture)

	require.NoError(t, writer.HandleBatch(t.Context(), []*meteringv1.MeterReading{nil, invalid, valid, valid}, nil))
	require.Len(t, capture.rows, 1)
	require.Equal(t, reading.ID(), capture.rows[0].ID)
	require.Equal(t, input.Value, capture.rows[0].Value)
	require.Equal(t, string(metering.MeasurementTiktokenO200kBase), capture.rows[0].Attributes["codec"])
}

func TestMeterReadingCHWriterPropagatesInsertFailure(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	message, _ := usageMessage(t, metering.UsageInput{
		Meter:       metering.AgentSessionStorage(),
		Scope:       metering.ProjectScope("org-"+uuid.NewString(), uuid.New()),
		OperationID: "chat_message:" + uuid.NewString(),
		Value:       4,
		OccurredAt:  now,
		ProducedAt:  now,
		Source:      "test",
		Attributes:  nil,
	})
	capture := &captureReadingInserter{rows: nil, err: errors.New("clickhouse unavailable")}
	writer := metering.NewMeterReadingCHWriter(testenv.NewLogger(t), capture)
	require.Error(t, writer.HandleBatch(t.Context(), []*meteringv1.MeterReading{message}, nil))
}

func TestMeterReadingCHWriterRedeliveryConvergesAndPreservesAdjustment(t *testing.T) {
	t.Parallel()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	definition := metering.AgentSessionStorage()
	scope := metering.ProjectScope("org-"+uuid.NewString(), uuid.New())
	now := time.Now().UTC()
	usageInput := metering.UsageInput{
		Meter:       definition,
		Scope:       scope,
		OperationID: "chat_message:" + uuid.NewString(),
		Value:       11,
		OccurredAt:  now,
		ProducedAt:  now,
		Source:      "test",
		Attributes:  nil,
	}
	usageEvent, usage := usageMessage(t, usageInput)
	adjustmentEvent, _ := adjustmentMessage(t, metering.AdjustmentInput{
		Meter:             definition,
		Scope:             scope,
		OperationID:       usageInput.OperationID + ":adjustment",
		Value:             -5,
		OccurredAt:        now,
		ProducedAt:        now.Add(time.Second),
		CorrectsReadingID: usage.ID(),
		Reason:            "source_reconciliation",
		Source:            "test",
		Attributes:        nil,
	})
	writer := metering.NewMeterReadingCHWriter(testenv.NewLogger(t), chrepo.New(conn))
	require.NoError(t, writer.HandleBatch(t.Context(), []*meteringv1.MeterReading{usageEvent}, nil))
	require.NoError(t, writer.HandleBatch(t.Context(), []*meteringv1.MeterReading{usageEvent}, nil))
	require.NoError(t, writer.HandleBatch(t.Context(), []*meteringv1.MeterReading{adjustmentEvent}, nil))

	var count uint64
	var net int64
	projectID, ok := scope.ProjectID()
	require.True(t, ok)
	require.NoError(t, conn.QueryRow(t.Context(), `
		SELECT count(), sum(value)
		FROM billing_meter_readings FINAL
		WHERE organization_id = ? AND project_id = ? AND meter_id = ?
	`, scope.OrganizationID(), projectID, string(metering.MeterAgentSessionStorage)).Scan(&count, &net))
	require.Equal(t, uint64(2), count)
	require.Equal(t, int64(6), net)
}
