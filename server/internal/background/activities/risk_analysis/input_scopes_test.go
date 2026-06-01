package risk_analysis

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/riskscope"
)

func TestFilterMessagesByInputScopes(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	assistantID := uuid.New()
	toolRequestID := uuid.New()
	toolResponseID := uuid.New()

	messages := []repo.GetMessageContentBatchRow{
		{ID: userID, Role: "user", Content: "hello"},
		{ID: assistantID, Role: "assistant", Content: "thinking"},
		{ID: toolRequestID, Role: "assistant", Content: "", ToolCalls: []byte(`[]`)},
		{ID: toolResponseID, Role: "tool", Content: "done"},
		{ID: uuid.New(), Role: "system", Content: "ignore"},
	}

	filtered := filterMessagesByInputScopes(messages, []string{riskscope.InputScopeToolRequest, riskscope.InputScopeToolResponse})
	require.Len(t, filtered, 2)
	require.Equal(t, toolRequestID, filtered[0].ID)
	require.Equal(t, toolResponseID, filtered[1].ID)

	all := filterMessagesByInputScopes(messages, nil)
	require.Len(t, all, 4)
	require.Equal(t, []uuid.UUID{userID, assistantID, toolRequestID, toolResponseID}, []uuid.UUID{all[0].ID, all[1].ID, all[2].ID, all[3].ID})
}
