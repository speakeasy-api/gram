package analytics

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	sigintchrepo "github.com/speakeasy-api/gram/server/internal/sigint/chrepo"
)

// readingFixture is one stored attempt of a reading of message by sensor.
func readingFixture(tenant Tenant, id, sensor uuid.UUID, message string, occurredAt, evaluatedAt time.Time) sigintchrepo.ReadingRow {
	conversation := uuid.NewSHA1(uuid.NameSpaceOID, []byte("conversation-"+message))
	return sigintchrepo.ReadingRow{
		ID:                         id,
		EvaluationAttemptID:        uuid.New(),
		OrganizationID:             tenant.OrganizationID,
		ProjectID:                  uuid.MustParse(tenant.ProjectID),
		SensorID:                   sensor,
		SensorSlug:                 "sensor",
		EventKind:                  "conversation.message",
		EventID:                    message,
		EventOccurredAt:            occurredAt,
		EvaluatedAt:                evaluatedAt,
		InsertedAt:                 evaluatedAt,
		Status:                     sigintchrepo.StatusAnswered,
		FailureReason:              "",
		DefinitionHash:             "hash",
		ConfiguredModel:            "model",
		Models:                     nil,
		CompilerVersion:            "v1",
		Provider:                   "test",
		InputStrategy:              "single_message",
		InputRequestedMessageCount: 1,
		InputMessageCount:          1,
		InputTruncated:             false,
		EventContextType:           "",
		ConversationID:             &conversation,
		ConversationRole:           "user",
		ToolName:                   "",
		ToolCallID:                 "",
		MCPServerID:                nil,
		ActorUserID:                "",
		ActorExternalUserID:        "",
		ActorUserEmail:             "dev@example.com",
		BillingUserID:              "",
		Source:                     "claude-code",
		UserAccountID:              nil,
		AccountType:                "",
		BillingMode:                "",
		AssistantID:                nil,
		Replayed:                   nil,
		Attributes:                 map[string]string{},
		ResultType:                 "",
		Probabilities:              nil,
		SelectedSignalID:           nil,
		SelectedSignalSlug:         "",
		ExpectedIndex:              nil,
		Confidence:                 nil,
	}
}

func outcomeFixture(row sigintchrepo.ReadingRow, status, reason string) sigintchrepo.ReadingRow {
	row.Status = status
	row.FailureReason = reason
	return row
}

func multiLabelFixture(row sigintchrepo.ReadingRow, probabilities map[uuid.UUID]float64, order []uuid.UUID) sigintchrepo.ReadingRow {
	row.ResultType = "multi_label"
	for _, id := range order {
		row.Probabilities = append(row.Probabilities, sigintchrepo.ProbabilityRow{SignalID: id, SignalSlug: id.String(), Probability: probabilities[id]})
	}
	return row
}

func choiceFixture(row sigintchrepo.ReadingRow, selected uuid.UUID, confidence float64) sigintchrepo.ReadingRow {
	row.ResultType = "choice"
	row.SelectedSignalID = &selected
	row.SelectedSignalSlug = selected.String()
	row.Probabilities = []sigintchrepo.ProbabilityRow{{SignalID: selected, SignalSlug: selected.String(), Probability: 0.7}}
	row.Confidence = &confidence
	return row
}

func scoreFixture(row sigintchrepo.ReadingRow, levels []uuid.UUID, probabilities []float64, confidence *float64) sigintchrepo.ReadingRow {
	row.ResultType = "score"
	expected := 0.0
	for i, id := range levels {
		row.Probabilities = append(row.Probabilities, sigintchrepo.ProbabilityRow{SignalID: id, SignalSlug: id.String(), Probability: probabilities[i]})
		expected += float64(i) * probabilities[i]
	}
	row.ExpectedIndex = &expected
	row.Confidence = confidence
	return row
}

