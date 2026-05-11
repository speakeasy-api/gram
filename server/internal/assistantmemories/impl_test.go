package assistantmemories

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/assistant_memories"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/memory"
	"github.com/speakeasy-api/gram/server/internal/memory/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

type fakeMemory struct {
	listFn   func(ctx context.Context, projectID uuid.UUID, params memory.ListParams) (memory.ListResult, error)
	getFn    func(ctx context.Context, projectID, id uuid.UUID) (repo.GetAssistantMemoryByIDRow, error)
	deleteFn func(ctx context.Context, projectID, id uuid.UUID) error
}

func (f *fakeMemory) List(ctx context.Context, projectID uuid.UUID, params memory.ListParams) (memory.ListResult, error) {
	if f.listFn == nil {
		return memory.ListResult{Memories: nil}, nil
	}
	return f.listFn(ctx, projectID, params)
}

func (f *fakeMemory) Get(ctx context.Context, projectID, id uuid.UUID) (repo.GetAssistantMemoryByIDRow, error) {
	if f.getFn == nil {
		return repo.GetAssistantMemoryByIDRow{}, errors.New("get not configured")
	}
	return f.getFn(ctx, projectID, id)
}

func (f *fakeMemory) DeleteByID(ctx context.Context, projectID, id uuid.UUID) error {
	if f.deleteFn == nil {
		return nil
	}
	return f.deleteFn(ctx, projectID, id)
}

type fakeFeatures struct {
	enabled bool
	err     error
}

func (f fakeFeatures) IsFeatureEnabled(context.Context, string, productfeatures.Feature) (bool, error) {
	return f.enabled, f.err
}

type testHarness struct {
	svc       *Service
	mem       *fakeMemory
	features  *fakeFeatures
	projectID uuid.UUID
	orgID     string
}

func newTestHarness(t *testing.T) (*testHarness, context.Context) {
	t.Helper()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	mem := &fakeMemory{listFn: nil, getFn: nil, deleteFn: nil}
	features := &fakeFeatures{enabled: true, err: nil}

	authzEngine := authz.NewEngine(logger, nil, nil, authztest.RBACAlwaysDisabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), cache.NoopCache)

	svc := &Service{
		tracer:   tracerProvider.Tracer("test"),
		logger:   logger,
		auth:     nil,
		authz:    authzEngine,
		features: features,
		memory:   mem,
	}

	projectID := uuid.New()
	projectSlug := "project-test"
	orgID := "org-test"
	sessionID := "session-test"

	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID:  orgID,
		UserID:                "user-test",
		ExternalUserID:        "",
		APIKeyID:              "",
		SessionID:             &sessionID,
		ProjectID:             &projectID,
		OrganizationSlug:      orgID,
		Email:                 nil,
		AccountType:           "enterprise",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           &projectSlug,
		APIKeyScopes:          nil,
		IsAdmin:               false,
	})

	return &testHarness{
		svc:       svc,
		mem:       mem,
		features:  features,
		projectID: projectID,
		orgID:     orgID,
	}, ctx
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

