package risk_analysis_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
)

func TestAnalyzeBatch_EmptyMessageIDs(t *testing.T) {
	t.Parallel()
	activity := risk_analysis.NewAnalyzeBatch(testLogger, nil)
	require.NotNil(t, activity)

	result, err := activity.Do(t.Context(), risk_analysis.AnalyzeBatchArgs{
		MessageIDs: nil,
		Sources:    []string{"gitleaks"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.Processed)
	assert.Equal(t, 0, result.Findings)
}
