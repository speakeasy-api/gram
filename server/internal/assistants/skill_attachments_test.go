package assistants

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	hooksrepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformskills "github.com/speakeasy-api/gram/server/internal/platformtools/skills"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func ensureAssistantTestOrganization(t *testing.T, conn skillsrepo.DBTX) {
	t.Helper()
	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID: "org-test", Name: "Test organization", Slug: "org-test", WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
}

func createSkillAttachmentFixture(t *testing.T, conn skillsrepo.DBTX, projectID, assistantID uuid.UUID, name, userID string) (skillsrepo.Skill, skillsrepo.SkillVersion) {
	t.Helper()
	queries := skillsrepo.New(conn)
	skill, err := queries.CreateSkill(t.Context(), skillsrepo.CreateSkillParams{
		ProjectID: projectID, Name: name, DisplayName: name, Summary: pgtype.Text{},
	})
	require.NoError(t, err)
	version, err := queries.CreateSkillVersion(t.Context(), skillsrepo.CreateSkillVersionParams{
		Content:         "---\nname: " + name + "\ndescription: first\n---\n\nbody\n",
		CanonicalSha256: uuid.NewString(), RawSha256: uuid.NewString(), Description: pgtype.Text{String: "first", Valid: true},
		Metadata: []byte(`{}`), SpecValid: true, ValidationErrors: []byte(`[]`), CreatedByUserID: userID,
		ProjectID: projectID, SkillID: skill.ID,
	})
	require.NoError(t, err)
	_, err = queries.CreateSkillDistribution(t.Context(), skillsrepo.CreateSkillDistributionParams{
		PluginID: uuid.NullUUID{}, AssistantID: uuid.NullUUID{UUID: assistantID, Valid: true},
		PinnedVersionID: uuid.NullUUID{}, Channel: "assistant", CreatedByUserID: userID,
		ProjectID: projectID, SkillID: skill.ID,
	})
	require.NoError(t, err)
	return skill, version
}

