package analytics

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/sigint/chrepo"
	"github.com/speakeasy-api/gram/server/internal/sigint/readings"
	signalquery "github.com/speakeasy-api/gram/server/internal/telemetry/analytics/signals"
)

func TestSignalCanonicalQueriesResolveAttemptsBeforeFiltering(t *testing.T) {
	t.Parallel()
	conn := newTestClickhouse(t)
	now := time.Now().UTC().Truncate(time.Second)
	project, sensor, signal, conversation := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	org := "signals-" + uuid.NewString()
	event := uuid.NewString()
	request := readings.Context{TriggerKind: readings.TriggerStream, RequestedAt: now.Add(-time.Minute)}
	row := chrepo.ReadingRow{
		ID:             request.ReadingID(org, project.String(), "conversation.message", event, sensor.String()),
		OrganizationID: org, ProjectID: project, SensorID: sensor, SensorSlug: "topic", EventKind: "conversation.message", EventID: event,
		TriggerKind: "stream", EvaluationRequestedAt: request.RequestedAt, EventOccurredAt: now.Add(-time.Hour), EvaluatedAt: now.Add(-30 * time.Second), InsertedAt: now,
		Status: "answered", ResultType: "multi_label", DefinitionHash: "definition-a", ConversationID: &conversation, ConversationCaptureKind: "original",
		Attributes: map[string]string{"department": "engineering"}, Probabilities: []chrepo.ProbabilityRow{{SignalID: signal, SignalSlug: "topic", Probability: 0.9}},
	}
	require.NoError(t, chrepo.New(conn).InsertReadings(t.Context(), []chrepo.ReadingRow{row, row}))
	corrected := row
	corrected.Status, corrected.ResultType, corrected.Probabilities = "failed", "", nil
	corrected.EvaluatedAt = now
	corrected.Attributes = map[string]string{"department": "marketing"}
	require.NoError(t, chrepo.New(conn).InsertReadings(t.Context(), []chrepo.ReadingRow{corrected}))
	other := row
	other.ID, other.EventID = uuid.New(), uuid.NewString()
	other.Probabilities = []chrepo.ProbabilityRow{{SignalID: signal, SignalSlug: "topic", Probability: 0.2}}
	require.NoError(t, chrepo.New(conn).InsertReadings(t.Context(), []chrepo.ReadingRow{other}))
	foreign := row
	foreign.ID, foreign.OrganizationID, foreign.ProjectID = uuid.New(), "another-org", uuid.New()
	foreign.ConversationCaptureKind = ""
	require.NoError(t, chrepo.New(conn).InsertReadings(t.Context(), []chrepo.ReadingRow{foreign}))
	selection := signalquery.Request{Selection: signalquery.Selection{SensorID: sensor.String(), SignalID: signal.String(), Threshold: 0.5}, Filters: []signalquery.Filter{{Dimension: signalquery.Dimension{Attribute: "department"}, Operator: "equals", Values: []string{"engineering"}}}}
	plan, err := signalquery.Compile(signalquery.Context{OrganizationID: org, ProjectID: project.String(), From: now.Add(-2 * time.Hour), To: now, Now: now, Step: "minute"}, selection)
	require.NoError(t, err)
	rows, err := (&Plan{SQL: plan.Groups.SQL, Args: plan.Groups.Args}).Run(t.Context(), conn)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.EqualValues(t, 1, rows[0]["observed_outcomes"])
	require.EqualValues(t, 1, rows[0]["answered"])
	require.EqualValues(t, 0, rows[0]["matched_messages"])
	series, err := plan.Series([]string{"[]"})
	require.NoError(t, err)
	rows, err = (&Plan{SQL: series.SQL, Args: series.Args}).Run(t.Context(), conn)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.EqualValues(t, 1, rows[0]["evaluated_sessions"])
	engine, err := NewEngine(nil, conn)
	require.NoError(t, err)
	selection.GroupBy = []signalquery.Dimension{{Attribute: "department"}}
	result, err := engine.SignalQuery(t.Context(), Tenant{OrganizationID: org, ProjectID: project.String()}, Window{FromUnixNano: now.Add(-2 * time.Hour).UnixNano(), ToUnixNano: now.UnixNano()}, "minute", selection)
	require.NoError(t, err)
	require.True(t, result.Metadata.SeriesComplete)
	require.True(t, result.Metadata.Exact)
	require.Equal(t, "unknown", result.Metadata.Coverage)
	require.Len(t, result.Rows, signalquery.BucketCount(now.Add(-2*time.Hour), now, "minute"))
	observed := 0
	for _, point := range result.Rows {
		require.Equal(t, []any{"engineering"}, point["group_values"])
		if point["observed"] == true {
			observed++
			require.EqualValues(t, 0, point["matched_messages"], "an answered negative is a supported zero")
		} else {
			require.Nil(t, point["matched_messages"], "an unobserved bucket is unknown")
			require.Nil(t, point["match_share"])
		}
	}
	require.Equal(t, 1, observed)
}

