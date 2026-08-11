package assistants

import (
	"encoding/json"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	assetsrepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
)

// Exercises the real turn path: an uploaded asset's bytes must reach the model
// as an inline content part, not just the metadata line DecodeTurn writes.
func TestDashboardTurnAttachmentPartsInlinesText(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, conn := newRBACServiceWithConn(t, "assistants_turn_attachment_parts")
	store := assetstest.NewTestBlobStore(t)
	svc.core.SetAssetStorage(store)

	const spec = "openapi: 3.1.0\ninfo:\n  title: Petstore\n"
	writer, assetURL, err := store.Write(ctx, "attachment-test.yaml", "application/yaml", int64(len(spec)))
	require.NoError(t, err)
	_, err = io.WriteString(writer, spec)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	asset, err := assetsrepo.New(conn).CreateAsset(ctx, assetsrepo.CreateAssetParams{
		Name:          "petstore.yaml",
		Url:           assetURL.String(),
		ProjectID:     projectID,
		Sha256:        uuid.NewString(),
		Kind:          "chat_attachment",
		ContentType:   "application/yaml",
		ContentLength: int64(len(spec)),
	})
	require.NoError(t, err)

	payload, err := json.Marshal(dashboardEventPayload{
		Text:         "what is in here?",
		UserID:       "user-test",
		SkillContext: nil,
		Attachments: []dashboardTurnAttachment{{
			AssetID:       asset.ID,
			Name:          "petstore.yaml",
			ContentType:   "application/yaml",
			ContentLength: int64(len(spec)),
		}},
	})
	require.NoError(t, err)

	parts := svc.core.dashboardTurnAttachmentParts(ctx, projectID, assistantThreadEventRecord{
		EventID:               "event-1",
		NormalizedPayloadJSON: payload,
	})
	require.Len(t, parts, 1)
	require.Equal(t, contentPartTypeText, parts[0].Type)
	require.Contains(t, parts[0].Text, "title: Petstore")
	require.Contains(t, parts[0].Text, "<attachment-context>")
	require.Contains(t, parts[0].Text, "name: petstore.yaml")
}