func TestAssistantSkillHydrationTracksLatestAndPin(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, conn := newRBACServiceWithConn(t, "assistant_skill_hydration")
	record, err := svc.core.CreateAssistant(ctx, "org-test", projectID, "user-test", "Skill assistant", "test-model", "", nil, nil, 60, 1, StatusActive)
	require.NoError(t, err)
	emptyView, err := toHTTPAssistant(record)
	require.NoError(t, err)
	require.NotNil(t, emptyView.Skills)
	require.Empty(t, emptyView.Skills)

	skill, first := createSkillAttachmentFixture(t, conn, projectID, record.ID, "hydrated-skill", "user-test")

	got, err := svc.core.GetAssistant(ctx, projectID, record.ID)
	require.NoError(t, err)
	require.Len(t, got.Skills, 1)
	require.Equal(t, skill.ID, got.Skills[0].SkillID)
	require.Equal(t, skill.Name, got.Skills[0].Name)
	require.Equal(t, first.ID, got.Skills[0].ResolvedVersionID)
	require.Equal(t, "first", got.Skills[0].Description)

	view, err := toHTTPAssistant(got)
	require.NoError(t, err)
	require.Len(t, view.Skills, 1)
	require.Equal(t, skill.ID.String(), view.Skills[0].SkillID)
	require.Nil(t, view.Skills[0].PinnedVersionID)
	require.Equal(t, first.ID.String(), view.Skills[0].ResolvedVersionID)

	listed, err := svc.core.ListAssistants(ctx, projectID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Len(t, listed[0].Skills, 1)

	second, err := skillsrepo.New(conn).CreateSkillVersion(ctx, skillsrepo.CreateSkillVersionParams{
		Content:         "---\nname: hydrated-skill\ndescription: second\n---\n\nbody\n",
		CanonicalSha256: uuid.NewString(), RawSha256: uuid.NewString(), Description: pgtype.Text{String: "second", Valid: true},
		Metadata: []byte(`{}`), SpecValid: true, ValidationErrors: []byte(`[]`), CreatedByUserID: "user-test",
		ProjectID: projectID, SkillID: skill.ID,
	})
	require.NoError(t, err)
	got, err = svc.core.GetAssistant(ctx, projectID, record.ID)
	require.NoError(t, err)
	require.Equal(t, second.ID, got.Skills[0].ResolvedVersionID)

	_, err = skillsrepo.New(conn).CreateSkillVersion(ctx, skillsrepo.CreateSkillVersionParams{
		Content:         "---\nname: Hydrated_Skill\ndescription: invalid\n---\n\nbody\n",
		CanonicalSha256: uuid.NewString(), RawSha256: uuid.NewString(), Description: pgtype.Text{String: "invalid", Valid: true},
		Metadata: []byte(`{}`), SpecValid: false, ValidationErrors: []byte(`[]`), CreatedByUserID: "user-test",
		ProjectID: projectID, SkillID: skill.ID,
	})
	require.NoError(t, err)
	got, err = svc.core.GetAssistant(ctx, projectID, record.ID)
	require.NoError(t, err)
	require.Equal(t, second.ID, got.Skills[0].ResolvedVersionID)

	updatedName := "Updated skill assistant"
	updated, err := svc.core.UpdateAssistant(ctx, projectID, record.ID, &updatedName, nil, nil, nil, nil, nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, updated.Skills, 1)
	require.Equal(t, second.ID, updated.Skills[0].ResolvedVersionID)

	_, err = skillsrepo.New(conn).UpdateSkillDistribution(ctx, skillsrepo.UpdateSkillDistributionParams{
		PinnedVersionID: uuid.NullUUID{UUID: first.ID, Valid: true}, ProjectID: projectID, SkillID: skill.ID,
		PluginID: uuid.NullUUID{}, AssistantID: uuid.NullUUID{UUID: record.ID, Valid: true}, Channel: "assistant",
	})
	require.NoError(t, err)
	got, err = svc.core.GetAssistant(ctx, projectID, record.ID)
	require.NoError(t, err)
	require.Equal(t, first.ID, got.Skills[0].ResolvedVersionID)
	require.Equal(t, first.ID, got.Skills[0].PinnedVersionID.UUID)
}

func TestBuildThreadBootstrapInitializesAndReusesPersistedSkillBaseline(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, conn := newRBACServiceWithConn(t, "assistant_skill_bootstrap_snapshot")
	record, err := svc.core.CreateAssistant(ctx, "org-test", projectID, "user-test", "Bootstrap skill assistant", "test-model", "", nil, nil, 60, 1, StatusActive)
	require.NoError(t, err)
	skill, _ := createSkillAttachmentFixture(t, conn, projectID, record.ID, "bootstrap-skill", "user-test")

	chatID := uuid.New()
	err = assistantrepo.New(conn).UpsertAssistantChat(ctx, assistantrepo.UpsertAssistantChatParams{
		ChatID: chatID, ProjectID: projectID, OrganizationID: "org-test", Title: pgtype.Text{},
	})
	require.NoError(t, err)
	threadID, err := assistantrepo.New(conn).UpsertAssistantThread(ctx, assistantrepo.UpsertAssistantThreadParams{
		AssistantID: record.ID, ProjectID: projectID, CorrelationID: "bootstrap-snapshot", ChatID: chatID,
		SourceKind: sourceKindSlack, SourceRefJson: []byte(`{}`),
	})
	require.NoError(t, err)

	first, err := svc.core.BuildThreadBootstrap(ctx, projectID, threadID, record.ID)
	require.NoError(t, err)
	require.Contains(t, first.Instructions, `Name: "bootstrap-skill"; description: "first"`)

	_, err = skillsrepo.New(conn).CreateSkillVersion(ctx, skillsrepo.CreateSkillVersionParams{
		Content: "---\nname: bootstrap-skill\ndescription: second\n---\n\nbody\n", CanonicalSha256: uuid.NewString(), RawSha256: uuid.NewString(),
		Description: pgtype.Text{String: "second", Valid: true}, Metadata: []byte(`{}`), SpecValid: true,
		ValidationErrors: []byte(`[]`), CreatedByUserID: "user-test", ProjectID: projectID, SkillID: skill.ID,
	})
	require.NoError(t, err)

	second, err := svc.core.BuildThreadBootstrap(ctx, projectID, threadID, record.ID)
	require.NoError(t, err)
	require.Contains(t, second.Instructions, `Name: "bootstrap-skill"; description: "first"`)
	require.NotContains(t, second.Instructions, `description: "second"`)
}

func TestAssistantSkillQueriesResolveLatestPinArchiveAndRevoke(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, conn := newRBACServiceWithConn(t, "assistant_skill_query_resolution")
	record, err := svc.core.CreateAssistant(ctx, "org-test", projectID, "user-test", "Query skill assistant", "test-model", "", nil, nil, 60, 1, StatusActive)
	require.NoError(t, err)
	skill, first := createSkillAttachmentFixture(t, conn, projectID, record.ID, "query-skill", "user-test")
	queries := assistantrepo.New(conn)
	loadParams := assistantrepo.LoadAttachedAssistantSkillParams{
		AssistantID: uuid.NullUUID{UUID: record.ID, Valid: true},
		ProjectID:   projectID,
		Name:        skill.Name,
	}
	listParams := assistantrepo.LoadAssistantSkillsParams{
		AssistantIds: []uuid.UUID{record.ID},
		ProjectID:    projectID,
	}

	loaded, err := queries.LoadAttachedAssistantSkill(ctx, loadParams)
	require.NoError(t, err)
	require.Contains(t, loaded.Content, "description: first")

	second, err := skillsrepo.New(conn).CreateSkillVersion(ctx, skillsrepo.CreateSkillVersionParams{
		Content:          "---\nname: query-skill\ndescription: second\n---\n\nsecond body\n",
		CanonicalSha256:  uuid.NewString(),
		RawSha256:        uuid.NewString(),
		Description:      pgtype.Text{String: "second", Valid: true},
		Metadata:         []byte(`{}`),
		SpecValid:        true,
		ValidationErrors: []byte(`[]`),
		CreatedByUserID:  "user-test",
		ProjectID:        projectID,
		SkillID:          skill.ID,
	})
	require.NoError(t, err)
	loaded, err = queries.LoadAttachedAssistantSkill(ctx, loadParams)
	require.NoError(t, err)
	require.Equal(t, second.Content, loaded.Content)

	invalid, err := skillsrepo.New(conn).CreateSkillVersion(ctx, skillsrepo.CreateSkillVersionParams{
		Content:          "invalid",
		CanonicalSha256:  uuid.NewString(),
		RawSha256:        uuid.NewString(),
		Description:      pgtype.Text{String: "invalid", Valid: true},
		Metadata:         []byte(`{}`),
		SpecValid:        false,
		ValidationErrors: []byte(`[]`),
		CreatedByUserID:  "user-test",
		ProjectID:        projectID,
		SkillID:          skill.ID,
	})
	require.NoError(t, err)
	_, err = skillsrepo.New(conn).UpdateSkillDistribution(ctx, skillsrepo.UpdateSkillDistributionParams{
		PinnedVersionID: uuid.NullUUID{UUID: invalid.ID, Valid: true},
		ProjectID:       projectID,
		SkillID:         skill.ID,
		PluginID:        uuid.NullUUID{},
		AssistantID:     uuid.NullUUID{UUID: record.ID, Valid: true},
		Channel:         "assistant",
	})
	require.NoError(t, err)
	_, err = queries.LoadAttachedAssistantSkill(ctx, loadParams)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	attached, err := queries.LoadAssistantSkills(ctx, listParams)
	require.NoError(t, err)
	require.Empty(t, attached)

	_, err = skillsrepo.New(conn).UpdateSkillDistribution(ctx, skillsrepo.UpdateSkillDistributionParams{
		PinnedVersionID: uuid.NullUUID{UUID: first.ID, Valid: true},
		ProjectID:       projectID,
		SkillID:         skill.ID,
		PluginID:        uuid.NullUUID{},
		AssistantID:     uuid.NullUUID{UUID: record.ID, Valid: true},
		Channel:         "assistant",
	})
	require.NoError(t, err)
	loaded, err = queries.LoadAttachedAssistantSkill(ctx, loadParams)
	require.NoError(t, err)
	require.Equal(t, first.Content, loaded.Content)
	_, err = skillsrepo.New(conn).ArchiveSkill(ctx, skillsrepo.ArchiveSkillParams{ProjectID: projectID, ID: skill.ID})
	require.NoError(t, err)
	_, err = queries.LoadAttachedAssistantSkill(ctx, loadParams)
	require.ErrorIs(t, err, pgx.ErrNoRows)

	revokedSkill, _ := createSkillAttachmentFixture(t, conn, projectID, record.ID, "revoked-skill", "user-test")
	_, err = skillsrepo.New(conn).RevokeActiveSkillDistribution(ctx, skillsrepo.RevokeActiveSkillDistributionParams{
		ProjectID:   projectID,
		SkillID:     revokedSkill.ID,
		PluginID:    uuid.NullUUID{},
		AssistantID: uuid.NullUUID{UUID: record.ID, Valid: true},
		Channel:     "assistant",
	})
	require.NoError(t, err)
	_, err = queries.LoadAttachedAssistantSkill(ctx, assistantrepo.LoadAttachedAssistantSkillParams{
		AssistantID: uuid.NullUUID{UUID: record.ID, Valid: true},
		ProjectID:   projectID,
		Name:        revokedSkill.Name,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	attached, err = queries.LoadAssistantSkills(ctx, listParams)
	require.NoError(t, err)
	require.Empty(t, attached)
}

func TestSkillsLoadRequiresAssistantPrincipal(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, conn := newRBACServiceWithConn(t, "skills_load_unauthorized")
	record, err := svc.core.CreateAssistant(ctx, "org-test", projectID, "user-test", "Unauthorized skill assistant", "test-model", "", nil, nil, 60, 1, StatusActive)
	require.NoError(t, err)
	createSkillAttachmentFixture(t, conn, projectID, record.ID, "skill", "user-test")

	var out bytes.Buffer
	err = platformskills.NewLoadTool(testenv.NewLogger(t), conn).Call(ctx, skillToolCallEnv(""), bytes.NewBufferString(`{"name":"skill"}`), &out)
	requireOopsCode(t, err, oops.CodeUnauthorized)
	require.ErrorContains(t, err, "assistant principal")
	observations, err := hooksrepo.New(conn).ListSkillObservations(ctx, projectID)
	require.NoError(t, err)
	require.Empty(t, observations)
}

func TestSkillsLoadReturnsAttachedContent(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, conn := newRBACServiceWithConn(t, "skills_load_content")
	record, err := svc.core.CreateAssistant(ctx, "org-test", projectID, "user-test", "Load skill assistant", "test-model", "", nil, nil, 60, 1, StatusActive)
	require.NoError(t, err)
	skill, version := createSkillAttachmentFixture(t, conn, projectID, record.ID, "loaded-skill", "user-test")
	threadID := uuid.New()
	chatID := uuid.New()
	ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{AssistantID: record.ID, ThreadID: threadID})

	var out bytes.Buffer
	tool := platformskills.NewLoadTool(testenv.NewLogger(t), conn)
	err = tool.Call(ctx, skillToolCallEnv(chatID.String()), bytes.NewBufferString(`{"name":"loaded-skill"}`), &out)
	require.NoError(t, err)
	require.Equal(t, version.Content, out.String())
	out.Reset()
	require.NoError(t, tool.Call(ctx, skillToolCallEnv(chatID.String()), bytes.NewBufferString(`{"name":"loaded-skill"}`), &out))

	observations, err := hooksrepo.New(conn).ListSkillObservations(ctx, projectID)
	require.NoError(t, err)
	require.Len(t, observations, 1)
	observation := observations[0]
	require.Equal(t, projectID, observation.ProjectID)
	require.Equal(t, "assistant:"+chatID.String()+":"+version.ID.String(), observation.IdempotencyKey.String)
	require.Equal(t, "assistant", observation.Provider)
	require.Equal(t, chatID.String(), observation.SessionID.String)
	require.NotEqual(t, threadID.String(), observation.SessionID.String)
	require.Equal(t, skill.Name, observation.SkillName)
	require.Equal(t, version.RawSha256, observation.RawSha256.String)
	require.Equal(t, skill.ID, observation.SkillID.UUID)
	require.Equal(t, version.ID, observation.SkillVersionID.UUID)
	require.True(t, observation.SeenAt.Valid)
	require.True(t, observation.ReconciledAt.Valid)
	require.Equal(t, observation.SeenAt.Time, observation.ReconciledAt.Time)
	require.False(t, observation.MetricsSyncedAt.Valid)
	require.False(t, observation.EfficacyEnqueuedAt.Valid)

	second, err := skillsrepo.New(conn).CreateSkillVersion(ctx, skillsrepo.CreateSkillVersionParams{
		Content:          "---\nname: loaded-skill\ndescription: second\n---\n\nsecond body\n",
		CanonicalSha256:  uuid.NewString(),
		RawSha256:        uuid.NewString(),
		Description:      pgtype.Text{String: "second", Valid: true},
		Metadata:         []byte(`{}`),
		SpecValid:        true,
		ValidationErrors: []byte(`[]`),
		CreatedByUserID:  "user-test",
		ProjectID:        projectID,
		SkillID:          skill.ID,
	})
	require.NoError(t, err)
	out.Reset()
	require.NoError(t, tool.Call(ctx, skillToolCallEnv(chatID.String()), bytes.NewBufferString(`{"name":"loaded-skill"}`), &out))
	require.Equal(t, second.Content, out.String())

	observations, err = hooksrepo.New(conn).ListSkillObservations(ctx, projectID)
	require.NoError(t, err)
	require.Len(t, observations, 2)
	pending, err := skillsrepo.New(conn).ListPendingSkillSessionVersions(ctx, skillsrepo.ListPendingSkillSessionVersionsParams{
		ProjectID: projectID,
		BatchSize: 10,
	})
	require.NoError(t, err)
	require.Len(t, pending, 2)
	versions := map[uuid.UUID]string{
		version.ID: version.CanonicalSha256,
		second.ID:  second.CanonicalSha256,
	}
	for _, row := range pending {
		require.Equal(t, chatID.String(), row.SessionID)
		require.Equal(t, skill.ID, row.SkillID)
		require.Equal(t, "assistant", row.Surface)
		require.Equal(t, versions[row.SkillVersionID], row.CanonicalSha256)
	}
}

func TestSkillsLoadReportsNoAttachedSkills(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, conn := newRBACServiceWithConn(t, "skills_load_empty")
	record, err := svc.core.CreateAssistant(ctx, "org-test", projectID, "user-test", "Empty skill assistant", "test-model", "", nil, nil, 60, 1, StatusActive)
	require.NoError(t, err)
	ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{AssistantID: record.ID, ThreadID: uuid.New()})

	var out bytes.Buffer
	err = platformskills.NewLoadTool(testenv.NewLogger(t), conn).Call(ctx, skillToolCallEnv(""), bytes.NewBufferString(`{"name":"missing"}`), &out)
	require.NoError(t, err)
	require.Equal(t, "no skills attached", out.String())
	observations, err := hooksrepo.New(conn).ListSkillObservations(ctx, projectID)
	require.NoError(t, err)
	require.Empty(t, observations)
}

func TestSkillsLoadHidesUnattachedSkillWhenAnotherIsAttached(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, conn := newRBACServiceWithConn(t, "skills_load_not_attached")
	record, err := svc.core.CreateAssistant(ctx, "org-test", projectID, "user-test", "Missing skill assistant", "test-model", "", nil, nil, 60, 1, StatusActive)
	require.NoError(t, err)
	createSkillAttachmentFixture(t, conn, projectID, record.ID, "attached-skill", "user-test")
	ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{AssistantID: record.ID, ThreadID: uuid.New()})

	var out bytes.Buffer
	err = platformskills.NewLoadTool(testenv.NewLogger(t), conn).Call(ctx, skillToolCallEnv(""), bytes.NewBufferString(`{"name":"missing"}`), &out)
	requireOopsCode(t, err, oops.CodeNotFound)
	require.EqualError(t, err, "skill is not attached to this assistant")
	observations, err := hooksrepo.New(conn).ListSkillObservations(ctx, projectID)
	require.NoError(t, err)
	require.Empty(t, observations)
}

