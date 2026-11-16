package memorygate_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/businessmemory"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
	"github.com/speakeasy-api/gram/server/internal/memorygate"
)

// The gate must follow the staff admin Organizational Memory toggle.
func TestJudgeNameMatchesBusinessMemory(t *testing.T) {
	t.Parallel()
	require.Equal(t, businessmemory.JudgeName, memorygate.JudgeName)
}

// Set must switch memory on with the same cap the admin dashboard uses.
func TestDefaultDailyCapMatchesAnalysis(t *testing.T) {
	t.Parallel()
	require.Equal(t, analysis.DefaultJudgeDailyCap, memorygate.DefaultDailyCap)
}
