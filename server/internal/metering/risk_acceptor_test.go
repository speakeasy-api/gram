package metering_test

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/metering/chrepo"
	meteringrepo "github.com/speakeasy-api/gram/server/internal/metering/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func riskCandidate(t *testing.T, reading *meteringv1.MeterReading) *meteringv1.RiskMeterReading {
	t.Helper()
	envelope, err := proto.Marshal(reading)
	require.NoError(t, err)
	candidate := new(meteringv1.RiskMeterReading)
	candidate.SetReading(envelope)
	return candidate
}

func TestRiskMeterAcceptorConcurrentFirstEnvelopeWinsAndReplayDoesNotReenqueue(t *testing.T) {
	t.Parallel()
	conn, organizationID, projectID, userID, first := newAcceptedRiskReading(t)
	second, ok := proto.Clone(first).(*meteringv1.MeterReading)
	require.True(t, ok)
	second.SetValue(91)
	second.SetOccurredAt("2026-09-12T01:02:03.123456789Z")
	second.SetProducedAt("2026-09-12T02:03:04.987654321Z")
	secondAttributes := second.GetAttributes()
	secondAttributes[metering.AttributeRiskPolicyVersion] = "99"
	second.SetAttributes(secondAttributes)

	acceptor := metering.NewRiskMeterAcceptor(testenv.NewLogger(t), conn)
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, candidate := range []*meteringv1.RiskMeterReading{riskCandidate(t, first), riskCandidate(t, second)} {
		wg.Go(func() {
			<-start
			errs <- acceptor.Handle(t.Context(), candidate, gcp.MessageMetadata{})
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	rows, err := testrepo.New(conn).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	accepted := new(meteringv1.MeterReading)
	require.NoError(t, proto.Unmarshal(rows[0].Message, accepted))
	require.Equal(t, first.GetId(), accepted.GetId())
	require.Contains(t, []int64{17, 91}, accepted.GetValue())
	if accepted.GetValue() == 17 {
		require.Equal(t, first.GetOccurredAt(), accepted.GetOccurredAt())
		require.Equal(t, first.GetProducedAt(), accepted.GetProducedAt())
		require.Equal(t, "3", accepted.GetAttributes()[metering.AttributeRiskPolicyVersion])
	} else {
		require.Equal(t, second.GetOccurredAt(), accepted.GetOccurredAt())
		require.Equal(t, second.GetProducedAt(), accepted.GetProducedAt())
		require.Equal(t, "99", accepted.GetAttributes()[metering.AttributeRiskPolicyVersion])
	}
	require.Equal(t, userID, accepted.GetAttributes()[metering.AttributeBillingUserID])
	require.Equal(t, "first@example.test", accepted.GetAttributes()[metering.AttributeBillingUserAccountEmail])

	require.NoError(t, meteringrepo.New(conn).DeleteRiskMeterReadingOutboxFixture(t.Context(), meteringrepo.DeleteRiskMeterReadingOutboxFixtureParams{
		ID: firstID(t, first), OrganizationID: organizationID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true},
	}))
	seedMeteringFacetUser(t, conn, organizationID, userID, "changed@example.test", meteringDirectoryFacets{DivisionName: "Changed Division"}, true)
	acceptor = metering.NewRiskMeterAcceptor(testenv.NewLogger(t), conn)
	require.NoError(t, acceptor.Handle(t.Context(), riskCandidate(t, second), gcp.MessageMetadata{}))
	rows, err = testrepo.New(conn).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)
	require.Empty(t, rows)
	envelope, err := meteringrepo.New(conn).GetRiskMeterReadingAcceptance(t.Context(), meteringrepo.GetRiskMeterReadingAcceptanceParams{
		ID: firstID(t, first), OrganizationID: organizationID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true},
	})
	require.NoError(t, err)
	persisted := new(meteringv1.MeterReading)
	require.NoError(t, proto.Unmarshal(envelope, persisted))
	require.True(t, proto.Equal(accepted, persisted))
}

