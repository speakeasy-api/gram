package evaluation

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/speakeasy-api/gram/server/internal/sigint/chrepo"
	"github.com/speakeasy-api/gram/server/internal/sigint/readings"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestEvaluationPublishesRunScopedReadingsAndOutcomes(t *testing.T) {
	t.Parallel()
	m := message()
	definitions := sensors()[:1]
	definitions[0].ID = uuid.NewString()
	definitions[0].SignalSlugs = make(map[classifier.OptionKey]string)
	for i := range definitions[0].Signals {
		key := classifier.OptionKey(uuid.NewString())
		definitions[0].Signals[i].Key = key
		definitions[0].SignalSlugs[key] = "example-signal"
	}
	c, _ := newRecordingClassifier(t)
	h, pub := handler(t, m, definitions, c, testenv.NewMeterProvider(t))
	in := &eventInput{event: (&conversationInput{message: m}).Event(), data: classifier.Text("Synthetic evaluation target")}
	stream := in.event.Evaluation
	manualA := readings.Context{TriggerKind: readings.TriggerManual, RunID: uuid.New(), RequestedAt: stream.RequestedAt.Add(time.Hour)}
	manualB := readings.Context{TriggerKind: readings.TriggerManual, RunID: uuid.New(), RequestedAt: stream.RequestedAt.Add(2 * time.Hour)}
	for _, execution := range []readings.Context{stream, manualA, manualA, manualB} {
		in.event.Evaluation = execution
		require.NoError(t, h.evaluator.Evaluate(t.Context(), in))
		r := pub.readings[len(pub.readings)-1]
		row, err := chrepo.ParseReading(r, execution.RequestedAt)
		require.NoError(t, err)
		require.Equal(t, string(execution.TriggerKind), row.TriggerKind)
		require.Equal(t, execution.RunID, row.RunID)
		require.Equal(t, execution.RequestedAt, row.EvaluationRequestedAt)
		require.Equal(t, m.GetMessageCreatedAt(), r.GetEvent().GetOccurredAt())
	}
	require.Len(t, pub.readings, 4)
	require.NotEqual(t, pub.readings[0].GetId(), pub.readings[1].GetId())
	require.Equal(t, pub.readings[1].GetId(), pub.readings[2].GetId())
	require.NotEqual(t, pub.readings[1].GetEvaluationAttemptId(), pub.readings[2].GetEvaluationAttemptId())
	require.NotEqual(t, pub.readings[1].GetId(), pub.readings[3].GetId())

	in.event.Evaluation = manualA
	in.err = ErrInvalidInput
	require.NoError(t, h.evaluator.Evaluate(t.Context(), in))
	require.Len(t, pub.outcomes.messages, 1)
	o := pub.outcomes.messages[0]
	require.Equal(t, pub.readings[1].GetId(), o.GetId())
	row, err := chrepo.ParseOutcome(o, manualA.RequestedAt)
	require.NoError(t, err)
	require.Equal(t, "manual", row.TriggerKind)
	require.Equal(t, manualA.RunID, row.RunID)
	require.Equal(t, manualA.RequestedAt, row.EvaluationRequestedAt)
	require.Equal(t, chrepo.StatusFailed, row.Status)
}
