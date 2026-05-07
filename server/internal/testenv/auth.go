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
	"github.com/speakeasy-api/gram/server/internal/auth/speakeasyclient"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/pylon"
)

// NewTestManager creates a sessions.Manager backed by a mock IDP httptest.Server.
// This replaces the old NewUnsafeManager pattern - tests now exercise the real
// auth code path (Speakeasy IDP HTTP calls) against a local mock.
func NewTestManager(t *testing.T, logger *slog.Logger, tracerProvider trace.TracerProvider, guardianPolicy *guardian.Policy, db *pgxpool.Pool, redisClient *redis.Client, suffix cache.Suffix, billingRepo billing.Repository) *sessions.Manager {
	t.Helper()

	cfg := mockidp.NewConfig()
	srv := httptest.NewServer(mockidp.Handler(cfg))
	t.Cleanup(srv.Close)

	fakePylon, err := pylon.NewPylon(logger, "")
	require.NoError(t, err)

	fakePosthog := posthog.New(context.Background(), logger, "test-posthog-key", "test-posthog-host", "")

	speakeasyIDPClient := speakeasyclient.NewClient(
		logger,
		tracerProvider,
		guardianPolicy,
		srv.URL,
		mockidp.MockSecretKey,
		db,
		nil,
		fakePosthog,
	)

	return sessions.NewManager(
		logger,
		tracerProvider,
		guardianPolicy,
		db,
		redisClient,
		suffix,
		srv.URL,
		mockidp.MockSecretKey,
		fakePylon,
		fakePosthog,
		billingRepo,
		nil,
		speakeasyIDPClient,
	)
}

// InitAuthContext creates a fully authenticated context by exercising the real
// auth flow against the mock IDP. It exchanges a code for a token, fetches user
// info (upserting the user in the database), creates org metadata, stores the
// session, and authenticates. A test project is also created.
func InitAuthContext(t *testing.T, ctx context.Context, conn *pgxpool.Pool, sessionManager *sessions.Manager) context.Context {
	t.Helper()

	// Exchange a mock code for an ID token (calls mock IDP /exchange)
	idToken, err := sessionManager.ExchangeTokenFromSpeakeasy(ctx, "test-code")
	require.NoError(t, err)

	// Get user info from mock IDP (calls /validate, upserts user in DB)
	userInfo, err := sessionManager.GetUserInfoFromSpeakeasy(ctx, idToken)
	require.NoError(t, err)
	require.NotEmpty(t, userInfo.Organizations, "mock IDP must return at least one organization")

	activeOrg := userInfo.Organizations[0]

	// Upsert organization metadata in the database
	orgQueries := orgRepo.New(conn)
	_, err = orgQueries.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          activeOrg.ID,
		Name:        activeOrg.Name,
		Slug:        activeOrg.Slug,
		WorkosID:    conv.PtrToPGText(activeOrg.WorkosID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	// Store session in cache
	session := sessions.Session{
		SessionID:            idToken,
		UserID:               userInfo.UserID,
		ActiveOrganizationID: activeOrg.ID,
	}
	err = sessionManager.StoreSession(ctx, session)
	require.NoError(t, err)

	// Authenticate using the session key
	ctx, err = sessionManager.Authenticate(ctx, idToken)
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