type failedSkillContentWriter struct{}

func (failedSkillContentWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestSkillsLoadWriteFailureDoesNotRecordObservation(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, conn := newRBACServiceWithConn(t, "skills_load_write_failure")
	record, err := svc.core.CreateAssistant(ctx, "org-test", projectID, "user-test", "Writer failure assistant", "test-model", "", nil, nil, 60, 1, StatusActive)
	require.NoError(t, err)
	createSkillAttachmentFixture(t, conn, projectID, record.ID, "loaded-skill", "user-test")
	ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{AssistantID: record.ID, ThreadID: uuid.New()})

	err = platformskills.NewLoadTool(testenv.NewLogger(t), conn).Call(ctx, skillToolCallEnv(uuid.NewString()), bytes.NewBufferString(`{"name":"loaded-skill"}`), failedSkillContentWriter{})
	require.ErrorIs(t, err, io.ErrClosedPipe)
	observations, err := hooksrepo.New(conn).ListSkillObservations(ctx, projectID)
	require.NoError(t, err)
	require.Empty(t, observations)
}

type closePoolWriter struct {
	content []byte
	pool    *pgxpool.Pool
}

func (w *closePoolWriter) Write(p []byte) (int, error) {
	w.content = append(w.content, p...)
	w.pool.Close()
	return len(p), nil
}

