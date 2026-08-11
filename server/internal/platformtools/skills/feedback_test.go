package skills

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	hooksrepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	feedbackrecorder "github.com/speakeasy-api/gram/server/internal/skills/feedback"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestAssistantFeedbackRecordsExactLoadedObservationWithoutHeaderTrust(t *testing.T) {
	t.Parallel()

	ctx, fixture := newSkillLoadFixture(t, "feedback-skill")
	load := NewLoadTool(testenv.NewLogger(t), fixture.conn)
	var loaded bytes.Buffer
	require.NoError(t, load.Call(ctx, skillToolCallEnv(fixture.chatID.String()), bytes.NewBufferString(`{"name":"feedback-skill"}`), &loaded))

	recorder := feedbackrecorder.NewRecorder(fixture.conn, testenv.NewLogger(t), nil)
	feedback := NewAssistantFeedbackTool(fixture.conn, recorder)
	var out bytes.Buffer
	require.NoError(t, feedback.Call(ctx, skillToolCallEnv(uuid.NewString()), bytes.NewBufferString(`{"skill":"feedback-skill","outcome":"helped"}`), &out))
	require.JSONEq(t, `{"recorded":true}`, out.String())

	rows, err := skillsrepo.New(fixture.conn).ListRecentSkillFeedback(ctx, skillsrepo.ListRecentSkillFeedbackParams{
		ProjectID: fixture.projectID,
		SkillName: "feedback-skill",
		PageLimit: 10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, fixture.version.SkillID, rows[0].SkillID.UUID)
	require.Equal(t, fixture.version.ID, rows[0].SkillVersionID.UUID)
	require.Equal(t, fixture.chatID.String(), rows[0].SessionID.String)
	require.Equal(t, "user-test", rows[0].UserID.String)
	require.Equal(t, "user@example.test", rows[0].UserEmail.String)
}

func TestAssistantFeedbackPreservesHistoricalLoadedVersion(t *testing.T) {
	t.Parallel()

	ctx, fixture := newSkillLoadFixture(t, "historical-skill")
	load := NewLoadTool(testenv.NewLogger(t), fixture.conn)
	var loaded bytes.Buffer
	require.NoError(t, load.Call(ctx, skillToolCallEnv(fixture.chatID.String()), bytes.NewBufferString(`{"name":"historical-skill"}`), &loaded))

	queries := skillsrepo.New(fixture.conn)
	newVersion, err := queries.CreateSkillVersion(ctx, skillsrepo.CreateSkillVersionParams{
		Content:          "---\nname: historical-skill\ndescription: second\n---\n\nnew body\n",
		CanonicalSha256:  uuid.NewString(),
		RawSha256:        uuid.NewString(),
		Description:      pgtype.Text{String: "second", Valid: true},
		Metadata:         []byte(`{}`),
		SpecValid:        true,
		ValidationErrors: []byte(`[]`),
		CreatedByUserID:  "user-test",
		ProjectID:        fixture.projectID,
		SkillID:          fixture.version.SkillID,
	})
	require.NoError(t, err)
	_, err = queries.UpdateSkillDistribution(ctx, skillsrepo.UpdateSkillDistributionParams{
		PinnedVersionID: uuid.NullUUID{UUID: newVersion.ID, Valid: true},
		ProjectID:       fixture.projectID,
		SkillID:         fixture.version.SkillID,
		PluginID:        uuid.NullUUID{},
		AssistantID:     uuid.NullUUID{UUID: fixture.assistantID, Valid: true},
		Channel:         "assistant",
	})
	require.NoError(t, err)

	recorder := feedbackrecorder.NewRecorder(fixture.conn, testenv.NewLogger(t), nil)
	feedback := NewAssistantFeedbackTool(fixture.conn, recorder)
	var out bytes.Buffer
	require.NoError(t, feedback.Call(ctx, skillToolCallEnv(""), bytes.NewBufferString(`{"skill":"historical-skill","outcome":"partially_helped"}`), &out))

	rows, err := queries.ListRecentSkillFeedback(ctx, skillsrepo.ListRecentSkillFeedbackParams{
		ProjectID: fixture.projectID,
		SkillName: "historical-skill",
		PageLimit: 10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, fixture.version.ID, rows[0].SkillVersionID.UUID)
	require.NotEqual(t, newVersion.ID, rows[0].SkillVersionID.UUID)
}

func TestAssistantFeedbackUsesFinalVersionAfterLoadABA(t *testing.T) {
	t.Parallel()

	ctx, fixture := newSkillLoadFixture(t, "recency-skill")
	load := NewLoadTool(testenv.NewLogger(t), fixture.conn)
	var out bytes.Buffer
	require.NoError(t, load.Call(ctx, skillToolCallEnv(fixture.chatID.String()), bytes.NewBufferString(`{"name":"recency-skill"}`), &out))

	queries := skillsrepo.New(fixture.conn)
	versionB, err := queries.CreateSkillVersion(ctx, skillsrepo.CreateSkillVersionParams{
		Content:          "---\nname: recency-skill\ndescription: second\n---\n\nsecond body\n",
		CanonicalSha256:  uuid.NewString(),
		RawSha256:        uuid.NewString(),
		Description:      pgtype.Text{String: "second", Valid: true},
		Metadata:         []byte(`{}`),
		SpecValid:        true,
		ValidationErrors: []byte(`[]`),
		CreatedByUserID:  "user-test",
		ProjectID:        fixture.projectID,
		SkillID:          fixture.version.SkillID,
	})
	require.NoError(t, err)

	for _, versionID := range []uuid.UUID{versionB.ID, fixture.version.ID} {
		_, err = queries.UpdateSkillDistribution(ctx, skillsrepo.UpdateSkillDistributionParams{
			PinnedVersionID: uuid.NullUUID{UUID: versionID, Valid: true},
			ProjectID:       fixture.projectID,
			SkillID:         fixture.version.SkillID,
			PluginID:        uuid.NullUUID{},
			AssistantID:     uuid.NullUUID{UUID: fixture.assistantID, Valid: true},
			Channel:         "assistant",
		})
		require.NoError(t, err)
		out.Reset()
		require.NoError(t, load.Call(ctx, skillToolCallEnv(fixture.chatID.String()), bytes.NewBufferString(`{"name":"recency-skill"}`), &out))
	}

	recorder := feedbackrecorder.NewRecorder(fixture.conn, testenv.NewLogger(t), nil)
	feedback := NewAssistantFeedbackTool(fixture.conn, recorder)
	out.Reset()
	require.NoError(t, feedback.Call(ctx, skillToolCallEnv(""), bytes.NewBufferString(`{"skill":"recency-skill","outcome":"helped"}`), &out))

	rows, err := queries.ListRecentSkillFeedback(ctx, skillsrepo.ListRecentSkillFeedbackParams{
		ProjectID: fixture.projectID,
		SkillName: "recency-skill",
		PageLimit: 10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, fixture.version.ID, rows[0].SkillVersionID.UUID)
}

func TestAssistantFeedbackUsesObservationAfterSkillArchived(t *testing.T) {
	t.Parallel()

	ctx, fixture := newSkillLoadFixture(t, "archived-skill")
	load := NewLoadTool(testenv.NewLogger(t), fixture.conn)
	var out bytes.Buffer
	require.NoError(t, load.Call(ctx, skillToolCallEnv(fixture.chatID.String()), bytes.NewBufferString(`{"name":"archived-skill"}`), &out))

	queries := skillsrepo.New(fixture.conn)
	_, err := queries.ArchiveSkill(ctx, skillsrepo.ArchiveSkillParams{
		ProjectID: fixture.projectID,
		ID:        fixture.version.SkillID,
	})
	require.NoError(t, err)

	recorder := feedbackrecorder.NewRecorder(fixture.conn, testenv.NewLogger(t), nil)
	feedback := NewAssistantFeedbackTool(fixture.conn, recorder)
	out.Reset()
	require.NoError(t, feedback.Call(ctx, skillToolCallEnv(""), bytes.NewBufferString(`{"skill":"archived-skill","outcome":"helped"}`), &out))

	rows, err := queries.ListRecentSkillFeedback(ctx, skillsrepo.ListRecentSkillFeedbackParams{
		ProjectID: fixture.projectID,
		SkillName: "archived-skill",
		PageLimit: 10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, fixture.version.ID, rows[0].SkillVersionID.UUID)
	require.Equal(t, "archived-skill", rows[0].SkillName)
}

func TestAssistantFeedbackDoesNotCrossResolveSharedChat(t *testing.T) {
	t.Parallel()

	ctx, fixture := newSkillLoadFixture(t, "shared-chat-skill")
	load := NewLoadTool(testenv.NewLogger(t), fixture.conn)
	var out bytes.Buffer
	require.NoError(t, load.Call(ctx, skillToolCallEnv(fixture.chatID.String()), bytes.NewBufferString(`{"name":"shared-chat-skill"}`), &out))

	assistantQueries := assistantrepo.New(fixture.conn)
	secondAssistant, err := assistantQueries.CreateAssistant(ctx, assistantrepo.CreateAssistantParams{
		ProjectID:       fixture.projectID,
		OrganizationID:  fixture.organizationID,
		CreatedByUserID: pgtype.Text{String: "user-test", Valid: true},
		Name:            "Shared chat assistant",
		Model:           "test-model",
		Instructions:    "",
		WarmTtlSeconds:  60,
		MaxConcurrency:  1,
		Status:          "active",
	})
	require.NoError(t, err)
	secondThreadID, err := assistantQueries.UpsertAssistantThread(ctx, assistantrepo.UpsertAssistantThreadParams{
		AssistantID:   secondAssistant.ID,
		ProjectID:     fixture.projectID,
		CorrelationID: "shared-chat-" + uuid.NewString(),
		ChatID:        fixture.chatID,
		SourceKind:    "dashboard",
		SourceRefJson: []byte(`{}`),
	})
	require.NoError(t, err)
	secondCtx := contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{
		AssistantID: secondAssistant.ID,
		ThreadID:    secondThreadID,
	})

	recorder := feedbackrecorder.NewRecorder(fixture.conn, testenv.NewLogger(t), nil)
	feedback := NewAssistantFeedbackTool(fixture.conn, recorder)
	out.Reset()
	err = feedback.Call(secondCtx, skillToolCallEnv(""), bytes.NewBufferString(`{"skill":"shared-chat-skill","outcome":"helped"}`), &out)
	require.ErrorContains(t, err, "call skills_load")

	_, err = skillsrepo.New(fixture.conn).CreateSkillDistribution(ctx, skillsrepo.CreateSkillDistributionParams{
		PluginID:        uuid.NullUUID{},
		AssistantID:     uuid.NullUUID{UUID: secondAssistant.ID, Valid: true},
		PinnedVersionID: uuid.NullUUID{},
		Channel:         "assistant",
		CreatedByUserID: "user-test",
		ProjectID:       fixture.projectID,
		SkillID:         fixture.version.SkillID,
	})
	require.NoError(t, err)
	out.Reset()
	require.NoError(t, load.Call(secondCtx, skillToolCallEnv(fixture.chatID.String()), bytes.NewBufferString(`{"name":"shared-chat-skill"}`), &out))

	observations, err := hooksrepo.New(fixture.conn).ListSkillObservations(ctx, fixture.projectID)
	require.NoError(t, err)
	require.Len(t, observations, 2)
	require.NotEqual(t, observations[0].IdempotencyKey.String, observations[1].IdempotencyKey.String)
}

func TestAssistantFeedbackRejectsMissingOrCrossScopedObservation(t *testing.T) {
	t.Parallel()

	ctx, fixture := newSkillLoadFixture(t, "scoped-skill")
	recorder := feedbackrecorder.NewRecorder(fixture.conn, testenv.NewLogger(t), nil)
	feedback := NewAssistantFeedbackTool(fixture.conn, recorder)
	payload := []byte(`{"skill":"scoped-skill","outcome":"did_not_help"}`)

	var out bytes.Buffer
	err := feedback.Call(ctx, skillToolCallEnv(""), bytes.NewReader(payload), &out)
	require.ErrorContains(t, err, "call skills_load")

	missingThreadCtx := contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{AssistantID: fixture.assistantID, ThreadID: uuid.Nil})
	err = feedback.Call(missingThreadCtx, skillToolCallEnv(""), bytes.NewReader(payload), &out)
	require.ErrorContains(t, err, "assistant thread")

	load := NewLoadTool(testenv.NewLogger(t), fixture.conn)
	require.NoError(t, load.Call(ctx, skillToolCallEnv(fixture.chatID.String()), bytes.NewBufferString(`{"name":"scoped-skill"}`), &out))

	wrongAssistantCtx := contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{AssistantID: uuid.New(), ThreadID: fixture.threadID})
	err = feedback.Call(wrongAssistantCtx, skillToolCallEnv(""), bytes.NewReader(payload), &out)
	require.ErrorContains(t, err, "call skills_load")

	wrongThreadCtx := contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{AssistantID: fixture.assistantID, ThreadID: uuid.New()})
	err = feedback.Call(wrongThreadCtx, skillToolCallEnv(""), bytes.NewReader(payload), &out)
	require.ErrorContains(t, err, "call skills_load")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	otherAuth := *authCtx
	otherProjectID := uuid.New()
	otherAuth.ProjectID = &otherProjectID
	wrongProjectCtx := contextvalues.SetAuthContext(ctx, &otherAuth)
	err = feedback.Call(wrongProjectCtx, skillToolCallEnv(""), bytes.NewReader(payload), &out)
	require.ErrorContains(t, err, "call skills_load")
}

