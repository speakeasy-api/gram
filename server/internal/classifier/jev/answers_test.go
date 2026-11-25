package jev

import (
	"fmt"
	"math"
	"net/http"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/stretchr/testify/require"
)

func TestScorePreservesProviderExpectedIndex(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		score         float64
		probabilities string
	}{
		{name: "concentrated", score: 2.99, probabilities: `{"0":0,"1":0,"2":0,"3":1,"4":0}`},
		{name: "near_upper_bound", score: 3.97, probabilities: `{"0":0.01,"1":0,"2":0,"3":0.01,"4":0.98}`},
		{name: "spread", score: 2.66, probabilities: `{"0":0.02,"1":0.06,"2":0.19,"3":0.69,"4":0.04}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprintf(w, `{"answers":{"0":{"type":"score","score":%g,"probabilities":%s}}}`, tc.score, tc.probabilities)
			})
			req := classifier.NewRequest(classifier.Text("Synthetic input")).Ask(classifier.Score("score", classifier.Text("Rate from low to high"),
				classifier.NewOption("a", classifier.Text("First")),
				classifier.NewOption("b", classifier.Text("Second")),
				classifier.NewOption("c", classifier.Text("Third")),
				classifier.NewOption("d", classifier.Text("Fourth")),
				classifier.NewOption("e", classifier.Text("Fifth")),
			))
			result := c.Classify(t.Context(), req)
			require.NoError(t, result.Err())
			require.Len(t, result.Outcomes, 1)
			require.Nil(t, result.Outcomes[0].Failure)
			require.NotNil(t, result.Outcomes[0].Answer)
			require.NotNil(t, result.Outcomes[0].Answer.Score)
			require.InDelta(t, tc.score, result.Outcomes[0].Answer.Score.ExpectedIndex, 0)
		})
	}
}

func TestProviderDistributionValidation(t *testing.T) {
	t.Parallel()
	options := []classifier.Option{
		classifier.NewOption("first", classifier.Text("First")),
		classifier.NewOption("second", classifier.Text("Second")),
		classifier.NewOption("third", classifier.Text("Third")),
	}
	req := classifier.NewRequest(classifier.Text("Synthetic input")).
		Ask(classifier.Choice("choice", classifier.Text("Choose"), options...)).
		Ask(classifier.Score("score", classifier.Text("Rate"), options...))
	planned, err := compileRequest(req)
	require.NoError(t, err)

	for _, q := range planned {
		t.Run(q.wire.Type, func(t *testing.T) {
			t.Parallel()
			for _, tc := range []struct {
				name          string
				probabilities map[string]*float64
				accepted      bool
			}{
				{name: "exact", probabilities: map[string]*float64{"0": new(0.2), "1": new(0.3), "2": new(0.5)}, accepted: true},
				{name: "rounded_below", probabilities: map[string]*float64{"0": new(0.2), "1": new(0.3), "2": new(0.49)}, accepted: true},
				{name: "rounded_above", probabilities: map[string]*float64{"0": new(0.2), "1": new(0.3), "2": new(0.51)}, accepted: true},
				{name: "sum_below_one", probabilities: map[string]*float64{"0": new(0.2), "1": new(0.3), "2": new(0.48)}, accepted: true},
				{name: "sum_above_one", probabilities: map[string]*float64{"0": new(0.2), "1": new(0.3), "2": new(0.52)}, accepted: true},
				{name: "unnormalized", probabilities: map[string]*float64{"0": new(0.5), "1": new(0.0), "2": new(0.9)}, accepted: true},
				{name: "zero_mass", probabilities: map[string]*float64{"0": new(0.0), "1": new(0.0), "2": new(0.0)}, accepted: true},
				{name: "missing_key", probabilities: map[string]*float64{"0": new(0.5), "2": new(0.5)}},
				{name: "wrong_key", probabilities: map[string]*float64{"0": new(0.2), "3": new(0.3), "2": new(0.5)}},
				{name: "null_probability", probabilities: map[string]*float64{"0": nil, "1": new(0.5), "2": new(0.5)}},
				{name: "negative_probability", probabilities: map[string]*float64{"0": new(-0.01), "1": new(0.5), "2": new(0.51)}},
				{name: "probability_above_one", probabilities: map[string]*float64{"0": new(0.0), "1": new(0.0), "2": new(1.01)}},
				{name: "nan_probability", probabilities: map[string]*float64{"0": new(math.NaN()), "1": new(0.5), "2": new(0.5)}},
				{name: "infinite_probability", probabilities: map[string]*float64{"0": new(math.Inf(1)), "1": new(0.5), "2": new(0.5)}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()
					wire := wireAnswer{Type: q.wire.Type, Choice: new("2"), Score: new(1.29), Probabilities: tc.probabilities, Confidence: new(0.5)}
					answer := decodeAnswer(q, wire)
					if !tc.accepted {
						require.Nil(t, answer)
						return
					}
					require.NotNil(t, answer)
					var distribution []classifier.Probability
					if q.question.Choice != nil {
						require.Equal(t, classifier.OptionKey("third"), answer.Choice.Selected)
						distribution = answer.Choice.Distribution
					} else {
						require.InDelta(t, 1.29, answer.Score.ExpectedIndex, 0)
						distribution = answer.Score.Distribution
					}
					require.Len(t, distribution, len(options))
					for i, option := range options {
						require.Equal(t, option.Key, distribution[i].Option)
						require.InDelta(t, *tc.probabilities[fmt.Sprint(i)], distribution[i].Value, 0)
					}
				})
			}
		})
	}
}