func TestSkillsLoadObservationFailureDoesNotAlterSuccessfulResult(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, conn := newRBACServiceWithConn(t, "skills_load_observation_failure")
	record, err := svc.core.CreateAssistant(ctx, "org-test", projectID, "user-test", "Observation failure assistant", "test-model", "", nil, nil, 60, 1, StatusActive)
	require.NoError(t, err)
	_, version := createSkillAttachmentFixture(t, conn, projectID, record.ID, "loaded-skill", "user-test")
	ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{AssistantID: record.ID, ThreadID: uuid.New()})
	writer := &closePoolWriter{content: nil, pool: conn}

	err = platformskills.NewLoadTool(testenv.NewLogger(t), conn).Call(ctx, skillToolCallEnv(uuid.NewString()), bytes.NewBufferString(`{"name":"loaded-skill"}`), writer)
	require.NoError(t, err)
	require.Equal(t, version.Content, string(writer.content))
}

type cancelContextWriter struct {
	content []byte
	cancel  context.CancelFunc
}

func (w *cancelContextWriter) Write(p []byte) (int, error) {
	w.content = append(w.content, p...)
	w.cancel()
	return len(p), nil
}

func TestSkillsLoadRecordsObservationAfterCallContextCancellation(t *testing.T) {
	t.Parallel()

	svc, baseCtx, projectID, conn := newRBACServiceWithConn(t, "skills_load_canceled_context")
	record, err := svc.core.CreateAssistant(baseCtx, "org-test", projectID, "user-test", "Canceled context assistant", "test-model", "", nil, nil, 60, 1, StatusActive)
	require.NoError(t, err)
	_, version := createSkillAttachmentFixture(t, conn, projectID, record.ID, "loaded-skill", "user-test")
	baseCtx = contextvalues.SetAssistantPrincipal(baseCtx, contextvalues.AssistantPrincipal{AssistantID: record.ID, ThreadID: uuid.New()})
	ctx, cancel := context.WithCancel(baseCtx)
	writer := &cancelContextWriter{content: nil, cancel: cancel}
	chatID := uuid.New()

	err = platformskills.NewLoadTool(testenv.NewLogger(t), conn).Call(ctx, skillToolCallEnv(chatID.String()), bytes.NewBufferString(`{"name":"loaded-skill"}`), writer)
	require.NoError(t, err)
	require.Equal(t, version.Content, string(writer.content))
	observations, err := hooksrepo.New(conn).ListSkillObservations(baseCtx, projectID)
	require.NoError(t, err)
	require.Len(t, observations, 1)
	require.Equal(t, chatID.String(), observations[0].SessionID.String)
}

