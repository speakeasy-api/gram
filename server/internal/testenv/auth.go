package testenv

import (
	"context"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/pylon"
)

// NewTestManager creates a sessions.Manager backed by a mock OIDC httptest.Server.
func NewTestManager(t *testing.T, logger *slog.Logger, tracerProvider trace.TracerProvider, guardianPolicy *guardian.Policy, db *pgxpool.Pool, redisClient *redis.Client, suffix cache.Suffix, billingRepo billing.Repository) *sessions.Manager {
	t.Helper()

	cfg := mockidp.NewConfig()
	srv := httptest.NewServer(mockidp.Handler(cfg))
	t.Cleanup(srv.Close)

	fakePylon, err := pylon.NewPylon(logger, "")
	require.NoError(t, err)

	fakePosthog := posthog.New(context.Background(), logger, "test-posthog-key", "test-posthog-host", "")

	return sessions.NewManager(
		logger,
		tracerProvider,
		guardianPolicy,
		db,
		redisClient,
		suffix,
		srv.URL+"/oauth2",
		"test-client-id",
		fakePylon,
		fakePosthog,
		billingRepo,
	)
}

// InitAuthContext creates a fully authenticated context by exercising the real
// auth flow against the mock OIDC IDP. It exchanges a code for a token, fetches
// user info, upserts the user, creates org metadata, stores a UUID session, and
// authenticates. A test project is also created.
func InitAuthContext(t *testing.T, ctx context.Context, conn *pgxpool.Pool, sessionManager *sessions.Manager) context.Context {
	t.Helper()

	// Exchange a mock code for an access token (calls mock IDP /oauth2/token)
	accessToken, err := sessionManager.ExchangeCodeForTokens(ctx, "test-code", "http://localhost/callback")
	require.NoError(t, err)

	// Get user info from mock IDP (calls /oauth2/userinfo)
	idpUser, err := sessionManager.FetchUserInfoFromIDP(ctx, accessToken)
	require.NoError(t, err)

	// Upsert user in DB
	userID, err := sessionManager.UpsertUserFromIDP(ctx, idpUser)
	require.NoError(t, err)

	// Upsert organization metadata in the database
	orgQueries := orgRepo.New(conn)
	_, err = orgQueries.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          mockidp.MockOrgID,
		Name:        mockidp.MockOrgName,
		Slug:        mockidp.MockOrgSlug,
		WorkosID:    pgtype.Text{String: mockidp.MockOrgID, Valid: true},
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	// Create org-user relationship
	_, err = orgQueries.UpsertOrganizationUserRelationship(ctx, orgRepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: mockidp.MockOrgID,
		UserID:         userID,
	})
	require.NoError(t, err)

	// Build user info from DB to populate cache
	userInfo, err := sessionManager.BuildUserInfoFromDB(ctx, userID)
	require.NoError(t, err)
	require.NotEmpty(t, userInfo.Organizations, "mock IDP must return at least one organization")

	activeOrg := userInfo.Organizations[0]

	// Mint our own session ID and store
	sessionID := uuid.New().String()
	session := sessions.Session{
		SessionID:            sessionID,
		UserID:               userID,
		ActiveOrganizationID: activeOrg.ID,
	}
	err = sessionManager.StoreSession(ctx, session)
	require.NoError(t, err)

	// Authenticate using the session key
	ctx, err = sessionManager.Authenticate(ctx, sessionID)
	require.NoError(t, err)

	authctx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok, "auth context not found")

	// Generate unique project slug to avoid conflicts when tests run in parallel
	projectSlug := fmt.Sprintf("test-%s", uuid.New().String()[:8])

	p, err := projectsRepo.New(conn).CreateProject(ctx, projectsRepo.CreateProjectParams{
		Name:           projectSlug,
		Slug:           projectSlug,
		OrganizationID: authctx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	authctx.ProjectID = &p.ID
	authctx.ProjectSlug = &p.Slug

	return ctx
}
