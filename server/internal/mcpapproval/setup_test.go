package mcpapproval_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: true})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
	}

	infra = res
	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

type testInstance struct {
	service        *mcpapproval.Service
	conn           *pgxpool.Pool
	repo           *repo.Queries
	sessionManager *sessions.Manager
	authContext    *contextvalues.AuthContext
	organizationID string
	projectID      uuid.UUID
}

func newTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billingClient)
	ctx = authztest.InitAuthContext(t, ctx, conn, sessionManager)

	authContext, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authContext)

	organizationID := "mcpapproval-org-" + uuid.NewString()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        organizationID,
		Slug:        organizationID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	projectID := createProject(t, ctx, conn, organizationID)
	authContext.ActiveOrganizationID = organizationID
	authContext.ProjectID = &projectID
	ctx = contextvalues.SetAuthContext(ctx, authContext)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	authzEngine := authz.NewEngine(logger, conn, chConn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())

	ti := &testInstance{
		service:        mcpapproval.NewService(logger, tracerProvider, conn, sessionManager, authzEngine),
		conn:           conn,
		repo:           repo.New(conn),
		sessionManager: sessionManager,
		authContext:    authContext,
		organizationID: organizationID,
		projectID:      projectID,
	}

	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeMCPApprovalDecide, projectID.String()))

	return ctx, ti
}

func createProject(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string) uuid.UUID {
	t.Helper()

	slug := "mcpapproval-" + uuid.NewString()[:8]
	project, err := projectrepo.New(conn).CreateProject(ctx, projectrepo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	return project.ID
}

// withProject rebinds the auth context onto a project, so a test can act as the
// same caller with a different set of grants.
func withProject(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, grants ...authz.Scope) context.Context {
	t.Helper()

	authContext := *ti.authContext
	authContext.ProjectID = &projectID
	scoped := contextvalues.SetAuthContext(ctx, &authContext)

	exact := make([]authz.Grant, len(grants))
	for i, scope := range grants {
		exact[i] = authz.NewGrant(scope, projectID.String())
	}

	return authztest.WithExactGrants(t, scoped, exact...)
}

// seededRequest describes an approval request to plant.
//
// The intake endpoint that writes these lands with the request-submission
// work, so until then the read and decide handlers are exercised against rows
// seeded through the repo.
type seededRequest struct {
	// targetKey deduplicates the request within its project. Empty picks a
	// unique one.
	targetKey string

	// status the request starts in. Empty means "requested".
	status string

	// evidence is the current gather, as a JSON object. Empty leaves the
	// column at its default.
	evidence string

	// version is the evidence shape version. Zero leaves the default.
	version int32
}

func seedRequest(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, seed seededRequest) uuid.UUID {
	t.Helper()

	if seed.targetKey == "" {
		seed.targetKey = "https://mcp.example.com/" + uuid.NewString()[:8]
	}
	if seed.status == "" {
		seed.status = "requested"
	}

	request, err := ti.repo.UpsertApprovalRequest(ctx, repo.UpsertApprovalRequestParams{
		OrganizationID: ti.organizationID,
		ProjectID:      projectID,
		TargetKind:     "server_url",
		TargetRaw:      seed.targetKey,
		TargetKey:      seed.targetKey,
		ArtifactRef:    conv.ToPGText("npm:@scope/pkg@1.2.3"),
		VersionPinned:  true,
		Status:         seed.status,
	})
	require.NoError(t, err)

	if seed.evidence != "" || seed.version != 0 {
		evidence := conv.Default(seed.evidence, "{}")
		seedEvidence(t, ctx, ti, projectID, request.ID, evidence, max(seed.version, 1))
	}

	return request.ID
}

// seedUnresolvedRequest plants a request identity resolution could not place,
// which is a legitimate outcome rather than an error state.
func seedUnresolvedRequest(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, raw string) uuid.UUID {
	t.Helper()

	request, err := ti.repo.UpsertApprovalRequest(ctx, repo.UpsertApprovalRequestParams{
		OrganizationID: ti.organizationID,
		ProjectID:      projectID,
		TargetKind:     "stdio_command",
		TargetRaw:      raw,
		TargetKey:      raw,
		ArtifactRef:    pgtype.Text{},
		VersionPinned:  false,
		Status:         "requested",
	})
	require.NoError(t, err)

	return request.ID
}

func seedEvidence(t *testing.T, ctx context.Context, ti *testInstance, projectID, requestID uuid.UUID, evidence string, version int32) {
	t.Helper()

	require.NoError(t, ti.repo.SetApprovalRequestEvidence(ctx, repo.SetApprovalRequestEvidenceParams{
		CurrentEvidence: []byte(evidence),
		EvidenceVersion: version,
		ID:              requestID,
		ProjectID:       projectID,
	}))
}

func seedRequester(t *testing.T, ctx context.Context, ti *testInstance, projectID, requestID uuid.UUID, userID, note string) {
	t.Helper()

	_, err := ti.repo.CreateApprovalRequestRequester(ctx, repo.CreateApprovalRequestRequesterParams{
		OrganizationID:       ti.organizationID,
		ProjectID:            projectID,
		McpApprovalRequestID: requestID,
		UserID:               userID,
		UserEmail:            conv.ToPGText(userID + "@example.test"),
		Note:                 conv.ToPGText(note),
	})
	require.NoError(t, err)
}

func decisionsFor(t *testing.T, ctx context.Context, ti *testInstance, projectID, requestID uuid.UUID) []repo.McpApprovalDecision {
	t.Helper()

	decisions, err := ti.repo.ListDecisionsForApprovalRequest(ctx, repo.ListDecisionsForApprovalRequestParams{
		McpApprovalRequestID: requestID,
		ProjectID:            projectID,
	})
	require.NoError(t, err)

	return decisions
}

func requestStatus(t *testing.T, ctx context.Context, ti *testInstance, projectID, requestID uuid.UUID) string {
	t.Helper()

	request, err := ti.repo.GetApprovalRequest(ctx, repo.GetApprovalRequestParams{ID: requestID, ProjectID: projectID})
	require.NoError(t, err)

	return request.Status
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}