func TestSkillsLoadV2PrincipalRecordsGramChatID(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, conn := newRBACServiceWithConn(t, "skills_load_v2_principal")
	record, err := svc.core.CreateAssistant(ctx, "org-test", projectID, "user-test", "V2 skill assistant", "test-model", "", nil, nil, 60, 1, StatusActive)
	require.NoError(t, err)
	_, version := createSkillAttachmentFixture(t, conn, projectID, record.ID, "loaded-skill", "user-test")
	chatID := uuid.New()
	ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{AssistantID: record.ID, ThreadID: uuid.Nil})

	var out bytes.Buffer
	err = platformskills.NewLoadTool(testenv.NewLogger(t), conn).Call(ctx, skillToolCallEnv(chatID.String()), bytes.NewBufferString(`{"name":"loaded-skill"}`), &out)
	require.NoError(t, err)
	require.Equal(t, version.Content, out.String())
	observations, err := hooksrepo.New(conn).ListSkillObservations(ctx, projectID)
	require.NoError(t, err)
	require.Len(t, observations, 1)
	require.Equal(t, chatID.String(), observations[0].SessionID.String)
	require.Equal(t, "assistant:"+chatID.String()+":"+version.ID.String(), observations[0].IdempotencyKey.String)
}

func TestSkillsLoadInvalidGramChatIDSkipsObservation(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, conn := newRBACServiceWithConn(t, "skills_load_invalid_chat")
	record, err := svc.core.CreateAssistant(ctx, "org-test", projectID, "user-test", "Invalid chat skill assistant", "test-model", "", nil, nil, 60, 1, StatusActive)
	require.NoError(t, err)
	_, version := createSkillAttachmentFixture(t, conn, projectID, record.ID, "loaded-skill", "user-test")
	ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{AssistantID: record.ID, ThreadID: uuid.New()})
	tool := platformskills.NewLoadTool(testenv.NewLogger(t), conn)

	for _, chatID := range []string{"", "not-a-uuid", uuid.Nil.String()} {
		var out bytes.Buffer
		err = tool.Call(ctx, skillToolCallEnv(chatID), bytes.NewBufferString(`{"name":"loaded-skill"}`), &out)
		require.NoError(t, err)
		require.Equal(t, version.Content, out.String())
	}
	observations, err := hooksrepo.New(conn).ListSkillObservations(ctx, projectID)
	require.NoError(t, err)
	require.Empty(t, observations)
}

