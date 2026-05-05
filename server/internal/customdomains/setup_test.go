package customdomains_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	cdrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type stubTemporalRun struct{}

func (stubTemporalRun) Get(ctx context.Context, valuePtr any) error { return nil }
func (stubTemporalRun) GetWithOptions(ctx context.Context, valuePtr any, options client.WorkflowRunGetOptions) error {
	return nil
}
func (stubTemporalRun) GetID() string    { return "workflow" }
func (stubTemporalRun) GetRunID() string { return "run" }

type stubTemporalClient struct {
	registrationCalls int
	deletionCalls     int
	lastDomain        string
}

func (s *stubTemporalClient) GetWorkflowInfo(ctx context.Context, orgID string, domain string) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
	return nil, nil
}

func (s *stubTemporalClient) ExecuteCustomDomainRegistration(ctx context.Context, orgID string, domain string, createdBy urn.Principal, createdByName *string) (client.WorkflowRun, error) {
	s.registrationCalls++
	s.lastDomain = domain
	return stubTemporalRun{}, nil
}

func (s *stubTemporalClient) ExecuteCustomDomainDeletion(ctx context.Context, orgID, domain, ingressName, certSecretName string) (client.WorkflowRun, error) {
	s.deletionCalls++
	s.lastDomain = domain
	return stubTemporalRun{}, nil
}

type serviceTestInstance struct {
	service        *customdomains.Service
	conn           *pgxpool.Pool
	sessionManager *sessions.Manager
	temporal       *stubTemporalClient
	repo           *cdrepo.Queries
}

func newTestCustomDomainsService(t *testing.T) (context.Context, *serviceTestInstance) {
	t.Helper()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	conn, err := infra.CloneTestDatabase(t, "service_testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, guardianPolicy, conn, redisClient, cache.Suffix("gram-local"), billingClient)
	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	temporal := &stubTemporalClient{}
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	authzEngine := authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)
	auditLogger := audit.NewLogger()
	svc := customdomains.NewService(logger, tracerProvider, conn, sessionManager, temporal, authzEngine, auditLogger)

	return ctx, &serviceTestInstance{service: svc, conn: conn, sessionManager: sessionManager, temporal: temporal, repo: cdrepo.New(conn)}
}
