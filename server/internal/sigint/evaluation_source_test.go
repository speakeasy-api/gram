package sigint_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/sigint"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/sigint/evaluation"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestEvaluationMessageBatchScopesAndSplitProjection(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	project := *auth.ProjectID
	fixtures := testrepo.New(ti.conn)
	chatID, err := fixtures.SeedCapturedAgentChatFixture(ctx, testrepo.SeedCapturedAgentChatFixtureParams{ID: uuid.New(), ProjectID: project, OrganizationID: auth.ActiveOrganizationID})
	require.NoError(t, err)
	queries := chatrepo.New(ti.conn)
	var ids []uuid.UUID
	for _, externalID := range []string{"split/block:0", "split", "single"} {
		id, err := queries.CreateExternalChatMessage(ctx, chatrepo.CreateExternalChatMessageParams{
			ID: uuid.New(), ChatID: chatID, ProjectID: project, Role: "user", Content: "row text", ContentRaw: []byte(`{"text":"archival content"}`),
			ExternalMessageID: conv.ToPGText(externalID), Origin: conv.ToPGText("anthropic-inference"), CreatedAt: conv.ToPGTimestamptz(time.Now()),
		})
		require.NoError(t, err)
		ids = append(ids, id)
	}
	uri := "gs://test-bucket/" + project.String() + "/attachment.txt"
	_, err = queries.CreateChatContentPart(ctx, []chatrepo.CreateChatContentPartParams{{ChatID: chatID, ProjectID: project, Kind: "prompt_attachment", ContentAssetUrl: uri, ParentChatMessageID: uuid.NullUUID{UUID: ids[1], Valid: true}, CreatedAt: conv.ToPGTimestamptz(time.Now())}})
	require.NoError(t, err)
	source := evaluation.NewRepository(ti.conn)
	rows, err := source.LoadMessages(ctx, auth.ActiveOrganizationID, project, ids)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	for _, row := range rows {
		require.Equal(t, row.ChatMessage.ID != ids[2], row.RowLocalContent)
		if row.ChatMessage.ID == ids[1] {
			require.Equal(t, []string{uri}, row.AttachmentUris)
		} else {
			require.Empty(t, row.AttachmentUris)
		}
	}
	rows, err = source.LoadMessages(ctx, "another-org", project, ids)
	require.NoError(t, err)
	require.Empty(t, rows)
	rows, err = source.LoadMessages(ctx, auth.ActiveOrganizationID, uuid.New(), ids)
	require.NoError(t, err)
	require.Empty(t, rows)
	rows, err = source.LoadMessages(ctx, auth.ActiveOrganizationID, project, []uuid.UUID{uuid.New()})
	require.NoError(t, err)
	require.Empty(t, rows)
	require.NoError(t, fixtures.ForceSoftDeleteChat(ctx, chatID))
	rows, err = source.LoadMessages(ctx, auth.ActiveOrganizationID, project, ids)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestEvaluationSourceScopesAndOrdersDefinitions(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	a := createSignal(t, ctx, ti, "first")
	b := createSignal(t, ctx, ti, "second")
	sensor := createSensor(t, ctx, ti, "sensor", "ordered_score", b.ID, a.ID)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	source := evaluation.NewRepository(ti.conn)
	rows, err := source.Load(ctx, auth.ActiveOrganizationID, uuid.MustParse(sensor.ProjectID), evaluation.ConversationMessageKind)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, string(sensor.Slug), rows[0].Slug)
	require.Equal(t, string(b.Slug), rows[0].SignalSlugs[rows[0].Signals[0].Key])
	require.Equal(t, string(a.Slug), rows[0].SignalSlugs[rows[0].Signals[1].Key])
	require.Equal(t, b.ID, string(rows[0].Signals[0].Key))
	require.Equal(t, a.ID, string(rows[0].Signals[1].Key))
	rows, err = source.Load(ctx, "different-organization", uuid.MustParse(sensor.ProjectID), evaluation.ConversationMessageKind)
	require.NoError(t, err)
	require.Empty(t, rows)
	_, err = ti.service.DeleteSignal(ctx, &gen.DeleteSignalPayload{ID: b.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	rows, err = source.Load(ctx, auth.ActiveOrganizationID, uuid.MustParse(sensor.ProjectID), evaluation.ConversationMessageKind)
	require.NoError(t, err)
	require.Len(t, rows[0].Signals, 1)
	require.Equal(t, a.ID, string(rows[0].Signals[0].Key))
	_, err = ti.service.DeleteSignal(ctx, &gen.DeleteSignalPayload{ID: a.ID})
	require.NoError(t, err)
	rows, err = source.Load(ctx, auth.ActiveOrganizationID, uuid.MustParse(sensor.ProjectID), evaluation.ConversationMessageKind)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Empty(t, rows[0].Signals)
	empty := createSensor(t, ctx, ti, "empty", "multi_label")
	rows, err = source.Load(ctx, auth.ActiveOrganizationID, uuid.MustParse(empty.ProjectID), evaluation.ConversationMessageKind)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.Empty(t, row.Signals)
	}
	rows, err = source.Load(ctx, auth.ActiveOrganizationID, uuid.MustParse(sensor.ProjectID), "mcp.tool_call")
	require.NoError(t, err)
	require.Empty(t, rows, "conversation configuration must not implicitly apply to new event kinds")
}
