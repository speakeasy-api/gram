package memory

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/threadsource"
)

const provenanceTestOrgID = "org-test"

// insertMemoryThreadFixture creates the project/assistant/chat/thread rows a
// Remember call resolves provenance from. sourceKind, correlationID and
// sourceRefJSON shape the thread's source surface.
func insertMemoryThreadFixture(t *testing.T, conn *pgxpool.Pool, sourceKind string, correlationID string, sourceRefJSON []byte) (projectID, assistantID, threadID uuid.UUID) {
	t.Helper()
	ctx := t.Context()

	proj, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Project",
		Slug:           "project",
		OrganizationID: provenanceTestOrgID,
	})
	require.NoError(t, err)

	assistant, err := assistantsrepo.New(conn).CreateAssistant(ctx, assistantsrepo.CreateAssistantParams{
		ProjectID:       proj.ID,
		OrganizationID:  provenanceTestOrgID,
		CreatedByUserID: pgtype.Text{String: "", Valid: false},
		Name:            "Assistant",
		Model:           "openai/gpt-4o-mini",
		Instructions:    "",
		WarmTtlSeconds:  300,
		MaxConcurrency:  1,
		Status:          "active",
	})
	require.NoError(t, err)

	chatID := uuid.New()
	err = assistantsrepo.New(conn).UpsertAssistantChat(ctx, assistantsrepo.UpsertAssistantChatParams{
		ChatID:         chatID,
		ProjectID:      proj.ID,
		OrganizationID: provenanceTestOrgID,
		UserID:         pgtype.Text{String: "", Valid: false},
		Title:          pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	threadID, err = assistantsrepo.New(conn).UpsertAssistantThread(ctx, assistantsrepo.UpsertAssistantThreadParams{
		AssistantID:   assistant.ID,
		ProjectID:     proj.ID,
		CorrelationID: correlationID,
		ChatID:        chatID,
		SourceKind:    sourceKind,
		SourceRefJson: sourceRefJSON,
	})
	require.NoError(t, err)

	return proj.ID, assistant.ID, threadID
}

// newMemoryServiceForDBTest wires a MemoryService against the cloned test
// database with a mocked embeddings client that returns a fixed vector.
func newMemoryServiceForDBTest(t *testing.T, conn *pgxpool.Pool) *MemoryService {
	t.Helper()

	vec := make([]float32, embeddingDimensions)
	vec[0] = 1

	client := &mockCompletionClient{}
	client.On("CreateEmbeddings", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return([][]float32{vec}, nil)

	return NewMemoryService(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		conn,
		client,
		nil,
	)
}

func authedAssistantContext(t *testing.T, projectID, assistantID, threadID uuid.UUID) context.Context {
	t.Helper()

	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID:  provenanceTestOrgID,
		UserID:                "owner-user",
		ExternalUserID:        "",
		APIKeyID:              "",
		SessionID:             nil,
		ProjectID:             &projectID,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           "",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
		IsAdmin:               false,
	})
	return contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{
		AssistantID: assistantID,
		ThreadID:    threadID,
	})
}

func TestRememberStampsSlackProvenanceAndRecallReturnsIt(t *testing.T) {
	t.Parallel()

	conn, err := memoryInfra.CloneTestDatabase(t, "memory_provenance_slack")
	require.NoError(t, err)

	projectID, assistantID, threadID := insertMemoryThreadFixture(t, conn, threadsource.KindSlack,
		"slack:T1:C123:171.001",
		[]byte(`{"team_id":"T1","channel_id":"C123","thread_id":"171.001","user_id":"U456"}`))

	svc := newMemoryServiceForDBTest(t, conn)
	ctx := authedAssistantContext(t, projectID, assistantID, threadID)

	remembered, err := svc.Remember(ctx, assistantID, projectID, provenanceTestOrgID, "user prefers tea", nil)
	require.NoError(t, err)
	require.False(t, remembered.Deduped)

	results, err := svc.Recall(ctx, assistantID, provenanceTestOrgID, "tea", 8, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)

	got := results[0]
	require.Equal(t, remembered.ID, got.ID)
	require.NotNil(t, got.SourceKind)
	require.Equal(t, threadsource.KindSlack, *got.SourceKind)
	require.NotNil(t, got.SourceUserID)
	require.Equal(t, "U456", *got.SourceUserID)
	require.NotNil(t, got.SourceCorrelationID)
	require.Equal(t, "slack:T1:C123:171.001", *got.SourceCorrelationID)
	require.NotNil(t, got.SourceTimestamp, "source_timestamp records the time of write")
	require.False(t, got.SourceTimestamp.IsZero())
}

func TestRememberStampsDashboardProvenance(t *testing.T) {
	t.Parallel()

	conn, err := memoryInfra.CloneTestDatabase(t, "memory_provenance_dashboard")
	require.NoError(t, err)

	projectID, assistantID, threadID := insertMemoryThreadFixture(t, conn, threadsource.KindDashboard,
		"0d9e0001-aaaa-bbbb-cccc-000000000001",
		[]byte(`{"user_id":"user_abc"}`))

	svc := newMemoryServiceForDBTest(t, conn)
	ctx := authedAssistantContext(t, projectID, assistantID, threadID)

	_, err = svc.Remember(ctx, assistantID, projectID, provenanceTestOrgID, "user works at acme", nil)
	require.NoError(t, err)

	results, err := svc.Recall(ctx, assistantID, provenanceTestOrgID, "acme", 8, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)

	got := results[0]
	require.NotNil(t, got.SourceKind)
	require.Equal(t, threadsource.KindDashboard, *got.SourceKind)
	require.NotNil(t, got.SourceUserID)
	require.Equal(t, "user_abc", *got.SourceUserID)
	require.NotNil(t, got.SourceCorrelationID, "correlation id is recorded for every source kind")
	require.Equal(t, "0d9e0001-aaaa-bbbb-cccc-000000000001", *got.SourceCorrelationID)
	require.NotNil(t, got.SourceTimestamp)
}

func TestRememberWithoutOriginThreadHasNoProvenance(t *testing.T) {
	t.Parallel()

	conn, err := memoryInfra.CloneTestDatabase(t, "memory_provenance_none")
	require.NoError(t, err)

	projectID, assistantID, _ := insertMemoryThreadFixture(t, conn, threadsource.KindSlack,
		"slack:T1:C123:171.001",
		[]byte(`{"team_id":"T1","channel_id":"C123","thread_id":"171.001","user_id":"U456"}`))

	svc := newMemoryServiceForDBTest(t, conn)
	// Principal without a thread id: provenance cannot be resolved and must
	// stay NULL rather than failing the write.
	ctx := authedAssistantContext(t, projectID, assistantID, uuid.Nil)

	_, err = svc.Remember(ctx, assistantID, projectID, provenanceTestOrgID, "fact with no origin", nil)
	require.NoError(t, err)

	results, err := svc.Recall(ctx, assistantID, provenanceTestOrgID, "fact", 8, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)

	got := results[0]
	require.Nil(t, got.SourceKind)
	require.Nil(t, got.SourceUserID)
	require.Nil(t, got.SourceCorrelationID)
	require.Nil(t, got.SourceTimestamp)
}