func TestRiskStripeExporterUsesAcceptedQuantityAndEffectiveTime(t *testing.T) {
	t.Parallel()
	conn, organizationID, _, _, first := newAcceptedRiskReading(t)
	acceptor := metering.NewRiskMeterAcceptor(testenv.NewLogger(t), conn)
	require.NoError(t, acceptor.Handle(t.Context(), riskCandidate(t, first), gcp.MessageMetadata{}))
	require.NoError(t, testrepo.New(conn).CreateStripeBillingMetadataFixture(t.Context(), testrepo.CreateStripeBillingMetadataFixtureParams{
		OrganizationID:   organizationID,
		StripeCustomerID: pgtype.Text{String: "cus_risk_acceptance", Valid: true},
	}))

	changed, ok := proto.Clone(first).(*meteringv1.MeterReading)
	require.True(t, ok)
	changed.SetValue(999)
	changed.SetOccurredAt("2026-09-13T01:02:03.111222333Z")
	client := &captureV2MeterEventClient{inputs: nil, err: nil}
	exporter := metering.NewMeterReadingStripeExporter(
		testenv.NewLogger(t), testenv.NewMeterProvider(t), conn, conn, client,
		metering.StripeCatalogFunc(func(metering.Definition) (string, error) { return "risk", nil }), true,
	)
	require.NoError(t, exporter.Handle(t.Context(), changed, gcp.MessageMetadata{}))
	require.Len(t, client.inputs, 1)
	require.Equal(t, first.GetValue(), client.inputs[0].Value)
	expected, err := time.Parse(time.RFC3339Nano, first.GetOccurredAt())
	require.NoError(t, err)
	require.Equal(t, expected, client.inputs[0].Timestamp)
}

func TestRiskMeterAcceptorRollsBackReceiptWhenOutboxEnqueueFails(t *testing.T) {
	t.Parallel()
	conn, organizationID, projectID, _, reading := newAcceptedRiskReading(t)
	require.NoError(t, testrepo.New(conn).RejectPublishOutboxWritesFixture(t.Context()))
	acceptor := metering.NewRiskMeterAcceptor(testenv.NewLogger(t), conn)
	require.Error(t, acceptor.Handle(t.Context(), riskCandidate(t, reading), gcp.MessageMetadata{}))
	_, err := meteringrepo.New(conn).GetRiskMeterReadingAcceptance(t.Context(), meteringrepo.GetRiskMeterReadingAcceptanceParams{
		ID: firstID(t, reading), OrganizationID: organizationID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true},
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestRiskMeterAcceptorKeepsSignedAdjustmentsDistinct(t *testing.T) {
	t.Parallel()
	conn, organizationID, projectID, _, usage := newAcceptedRiskReading(t)
	acceptor := metering.NewRiskMeterAcceptor(testenv.NewLogger(t), conn)
	for _, operationID := range []string{"adjustment-a", "adjustment-b"} {
		message, _ := adjustmentMessage(t, metering.AdjustmentInput{
			Meter: metering.RiskPresidio(), Scope: metering.ProjectScope(organizationID, projectID), OperationID: operationID,
			Value: -3, OccurredAt: time.Now().UTC(), ProducedAt: time.Now().UTC(), CorrectsReadingID: firstID(t, usage),
			Reason: "source_reconciliation", Source: "risk_scanner", Attributes: nil,
		})
		require.NoError(t, acceptor.Handle(t.Context(), riskCandidate(t, message), gcp.MessageMetadata{}))
	}
	rows, err := testrepo.New(conn).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)
	require.Len(t, rows, 2)
	ids := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		accepted := new(meteringv1.MeterReading)
		require.NoError(t, proto.Unmarshal(row.Message, accepted))
		require.NotContains(t, ids, accepted.GetId())
		ids[accepted.GetId()] = struct{}{}
		require.Equal(t, meteringv1.MeterReading_KIND_ADJUSTMENT, accepted.GetKind())
		require.Equal(t, int64(-3), accepted.GetValue())
		require.Equal(t, usage.GetId(), accepted.GetCorrectsReadingId())
	}
}

