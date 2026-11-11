package bench

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval"
)

// TestQuestionRecallUsesTheCorpusChatIDs checks that recall scores a corpus
// loaded under tenant-derived chat IDs, as the demo seed loads it, by the IDs
// its chats were stored under rather than the default key-derived ones.
func TestQuestionRecallUsesTheCorpusChatIDs(t *testing.T) {
	t.Parallel()
	corpus := memoryeval.Build(testNow).WithChatIDs(func(key string) uuid.UUID {
		return uuid.NewSHA1(uuid.NameSpaceOID, []byte("tenant-"+key))
	})
	everything := make([]uuid.UUID, 0, len(corpus.Chats))
	for _, chat := range corpus.Chats {
		everything = append(everything, chat.ID)
	}
	arm := Arm{Name: "all", Search: func(context.Context, string) (ArmResult, error) {
		return ArmResult{Output: "", ChatIDs: everything}, nil
	}}

	report, err := QuestionRecall(t.Context(), corpus, []Arm{arm})
	require.NoError(t, err)
	require.Len(t, report.Rows, 1)
	row := report.Rows[0]
	require.Positive(t, row.Questions)
	require.Equal(t, row.Questions, row.Keyword)
	require.Equal(t, row.Questions, row.Natural)
}