func skillToolCallEnv(chatID string) toolconfig.ToolCallEnv {
	return toolconfig.ToolCallEnv{
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
		GramChatID: chatID,
	}
}

func TestDeleteAssistantRevokesAndAuditsSkillAttachments(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, conn := newRBACServiceWithConn(t, "assistant_skill_delete")
	ensureAssistantTestOrganization(t, conn)
	record, err := svc.core.CreateAssistant(ctx, "org-test", projectID, "user-test", "Delete skill assistant", "test-model", "", nil, nil, 60, 1, StatusActive)
	require.NoError(t, err)
	skill, _ := createSkillAttachmentFixture(t, conn, projectID, record.ID, "delete-skill", "user-test")

	before, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionSkillUndistribute)
	require.NoError(t, err)
	require.NoError(t, svc.core.DeleteAssistant(ctx, projectID, record.ID, urn.NewPrincipal(urn.PrincipalTypeUser, "user-test"), nil))
	after, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionSkillUndistribute)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	_, err = skillsrepo.New(conn).GetActiveSkillDistributionRecord(ctx, skillsrepo.GetActiveSkillDistributionRecordParams{
		ProjectID: projectID, SkillID: skill.ID, PluginID: uuid.NullUUID{},
		AssistantID: uuid.NullUUID{UUID: record.ID, Valid: true}, Channel: "assistant",
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	details, err := skillsrepo.New(conn).GetSkillDetails(ctx, skillsrepo.GetSkillDetailsParams{ProjectID: projectID, SkillID: skill.ID})
	require.NoError(t, err)
	require.Zero(t, details.AssistantCount)

	entry, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionSkillUndistribute)
	require.NoError(t, err)
	snapshot, err := audittest.DecodeAuditData(entry.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, record.ID.String(), snapshot["AssistantID"])
}