// runSensorQuery runs a grouped count over the dataset and keys each row by
// its dimension values.
func runSensorQuery(t *testing.T, tenant Tenant, maps LookupMaps, window Window, dataset string, dimensions []string, measures []Measure, filters []Filter) map[string]map[string]any {
	t.Helper()
	plan, err := Compile(Default, tenant, maps, Request{
		Dataset:      dataset,
		FromUnixNano: window.FromUnixNano,
		ToUnixNano:   window.ToUnixNano,
		Grain:        TimeGrainNone,
		Dimensions:   dimensions,
		Measures:     measures,
		Filters:      filters,
		OrderBy:      nil,
		Limit:        0,
		Ungrouped:    false,
	})
	require.NoError(t, err)
	rows, err := plan.Run(t.Context(), newTestClickhouse(t))
	require.NoError(t, err)
	out := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		key := ""
		for i, dim := range dimensions {
			if i > 0 {
				key += "|"
			}
			key += fmt.Sprint(row[dim])
		}
		out[key] = row
	}
	return out
}

func TestSensorEvaluationsKeepTheLatestAttempt(t *testing.T) {
	t.Parallel()

	conn := newTestClickhouse(t)
	tenant := Tenant{OrganizationID: "org-" + uuid.NewString(), ProjectID: uuid.NewString()}
	other := Tenant{OrganizationID: "org-" + uuid.NewString(), ProjectID: uuid.NewString()}
	sensorA, sensorB := uuid.New(), uuid.New()
	base := time.Now().UTC().Add(-time.Hour)
	later := base.Add(10 * time.Minute)

	failedThenAnswered := uuid.New()
	answeredThenFailed := uuid.New()
	rows := []sigintchrepo.ReadingRow{
		// A failed attempt the retry answered: one answered evaluation.
		outcomeFixture(readingFixture(tenant, failedThenAnswered, sensorA, "m1", base, base), sigintchrepo.StatusFailed, "classifier_rejected"),
		choiceFixture(readingFixture(tenant, failedThenAnswered, sensorA, "m1", base, later), uuid.New(), 0.9),
		// An answer the retry replaced with a failure: one failed evaluation.
		choiceFixture(readingFixture(tenant, answeredThenFailed, sensorA, "m2", base, base), uuid.New(), 0.9),
		outcomeFixture(readingFixture(tenant, answeredThenFailed, sensorA, "m2", base, later), sigintchrepo.StatusFailed, "invalid_answer"),
		// The same answer delivered twice is still one evaluation.
		choiceFixture(readingFixture(tenant, uuid.New(), sensorB, "m1", base, base), uuid.New(), 0.9),
		outcomeFixture(readingFixture(tenant, uuid.New(), sensorA, "m3", base, base), sigintchrepo.StatusNotApplicable, ""),
		// Another tenant's evaluation and one outside the window.
		outcomeFixture(readingFixture(other, uuid.New(), sensorA, "m4", base, base), sigintchrepo.StatusNotApplicable, ""),
		outcomeFixture(readingFixture(tenant, uuid.New(), sensorA, "m5", base.Add(-48*time.Hour), base), sigintchrepo.StatusDraft, ""),
	}
	duplicate := rows[4]
	rows = append(rows, duplicate)
	require.NoError(t, sigintchrepo.New(conn).InsertReadings(t.Context(), rows))

	window := Window{FromUnixNano: base.Add(-time.Hour).UnixNano(), ToUnixNano: base.Add(time.Hour).UnixNano()}
	byStatus := runSensorQuery(t, tenant, nil, window, SensorEvaluations.Name, []string{"status", "failure_reason"}, []Measure{{Op: "count", Field: "", Alias: ""}}, nil)
	require.Len(t, byStatus, 3)
	require.EqualValues(t, 2, byStatus["answered|"]["count"])
	require.EqualValues(t, 1, byStatus["failed|invalid_answer"]["count"], "the latest attempt wins over the earlier answer")
	require.EqualValues(t, 1, byStatus["not_applicable|"]["count"])

	coverage := runSensorQuery(t, tenant, nil, window, SensorEvaluations.Name, []string{"sensor"}, []Measure{
		{Op: "count", Field: "", Alias: ""},
		{Op: "count_distinct", Field: "message", Alias: "messages"},
		{Op: "count_distinct", Field: "conversation", Alias: "conversations"},
		{Op: "count_distinct", Field: "user", Alias: "people"},
	}, nil)
	require.EqualValues(t, 3, coverage[sensorA.String()]["count"])
	require.EqualValues(t, 3, coverage[sensorA.String()]["messages"])
	require.EqualValues(t, 3, coverage[sensorA.String()]["conversations"])
	require.EqualValues(t, 1, coverage[sensorA.String()]["people"])
	require.EqualValues(t, 1, coverage[sensorB.String()]["count"], "a redelivered row is not a second evaluation")

	names := LookupMaps{SensorNamesLookup: {sensorA.String(): "Intent"}}
	named := runSensorQuery(t, tenant, names, window, SensorEvaluations.Name, []string{"sensor"}, []Measure{{Op: "count", Field: "", Alias: ""}}, []Filter{{Field: "sensor", Operator: "equals", Values: []string{"Intent"}}})
	require.Len(t, named, 1)
	require.EqualValues(t, 3, named["Intent"]["count"], "a sensor reads and filters under its current name")
}

