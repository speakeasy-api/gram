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

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/aivendors/aivendorstest"
	"github.com/speakeasy-api/gram/server/internal/aivendors/cimdcandidates"
	"github.com/speakeasy-api/gram/server/internal/attr"
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
	newIssuer := func(slug string) uuid.UUID {
		issuer, err := usersessions_repo.New(conn).CreateUserSessionIssuer(ctx, usersessions_repo.CreateUserSessionIssuerParams{
			ProjectID:               project.ID,
			OrganizationID:          conv.ToPGText("org-cimd"),
			Slug:                    slug,
			AuthnChallengeMode:      "interactive",
			SessionDuration:         pgtype.Interval{Microseconds: 3_600_000_000, Days: 0, Months: 0, Valid: true},
			AuthorizationServerMode: pgtype.Text{},
			UseAuthenticationHost:   pgtype.Bool{},
		})
		require.NoError(t, err)
		return issuer.ID
	}

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
	brokenWrites := sdkmetric.NewManualReader()
	svc := &Service{
		aiCatalog:            aivendorstest.NewStore(),
		logger:               logger,
		db:                   conn,
		serverURL:            serverURL,
		cimdResolver:         cimd.NewResolver(policy, testenv.NewMeterProvider(t), logger),
		cimdAdmissionMetrics: nil,
		cimdCandidates:       cimdcandidates.NewRecorder(broken, sdkmetric.NewMeterProvider(sdkmetric.WithReader(brokenWrites)), logger),
	}
	endpoint := func(issuerID uuid.UUID) *ResolvedMcpEndpoint {
		return &ResolvedMcpEndpoint{
			AudienceURN: "toolset:test", CIMDAdmissionModeRaw: conv.ToPGText(string(admission.ModeOpen)), CustomDomainID: uuid.NullUUID{},
			IsPublic: true, McpServerID: uuid.NullUUID{}, OrganizationID: "org-cimd", ProjectID: project.ID, RouteBase: mcproute.MCP, Slug: "test",
			ToolsetID: uuid.NullUUID{}, UpstreamResource: "", UserSessionIssuerID: issuerID,
		}
	}

	row, err := svc.resolveUserSessionClient(ctx, logger, endpoint(newIssuer("broken")), svc.aiCatalog.Snapshot(), clientID, resolveClientCIMD)
	require.NoError(t, err)
	require.Equal(t, clientID, row.ClientID)
	require.Equal(t, "Uncovered Client", row.ClientName)
	svc.cimdCandidates.Wait()

	var rm metricdata.ResourceMetrics
	require.NoError(t, brokenWrites.Collect(ctx, &rm))
	require.Len(t, rm.ScopeMetrics, 1, "the write was attempted")
	sum, ok := rm.ScopeMetrics[0].Metrics[0].Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.Len(t, sum.DataPoints, 1)
	outcome, _ := sum.DataPoints[0].Attributes.Value(attr.CIMDCandidateOutcomeKey)
	require.Equal(t, "failed", outcome.AsString())
	candidates, err := testrepo.New(conn).ListCimdClientCandidatesFixture(ctx)
	require.NoError(t, err)
	require.Empty(t, candidates)

	// Positive control: the same resolution with a working recorder records.
	svc.cimdCandidates = cimdcandidates.NewRecorder(conn, testenv.NewMeterProvider(t), logger)
	_, err = svc.resolveUserSessionClient(ctx, logger, endpoint(newIssuer("working")), svc.aiCatalog.Snapshot(), clientID, resolveClientCIMD)
	require.NoError(t, err)
	svc.cimdCandidates.Wait()
	candidates, err = testrepo.New(conn).ListCimdClientCandidatesFixture(ctx)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, clientID, candidates[0].Url)
}
