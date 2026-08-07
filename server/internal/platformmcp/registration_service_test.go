package platformmcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRegistrationServiceRegistersReviewedCandidateWithServerComputedHash(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Name: "Project", Slug: "project"}
	registrationID := uuid.New()
	store := &recordingRegistrationStore{
		project: project,
		begin: OperationReceipt{
			ID: uuid.New(),
		},
		converged: OperationReceipt{
			ID:             uuid.New(),
			RegistrationID: uuid.NullUUID{UUID: registrationID, Valid: true},
			Status:         receiptStatusPending,
		},
		completed: OperationReceipt{
			ID:             uuid.New(),
			RegistrationID: uuid.NullUUID{UUID: registrationID, Valid: true},
			Status:         receiptStatusSucceeded,
		},
	}
	service := NewRegistrationService(
		testCatalog{details: CatalogDetails{CatalogCandidate: CatalogCandidate{ProviderKey: "provider", CatalogRef: "reviewed/mcp", SetupIntent: "authorize"}, Transport: "streamable-http", remoteURL: "https://provider.test/mcp"}},
		&testRegistrationGate{enabled: true},
		store,
	)
	service.now = func() time.Time { return time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC) }

	result, err := service.RegisterCatalogMCP(t.Context(), registrationServicePrincipal(), RegisterCatalogMCPInput{
		ProjectSlug:    project.Slug,
		ProviderKey:    "provider",
		CatalogRef:     "reviewed/mcp",
		IdempotencyKey: "request-key",
	})

	require.NoError(t, err)
	require.Equal(t, project, result.Project)
	require.Equal(t, "provider", result.ProviderKey)
	require.Equal(t, "reviewed/mcp", result.CatalogRef)
	require.Equal(t, "authorize", result.SetupIntent)
	require.Equal(t, registrationID.String(), result.Registration)
	require.Equal(t, "73d3fd5eb2797fecdc9e976e1567f48587eb4920f384eb0fca7d4e5fbe99f29b", store.request.InputHash)
	require.NotEqual(t, catalogRegistrationInputHash(project.Slug, "catalog", "other-provider", "other/reference"), store.request.InputHash)
	require.Equal(t, "catalog", store.request.SourceKind)
	require.Equal(t, 1, store.beginCalls)
	require.Equal(t, 1, store.convergeCalls)
	require.Equal(t, 1, store.completeCalls)
}

func TestRegistrationServiceRejectsUnreviewedCandidateAfterMutationGate(t *testing.T) {
	t.Parallel()

	catalog := testCatalog{err: ErrCatalogRejected}
	gate := testRegistrationGate{enabled: true}
	store := &recordingRegistrationStore{}
	service := NewRegistrationService(catalog, &gate, store)

	_, err := service.RegisterCatalogMCP(t.Context(), registrationServicePrincipal(), RegisterCatalogMCPInput{
		ProjectSlug:    "project",
		ProviderKey:    "unreviewed",
		CatalogRef:     "unreviewed/mcp",
		IdempotencyKey: "request-key",
	})

	require.ErrorIs(t, err, ErrCatalogRejected)
	require.Equal(t, 1, gate.calls)
	require.Zero(t, store.resolveCalls)
}

func TestRegistrationServiceFailsClosedWhenRegistrationGateIsDisabled(t *testing.T) {
	t.Parallel()

	gate := testRegistrationGate{}
	store := &recordingRegistrationStore{}
	service := NewRegistrationService(
		testCatalog{details: CatalogDetails{CatalogCandidate: CatalogCandidate{ProviderKey: "provider", CatalogRef: "reviewed/mcp", SetupIntent: "authorize"}, Transport: "streamable-http"}},
		&gate,
		store,
	)

	_, err := service.RegisterCatalogMCP(t.Context(), registrationServicePrincipal(), RegisterCatalogMCPInput{
		ProjectSlug:    "project",
		ProviderKey:    "provider",
		CatalogRef:     "reviewed/mcp",
		IdempotencyKey: "request-key",
	})

	require.ErrorIs(t, err, ErrRegistrationUnavailable)
	require.Equal(t, 1, gate.calls)
	require.Zero(t, store.resolveCalls)
}

func TestRegistrationServiceIssuesSetupHandoffOnlyWhenRegistrationGateIsEnabled(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	store := &recordingRegistrationStore{project: project}
	gate := &testRegistrationGate{enabled: true}
	service := NewRegistrationService(
		testCatalog{details: CatalogDetails{CatalogCandidate: CatalogCandidate{ProviderKey: "provider", CatalogRef: "reviewed/mcp", SetupIntent: "setup"}, Transport: "streamable-http"}},
		gate,
		store,
	)

	_, err := service.IssueSetupHandoff(t.Context(), registrationServicePrincipal(), IssueSetupHandoffInput{
		ProjectSlug:    project.Slug,
		RegistrationID: uuid.NewString(),
		ProviderKey:    "provider",
		CatalogRef:     "reviewed/mcp",
	})

	require.NoError(t, err)
	require.Equal(t, 1, gate.calls)
	require.Equal(t, 1, store.resolveCalls)

	gate.enabled = false
	_, err = service.IssueSetupHandoff(t.Context(), registrationServicePrincipal(), IssueSetupHandoffInput{
		ProjectSlug:    project.Slug,
		RegistrationID: uuid.NewString(),
		ProviderKey:    "provider",
		CatalogRef:     "reviewed/mcp",
	})
	require.ErrorIs(t, err, ErrRegistrationUnavailable)
	require.Equal(t, 2, gate.calls)
	require.Equal(t, 1, store.resolveCalls)
}