func TestFeedbackSchemaRequiresOutcomeButNotNote(t *testing.T) {
	t.Parallel()

	descriptor := NewAssistantFeedbackTool(nil, nil).Descriptor()
	require.JSONEq(t, `{
		"type":"object",
		"additionalProperties":false,
		"properties":{
			"skill":{"type":"string","description":"Canonical name of the skill that was used.","minLength":1,"maxLength":64,"pattern":"^[a-z0-9]+(?:-[a-z0-9]+)*$"},
			"outcome":{"type":"string","description":"How the skill affected the task.","enum":["helped","partially_helped","did_not_help","misleading","harmful"]},
			"note":{"type":["null","string"],"description":"Optional concise context about the outcome.","maxLength":4000}
		},
		"required":["skill","outcome"]
	}`, string(descriptor.InputSchema))
}

func TestFeedbackRejectsInvalidOutcomeBeforeRecorder(t *testing.T) {
	t.Parallel()

	tool := NewAssistantFeedbackTool(nil, nil)
	projectID := uuid.New()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ProjectID: &projectID})
	err := tool.Call(ctx, skillToolCallEnv(""), bytes.NewBufferString(`{"skill":"feedback-skill","outcome":"unknown"}`), &bytes.Buffer{})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestFeedbackRejectsOversizedNoteBeforeRecorder(t *testing.T) {
	t.Parallel()

	tool := NewAssistantFeedbackTool(nil, nil)
	projectID := uuid.New()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ProjectID: &projectID})
	payload := `{"skill":"feedback-skill","outcome":"helped","note":"` + strings.Repeat("界", 4001) + `"}`
	err := tool.Call(ctx, skillToolCallEnv(""), strings.NewReader(payload), &bytes.Buffer{})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}
