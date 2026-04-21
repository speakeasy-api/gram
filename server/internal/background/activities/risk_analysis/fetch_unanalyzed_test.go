package risk_analysis_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
)

func TestFetchUnanalyzed(t *testing.T) {
	t.Parallel()
	t.Skip("requires database test environment")

	ctx := t.Context()
	activity := risk_analysis.NewFetchUnanalyzed(testLogger, testTracerProvider, nil)
	require.NotNil(t, activity)

	result, err := activity.Do(ctx, risk_analysis.FetchUnanalyzedArgs{
		BatchLimit: 10,
	})
	// Expect an error since we don't have a real DB connection
	require.Error(t, err)
	require.Nil(t, result)
}
