package mcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	authrepo "github.com/speakeasy-api/gram/server/internal/auth/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestMCPPrincipalCredentialReadmissionErrors(t *testing.T) {
	t.Parallel()
	for _, caller := range []string{"hosted", "proxy"} {
		for _, tt := range []struct {
			name         string
			admissionErr error
			status       int
		}{
			{"revoked", fmt.Errorf("credential revoked: %w", oops.C(oops.CodeUnauthorized)), http.StatusUnauthorized},
			{"unexpected", errors.New("admission backend unavailable"), http.StatusInternalServerError},
			{"unexpected shareable", oops.E(oops.CodeUnexpected, nil, "admission backend unavailable"), http.StatusInternalServerError},
		} {
			t.Run(caller+"/"+tt.name, func(t *testing.T) {
				t.Parallel()
				var logs bytes.Buffer
				logger := slog.New(slog.NewJSONHandler(&logs, nil))
				calls := 0
				engine := authz.NewEngine(logger, nil, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient(), authz.EngineOpts{
					AdmitPrincipalCredential: func(context.Context, *pgxpool.Pool) (authz.PrincipalCredentialAdmission, error) {
						calls++
						if calls == 1 {
							return authz.PrincipalCredentialAdmission{OwnerUserID: "test-owner"}, nil
						}
						return authz.PrincipalCredentialAdmission{}, tt.admissionErr
					},
				})
				ctx := contextvalues.WithPrincipalCredentialAuthorization(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org-test"}, urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString()), contextvalues.PrincipalCredential{})
				// Identity authentication already admitted this credential. The MCP caller
				// must still re-admit it and preserve a subsequent live denial as a 401.
				ctx, err := engine.PrepareContext(ctx)
				require.NoError(t, err)
				projectID := uuid.New()
				serverURL, err := url.Parse("https://example.com")
				require.NoError(t, err)
				service := &Service{logger: logger, authz: engine, serverURL: serverURL,
					authRepo: authrepo.New(&admissionProjectsDB{projectID: projectID}),
				}
				switch caller {
				case "hosted":
					req := httptest.NewRequest(http.MethodPost, "/mcp/test", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)).WithContext(ctx)
					err = service.serveToolsetResolved(httptest.NewRecorder(), req, &toolsetsrepo.Toolset{ID: uuid.New(), ProjectID: projectID}, "test", "mcp", &hostedServing{callerGated: true}, nil, nil, nil, nil)
				case "proxy":
					_, err = service.authorizeProxyBackendAccess(ctx, logger, projectID, &mcpserversrepo.McpServer{ID: uuid.New(), Visibility: mcpservers.VisibilityPrivate})
				}
				require.Equal(t, 2, calls)
				require.ErrorIs(t, err, tt.admissionErr)
				var shareable *oops.ShareableError
				require.ErrorAs(t, err, &shareable)
				require.Equal(t, tt.status, shareable.HTTPStatus(ctx))
				if tt.status == http.StatusInternalServerError {
					require.Contains(t, logs.String(), `"level":"ERROR"`)
				} else {
					require.Empty(t, logs.String(), "expected denial must not be logged as an unexpected failure")
				}
			})
		}
	}
}

// The hosted caller checks organization project membership before admission.
// Supply that one result without requiring a database for error classification.
type admissionProjectsDB struct {
	authrepo.DBTX
	projectID uuid.UUID
}

func (db *admissionProjectsDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return &admissionProjectRows{projectID: db.projectID}, nil
}

type admissionProjectRows struct {
	pgx.Rows
	projectID uuid.UUID
	read      bool
}

func (r *admissionProjectRows) Next() bool {
	if r.read {
		return false
	}
	r.read = true
	return true
}
func (r *admissionProjectRows) Scan(dest ...any) error {
	id, ok := dest[0].(*uuid.UUID)
	if !ok {
		return fmt.Errorf("expected UUID destination, got %T", dest[0])
	}
	*id = r.projectID
	return nil
}
func (*admissionProjectRows) Close()     {}
func (*admissionProjectRows) Err() error { return nil }
