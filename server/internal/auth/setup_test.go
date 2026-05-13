package auth_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/usermanagement"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/pylon"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	userRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

var (
	infra *testenv.Environment
)

type noopCancelScheduler struct{}

func (noopCancelScheduler) ScheduleCancelAssistantsSubscription(ctx context.Context, subscriptionID string) error {
	return nil
}

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: true})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
		os.Exit(1)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
		os.Exit(1)
	}

	os.Exit(code)
}

type testInstance struct {
	service          *auth.Service
	conn             *pgxpool.Pool
	sessionManager   *sessions.Manager
	identityResolver *identity.Resolver
	mockAuthServer   *httptest.Server
	authConfigs      auth.AuthConfigurations
	nonceStore       cache.Cache
}

// MockUserInfo represents the user info used by the mock OIDC server
type MockUserInfo struct {
	UserID         string
	Email          string
	ExternalID     string // WorkOS external_id — simulates a backfilled user
	OrganizationID string // WorkOS org ID returned by AuthenticateWithCode
	Admin          bool
	Organizations  []MockOrganizationEntry
}

type MockOrganizationEntry struct {
	ID                 string
	Name               string
	Slug               string
	WorkosID           *string
	UserWorkspaceSlugs []string
}

// createMockWorkOSServer creates an httptest.Server that serves the WorkOS
// /user_management/authenticate endpoint. The WorkOS Go SDK sends requests here,
// so this mock allows the sessions.Manager's single code path to work in tests.
func createMockWorkOSServer(userInfo *MockUserInfo) *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /user_management/authenticate", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"access_token":    fmt.Sprintf("mock_access_token_%p", userInfo),
			"refresh_token":   "",
			"organization_id": userInfo.OrganizationID,
			"user": map[string]string{
				"id":                  userInfo.UserID,
				"first_name":          userInfo.Email,
				"last_name":           "",
				"email":               userInfo.Email,
				"profile_picture_url": "",
				"external_id":         userInfo.ExternalID,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	return httptest.NewServer(mux)
}

func newTestAuthService(t *testing.T, userInfo *MockUserInfo) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	conn, err := infra.CloneTestDatabase(t, "authtest")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	mockServer := createMockWorkOSServer(userInfo)
	t.Cleanup(mockServer.Close)

	umClient := usermanagement.NewClient("test-api-key")
	umClient.Endpoint = mockServer.URL
	umClient.HTTPClient = mockServer.Client()
	idpClient := identity.NewWorkOSAdapter(umClient)

	pylon, err := pylon.NewPylon(logger, "")
	require.NoError(t, err)

	posthog := posthog.New(ctx, logger, "test-posthog-key", "test-posthog-host", "")

	billingClient := billing.NewStubClient(logger, tracerProvider)

	resolver := identity.NewResolver(logger, tracerProvider, cache.NewRedisCacheAdapter(redisClient), mockServer.URL, "test-client-id", idpClient, nil, orgRepo.New(conn), userRepo.New(conn), pylon, posthog)
	sessionManager := sessions.NewManager(logger, testenv.NewTracerProvider(t), conn, redisClient, cache.Suffix("gram-test"), idpClient, billingClient, resolver)

	authConfigs := auth.AuthConfigurations{
		IDPBaseURL:        mockServer.URL,
		GramServerURL:     "http://localhost:8080",
		SignInRedirectURL: "http://localhost:3000/dashboard",
		Environment:       "test",
	}

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	nonceStore := cache.NewRedisCacheAdapter(redisClient)
	authzEngine := authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), cache.NoopCache)
	svc := auth.NewService(logger, tracerProvider, conn, sessionManager, resolver, authConfigs, authzEngine, billingClient, noopCancelScheduler{}, posthog, nonceStore)

	return ctx, newTestAuthServiceResult(t, svc, conn, sessionManager, resolver, mockServer, authConfigs, nonceStore)
}

func newTestAuthServiceWithAuthz(t *testing.T, userInfo *MockUserInfo) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	conn, err := infra.CloneTestDatabase(t, "authtest")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	mockServer := createMockWorkOSServer(userInfo)
	t.Cleanup(mockServer.Close)

	umClient := usermanagement.NewClient("test-api-key")
	umClient.Endpoint = mockServer.URL
	umClient.HTTPClient = mockServer.Client()
	idpClient := identity.NewWorkOSAdapter(umClient)

	pylon, err := pylon.NewPylon(logger, "")
	require.NoError(t, err)

	posthog := posthog.New(ctx, logger, "test-posthog-key", "test-posthog-host", "")

	billingClient := billing.NewStubClient(logger, tracerProvider)

	resolver := identity.NewResolver(logger, tracerProvider, cache.NewRedisCacheAdapter(redisClient), mockServer.URL, "test-client-id", idpClient, nil, orgRepo.New(conn), userRepo.New(conn), pylon, posthog)
	sessionManager := sessions.NewManager(logger, testenv.NewTracerProvider(t), conn, redisClient, cache.Suffix("gram-test"), idpClient, billingClient, resolver)

	authConfigs := auth.AuthConfigurations{
		IDPBaseURL:        mockServer.URL,
		GramServerURL:     "http://localhost:8080",
		SignInRedirectURL: "http://localhost:3000/dashboard",
		Environment:       "test",
	}

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	nonceStore := cache.NewRedisCacheAdapter(redisClient)
	authzEngine := authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), cache.NoopCache)
	svc := auth.NewService(logger, tracerProvider, conn, sessionManager, resolver, authConfigs, authzEngine, billingClient, noopCancelScheduler{}, posthog, nonceStore)

	return ctx, newTestAuthServiceResult(t, svc, conn, sessionManager, resolver, mockServer, authConfigs, nonceStore)
}

