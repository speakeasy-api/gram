package analytics

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/analytics"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	sigintchrepo "github.com/speakeasy-api/gram/server/internal/sigint/chrepo"
	sigintRepo "github.com/speakeasy-api/gram/server/internal/sigint/repo"
)

func sensorQueryPayload(dataset, from, to string, dimensions ...string) *gen.QueryPayload {
	return &gen.QueryPayload{
		SessionToken: nil, ProjectSlugInput: nil,
		Dataset: dataset, From: from, To: to, Grain: nil,
		Dimensions: dimensions,
		Measures:   []*gen.AnalyticsMeasure{{Op: "count", Field: nil, Alias: nil}},
		Filters:    nil, OrderBy: nil, Limit: 0, Ungrouped: false,
	}
}

func TestSensorDatasetsNeedSignalsIntelligence(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	from, to := time.Now().Add(-time.Hour).Format(time.RFC3339), time.Now().Format(time.RFC3339)

	described, err := ti.service.Describe(ctx, &gen.DescribePayload{SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	for _, ds := range described.Datasets {
		require.NotContains(t, []string{SensorEvaluations.Name, SensorSignals.Name}, ds.Name, "describe hides a dataset the organization is not entitled to")
	}
	for _, dataset := range []string{SensorEvaluations.Name, SensorSignals.Name} {
		_, err = ti.service.Query(ctx, sensorQueryPayload(dataset, from, to, "sensor"))
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "unknown_dataset", "the refusal is the one an unknown dataset gets")
		_, err = ti.service.DimensionValues(ctx, &gen.DimensionValuesPayload{SessionToken: nil, ProjectSlugInput: nil, Dataset: dataset, Dimension: "sensor", From: from, To: to, Limit: 0})
		requireOopsCode(t, err, oops.CodeBadRequest)
	}

	ti.features.set(productfeatures.FeatureSignalsIntelligence, true)
	described, err = ti.service.Describe(ctx, &gen.DescribePayload{SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	evaluations := datasetNamed(t, described.Datasets, SensorEvaluations.Name)
	require.Contains(t, evaluations.Description, "never processed", "describe says coverage excludes unprocessed messages")
	signals := datasetNamed(t, described.Datasets, SensorSignals.Name)
	require.Contains(t, signals.Description, "not measured accuracy")
	require.Contains(t, signals.Description, "count_distinct over message")
	require.Equal(t, []string{"sensor", "signal"}, defaultFields(signals))
	_, err = ti.service.Query(ctx, sensorQueryPayload(SensorEvaluations.Name, from, to, "sensor"))
	require.NoError(t, err)
}

func TestSensorSignalsReadCurrentNamesAndMinimumConfidence(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ti.features.set(productfeatures.FeatureSignalsIntelligence, true)

	projectID := uuid.MustParse(ti.projectID)
	queries := sigintRepo.New(ti.db)
	sensorID, signalID := uuid.New(), uuid.New()
	_, err := queries.CreateSensor(ctx, sigintRepo.CreateSensorParams{
		ID: sensorID, ProjectID: projectID, Name: "Primary intent", Slug: "primary-intent",
		Description: pgtype.Text{String: "", Valid: false}, Instructions: pgtype.Text{String: "", Valid: false},
		Mode: "exclusive", Enabled: true, MatchExpression: `message.role == "user"`,
		InputStrategy: pgtype.Text{String: "", Valid: false}, WindowSize: pgtype.Int4{Int32: 0, Valid: false},
		MinConfidence: pgtype.Float8{Float64: 0.6, Valid: true},
	})
	require.NoError(t, err)
	_, err = queries.CreateSignal(ctx, sigintRepo.CreateSignalParams{
		ID: signalID, ProjectID: projectID, Name: "Billing", Slug: "billing",
		Description: pgtype.Text{String: "", Valid: false}, ClassifierCriteria: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	tenant := Tenant{OrganizationID: ti.organizationID, ProjectID: ti.projectID}
	base := time.Now().UTC().Add(-time.Hour)
	require.NoError(t, sigintchrepo.New(ti.ch).InsertReadings(ctx, []sigintchrepo.ReadingRow{
		choiceFixture(readingFixture(tenant, uuid.New(), sensorID, "m1", base, base), signalID, 0.55),
		choiceFixture(readingFixture(tenant, uuid.New(), sensorID, "m2", base, base), signalID, 0.65),
	}))

	from, to := base.Add(-time.Hour).Format(time.RFC3339), base.Add(time.Hour).Format(time.RFC3339)
	result, err := ti.service.Query(ctx, sensorQueryPayload(SensorSignals.Name, from, to, "sensor", "signal", "answer_status"))
	require.NoError(t, err)
	require.Len(t, result.Rows, 2)
	statuses := map[string]any{}
	for _, row := range result.Rows {
		require.Equal(t, "Primary intent", row["sensor"])
		require.Equal(t, "Billing", row["signal"])
		status, ok := row["answer_status"].(string)
		require.True(t, ok)
		statuses[status] = row["count"]
	}
	require.EqualValues(t, 1, statuses["confident"], "0.65 reaches the sensor's 0.6 minimum")
	require.EqualValues(t, 1, statuses["uncertain"], "0.55 does not")
	require.Equal(t, "sensor_signals.sigint_readings", result.Plan)

	// Clearing the minimum reinterprets the same readings without re-evaluation.
	_, err = queries.UpdateSensor(ctx, sigintRepo.UpdateSensorParams{
		ID: sensorID, ProjectID: projectID, Name: "Primary intent", Slug: "primary-intent",
		Description: pgtype.Text{String: "", Valid: false}, Instructions: pgtype.Text{String: "", Valid: false},
		Mode: "exclusive", Enabled: true, MatchExpression: `message.role == "user"`,
		InputStrategy: pgtype.Text{String: "", Valid: false}, WindowSize: pgtype.Int4{Int32: 0, Valid: false},
		MinConfidence: pgtype.Float8{Float64: 0, Valid: false},
	})
	require.NoError(t, err)
	result, err = ti.service.Query(ctx, sensorQueryPayload(SensorSignals.Name, from, to, "answer_status"))
	require.NoError(t, err)
	require.Len(t, result.Rows, 1)
	require.Equal(t, "confident", result.Rows[0]["answer_status"])
	require.EqualValues(t, 2, result.Rows[0]["count"])
}