func TestSensorSignalsAnswerEachMode(t *testing.T) {
	t.Parallel()

	conn := newTestClickhouse(t)
	tenant := Tenant{OrganizationID: "org-" + uuid.NewString(), ProjectID: uuid.NewString()}
	multi, exclusive, ordered := uuid.New(), uuid.New(), uuid.New()
	billing, outage := uuid.New(), uuid.New()
	choice := uuid.New()
	low, medium, high := uuid.New(), uuid.New(), uuid.New()
	base := time.Now().UTC().Add(-time.Hour)
	later := base.Add(time.Minute)

	superseded := uuid.New()
	rows := []sigintchrepo.ReadingRow{
		multiLabelFixture(readingFixture(tenant, uuid.New(), multi, "m1", base, base), map[uuid.UUID]float64{billing: 0.9, outage: 0.3}, []uuid.UUID{billing, outage}),
		choiceFixture(readingFixture(tenant, uuid.New(), exclusive, "m1", base, base), choice, 0.5),
		choiceFixture(readingFixture(tenant, uuid.New(), exclusive, "m2", base, base), choice, 0.75),
		// The most likely level is medium, while the expected index (1.15)
		// would not say so on its own; no confidence was reported.
		scoreFixture(readingFixture(tenant, uuid.New(), ordered, "m1", base, base), []uuid.UUID{low, medium, high}, []float64{0.2, 0.45, 0.35}, nil),
		// An answer a later failure replaced contributes no signal rows.
		multiLabelFixture(readingFixture(tenant, superseded, multi, "m2", base, base), map[uuid.UUID]float64{billing: 0.9, outage: 0.9}, []uuid.UUID{billing, outage}),
		outcomeFixture(readingFixture(tenant, superseded, multi, "m2", base, later), sigintchrepo.StatusFailed, "invalid_answer"),
		outcomeFixture(readingFixture(tenant, uuid.New(), multi, "m3", base, base), sigintchrepo.StatusNotApplicable, ""),
	}
	require.NoError(t, sigintchrepo.New(conn).InsertReadings(t.Context(), rows))

	window := Window{FromUnixNano: base.Add(-time.Hour).UnixNano(), ToUnixNano: base.Add(time.Hour).UnixNano()}
	maps := LookupMaps{
		SensorMinConfidenceLookup: {exclusive.String(): "0.6"},
		SignalNamesLookup:         {billing.String(): "Billing", outage.String(): "Outage", choice.String(): "Other", medium.String(): "Medium"},
	}
	got := runSensorQuery(t, tenant, maps, window, SensorSignals.Name, []string{"signal", "matched", "answer_status"}, []Measure{
		{Op: "count", Field: "", Alias: ""},
		{Op: "count_distinct", Field: "message", Alias: "messages"},
	}, nil)
	require.Len(t, got, 5)
	require.EqualValues(t, 1, got["Billing|true|confident"]["count"], "|2*0.9-1| = 0.8 and no minimum is set")
	require.EqualValues(t, 1, got["Outage|false|confident"]["count"], "a multi-label signal below 0.5 is a confident non-match")
	require.EqualValues(t, 1, got["Other|true|uncertain"]["count"], "0.5 is below the exclusive sensor's 0.6 minimum")
	require.EqualValues(t, 1, got["Other|true|confident"]["count"])
	require.EqualValues(t, 1, got["Medium|true|unrated"]["count"], "an ordered score reports its most likely level")
	require.NotContains(t, got, high.String()+"|true|unrated", "the rounded expected index is not the answer")

	messages := runSensorQuery(t, tenant, maps, window, SensorSignals.Name, []string{"sensor"}, []Measure{
		{Op: "count", Field: "", Alias: ""},
		{Op: "count_distinct", Field: "message", Alias: "messages"},
	}, nil)
	require.EqualValues(t, 2, messages[multi.String()]["count"], "one message yields a row per multi-label signal")
	require.EqualValues(t, 1, messages[multi.String()]["messages"])

	buckets := runSensorQuery(t, tenant, maps, window, SensorSignals.Name, []string{"confidence_bucket"}, []Measure{{Op: "count", Field: "", Alias: ""}}, nil)
	require.EqualValues(t, 1, buckets["0.8-0.9"]["count"])
	require.EqualValues(t, 1, buckets["0.4-0.5"]["count"])
	require.EqualValues(t, 1, buckets["0.5-0.6"]["count"])
	require.EqualValues(t, 1, buckets["0.7-0.8"]["count"])
	require.EqualValues(t, 1, buckets[""]["count"], "an unrated answer has no band")

	withoutMinimum := runSensorQuery(t, tenant, nil, window, SensorSignals.Name, []string{"answer_status"}, []Measure{{Op: "count", Field: "", Alias: ""}}, nil)
	require.EqualValues(t, 4, withoutMinimum["confident"]["count"], "with no minimum configured every rated answer counts")
	require.NotContains(t, withoutMinimum, "uncertain")
}