func TestRiskAcceptanceOutboxProjectsImmutableEnvelopeToClickHouse(t *testing.T) {
	t.Parallel()
	conn, organizationID, projectID, userID, first := newAcceptedRiskReading(t)
	acceptor := metering.NewRiskMeterAcceptor(testenv.NewLogger(t), conn)
	require.NoError(t, acceptor.Handle(t.Context(), riskCandidate(t, first), gcp.MessageMetadata{}))

	changed, ok := proto.Clone(first).(*meteringv1.MeterReading)
	require.True(t, ok)
	changed.SetValue(999)
	changed.SetOccurredAt("2026-10-13T01:02:03.111222333Z")
	changed.SetProducedAt("2026-10-13T04:05:06.777888999Z")
	changedAttributes := changed.GetAttributes()
	changedAttributes[metering.AttributeRiskPolicyVersion] = "999"
	changed.SetAttributes(changedAttributes)
	seedMeteringFacetUser(t, conn, organizationID, userID, "changed@example.test", meteringDirectoryFacets{DivisionName: "Changed Division"}, true)
	require.NoError(t, acceptor.Handle(t.Context(), riskCandidate(t, changed), gcp.MessageMetadata{}))

	outboxRows, err := testrepo.New(conn).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)
	require.Len(t, outboxRows, 1)
	canonical := new(meteringv1.MeterReading)
	require.NoError(t, proto.Unmarshal(outboxRows[0].Message, canonical))
	clickhouse, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	writer := metering.NewMeterReadingCHWriter(testenv.NewLogger(t), conn, chrepo.New(clickhouse))
	require.NoError(t, writer.HandleBatch(t.Context(), []*meteringv1.MeterReading{canonical, changed}, nil))
	require.NoError(t, writer.HandleBatch(t.Context(), []*meteringv1.MeterReading{canonical}, nil))

	var count uint64
	var value int64
	var occurredAt, producedAt time.Time
	var attributes map[string]string
	require.NoError(t, clickhouse.QueryRow(t.Context(), `
		SELECT count(), any(value), any(occurred_at), any(produced_at), any(attributes)
		FROM billing_meter_readings_by_time FINAL
		WHERE organization_id = ? AND project_id = ? AND meter_id = ? AND id = ?
		SETTINGS do_not_merge_across_partitions_select_final = 1
	`, organizationID, projectID, string(metering.MeterRiskPresidio), firstID(t, first)).Scan(&count, &value, &occurredAt, &producedAt, &attributes))
	require.Equal(t, uint64(1), count)
	require.Equal(t, first.GetValue(), value)
	expectedOccurredAt, err := time.Parse(time.RFC3339Nano, first.GetOccurredAt())
	require.NoError(t, err)
	expectedProducedAt, err := time.Parse(time.RFC3339Nano, first.GetProducedAt())
	require.NoError(t, err)
	require.Equal(t, expectedOccurredAt, occurredAt)
	require.Equal(t, expectedProducedAt, producedAt)
	require.Equal(t, "3", attributes[metering.AttributeRiskPolicyVersion])
	require.Equal(t, "first@example.test", attributes[metering.AttributeBillingUserAccountEmail])
}

