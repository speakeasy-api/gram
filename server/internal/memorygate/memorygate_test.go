package memorygate_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/businessmemory"
	"github.com/speakeasy-api/gram/server/internal/memorygate"
)

// The gate must follow the staff admin Business Memory Extraction toggle.
func TestJudgeNameMatchesBusinessMemory(t *testing.T) {
	t.Parallel()
	require.Equal(t, businessmemory.JudgeName, memorygate.JudgeName)
}
