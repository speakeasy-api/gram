package promptinjection

import (
	"testing"

	"github.com/google/uuid"
	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/stretchr/testify/require"
)

func TestParentlessPartDoesNotLoadLiveHistory(t *testing.T) {
	t.Parallel()
	event := new(riskv1.PromptInjectionAnalysis)
	event.SetChatId(uuid.NewString())
	event.SetContentPartId(uuid.NewString())
	event.SetBody("persisted part")
	target := promptInjectionJudgeMessage(event)
	require.Equal(t, uuid.Nil, target.AnchorID)
	require.Equal(t, uuid.Nil, target.ChatID)
	window, err := judgemessage.NewWindowLoader(nil).Load(t.Context(), "", "", target)
	require.NoError(t, err)
	require.Len(t, window.Messages, 1)
	parentID := uuid.New()
	event.SetParentChatMessageId(parentID.String())
	target = promptInjectionJudgeMessage(event)
	require.Equal(t, parentID, target.AnchorID)
	require.NotEqual(t, uuid.Nil, target.ChatID)
}
