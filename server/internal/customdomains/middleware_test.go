package customdomains_test

import (
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var (
	infra *testenv.Environment
)

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
	conn        *pgxpool.Pool
	domainsRepo *repo.Queries
}

func newTestInstance(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	conn, err := infra.CloneTestDatabase(t, "middleware_testdb")
	require.NoError(t, err)

	domainsRepo := repo.New(conn)

	return ctx, &testInstance{
		conn:        conn,
		domainsRepo: domainsRepo,
	}
}

func TestCustomDomainsMiddleware(t *testing.T) {
	t.Parallel()
	ctx, instance := newTestInstance(t)
	logger := testenv.NewLogger(t)

	serverURL, err := url.Parse("https://api.speakeasyapi.dev")
	require.NoError(t, err)

	tests := []struct {
		name            string
		env             string
		host            string
		setupDomain     func(t *testing.T) *repo.CustomDomain
		expectedStatus  int
		expectedCtx     bool
		expectedSurface requestorigin.Surface
		expectedBaseURL string
		description     string
	}{
		{
			name:            "valid_verified_activated_domain",
			env:             "prod",
			host:            "custom.example.com",
			expectedStatus:  http.StatusOK,
			expectedCtx:     true,
			expectedSurface: requestorigin.SurfaceCustomDomain,
			expectedBaseURL: "https://custom.example.com",
			description:     "Should allow request through with domain context when domain is verified and activated",
			setupDomain: func(t *testing.T) *repo.CustomDomain {
				t.Helper()
				domain, err := instance.domainsRepo.CreateCustomDomain(ctx, repo.CreateCustomDomainParams{
					OrganizationID: "org-123",
					Domain:         "custom.example.com",
					IngressName:    pgtype.Text{String: "", Valid: false},
					CertSecretName: pgtype.Text{String: "", Valid: false},
					IpAllowlist:    []string{},
				})
				require.NoError(t, err)

				// Update to verified and activated
				updatedDomain, err := instance.domainsRepo.UpdateCustomDomain(ctx, repo.UpdateCustomDomainParams{
					ID:             domain.ID,
					Verified:       true,
					Activated:      true,
					IngressName:    pgtype.Text{String: "", Valid: false},
					CertSecretName: pgtype.Text{String: "", Valid: false},
				})
				require.NoError(t, err)
				return &updatedDomain
			},
		},
		{
			name:           "unverified_domain",
			env:            "prod",
			host:           "unverified.example.com",
			expectedStatus: http.StatusForbidden,
			expectedCtx:    false,
			description:    "Should reject request when domain exists but is not verified",
			setupDomain: func(t *testing.T) *repo.CustomDomain {
				t.Helper()
				domain, err := instance.domainsRepo.CreateCustomDomain(ctx, repo.CreateCustomDomainParams{
					OrganizationID: "org-456",
					Domain:         "unverified.example.com",
					IngressName:    pgtype.Text{String: "", Valid: false},
					CertSecretName: pgtype.Text{String: "", Valid: false},
					IpAllowlist:    []string{},
				})
				require.NoError(t, err)
				return &domain
			},
		},
		{
			name:           "verified_but_not_activated_domain",
			env:            "prod",
			host:           "notactivated.example.com",
			expectedStatus: http.StatusForbidden,
			expectedCtx:    false,
			description:    "Should reject request when domain is verified but not activated",
			setupDomain: func(t *testing.T) *repo.CustomDomain {
				t.Helper()
				domain, err := instance.domainsRepo.CreateCustomDomain(ctx, repo.CreateCustomDomainParams{
					OrganizationID: "org-789",
					Domain:         "notactivated.example.com",
					IngressName:    pgtype.Text{String: "", Valid: false},
					CertSecretName: pgtype.Text{String: "", Valid: false},
					IpAllowlist:    []string{},
				})
				require.NoError(t, err)

				// Update to verified but not activated
				updatedDomain, err := instance.domainsRepo.UpdateCustomDomain(ctx, repo.UpdateCustomDomainParams{
					ID:             domain.ID,
					Verified:       true,
					Activated:      false,
					IngressName:    pgtype.Text{String: "", Valid: false},
					CertSecretName: pgtype.Text{String: "", Valid: false},
				})
				require.NoError(t, err)
				return &updatedDomain
			},
		},
		{
			name:           "nonexistent_domain",
			env:            "prod",
			host:           "nonexistent.example.com",
			expectedStatus: http.StatusForbidden,
			expectedCtx:    false,
			description:    "Should reject request when domain does not exist in database",
			setupDomain:    nil,
		},
		{
			name:            "server_url_host_allowed",
			env:             "prod",
			host:            "api.speakeasyapi.dev",
			expectedStatus:  http.StatusOK,
			expectedCtx:     false,
			expectedSurface: requestorigin.SurfacePlatform,
			expectedBaseURL: "https://api.speakeasyapi.dev",
			description:     "Should allow request through without domain context when host matches server URL",
			setupDomain:     nil,
		},
		{
			name:            "server_url_host_case_and_port_are_canonical",
			env:             "prod",
			host:            "API.SPEAKEASYAPI.DEV:443",
			expectedStatus:  http.StatusOK,
			expectedCtx:     false,
			expectedSurface: requestorigin.SurfacePlatform,
			expectedBaseURL: "https://api.speakeasyapi.dev",
			description:     "Should classify a case-variant platform Host with a legal port",
			setupDomain:     nil,
		},
		{
			name:           "trailing_dot_is_rejected",
			env:            "prod",
			host:           "api.speakeasyapi.dev.",
			expectedStatus: http.StatusBadRequest,
			description:    "Should reject trailing-dot Host ambiguity",
		},
		{
			name:           "malformed_port_is_rejected",
			env:            "prod",
			host:           "api.speakeasyapi.dev:not-a-port",
			expectedStatus: http.StatusBadRequest,
			description:    "Should reject malformed Host ports",
		},
		{
			name:            "dev_environment_custom_domain",
			env:             "dev",
			host:            "custom-dev.example.com",
			expectedStatus:  http.StatusOK,
			expectedCtx:     true,
			expectedSurface: requestorigin.SurfaceCustomDomain,
			expectedBaseURL: "https://custom-dev.example.com",
			description:     "Should work in dev environment with custom domains",
			setupDomain: func(t *testing.T) *repo.CustomDomain {
				t.Helper()
				domain, err := instance.domainsRepo.CreateCustomDomain(ctx, repo.CreateCustomDomainParams{
					OrganizationID: "org-dev-123",
					Domain:         "custom-dev.example.com",
					IngressName:    pgtype.Text{String: "", Valid: false},
					CertSecretName: pgtype.Text{String: "", Valid: false},
					IpAllowlist:    []string{},
				})
				require.NoError(t, err)

				updatedDomain, err := instance.domainsRepo.UpdateCustomDomain(ctx, repo.UpdateCustomDomainParams{
					ID:             domain.ID,
					Verified:       true,
					Activated:      true,
					IngressName:    pgtype.Text{String: "", Valid: false},
					CertSecretName: pgtype.Text{String: "", Valid: false},
				})
				require.NoError(t, err)
				return &updatedDomain
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Setup domain if needed
			var expectedDomain *repo.CustomDomain
			if tt.setupDomain != nil {
				expectedDomain = tt.setupDomain(t)
			}

			// Create the middleware
			middlewareFunc := customdomains.Middleware(logger, instance.conn, tt.env, serverURL)

			// Create a test handler that captures context and responds
			var capturedCtx context.Context
			testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedCtx = r.Context()
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("success"))
			})

			// Wrap the test handler with middleware
			handler := middlewareFunc(testHandler)

			// Use a valid request URL and set Host separately so malformed Host
			// inputs reach the middleware instead of failing in httptest setup.
			req := httptest.NewRequest("GET", "https://example.com/test", nil)
			req.Host = tt.host
			req = req.WithContext(ctx)

			recorder := httptest.NewRecorder()

			// Execute the request
			handler.ServeHTTP(recorder, req)

			// Verify response status
			require.Equal(t, tt.expectedStatus, recorder.Code, tt.description)

			// Verify context if request was successful
			if tt.expectedStatus == http.StatusOK {
				domainCtx := customdomains.FromContext(capturedCtx)
				origin, ok := requestorigin.FromContext(capturedCtx)
				require.True(t, ok)
				require.Equal(t, tt.expectedSurface, origin.Surface)
				require.Equal(t, tt.expectedBaseURL, origin.BaseURL)

				if tt.expectedCtx {
					require.NotNil(t, domainCtx, "Expected domain context to be set")
					require.NotNil(t, expectedDomain, "Expected domain should be set for context validation")
					require.Equal(t, expectedDomain.OrganizationID, domainCtx.OrganizationID)
					require.Equal(t, expectedDomain.Domain, domainCtx.Domain)
					require.Equal(t, expectedDomain.ID, domainCtx.DomainID)
				} else {
					require.Nil(t, domainCtx, "Expected no domain context to be set")
				}
			}
		})
	}
}

