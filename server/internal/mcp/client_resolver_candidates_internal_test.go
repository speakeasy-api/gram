package mcp

import (
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors/aivendorstest"
	"github.com/speakeasy-api/gram/server/internal/aivendors/cimdcandidates"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	orgsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// TestResolveUserSessionClient_CandidateWriteFailureKeepsResolution: an open
// issuer resolving an uncovered client still gets its client row when the
// candidate write fails.
func TestResolveUserSessionClient_CandidateWriteFailureKeepsResolution(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := TestInfra.CloneTestDatabase(t, "mcp_cimd_candidate_write_failure")
	require.NoError(t, err)
	broken, err := TestInfra.CloneTestDatabase(t, "mcp_cimd_candidate_write_failure_closed")
	require.NoError(t, err)
	broken.Close()

	require.NoError(t, orgsrepo.New(conn).CreateOrganizationMetadata(ctx, orgsrepo.CreateOrganizationMetadataParams{ID: "org-cimd", Name: "CIMD Org", Slug: "cimd-org"}))
	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Project", Slug: "project", OrganizationID: "org-cimd"})
	require.NoError(t, err)
	issuer, err := usersessions_repo.New(conn).CreateUserSessionIssuer(ctx, usersessions_repo.CreateUserSessionIssuerParams{
		ProjectID:               project.ID,
		OrganizationID:          conv.ToPGText("org-cimd"),
		Slug:                    "issuer",
		AuthnChallengeMode:      "interactive",
		SessionDuration:         pgtype.Interval{Microseconds: 3_600_000_000, Days: 0, Months: 0, Valid: true},
		AuthorizationServerMode: pgtype.Text{},
		UseAuthenticationHost:   pgtype.Bool{},
	})
	require.NoError(t, err)

	var clientID string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"client_id":                  clientID,
			"client_name":                "Uncovered Client",
			"redirect_uris":              []string{"http://127.0.0.1:33418/callback"},
			"token_endpoint_auth_method": "none",
		}); err != nil {
			t.Errorf("encode cimd document: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	clientID = srv.URL + "/oauth/client.json"
	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil, guardian.WithTLSRootCAs(roots))
	require.NoError(t, err)

	serverURL, err := url.Parse("https://gram.example.test")
	require.NoError(t, err)
	logger := testenv.NewLogger(t)
	svc := &Service{
		aiCatalog:            aivendorstest.NewStore(),
		logger:               logger,
		db:                   conn,
		serverURL:            serverURL,
		cimdResolver:         cimd.NewResolver(policy, testenv.NewMeterProvider(t), logger),
		cimdAdmissionMetrics: nil,
		cimdCandidates:       cimdcandidates.NewRecorder(broken),
	}
	endpoint := &ResolvedMcpEndpoint{
		AudienceURN: "toolset:test", CIMDAdmissionModeRaw: conv.ToPGText(string(admission.ModeOpen)), CustomDomainID: uuid.NullUUID{},
		IsPublic: true, McpServerID: uuid.NullUUID{}, OrganizationID: "org-cimd", ProjectID: project.ID, RouteBase: mcproute.MCP, Slug: "test",
		ToolsetID: uuid.NullUUID{}, UpstreamResource: "", UserSessionIssuerID: issuer.ID,
	}

	row, err := svc.resolveUserSessionClient(ctx, logger, endpoint, svc.aiCatalog.Snapshot(), clientID, resolveClientCIMD)
	require.NoError(t, err)
	require.Equal(t, clientID, row.ClientID)
	require.Equal(t, "Uncovered Client", row.ClientName)

	candidates, err := testrepo.New(conn).ListCimdClientCandidatesFixture(ctx)
	require.NoError(t, err)
	require.Empty(t, candidates)
}