func TestSensorDimensionValuesReadTheStoredRows(t *testing.T) {
	t.Parallel()

	conn := newTestClickhouse(t)
	tenant := Tenant{OrganizationID: "org-" + uuid.NewString(), ProjectID: uuid.NewString()}
	sensor, signal := uuid.New(), uuid.New()
	base := time.Now().UTC().Add(-time.Hour)
	require.NoError(t, sigintchrepo.New(conn).InsertReadings(t.Context(), []sigintchrepo.ReadingRow{
		choiceFixture(readingFixture(tenant, uuid.New(), sensor, "m1", base, base), signal, 0.9),
	}))

	window := Window{FromUnixNano: base.Add(-time.Hour).UnixNano(), ToUnixNano: base.Add(time.Hour).UnixNano()}
	for dataset, dimension := range map[string]string{SensorEvaluations.Name: "status", SensorSignals.Name: "signal"} {
		plan, err := CompileValues(Default, tenant, LookupMaps{SignalNamesLookup: {signal.String(): "Billing"}}, ValuesRequest{Dataset: dataset, Dimension: dimension, FromUnixNano: window.FromUnixNano, ToUnixNano: window.ToUnixNano, Limit: 0})
		require.NoError(t, err)
		require.Equal(t, dataset+".sigint_readings", plan.Name)
		values, err := plan.RunValues(t.Context(), conn)
		require.NoError(t, err)
		require.Len(t, values, 1, dataset)
		require.Contains(t, []string{"answered", "Billing"}, values[0].Value)
	}
}
