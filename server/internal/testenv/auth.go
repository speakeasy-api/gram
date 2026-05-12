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
	"github.com/workos/workos-go/v6/pkg/usermanagement"
	"go.opentelemetry.io/otel/trace"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/pylon"
	userRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// NewTestManager creates a sessions.Manager backed by a mock WorkOS httptest.Server.
// It also creates an identity.Resolver internally and wires it into the session
// manager as the UserResolver, matching the production wiring in start.go.
func NewTestManager(t *testing.T, logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, redisClient *redis.Client, suffix cache.Suffix, billingRepo billing.Repository) *sessions.Manager {
	t.Helper()

	cfg := mockidp.NewConfig()
	srv := httptest.NewServer(mockidp.Handler(cfg))
	t.Cleanup(srv.Close)

	// Point a real WorkOS SDK client at the mock server.
	umClient := usermanagement.NewClient("test-api-key")
	umClient.Endpoint = srv.URL
	umClient.HTTPClient = srv.Client()

	fakePylon, err := pylon.NewPylon(logger, "")
	require.NoError(t, err)

	fakePosthog := posthog.New(context.Background(), logger, "test-posthog-key", "test-posthog-host", "")

	resolver := identity.NewResolver(
		logger,
		tracerProvider,
		cache.NewRedisCacheAdapter(redisClient),
		srv.URL,
		"test-client-id",
		umClient,
		nil, // no WorkOS client in tests — fallback won't fire
		orgRepo.New(db),
		userRepo.New(db),
		fakePylon,
		fakePosthog,
	)

	return sessions.NewManager(
		logger,
		tracerProvider,
		db,
		redisClient,
		suffix,
		umClient,
		billingRepo,
		resolver,
	)
}

// InitAuthContext creates a fully authenticated context by inserting test data
// directly into the database. It creates a user, organization, org-user
// relationship, session, and project, then authenticates the session.
func InitAuthContext(t *testing.T, ctx context.Context, conn *pgxpool.Pool, sessionManager *sessions.Manager) context.Context {
	t.Helper()

	// Insert user directly into DB (instead of IDP code exchange).
	usersQueries := userRepo.New(conn)
	user, err := usersQueries.UpsertUser(ctx, userRepo.UpsertUserParams{
		ID:          mockidp.MockUserID,
		Email:       mockidp.MockUserEmail,
		DisplayName: "Dev User",
		PhotoUrl:    conv.PtrToPGText(nil),
		Admin:       false,
	})
	require.NoError(t, err)
	userID := user.ID

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

	// Mint our own session ID and store
	sessionID := uuid.New().String()
	session := sessions.Session{
		SessionID:            sessionID,
		UserID:               userID,
		ActiveOrganizationID: mockidp.MockOrgID,
		WorkOSSessionID:      "",
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