func TestDisableManagedAssistantRevokesAndAuditsSkillAttachments(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "managed_assistant_skill_disable")
	require.NoError(t, err)
	projectID := newProvisioningProject(t, conn, "managed-skill-disable")
	ensureAssistantTestOrganization(t, conn)
	core := newProvisioningCore(t, conn)
	record, err := core.EnableManagedAssistant(t.Context(), "org-test", projectID, "user-test")
	require.NoError(t, err)
	skill, _ := createSkillAttachmentFixture(t, conn, projectID, record.ID, "managed-delete-skill", "user-test")

	before, err := audittest.AuditLogCountByAction(t.Context(), conn, audit.ActionSkillUndistribute)
	require.NoError(t, err)
	require.NoError(t, core.DisableManagedAssistant(t.Context(), projectID, urn.NewPrincipal(urn.PrincipalTypeUser, "user-test"), nil))
	after, err := audittest.AuditLogCountByAction(t.Context(), conn, audit.ActionSkillUndistribute)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	_, err = skillsrepo.New(conn).GetActiveSkillDistributionRecord(t.Context(), skillsrepo.GetActiveSkillDistributionRecordParams{
		ProjectID: projectID, SkillID: skill.ID, PluginID: uuid.NullUUID{},
		AssistantID: uuid.NullUUID{UUID: record.ID, Valid: true}, Channel: "assistant",
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	details, err := skillsrepo.New(conn).GetSkillDetails(t.Context(), skillsrepo.GetSkillDetailsParams{ProjectID: projectID, SkillID: skill.ID})
	require.NoError(t, err)
	require.Zero(t, details.AssistantCount)

	entry, err := audittest.LatestAuditLogByAction(t.Context(), conn, audit.ActionSkillUndistribute)
	require.NoError(t, err)
	snapshot, err := audittest.DecodeAuditData(entry.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, record.ID.String(), snapshot["AssistantID"])
}
