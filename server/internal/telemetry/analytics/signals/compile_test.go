package signals

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCalendarResolutionAndCompletePointBudget(t *testing.T) {
	t.Parallel()
	from := time.Date(2024, 2, 28, 12, 0, 0, 0, time.UTC)
	to := time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC)
	require.Equal(t, 2, BucketCount(from, to, "month"))
	require.Equal(t, time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC), NextBucket(BucketStart(from, "month"), "month"))
	request := Request{Selection: Selection{SensorID: uuid.NewString(), SignalID: uuid.NewString(), Threshold: 0.5}, GroupBy: []Dimension{{Attribute: "department"}}, Groups: 4, Points: 2000}
	ctx := Context{OrganizationID: "example-org", ProjectID: uuid.NewString(), From: from, To: from.Add(6 * time.Hour), Now: from, Step: "auto"}
	plan, err := Compile(ctx, request)
	require.NoError(t, err)
	require.Equal(t, "minute", plan.Context.Step)
	request.Points = 1000
	ctx.Step = "minute"
	_, err = Compile(ctx, request)
	require.ErrorContains(t, err, "complete series exceeds")
	ctx.Step = "auto"
	plan, err = Compile(ctx, request)
	require.NoError(t, err)
	require.Equal(t, "hour", plan.Context.Step)
}

func TestSignalLabelsAndIdentityAreBound(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	key := "team'); DROP TABLE secret; --"
	request := Request{Selection: Selection{SensorID: uuid.NewString(), SignalID: uuid.NewString(), Threshold: 0.7}, GroupBy: []Dimension{{Attribute: key}}, Filters: []Filter{{Dimension: Dimension{Attribute: key}, Operator: "equals", Values: []string{"Engineering' OR 1=1"}}}}
	plan, err := Compile(Context{OrganizationID: "example-org", ProjectID: uuid.NewString(), From: now.Add(-time.Hour), To: now, Now: now, Step: "minute"}, request)
	require.NoError(t, err)
	require.NotContains(t, plan.Groups.SQL, key)
	require.Contains(t, plan.Groups.Args, key)
	require.Contains(t, plan.Groups.SQL, "FINAL WHERE organization_id = ? AND project_id = toUUID(?)")
	require.Contains(t, plan.Groups.SQL, "trigger_kind = ? AND run_id = toUUID(?) AND sensor_id = toUUID(?)")
	require.Len(t, plan.Groups.Args, strings.Count(plan.Groups.SQL, "?"))
	series, err := plan.Series([]string{"[[1,\"engineering\"]]"})
	require.NoError(t, err)
	require.Len(t, series.Args, strings.Count(series.SQL, "?"))
}

func TestSelectionRejectsAmbiguousPopulations(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	ctx := Context{OrganizationID: "example-org", ProjectID: uuid.NewString(), From: now.Add(-time.Hour), To: now, Now: now, Step: "hour"}
	request := Request{Selection: Selection{SensorID: uuid.NewString(), SignalID: uuid.NewString(), Threshold: 0.5}, Trigger: "manual"}
	_, err := Compile(ctx, request)
	require.ErrorContains(t, err, "explicit run")
	request.Trigger = "stream"
	request.Selection.MinimumLevel = new(1)
	_, err = Compile(ctx, request)
	require.ErrorContains(t, err, "without signal_id")
	request.Selection.SignalID = ""
	plan, err := Compile(ctx, request)
	require.NoError(t, err)
	require.Contains(t, plan.Prepared.SQL, "arraySlice(signal_probabilities, 2)")
	require.NotContains(t, plan.Prepared.SQL, "expected_index")
}