func TestSignalGroupsDistinguishMissingFromEmpty(t *testing.T) {
	t.Parallel()
	missing := Row{"group_key": `[[0,""]]`, "answered": int64(0)}
	empty := Row{"group_key": `[[1,""]]`, "answered": int64(0)}
	require.NoError(t, normalizeSignalRow(missing))
	require.NoError(t, normalizeSignalRow(empty))
	require.Equal(t, []any{nil}, missing["group_values"])
	require.Equal(t, []any{""}, empty["group_values"])
	require.NotEqual(t, missing["series_id"], empty["series_id"])
}

func TestSignalScopeRetentionAndModeRules(t *testing.T) {
	t.Parallel()
	conn := newTestClickhouse(t)
	now := time.Now().UTC().Truncate(time.Second)
	project, sensor, signal, conversation := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	run, orderedSensor := uuid.New(), uuid.New()
	org := "signal-scope-" + uuid.NewString()
	from := now.Add(-time.Hour)
	base := chrepo.ReadingRow{
		ID: uuid.New(), OrganizationID: org, ProjectID: project, SensorID: sensor, SensorSlug: "topic", EventKind: "conversation.message", EventID: uuid.NewString(),
		TriggerKind: "stream", EvaluationRequestedAt: now, EventOccurredAt: from.Add(time.Nanosecond), EvaluatedAt: now, InsertedAt: now,
		Status: "answered", ResultType: "multi_label", DefinitionHash: "definition", ConversationID: &conversation, ConversationCaptureKind: "original",
		Probabilities: []chrepo.ProbabilityRow{{SignalID: signal, SignalSlug: "target", Probability: 0.2}},
	}
	boundary := base
	boundary.ID, boundary.EventID, boundary.EventOccurredAt = uuid.New(), uuid.NewString(), from.Add(-time.Nanosecond)
	expired := base
	expired.ID, expired.EventID, expired.EvaluationRequestedAt = uuid.New(), uuid.NewString(), now.AddDate(-3, 0, 0)
	historical := base
	historical.ID, historical.EventID, historical.EventOccurredAt = uuid.New(), uuid.NewString(), now.AddDate(-5, 0, 0)
	manual := base
	manual.ID, manual.TriggerKind, manual.RunID = uuid.New(), "manual", run
	manual.ResultType, manual.SelectedSignalID = "choice", &signal
	ordered := base
	ordered.ID, ordered.SensorID, ordered.ResultType = uuid.New(), orderedSensor, "score"
	ordered.Probabilities = []chrepo.ProbabilityRow{{SignalID: signal, SignalSlug: "low", Probability: 0.3}, {SignalID: uuid.New(), SignalSlug: "high", Probability: 0.7}}
	require.NoError(t, chrepo.New(conn).InsertReadings(t.Context(), []chrepo.ReadingRow{base, boundary, expired, historical, manual, ordered}))
	engine, err := NewEngine(nil, conn)
	require.NoError(t, err)
	tenant := Tenant{OrganizationID: org, ProjectID: project.String()}
	window := Window{FromUnixNano: from.UnixNano(), ToUnixNano: now.UnixNano()}
	request := signalquery.Request{Selection: signalquery.Selection{SensorID: sensor.String(), SignalID: signal.String(), Threshold: 0.95}}
	stream, err := engine.SignalQuery(t.Context(), tenant, window, "hour", request)
	require.NoError(t, err)
	require.EqualValues(t, 1, stream.Summary["answered"], "nanosecond boundaries and request-time expiry must be honored")
	require.EqualValues(t, 0, stream.Summary["matched_messages"])
	request.Trigger, request.RunID = "manual", run.String()
	manualResult, err := engine.SignalQuery(t.Context(), tenant, window, "hour", request)
	require.NoError(t, err)
	require.EqualValues(t, 1, manualResult.Summary["matched_messages"], "exclusive mode uses the selected signal, not its probability")
	request.Trigger, request.RunID = "stream", ""
	request.Selection = signalquery.Selection{SensorID: orderedSensor.String(), MinimumLevel: new(1), Threshold: 0.6}
	orderedResult, err := engine.SignalQuery(t.Context(), tenant, window, "hour", request)
	require.NoError(t, err)
	require.EqualValues(t, 1, orderedResult.Summary["matched_messages"], "ordered matches use cumulative probability")
	request.Selection = signalquery.Selection{SensorID: sensor.String(), SignalID: signal.String(), Threshold: 0.1}
	historicalResult, err := engine.SignalQuery(t.Context(), tenant, Window{FromUnixNano: historical.EventOccurredAt.Add(-time.Minute).UnixNano(), ToUnixNano: historical.EventOccurredAt.Add(time.Minute).UnixNano()}, "minute", request)
	require.NoError(t, err)
	require.EqualValues(t, 1, historicalResult.Summary["matched_messages"], "retention is not an event-age cutoff")
}