func TestCustomDomainsMiddleware_DeletedDomain(t *testing.T) {
	t.Parallel()
	ctx, instance := newTestInstance(t)
	logger := testenv.NewLogger(t)

	serverURL, err := url.Parse("https://api.speakeasyapi.dev")
	require.NoError(t, err)

	_, err = instance.domainsRepo.CreateCustomDomain(ctx, repo.CreateCustomDomainParams{
		OrganizationID: "org-deleted",
		Domain:         "deleted.example.com",
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)

	// Delete the domain (soft delete)
	err = instance.domainsRepo.DeleteCustomDomain(ctx, "org-deleted")
	require.NoError(t, err)

	middlewareFunc := customdomains.Middleware(logger, instance.conn, "prod", serverURL)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for deleted domain")
	})

	handler := middlewareFunc(testHandler)

	req := httptest.NewRequest("GET", "https://deleted.example.com/test", nil)
	req.Host = "deleted.example.com"
	req = req.WithContext(ctx)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestCustomDomainsMiddleware_DatabaseErrors(t *testing.T) {
	t.Parallel()
	ctx, _ := newTestInstance(t)
	logger := testenv.NewLogger(t)

	serverURL, err := url.Parse("https://api.speakeasyapi.dev")
	require.NoError(t, err)

	// Create middleware with a closed connection to simulate database error
	closedConn, err := infra.CloneTestDatabase(t, "closed_testdb")
	require.NoError(t, err)
	closedConn.Close()

	middlewareFunc := customdomains.Middleware(logger, closedConn, "prod", serverURL)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called when database error occurs")
	})

	handler := middlewareFunc(testHandler)

	req := httptest.NewRequest("GET", "https://unknown.example.com/test", nil)
	req.Host = "unknown.example.com"
	req = req.WithContext(ctx)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	require.Contains(t, recorder.Body.String(), "domain check failed")
}

func TestCustomDomainsMiddleware_MissingHost(t *testing.T) {
	t.Parallel()
	ctx, instance := newTestInstance(t)
	logger := testenv.NewLogger(t)

	serverURL, err := url.Parse("https://api.speakeasyapi.dev")
	require.NoError(t, err)

	middlewareFunc := customdomains.Middleware(logger, instance.conn, "prod", serverURL)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called with empty host")
	})

	handler := middlewareFunc(testHandler)

	req := httptest.NewRequest("GET", "https://example.com/test", nil)
	req.Host = ""
	req = req.WithContext(ctx)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	require.Contains(t, recorder.Body.String(), "request host is invalid")
}
