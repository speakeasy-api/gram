package evaluation

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/classifier"
)

func TestReadinessFollowsSystemOneShapes(t *testing.T) {
	t.Parallel()
	definitions := sensors()
	for _, sensor := range definitions {
		_, ready := compileSensor(sensor)
		require.True(t, ready)
		sensor.Instructions = nil
		_, ready = compileSensor(sensor)
		require.False(t, ready)
	}
	choice := definitions[1]
	var null classifier.Entry
	choice.Signals = []classifier.Option{classifier.NewOption("a", null)}
	_, ready := compileSensor(choice)
	require.True(t, ready, "Choice permits one option with null description")
	score := definitions[2]
	score.Signals = choice.Signals
	_, ready = compileSensor(score)
	require.False(t, ready, "Score needs two levels")
	score.Signals = append(score.Signals, classifier.NewOption("b", null))
	_, ready = compileSensor(score)
	require.False(t, ready, "OpenRouter Score levels must be non-null")
}

func TestDefinitionHashIncludesEffectiveContentAndOrder(t *testing.T) {
	t.Parallel()
	sensor := sensors()[1]
	a, ok := compileSensor(sensor)
	require.True(t, ok)
	sensor.Signals[0], sensor.Signals[1] = sensor.Signals[1], sensor.Signals[0]
	b, ok := compileSensor(sensor)
	require.True(t, ok)
	require.NotEqual(t, a.hash, b.hash)
	sensor.Signals[0].Description = classifier.Text("new guidance")
	c, ok := compileSensor(sensor)
	require.True(t, ok)
	require.NotEqual(t, b.hash, c.hash)
}

func TestReadingIdentitySurvivesSensorConfigurationChanges(t *testing.T) {
	t.Parallel()
	sensor := sensors()[0]
	before, ok := compileSensor(sensor)
	require.True(t, ok)
	instructions := "Updated evaluation guidance"
	sensor.Instructions = &instructions
	after, ok := compileSensor(sensor)
	require.True(t, ok)
	var answer classifier.Answer
	answer.Noul = &classifier.NoulAnswer{Probability: 0.5}
	outcomes := map[classifier.QuestionKey]classifier.QuestionOutcome{
		"multi/a": {Key: "multi/a", Answer: &answer, Failure: nil},
		"multi/b": {Key: "multi/b", Answer: &answer, Failure: nil},
	}
	m := message()
	var result classifier.Result
	a, err := reading((&conversationInput{message: m}).Event(), before, outcomes, "first-attempt", "2026-09-29T00:00:00Z", result)
	require.NoError(t, err)
	id, err := uuid.Parse(a.GetId())
	require.NoError(t, err)
	require.Equal(t, uuid.Version(5), id.Version())
	require.Equal(t, uuid.RFC4122, id.Variant())
	require.Equal(t, id.String(), a.GetId())
	b, err := reading((&conversationInput{message: m}).Event(), after, outcomes, "second-attempt", "2026-09-29T00:01:00Z", result)
	require.NoError(t, err)
	require.Equal(t, a.GetId(), b.GetId())
	require.NotEqual(t, a.GetDefinitionHash(), b.GetDefinitionHash())
	require.NotEqual(t, a.GetEvaluationAttemptId(), b.GetEvaluationAttemptId())
	// Distinct messages and sensors must still have distinct logical readings.
	m.SetId("another-message")
	c, err := reading((&conversationInput{message: m}).Event(), after, outcomes, "third-attempt", "2026-09-29T00:02:00Z", result)
	require.NoError(t, err)
	require.NotEqual(t, b.GetId(), c.GetId())
	sensor.ID = "other"
	after, ok = compileSensor(sensor)
	require.True(t, ok)
	outcomes = map[classifier.QuestionKey]classifier.QuestionOutcome{
		"other/a": {Key: "other/a", Answer: &answer, Failure: nil},
		"other/b": {Key: "other/b", Answer: &answer, Failure: nil},
	}
	d, err := reading((&conversationInput{message: m}).Event(), after, outcomes, "fourth-attempt", "2026-09-29T00:03:00Z", result)
	require.NoError(t, err)
	require.NotEqual(t, c.GetId(), d.GetId())
}
