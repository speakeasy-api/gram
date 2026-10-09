package jev

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/stretchr/testify/require"
)

func TestScoreDerivesExpectedIndexFromDistribution(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		score         float64
		probabilities string
		expected      float64
	}{
		{name: "concentrated", score: 2.99, probabilities: `{"0":0,"1":0,"2":0,"3":1,"4":0}`, expected: 3},
		{name: "near_upper_bound", score: 3.97, probabilities: `{"0":0.01,"1":0,"2":0,"3":0.01,"4":0.98}`, expected: 3.95},
		{name: "spread", score: 2.66, probabilities: `{"0":0.02,"1":0.06,"2":0.19,"3":0.69,"4":0.04}`, expected: 2.67},
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
			require.InDelta(t, tc.expected, result.Outcomes[0].Answer.Score.ExpectedIndex, 1e-9)
		})
	}
}