func TestRegistrationServiceRejectsInvalidSetupHandoffInputs(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	store := &recordingRegistrationStore{project: project}
	gate := &testRegistrationGate{enabled: true}
	service := NewRegistrationService(
		testCatalog{details: CatalogDetails{CatalogCandidate: CatalogCandidate{ProviderKey: "provider", CatalogRef: "reviewed/mcp", SetupIntent: "setup"}, Transport: "streamable-http"}},
		gate,
		store,
	)

	_, err := service.IssueSetupHandoff(t.Context(), registrationServicePrincipal(), IssueSetupHandoffInput{
		ProjectSlug: "project", RegistrationID: uuid.NewString(), ProviderKey: "unreviewed", CatalogRef: "unreviewed/mcp",
	})
	require.ErrorIs(t, err, ErrCatalogRejected)
	require.Zero(t, store.resolveCalls)

	_, err = service.IssueSetupHandoff(t.Context(), registrationServicePrincipal(), IssueSetupHandoffInput{
		ProjectSlug: "project", RegistrationID: "not-a-uuid", ProviderKey: "provider", CatalogRef: "reviewed/mcp",
	})
	require.ErrorIs(t, err, ErrSetupHandoffInvalid)
	require.Equal(t, 1, store.resolveCalls)
}

func TestRegistrationServiceDoesNotReconvergeSucceededReplay(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	registrationID := uuid.New()
	store := &recordingRegistrationStore{
		project: project,
		begin: OperationReceipt{
			ID:             uuid.New(),
			RegistrationID: uuid.NullUUID{UUID: registrationID, Valid: true},
			Status:         receiptStatusSucceeded,
			Replayed:       true,
		},
	}
	service := NewRegistrationService(
		testCatalog{details: CatalogDetails{CatalogCandidate: CatalogCandidate{ProviderKey: "provider", CatalogRef: "reviewed/mcp", SetupIntent: "authorize"}, Transport: "streamable-http"}},
		&testRegistrationGate{enabled: true},
		store,
	)

	result, err := service.RegisterCatalogMCP(t.Context(), registrationServicePrincipal(), RegisterCatalogMCPInput{
		ProjectSlug:    project.Slug,
		ProviderKey:    "provider",
		CatalogRef:     "reviewed/mcp",
		IdempotencyKey: "request-key",
	})

	require.NoError(t, err)
	require.Equal(t, registrationID.String(), result.Registration)
	require.Zero(t, store.convergeCalls)
}

type testCatalog struct {
	details CatalogDetails
	err     error
}

func (c testCatalog) Search(context.Context, string) ([]CatalogCandidate, error) {
	return nil, c.err
}

func (c testCatalog) Inspect(context.Context, string, string) (CatalogDetails, error) {
	return c.details, c.err
}

type testRegistrationGate struct {
	enabled        bool
	err            error
	calls          int
	organizationID string
	projectSlug    string
}

func (g *testRegistrationGate) Enabled(_ context.Context, organizationID, projectSlug string) (bool, error) {
	g.calls++
	g.organizationID = organizationID
	g.projectSlug = projectSlug
	return g.enabled, g.err
}

type recordingRegistrationStore struct {
	project       ResolvedProject
	begin         OperationReceipt
	converged     OperationReceipt
	completed     OperationReceipt
	err           error
	request       CatalogRegistrationRequest
	resolveCalls  int
	beginCalls    int
	convergeCalls int
	completeCalls int
	handoffCalls  int
}

func (s *recordingRegistrationStore) ResolveProject(context.Context, string, string) (ResolvedProject, error) {
	s.resolveCalls++
	return s.project, s.err
}

func (s *recordingRegistrationStore) BeginReceipt(_ context.Context, _ Principal, _ ResolvedProject, request CatalogRegistrationRequest, _ time.Time) (OperationReceipt, error) {
	s.beginCalls++
	s.request = request
	return s.begin, s.err
}

func (s *recordingRegistrationStore) ConvergeRegistration(_ context.Context, _ Principal, _ ResolvedProject, request CatalogRegistrationRequest, _ OperationReceipt) (OperationReceipt, error) {
	s.convergeCalls++
	s.request = request
	return s.converged, s.err
}

func (s *recordingRegistrationStore) CompleteRegistration(_ context.Context, _ Principal, _ ResolvedProject, request CatalogRegistrationRequest, _ OperationReceipt, _ string) (OperationReceipt, error) {
	s.completeCalls++
	s.request = request
	return s.completed, s.err
}

func (s *recordingRegistrationStore) IssueSetupHandoff(_ context.Context, _ Principal, binding SetupHandoffBinding, _ time.Time) (IssuedSetupHandoff, error) {
	s.handoffCalls++
	return IssuedSetupHandoff{SetupHandoff: SetupHandoff{ProjectID: binding.ProjectID, RegistrationID: binding.RegistrationID, ProviderKey: binding.ProviderKey, Intent: binding.Intent}}, s.err
}

func registrationServicePrincipal() Principal {
	return Principal{UserID: "user", OrganizationID: "organization", ConnectionID: uuid.NewString(), Generation: uuid.NewString(), ClientID: "client"}
}

func TestRegistrationServiceReturnsCatalogInspectionErrors(t *testing.T) {
	t.Parallel()

	upstreamErr := errors.New("catalog unavailable")
	service := NewRegistrationService(testCatalog{err: upstreamErr}, &testRegistrationGate{enabled: true}, &recordingRegistrationStore{})

	_, err := service.RegisterCatalogMCP(t.Context(), registrationServicePrincipal(), RegisterCatalogMCPInput{
		ProjectSlug: "project", ProviderKey: "provider", CatalogRef: "reviewed/mcp", IdempotencyKey: "request-key",
	})

	require.ErrorIs(t, err, upstreamErr)
}
