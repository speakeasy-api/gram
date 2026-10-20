package analytics

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/analytics"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/sigint/chrepo"
	"github.com/speakeasy-api/gram/server/internal/sigint/evaluation"
	sigintrepo "github.com/speakeasy-api/gram/server/internal/sigint/repo"
	signalquery "github.com/speakeasy-api/gram/server/internal/telemetry/analytics/signals"
)

type signalFeaturesStub bool

func (s signalFeaturesStub) IsFeatureEnabled(context.Context, string, productfeatures.Feature) (bool, error) {
	return bool(s), nil
}

func TestSignalAPIEntitlementAndProjectAccess(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	_, err := ti.service.Query(ctx, &gen.QueryPayload{Dataset: "signals"})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.DimensionValues(ctx, &gen.DimensionValuesPayload{Dataset: "signals"})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.SignalDefinition(ctx, &gen.SignalDefinitionPayload{})
	requireOopsCode(t, err, oops.CodeForbidden)
	ti.service.signalFeatures = signalFeaturesStub(true)
	denied := authztest.WithExactGrants(t, ctx)
	_, err = ti.service.Describe(denied, &gen.DescribePayload{})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.Query(denied, &gen.QueryPayload{Dataset: "signals"})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestSignalAPIUsesSharedMeasurementAndDefinitionContracts(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ti.service.signalFeatures = signalFeaturesStub(true)
	project := uuid.MustParse(ti.projectID)
	q := sigintrepo.New(ti.db)
	label, err := q.CreateSignal(ctx, sigintrepo.CreateSignalParams{ID: uuid.New(), ProjectID: project, Name: "Topic", Slug: "topic", ClassifierCriteria: conv.ToPGText("A relevant topic")})
	require.NoError(t, err)
	sensor, err := q.CreateSensor(ctx, sigintrepo.CreateSensorParams{ID: uuid.New(), ProjectID: project, Name: "Example sensor", Slug: "example", Mode: "multi_label", Instructions: conv.ToPGText("Identify the topic"), Enabled: true, MatchExpression: "true"})
	require.NoError(t, err)
	require.NoError(t, q.ReplaceSensorSignals(ctx, sigintrepo.ReplaceSensorSignalsParams{ProjectID: project, SensorID: sensor.ID, SignalIds: []uuid.UUID{label.ID}}))
	loaded, err := evaluation.NewRepository(ti.db).Load(ctx, ti.organizationID, project, evaluation.ConversationMessageKind)
	require.NoError(t, err)
	snapshot, ready := evaluation.Snapshot(loaded[0])
	require.True(t, ready)
	catalog, err := ti.service.Describe(ctx, &gen.DescribePayload{})
	require.NoError(t, err)
	require.Len(t, catalog.SignalMetrics.Sensors, 1)
	require.Equal(t, "Example sensor", catalog.SignalMetrics.Sensors[0].Name)
	definition, err := ti.service.SignalDefinition(ctx, &gen.SignalDefinitionPayload{SensorID: sensor.ID.String(), DefinitionHash: snapshot.Hash})
	require.NoError(t, err)
	require.Equal(t, snapshot.Hash, definition.DefinitionHash)
	_, err = ti.service.engine.ReadSignalDefinition(ctx, Tenant{OrganizationID: "another-org", ProjectID: ti.projectID}, sensor.ID.String(), snapshot.Hash)
	require.Error(t, err)
	now := time.Now().UTC().Truncate(time.Second)
	conversation := uuid.New()
	for _, attributes := range []map[string]string{{"team.name": "engineering"}, {"team.name": ""}, {}} {
		row := chrepo.ReadingRow{ID: uuid.New(), OrganizationID: ti.organizationID, ProjectID: project, SensorID: sensor.ID, EventKind: "conversation.message", EventID: uuid.NewString(), TriggerKind: "stream", EvaluationRequestedAt: now, EventOccurredAt: now.Add(-time.Minute), EvaluatedAt: now, InsertedAt: now, Status: "answered", ResultType: "multi_label", DefinitionHash: snapshot.Hash, ConversationID: &conversation, ConversationCaptureKind: "original", Attributes: attributes, Probabilities: []chrepo.ProbabilityRow{{SignalID: label.ID, SignalSlug: "topic", Probability: 0.8}}}
		require.NoError(t, chrepo.New(ti.ch).InsertReadings(ctx, []chrepo.ReadingRow{row}))
	}
	payload := &gen.QueryPayload{Dataset: "signals", From: now.Add(-time.Hour).Format(time.RFC3339Nano), To: now.Format(time.RFC3339Nano), Grain: new("hour"), SignalQuery: &gen.AnalyticsSignalQuery{Selection: &gen.AnalyticsSignalSelection{SensorID: sensor.ID.String(), SignalID: new(label.ID.String()), Threshold: 0.5}, Groups: 5, Points: 100, GroupBy: []*gen.AnalyticsSignalDimension{{Attribute: new("team.name")}}}}
	api, err := ti.service.Query(ctx, payload)
	require.NoError(t, err)
	req, err := requestFromPayload(payload)
	require.NoError(t, err)
	shared, err := ti.service.engine.Query(ctx, Tenant{OrganizationID: ti.organizationID, ProjectID: ti.projectID}, req)
	require.NoError(t, err)
	require.Equal(t, api.Rows, signalRowsAsMaps(shared.Rows))
	columns := map[string]bool{}
	for _, column := range shared.Columns {
		columns[column.Name] = true
	}
	for _, row := range shared.Rows {
		for key := range row {
			require.True(t, columns[key], "undocumented signal row key %s", key)
		}
	}
	require.EqualValues(t, 3, api.SignalResult.Summary["matched_messages"])
	require.EqualValues(t, 1, api.SignalResult.Summary["matched_sessions"], "whole-window distinct sessions cannot be summed across attribute groups")
	require.True(t, api.SignalResult.Metadata.SeriesComplete)
	values, err := ti.service.DimensionValues(ctx, &gen.DimensionValuesPayload{Dataset: "signals", Dimension: "attribute", Attribute: new("team.name"), SignalScope: &gen.AnalyticsSignalScope{SensorID: sensor.ID.String()}, From: payload.From, To: payload.To, Limit: 10})
	require.NoError(t, err)
	require.Len(t, values.Values, 3)
	missing, empty := false, false
	for _, value := range values.Values {
		require.NotNil(t, value.Present)
		if !*value.Present {
			missing = true
		} else if value.Value == "" {
			empty = true
		}
	}
	require.True(t, missing)
	require.True(t, empty)
	_, err = q.DeleteSignal(ctx, sigintrepo.DeleteSignalParams{ID: label.ID, ProjectID: project})
	require.NoError(t, err)
	catalog, err = ti.service.Describe(ctx, &gen.DescribePayload{})
	require.NoError(t, err)
	require.Empty(t, catalog.SignalMetrics.Signals)
	require.Empty(t, catalog.SignalMetrics.Sensors[0].SignalIds)
}

func TestSignalManualValueDiscoveryRejectsInconsistentRuns(t *testing.T) {
	t.Parallel()
	conn := newTestClickhouse(t)
	engine, err := NewEngine(nil, conn)
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Second)
	project, sensor, run := uuid.New(), uuid.New(), uuid.New()
	tenant := Tenant{OrganizationID: "manual-values-" + uuid.NewString(), ProjectID: project.String()}
	row := chrepo.ReadingRow{ID: uuid.New(), OrganizationID: tenant.OrganizationID, ProjectID: project, SensorID: sensor, EventKind: "conversation.message", EventID: uuid.NewString(), TriggerKind: "manual", RunID: run, EvaluationRequestedAt: now, EventOccurredAt: now.Add(-time.Minute), EvaluatedAt: now, InsertedAt: now, Status: "failed", ConversationCaptureKind: "original", Attributes: map[string]string{"department": "engineering"}}
	require.NoError(t, chrepo.New(conn).InsertReadings(t.Context(), []chrepo.ReadingRow{row}))
	request := ValuesRequest{Dataset: "signals", Dimension: "attribute", Attribute: "department", FromUnixNano: now.Add(-time.Hour).UnixNano(), ToUnixNano: now.UnixNano(), SignalScope: &signalquery.Scope{SensorID: sensor.String(), Trigger: "manual", RunID: run.String()}}
	values, err := engine.signalValues(t.Context(), tenant, request)
	require.NoError(t, err)
	require.Len(t, values, 1)
	require.Equal(t, "engineering", values[0].Value)
	row.ID, row.EventID = uuid.New(), uuid.NewString()
	row.EvaluationRequestedAt, row.EventOccurredAt = now.Add(-time.Hour), now.Add(-2*time.Hour)
	require.NoError(t, chrepo.New(conn).InsertReadings(t.Context(), []chrepo.ReadingRow{row}))
	_, err = engine.signalValues(t.Context(), tenant, request)
	require.ErrorContains(t, err, "inconsistent request timestamps")
}

func signalRowsAsMaps(rows []Row) []map[string]any {
	out := make([]map[string]any, len(rows))
	for i, row := range rows {
		out[i] = row
	}
	return out
}
