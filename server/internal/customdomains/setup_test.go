package customdomains_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
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
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type stubTemporalRun struct {
	err   error
	onGet func(context.Context) error
}

func (s stubTemporalRun) Get(ctx context.Context, valuePtr any) error {
	if s.err != nil {
		return s.err
	}
	if s.onGet != nil {
		return s.onGet(ctx)
	}
	return nil
}
func (s stubTemporalRun) GetWithOptions(ctx context.Context, valuePtr any, options client.WorkflowRunGetOptions) error {
	return s.Get(ctx, valuePtr)
}
func (stubTemporalRun) GetID() string    { return "workflow" }
func (stubTemporalRun) GetRunID() string { return "run" }

type stubTemporalClient struct {
	registrationCalls int
	terminationCalls  int
	deletionCalls     int
	updateCalls       int
	reconcileCalls    int
	healthCheckCalls  int
	lastDomain        string
	lastOrganization  string
	lastHealthCheckID uuid.UUID
	lastReconcileID   uuid.UUID
	lastRegisteredID  uuid.UUID
	reconcileStartErr error
	reconcileErr      error
	reconcile         func(context.Context, uuid.UUID) error
}

func (s *stubTemporalClient) GetWorkflowInfo(ctx context.Context, orgID string, domain string, customDomainID uuid.UUID) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
	return nil, nil
}

func (s *stubTemporalClient) ExecuteCustomDomainRegistration(ctx context.Context, orgID string, domain string, customDomainID uuid.UUID, createdBy urn.Principal, createdByName *string, _ k8s.ProvisionerKind, _ []string) (client.WorkflowRun, error) {
	s.registrationCalls++
	s.lastDomain = domain
	s.lastRegisteredID = customDomainID
	return stubTemporalRun{err: nil, onGet: nil}, nil
}

func (s *stubTemporalClient) TerminateCustomDomainRegistration(ctx context.Context, orgID string, domain string, customDomainID uuid.UUID, reason string) error {
	s.terminationCalls++
	return nil
}

func (s *stubTemporalClient) ExecuteCustomDomainDeletion(ctx context.Context, orgID, domain, ingressName, certSecretName string, _ k8s.ProvisionerKind) (client.WorkflowRun, error) {
	s.deletionCalls++
	s.lastDomain = domain
	return stubTemporalRun{err: nil, onGet: nil}, nil
}

func (s *stubTemporalClient) ExecuteCustomDomainUpdate(ctx context.Context, orgID, domain string, _ k8s.ProvisionerKind, _ []string) (client.WorkflowRun, error) {
	s.updateCalls++
	s.lastDomain = domain
	return stubTemporalRun{err: nil, onGet: nil}, nil
}

func (s *stubTemporalClient) ExecuteCustomDomainReconcile(ctx context.Context, customDomainID uuid.UUID) (client.WorkflowRun, error) {
	s.reconcileCalls++
	s.lastReconcileID = customDomainID
	if s.reconcileStartErr != nil {
		return nil, s.reconcileStartErr
	}
	return stubTemporalRun{
		err: s.reconcileErr,
		onGet: func(ctx context.Context) error {
			if s.reconcile == nil {
				return nil
			}
			return s.reconcile(ctx, customDomainID)
		},
	}, nil
}

func (s *stubTemporalClient) ExecuteCustomDomainHealthCheck(ctx context.Context, organizationID string, customDomainID uuid.UUID) (client.WorkflowRun, error) {
	s.healthCheckCalls++
	s.lastOrganization = organizationID
	s.lastHealthCheckID = customDomainID
	return stubTemporalRun{err: nil, onGet: nil}, nil
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
	conn, err := infra.CloneTestDatabase(t, "service_testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billingClient)
	ctx = authztest.InitAuthContext(t, ctx, conn, sessionManager)

	temporal := &stubTemporalClient{}
	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	auditLogger := audit.NewLogger()
	svc := customdomains.NewService(logger, tracerProvider, conn, sessionManager, temporal, authzEngine, auditLogger, "cname.example.net.", nil)

	return ctx, &serviceTestInstance{service: svc, conn: conn, sessionManager: sessionManager, temporal: temporal, repo: cdrepo.New(conn)}
}
