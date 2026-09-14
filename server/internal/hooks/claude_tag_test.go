package hooks

import (
	"testing"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/stretchr/testify/require"
)

func TestClaudeTagTitle(t *testing.T) {
	t.Parallel()
	title, ok := claudeTagTitle(`<wake reason="channel-activity"><channel id="DEMO_CHANNEL"><message from="human">Earlier context</message><message from="human" trigger="true">&lt;@DEMO_BOT|Claude&gt; Hello &amp; welcome</message></channel></wake>`)
	require.True(t, ok)
	require.Equal(t, "Claude Tag in #DEMO_CHANNEL", title)
}

func TestClaudeTagRejectsUnrelatedOrMalformedInput(t *testing.T) {
	t.Parallel()
	for _, input := range []string{"hello", `<wake><channel`, `<wake><message from="human">Hi</message></wake>`, `<wake><channel id="DEMO_CHANNEL"><message from="assistant">Hi</message></channel></wake>`} {
		_, ok := claudeTagTitle(input)
		require.False(t, ok)
	}
}

func TestClaudeTagSurfaceSurvivesAmbiguousClaudeReports(t *testing.T) {
	t.Parallel()
	require.Equal(t, "claude-tag", preferClaudeServiceName("claude-code", "claude-tag"))
	require.Equal(t, "claude-tag", preferClaudeServiceName("claude", "claude-tag"))
	require.Equal(t, "cursor", preferClaudeServiceName("cursor", "claude-tag"))
}

func TestClaudeTagIngestCachesSurfaceForLaterEvents(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	chatID := sessionIDToUUID("demo-tag-session")
	_, err := ti.service.repo.UpsertClaudeCodeSession(ctx, repo.UpsertClaudeCodeSessionParams{
		ID: chatID, ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID,
		Title: conv.ToPGText("An unrelated earlier topic"),
	})
	require.NoError(t, err)
	prompt := `<wake><channel id="DEMO_CHANNEL"><message from="human" trigger="true">Summarize the release</message></channel></wake>`
	payload := canonicalIngestPayload("claude-code", "prompt.submitted", "demo-tag-session")
	payload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &prompt}}
	ti.service.recordCanonicalHook(ctx, payload, authCtx, canonicalActor{UserID: "", Email: ""}, time.Now(), "")
	later := canonicalIngestPayload("claude-code", "assistant.responded", "demo-tag-session")
	metadata := ti.service.canonicalSessionMetadata(ctx, later, authCtx, canonicalActor{UserID: "", Email: ""})
	require.Equal(t, "claude-tag", metadata.ServiceName)
	require.Equal(t, "Claude Tag in #DEMO_CHANNEL", canonicalChatTitle(payload, "", "claude-code"))
	stored, err := chatRepo.New(ti.conn).GetChat(ctx, chatRepo.GetChatParams{ID: chatID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.Equal(t, "Claude Tag in #DEMO_CHANNEL", stored.Title.String)
	messages, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{ChatID: chatID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Equal(t, "claude-tag", messages[0].Source.String)
	require.NoError(t, chatRepo.New(ti.conn).RenameChat(ctx, chatRepo.RenameChatParams{
		ID: chatID, ProjectID: *authCtx.ProjectID, Title: conv.ToPGText("My channel notes"), TitleManuallySet: true,
	}))
	require.NoError(t, ti.service.repo.SetClaudeTagChatTitle(ctx, repo.SetClaudeTagChatTitleParams{
		ID: chatID, ProjectID: *authCtx.ProjectID, Title: conv.ToPGText("Claude Tag in #dev-demo"),
	}))
	renamed, err := chatRepo.New(ti.conn).GetChat(ctx, chatRepo.GetChatParams{ID: chatID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.Equal(t, "My channel notes", renamed.Title.String)
}

func TestClaudeTagTitleUsesChannelAcrossTopics(t *testing.T) {
	t.Parallel()
	for _, prompt := range []string{
		`<wake><channel id="DEMO_CHANNEL" name="dev-demo"><message from="human" trigger="true">Fix the build</message></channel></wake>`,
		`<wake><channel id="DEMO_CHANNEL" channel-name="#dev-demo"><message from="human" trigger="true">Plan lunch</message></channel></wake>`,
	} {
		title, ok := claudeTagTitle(prompt)
		require.True(t, ok)
		require.Equal(t, "Claude Tag in #dev-demo", title)
	}
}