func newTestAuthServiceResult(_ *testing.T, svc *auth.Service, conn *pgxpool.Pool, sessionManager *sessions.Manager, resolver *identity.Resolver, mockServer *httptest.Server, authConfigs auth.AuthConfigurations, nonceStore cache.Cache) *testInstance {
	return &testInstance{
		service:          svc,
		conn:             conn,
		sessionManager:   sessionManager,
		identityResolver: resolver,
		mockAuthServer:   mockServer,
		authConfigs:      authConfigs,
		nonceStore:       nonceStore,
	}
}

// seedNonce stores a nonce in Redis so hand-crafted state params pass validation in tests.
func (ti *testInstance) seedNonce(ctx context.Context, t *testing.T, nonce string) {
	t.Helper()
	require.NoError(t, ti.nonceStore.Set(ctx, "auth:login_nonce:"+nonce, true, 10*time.Minute))
}

// stateWithNonce builds a base64-encoded state param with the given redirect and a seeded nonce.
func (ti *testInstance) stateWithNonce(ctx context.Context, t *testing.T, redirectURL string) string {
	t.Helper()
	nonce := fmt.Sprintf("test-nonce-%d", time.Now().UnixNano())
	ti.seedNonce(ctx, t, nonce)
	state := map[string]string{
		"final_destination_url": redirectURL,
		"nonce":                 nonce,
	}
	stateJSON, err := json.Marshal(state)
	require.NoError(t, err)
	return base64.RawURLEncoding.EncodeToString(stateJSON)
}

// callbackWithNonce creates a CallbackPayload with a valid nonce and calls Callback.
// Shorthand for the common pattern in e2e tests.
func (ti *testInstance) callbackWithNonce(ctx context.Context, t *testing.T) (*gen.CallbackResult, error) {
	t.Helper()
	stateParam := ti.stateWithNonce(ctx, t, "")
	res, err := ti.service.Callback(ctx, &gen.CallbackPayload{
		Code:  "mock_code",
		State: &stateParam,
	})
	if err != nil {
		return nil, fmt.Errorf("callback with nonce: %w", err)
	}
	return res, nil
}

// Helper function to create a default mock user info
func defaultMockUserInfo() *MockUserInfo {
	return &MockUserInfo{
		UserID: "test-user-123",
		Email:  "test@example.com",
		Admin:  false,
		Organizations: []MockOrganizationEntry{
			{
				ID:                 "org-123",
				Name:               "Test Organization",
				Slug:               "test-org",
				UserWorkspaceSlugs: []string{"workspace1", "workspace2"},
			},
		},
	}
}

// Helper function to create a speakeasy user mock info
func speakeasyMockUserInfo() *MockUserInfo {
	return &MockUserInfo{
		UserID: "speakeasy-user-123",
		Email:  "test@speakeasy.com",
		Admin:  false,
		Organizations: []MockOrganizationEntry{
			{
				ID:                 "speakeasy-team-123",
				Name:               "Speakeasy Team",
				Slug:               "speakeasy-team",
				UserWorkspaceSlugs: []string{"speakeasy-workspace"},
			},
			{
				ID:                 "other-org-123",
				Name:               "Other Organization",
				Slug:               "other-org",
				UserWorkspaceSlugs: []string{"other-workspace"},
			},
		},
	}
}

// Helper function to create an admin user mock info
func adminMockUserInfo() *MockUserInfo {
	return &MockUserInfo{
		UserID: "admin-user-123",
		Email:  "admin@speakeasyapi.dev",
		Admin:  true,
		Organizations: []MockOrganizationEntry{
			{
				ID:                 "admin-org-123",
				Name:               "Admin Organization",
				Slug:               "admin-org",
				UserWorkspaceSlugs: []string{"admin-workspace"},
			},
		},
	}
}

// createTestUser creates a user record in the database for testing
func (ti *testInstance) createTestUser(ctx context.Context, userInfo *MockUserInfo) error {
	usersQueries := userRepo.New(ti.conn)
	_, err := usersQueries.UpsertUser(ctx, userRepo.UpsertUserParams{
		ID:          userInfo.UserID,
		Email:       userInfo.Email,
		DisplayName: userInfo.Email,
		PhotoUrl:    conv.PtrToPGText(nil),
		Admin:       userInfo.Admin,
	})
	if err != nil {
		return fmt.Errorf("failed to upsert user: %w", err)
	}
	return nil
}

// createTestOrganization creates an organization record and org-user membership in the database for testing.
func (ti *testInstance) createTestOrganization(ctx context.Context, org MockOrganizationEntry, userID string) error {
	orgQueries := orgRepo.New(ti.conn)
	_, err := orgQueries.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:       org.ID,
		Name:     org.Name,
		Slug:     org.Slug,
		WorkosID: conv.PtrToPGText(org.WorkosID),
	})
	if err != nil {
		return fmt.Errorf("failed to upsert organization metadata: %w", err)
	}
	if userID != "" {
		_, err = orgQueries.UpsertOrganizationUserRelationship(ctx, orgRepo.UpsertOrganizationUserRelationshipParams{
			OrganizationID: org.ID,
			UserID:         userID,
		})
		if err != nil {
			return fmt.Errorf("failed to upsert organization user relationship: %w", err)
		}
	}
	return nil
}

// createTestProject creates a project record in the database for testing and returns its ID.
func (ti *testInstance) createTestProject(ctx context.Context, orgID, name, slug string) (projectsRepo.Project, error) {
	q := projectsRepo.New(ti.conn)
	project, err := q.CreateProject(ctx, projectsRepo.CreateProjectParams{
		OrganizationID: orgID,
		Name:           name,
		Slug:           slug,
	})
	if err != nil {
		return project, fmt.Errorf("failed to create project: %w", err)
	}
	return project, nil
}