func TestRiskMeterAcceptorRejectsPartialProvenanceBeforeFullRetry(t *testing.T) {
	t.Parallel()
	conn, organizationID, _, _, complete := newAcceptedRiskReading(t)
	incomplete := proto.CloneOf(complete)
	incomplete.SetAttributes(nil)
	acceptor := metering.NewRiskMeterAcceptor(testenv.NewLogger(t), conn)
	require.NoError(t, acceptor.Handle(t.Context(), riskCandidate(t, incomplete), gcp.MessageMetadata{}))
	rows, err := testrepo.New(conn).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)
	require.Empty(t, rows)

	// A legacy poison message must not prevent a complete reading in the same
	// delivery batch from being accepted and projected.
	capture := &captureReadingInserter{rows: nil, err: nil}
	writer := metering.NewMeterReadingCHWriter(testenv.NewLogger(t), conn, capture)
	require.NoError(t, writer.HandleBatch(t.Context(), []*meteringv1.MeterReading{incomplete, complete}, nil))
	require.Len(t, capture.rows, 1)
	require.Equal(t, organizationID, capture.rows[0].OrganizationID)
	require.Equal(t, complete.GetAttributes()[metering.AttributeRiskPolicyID], capture.rows[0].Attributes[metering.AttributeRiskPolicyID])
	rows, err = testrepo.New(conn).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

func TestRiskMeterAcceptorRejectsProjectFromAnotherOrganization(t *testing.T) {
	t.Parallel()
	conn, _, projectID, _, _ := newAcceptedRiskReading(t)
	provenance := riskProvenance()
	provenance.OrganizationID = "other-organization-" + uuid.NewString()
	provenance.ProjectID = projectID
	reading, err := metering.PrepareRiskReading(metering.RiskPresidio(), provenance, 7, time.Now().UTC())
	require.NoError(t, err)
	acceptor := metering.NewRiskMeterAcceptor(testenv.NewLogger(t), conn)
	require.Error(t, acceptor.Handle(t.Context(), riskCandidate(t, reading), gcp.MessageMetadata{}))
	rows, err := testrepo.New(conn).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)
	require.Empty(t, rows)
	_, err = meteringrepo.New(conn).GetRiskMeterReadingAcceptance(t.Context(), meteringrepo.GetRiskMeterReadingAcceptanceParams{
		ID: firstID(t, reading), OrganizationID: provenance.OrganizationID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true},
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestRiskStripeExporterDoesNotExportAcceptedAdjustmentAsUsage(t *testing.T) {
	t.Parallel()
	conn, organizationID, projectID, _, original := newAcceptedRiskReading(t)
	now := time.Now().UTC()
	operationID := "adjustment-" + uuid.NewString()
	adjustment, _ := adjustmentMessage(t, metering.AdjustmentInput{
		Meter: metering.RiskPresidio(), Scope: metering.ProjectScope(organizationID, projectID), OperationID: operationID,
		Value: -3, OccurredAt: now, ProducedAt: now, CorrectsReadingID: firstID(t, original),
		Reason: "source_reconciliation", Source: "risk_scanner", Attributes: nil,
	})
	usage, _ := usageMessage(t, metering.UsageInput{
		Meter: metering.RiskPresidio(), Scope: metering.ProjectScope(organizationID, projectID), OperationID: operationID,
		Value: 3, OccurredAt: now, ProducedAt: now, Source: "risk_scanner", Attributes: original.GetAttributes(),
	})
	require.Equal(t, adjustment.GetId(), usage.GetId())
	acceptor := metering.NewRiskMeterAcceptor(testenv.NewLogger(t), conn)
	require.NoError(t, acceptor.Handle(t.Context(), riskCandidate(t, adjustment), gcp.MessageMetadata{}))
	client := &captureV2MeterEventClient{inputs: nil, err: nil}
	exporter := metering.NewMeterReadingStripeExporter(
		testenv.NewLogger(t), testenv.NewMeterProvider(t), conn, nil, client,
		metering.StripeCatalogFunc(func(metering.Definition) (string, error) { return "risk", nil }), true,
	)
	require.NoError(t, exporter.Handle(t.Context(), usage, gcp.MessageMetadata{}))
	require.Empty(t, client.inputs)
}

func firstID(t *testing.T, reading *meteringv1.MeterReading) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(reading.GetId())
	require.NoError(t, err)
	return id
}