func TestListAssistantMemories_MissingAuthContext(t *testing.T) {
	t.Parallel()

	h, _ := newTestHarness(t)

	_, err := h.svc.ListAssistantMemories(t.Context(), &gen.ListAssistantMemoriesPayload{
		AssistantID:      uuid.NewString(),
		Tags:             nil,
		IncludeDeleted:   false,
		Cursor:           nil,
		Limit:            50,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestListAssistantMemories_FeatureDisabled(t *testing.T) {
	t.Parallel()

	h, ctx := newTestHarness(t)
	h.features.enabled = false

	_, err := h.svc.ListAssistantMemories(ctx, &gen.ListAssistantMemoriesPayload{
		AssistantID:      uuid.NewString(),
		Tags:             nil,
		IncludeDeleted:   false,
		Cursor:           nil,
		Limit:            50,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestListAssistantMemories_RBACDenied(t *testing.T) {
	t.Parallel()

	h, ctx := newTestHarness(t)
	logger := testenv.NewLogger(t)
	h.svc.authz = authz.NewEngine(logger, nil, nil, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), cache.NoopCache)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := h.svc.ListAssistantMemories(ctx, &gen.ListAssistantMemoriesPayload{
		AssistantID:      uuid.NewString(),
		Tags:             nil,
		IncludeDeleted:   false,
		Cursor:           nil,
		Limit:            50,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestListAssistantMemories_InvalidAssistantID(t *testing.T) {
	t.Parallel()

	h, ctx := newTestHarness(t)

	_, err := h.svc.ListAssistantMemories(ctx, &gen.ListAssistantMemoriesPayload{
		AssistantID:      "not-a-uuid",
		Tags:             nil,
		IncludeDeleted:   false,
		Cursor:           nil,
		Limit:            50,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestListAssistantMemories_HappyPathMapsResultAndCursor(t *testing.T) {
	t.Parallel()

	h, ctx := newTestHarness(t)
	assistantID := uuid.New()
	memoryID := uuid.New()
	createdAt := time.Date(2026, 5, 5, 12, 30, 0, 0, time.UTC)

	h.mem.listFn = func(ctx context.Context, projectID uuid.UUID, params memory.ListParams) (memory.ListResult, error) {
		require.Equal(t, h.projectID, projectID)
		require.Equal(t, assistantID, params.AssistantID)
		require.Equal(t, int32(2), params.Limit)
		return memory.ListResult{
			Memories: []repo.ListAssistantMemoriesForAdminRow{
				makeRepoMemory(uuid.New(), assistantID, h.projectID, createdAt.Add(-time.Minute)),
				makeRepoMemory(memoryID, assistantID, h.projectID, createdAt),
			},
		}, nil
	}

	out, err := h.svc.ListAssistantMemories(ctx, &gen.ListAssistantMemoriesPayload{
		AssistantID:      assistantID.String(),
		Tags:             []string{"foo"},
		IncludeDeleted:   false,
		Cursor:           nil,
		Limit:            2,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, out.Memories, 2)
	require.Equal(t, memoryID.String(), out.Memories[1].ID)
	require.NotNil(t, out.NextCursor)

	cursorTime, cursorID, decodeErr := decodeListCursor(out.NextCursor)
	require.NoError(t, decodeErr)
	require.NotNil(t, cursorTime)
	require.NotNil(t, cursorID)
	require.True(t, cursorTime.Equal(createdAt))
	require.Equal(t, memoryID, *cursorID)
}

func TestListAssistantMemories_ShortPageOmitsCursor(t *testing.T) {
	t.Parallel()

	h, ctx := newTestHarness(t)
	assistantID := uuid.New()

	h.mem.listFn = func(ctx context.Context, projectID uuid.UUID, params memory.ListParams) (memory.ListResult, error) {
		return memory.ListResult{
			Memories: []repo.ListAssistantMemoriesForAdminRow{
				makeRepoMemory(uuid.New(), assistantID, h.projectID, time.Now().UTC()),
			},
		}, nil
	}

	out, err := h.svc.ListAssistantMemories(ctx, &gen.ListAssistantMemoriesPayload{
		AssistantID:      assistantID.String(),
		Tags:             nil,
		IncludeDeleted:   false,
		Cursor:           nil,
		Limit:            10,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, out.Memories, 1)
	require.Nil(t, out.NextCursor)
}

func TestListAssistantMemories_DecodesCursor(t *testing.T) {
	t.Parallel()

	h, ctx := newTestHarness(t)
	assistantID := uuid.New()
	cursorTime := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	cursorMemID := uuid.New()
	cursor := encodeListCursor(cursorTime, cursorMemID)

	called := false
	h.mem.listFn = func(ctx context.Context, projectID uuid.UUID, params memory.ListParams) (memory.ListResult, error) {
		called = true
		require.NotNil(t, params.CursorCreatedAt)
		require.True(t, params.CursorCreatedAt.Equal(cursorTime))
		require.NotNil(t, params.CursorID)
		require.Equal(t, cursorMemID, *params.CursorID)
		return memory.ListResult{Memories: nil}, nil
	}

	_, err := h.svc.ListAssistantMemories(ctx, &gen.ListAssistantMemoriesPayload{
		AssistantID:      assistantID.String(),
		Tags:             nil,
		IncludeDeleted:   false,
		Cursor:           &cursor,
		Limit:            10,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.True(t, called)
}

func TestListAssistantMemories_BadCursor(t *testing.T) {
	t.Parallel()

	h, ctx := newTestHarness(t)
	bad := "not-base64!!"

	_, err := h.svc.ListAssistantMemories(ctx, &gen.ListAssistantMemoriesPayload{
		AssistantID:      uuid.NewString(),
		Tags:             nil,
		IncludeDeleted:   false,
		Cursor:           &bad,
		Limit:            10,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestGetAssistantMemory_MissingAuthContext(t *testing.T) {
	t.Parallel()

	h, _ := newTestHarness(t)
	_, err := h.svc.GetAssistantMemory(t.Context(), &gen.GetAssistantMemoryPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestGetAssistantMemory_FeatureDisabled(t *testing.T) {
	t.Parallel()

	h, ctx := newTestHarness(t)
	h.features.enabled = false
	_, err := h.svc.GetAssistantMemory(ctx, &gen.GetAssistantMemoryPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestGetAssistantMemory_RBACDenied(t *testing.T) {
	t.Parallel()

	h, ctx := newTestHarness(t)
	logger := testenv.NewLogger(t)
	h.svc.authz = authz.NewEngine(logger, nil, nil, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), cache.NoopCache)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := h.svc.GetAssistantMemory(ctx, &gen.GetAssistantMemoryPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestGetAssistantMemory_HappyPath(t *testing.T) {
	t.Parallel()

	h, ctx := newTestHarness(t)
	assistantID := uuid.New()
	memoryID := uuid.New()
	createdAt := time.Date(2026, 5, 5, 9, 0, 0, 0, time.UTC)

	h.mem.getFn = func(ctx context.Context, projectID, id uuid.UUID) (repo.GetAssistantMemoryByIDRow, error) {
		require.Equal(t, h.projectID, projectID)
		require.Equal(t, memoryID, id)
		return repo.GetAssistantMemoryByIDRow(makeRepoMemory(memoryID, assistantID, projectID, createdAt)), nil
	}

	view, err := h.svc.GetAssistantMemory(ctx, &gen.GetAssistantMemoryPayload{
		ID:               memoryID.String(),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, memoryID.String(), view.ID)
	require.Equal(t, assistantID.String(), view.AssistantID)
	require.Equal(t, "hello world", view.Content)
	require.Equal(t, []string{"alpha", "beta"}, view.Tags)
}

func TestGetAssistantMemory_PropagatesNotFound(t *testing.T) {
	t.Parallel()

	h, ctx := newTestHarness(t)
	notFound := oops.E(oops.CodeNotFound, nil, "memory not found")
	h.mem.getFn = func(ctx context.Context, projectID, id uuid.UUID) (repo.GetAssistantMemoryByIDRow, error) {
		return repo.GetAssistantMemoryByIDRow{}, notFound
	}

	_, err := h.svc.GetAssistantMemory(ctx, &gen.GetAssistantMemoryPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDeleteAssistantMemory_MissingAuthContext(t *testing.T) {
	t.Parallel()

	h, _ := newTestHarness(t)
	err := h.svc.DeleteAssistantMemory(t.Context(), &gen.DeleteAssistantMemoryPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestDeleteAssistantMemory_FeatureDisabled(t *testing.T) {
	t.Parallel()

	h, ctx := newTestHarness(t)
	h.features.enabled = false
	err := h.svc.DeleteAssistantMemory(ctx, &gen.DeleteAssistantMemoryPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestDeleteAssistantMemory_RBACDenied(t *testing.T) {
	t.Parallel()

	h, ctx := newTestHarness(t)
	logger := testenv.NewLogger(t)
	h.svc.authz = authz.NewEngine(logger, nil, nil, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), cache.NoopCache)

	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeProjectRead, h.projectID.String()))

	err := h.svc.DeleteAssistantMemory(ctx, &gen.DeleteAssistantMemoryPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestDeleteAssistantMemory_HappyPathDelegatesToMemoryService(t *testing.T) {
	t.Parallel()

	h, ctx := newTestHarness(t)
	memoryID := uuid.New()

	called := false
	h.mem.deleteFn = func(ctx context.Context, projectID, id uuid.UUID) error {
		called = true
		require.Equal(t, h.projectID, projectID)
		require.Equal(t, memoryID, id)
		return nil
	}

	err := h.svc.DeleteAssistantMemory(ctx, &gen.DeleteAssistantMemoryPayload{
		ID:               memoryID.String(),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.True(t, called)
}

func TestDeleteAssistantMemory_PropagatesError(t *testing.T) {
	t.Parallel()

	h, ctx := newTestHarness(t)
	notFound := oops.E(oops.CodeNotFound, nil, "memory not found")
	h.mem.deleteFn = func(ctx context.Context, projectID, id uuid.UUID) error {
		return notFound
	}

	err := h.svc.DeleteAssistantMemory(ctx, &gen.DeleteAssistantMemoryPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestMemoryToView_NullableFields(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	assistantID := uuid.New()
	projectID := uuid.New()
	supersededID := uuid.New()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	rec := repo.GetAssistantMemoryByIDRow{
		ID:             id,
		AssistantID:    uuid.NullUUID{UUID: assistantID, Valid: true},
		ProjectID:      uuid.NullUUID{UUID: projectID, Valid: true},
		OrganizationID: "org",
		Content:        "content",
		SupersedesID:   uuid.NullUUID{UUID: supersededID, Valid: true},
		SupersededAt:   pgtype.Timestamptz{Time: now, Valid: true, InfinityModifier: pgtype.Finite},
		ValidAt:        pgtype.Timestamptz{Time: now, Valid: true, InfinityModifier: pgtype.Finite},
		Tags:           nil,
		OriginThreadID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OriginChatID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		CreatedAt:      pgtype.Timestamptz{Time: now, Valid: true, InfinityModifier: pgtype.Finite},
		UpdatedAt:      pgtype.Timestamptz{Time: now, Valid: true, InfinityModifier: pgtype.Finite},
		LastAccess:     pgtype.Timestamptz{Time: now, Valid: true, InfinityModifier: pgtype.Finite},
		DeletedAt:      pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite},
	}

	v := memoryToView(rec)
	require.Equal(t, id.String(), v.ID)
	require.Equal(t, assistantID.String(), v.AssistantID)
	require.Equal(t, []string{}, v.Tags)
	require.NotNil(t, v.SupersededAt)
	require.NotNil(t, v.SupersedesID)
	require.Equal(t, supersededID.String(), *v.SupersedesID)
	require.Nil(t, v.DeletedAt)
}

func makeRepoMemory(id, assistantID, projectID uuid.UUID, createdAt time.Time) repo.ListAssistantMemoriesForAdminRow {
	return repo.ListAssistantMemoriesForAdminRow{
		ID:             id,
		AssistantID:    uuid.NullUUID{UUID: assistantID, Valid: true},
		ProjectID:      uuid.NullUUID{UUID: projectID, Valid: true},
		OrganizationID: "org-test",
		Content:        "hello world",
		SupersedesID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		SupersededAt:   pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite},
		ValidAt:        pgtype.Timestamptz{Time: createdAt, Valid: true, InfinityModifier: pgtype.Finite},
		Tags:           []string{"alpha", "beta"},
		OriginThreadID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OriginChatID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		CreatedAt:      pgtype.Timestamptz{Time: createdAt, Valid: true, InfinityModifier: pgtype.Finite},
		UpdatedAt:      pgtype.Timestamptz{Time: createdAt, Valid: true, InfinityModifier: pgtype.Finite},
		LastAccess:     pgtype.Timestamptz{Time: createdAt, Valid: true, InfinityModifier: pgtype.Finite},
		DeletedAt:      pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite},
	}
}
