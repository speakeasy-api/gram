package assistants

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAttachmentInlineLimit(t *testing.T) {
	t.Parallel()

	require.Equal(t, int64(maxTurnAttachmentInlineBytes), attachmentInlineLimit("image/png"))
	require.Equal(t, int64(maxTurnAttachmentInlineBytes), attachmentInlineLimit("image/jpeg; charset=binary"))
	require.Equal(t, int64(maxTurnAttachmentTextBytes), attachmentInlineLimit("text/csv"))
	require.Equal(t, int64(maxTurnAttachmentTextBytes), attachmentInlineLimit("application/json"))
	// Formats the completions protocol cannot carry: announced, never inlined.
	require.Equal(t, int64(0), attachmentInlineLimit("application/pdf"))
	require.Equal(t, int64(0), attachmentInlineLimit("audio/mpeg"))
	require.Equal(t, int64(0), attachmentInlineLimit("image/svg+xml"))
}

// The turn prompt must stay byte-stable across replay, so attachments appear as
// fixed metadata — never as a freshly minted download URL.
func TestDashboardDecodeTurnRendersAttachmentMetadata(t *testing.T) {
	t.Parallel()

	payload, err := json.Marshal(dashboardEventPayload{
		Text:         "what is in here?",
		UserID:       "user-1",
		SkillContext: nil,
		Attachments: []dashboardTurnAttachment{{
			AssetID:       uuid.New(),
			Name:          "report.pdf",
			ContentType:   "application/pdf",
			ContentLength: 2048,
		}},
	})
	require.NoError(t, err)

	prompt, err := dashboardAdapter{}.DecodeTurn(assistantThreadEventRecord{
		EventID:               "event-1",
		NormalizedPayloadJSON: payload,
	})
	require.NoError(t, err)
	require.Contains(t, prompt, "<attachments-context>")
	require.Contains(t, prompt, "- report.pdf (application/pdf, 2048 bytes)")
	require.Contains(t, prompt, "what is in here?")

	again, err := dashboardAdapter{}.DecodeTurn(assistantThreadEventRecord{
		EventID:               "event-1",
		NormalizedPayloadJSON: payload,
	})
	require.NoError(t, err)
	require.Equal(t, prompt, again)
}
