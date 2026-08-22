package platformmcp

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/ratelimit"
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
	service := newRegistrationService(
		testCatalog{details: CatalogDetails{CatalogCandidate: CatalogCandidate{Name: "Reviewed MCP", ProviderKey: "provider", CatalogRef: "reviewed/mcp", SetupIntent: "authorize"}, Transport: "streamable-http", remoteURL: "https://provider.test/mcp"}},
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
	require.Equal(t, "Reviewed MCP", store.configuration.displayName)
	require.Equal(t, 1, store.beginCalls)
	require.Equal(t, 1, store.convergeCalls)
	require.Equal(t, 1, store.completeCalls)
}

func TestRegistrationServiceRejectsSecretConfigurationBeforePersistence(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	store := &recordingRegistrationStore{project: project}
	service := newRegistrationService(testCatalog{details: CatalogDetails{
		CatalogCandidate:  CatalogCandidate{ProviderKey: "provider", CatalogRef: "reviewed/mcp", SetupIntent: "setup"},
		Transport:         "streamable-http",
		remoteURLTemplate: "https://provider.test/mcp",
		Configuration: []CatalogConfigurationField{{
			Key: "header:x-api-key", Kind: "header", Name: "X-API-Key", Secret: true,
		}},
	}}, &testRegistrationGate{enabled: true}, store)

	_, err := service.RegisterCatalogMCP(t.Context(), registrationServicePrincipal(), RegisterCatalogMCPInput{
		ProjectSlug: project.Slug, ProviderKey: "provider", CatalogRef: "reviewed/mcp", IdempotencyKey: "request-key",
		NonSecretConfig: CatalogConfigurationValues{"header:x-api-key": "not-permitted"},
	})

	require.ErrorIs(t, err, ErrCatalogConfigurationRejected)
	require.Zero(t, store.beginCalls)
}

func TestRegistrationServiceRejectsUnreviewedCandidateAfterMutationGate(t *testing.T) {
	t.Parallel()

	catalog := testCatalog{err: ErrCatalogRejected}
	gate := testRegistrationGate{enabled: true}
	store := &recordingRegistrationStore{}
	service := newRegistrationService(catalog, &gate, store)

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

func TestRegistrationServiceRejectsIneligibleTargetBeforeReceipt(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "legacy-project"}
	store := &recordingRegistrationStore{project: project, eligibilitySet: true}
	service := newRegistrationService(
		testCatalog{details: CatalogDetails{CatalogCandidate: CatalogCandidate{ProviderKey: "provider", CatalogRef: "reviewed/mcp", SetupIntent: "authorize"}, Transport: "streamable-http"}},
		&testRegistrationGate{enabled: true},
		store,
	)

	_, err := service.RegisterCatalogMCP(t.Context(), registrationServicePrincipal(), RegisterCatalogMCPInput{
		ProjectSlug: project.Slug, ProviderKey: "provider", CatalogRef: "reviewed/mcp", IdempotencyKey: "request-key",
	})

	require.ErrorIs(t, err, ErrTargetIneligible)
	require.Zero(t, store.beginCalls)
}

func TestRegistrationServiceFailsClosedWhenRegistrationGateIsDisabled(t *testing.T) {
	t.Parallel()

	gate := testRegistrationGate{}
	store := &recordingRegistrationStore{}
	service := newRegistrationService(
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
	service := newRegistrationService(
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
	service := newRegistrationService(
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
	require.Zero(t, store.resolveCalls)
}

func TestRegistrationServiceReturnsPersistedSameOriginDashboardSetupURL(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	registrationID := uuid.New()
	providerKey := "browser-catalog-registry-7e966bfa-4df0-43ef-a54c-9c8c2e5f1b0d"
	store := &recordingRegistrationStore{
		project:   project,
		candidate: CatalogCandidate{ProviderKey: providerKey, CatalogRef: "reviewed/mcp"},
		dashboard: RegistrationDashboardSetup{OrganizationSlug: "organization", MCPServerRoute: "server route"},
	}
	service := newRegistrationService(
		testCatalog{details: CatalogDetails{CatalogCandidate: CatalogCandidate{ProviderKey: providerKey, CatalogRef: "reviewed/mcp", SetupIntent: "dashboard_source_settings"}, Transport: "streamable-http"}},
		&testRegistrationGate{enabled: true},
		store,
	)
	service.WithDashboardURL(&url.URL{Scheme: "https", Host: "localhost:5173"})

	setupURL, err := service.DashboardSetupURL(t.Context(), registrationServicePrincipal(), IssueSetupHandoffInput{
		ProjectSlug: project.Slug, RegistrationID: registrationID.String(), ProviderKey: providerKey, CatalogRef: "reviewed/mcp",
	})

	require.NoError(t, err)
	require.Equal(t, "https://localhost:5173/organization/projects/project/mcp/x/server%20route/settings#authentication", setupURL)
	require.Equal(t, 1, store.resolveCalls)
}

func TestRegistrationServiceReturnsRemoteRegistrationAuthenticationSettingsURL(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	registrationID := uuid.New()
	remoteURL := "https://remote.example.test/mcp"
	store := &recordingRegistrationStore{
		project:   project,
		candidate: CatalogCandidate{ProviderKey: remoteURLCatalogProvider, CatalogRef: remoteURL},
		dashboard: RegistrationDashboardSetup{OrganizationSlug: "organization", MCPServerRoute: "server route"},
	}
	// The zero-value catalogue proves the reviewed-catalogue Inspect path is
	// never consulted: had it been, the empty candidate would be rejected.
	service := newRegistrationService(testCatalog{}, &testRegistrationGate{enabled: true}, store)
	service.WithDashboardURL(&url.URL{Scheme: "https", Host: "localhost:5173"})

	setupURL, err := service.DashboardSetupURL(t.Context(), registrationServicePrincipal(), IssueSetupHandoffInput{
		ProjectSlug: project.Slug, RegistrationID: registrationID.String(), ProviderKey: remoteURLCatalogProvider, CatalogRef: remoteURL,
	})

	require.NoError(t, err)
	require.Equal(t, "https://localhost:5173/organization/projects/project/mcp/x/server%20route/settings#authentication", setupURL)
}

func TestRegistrationServiceRejectsRemoteSetupURLForMismatchedPersistedIdentity(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	store := &recordingRegistrationStore{
		project:   project,
		candidate: CatalogCandidate{ProviderKey: remoteURLCatalogProvider, CatalogRef: "https://remote.example.test/mcp"},
		dashboard: RegistrationDashboardSetup{OrganizationSlug: "organization", MCPServerRoute: "server route"},
	}
	service := newRegistrationService(testCatalog{}, &testRegistrationGate{enabled: true}, store)
	service.WithDashboardURL(&url.URL{Scheme: "https", Host: "localhost:5173"})

	_, err := service.DashboardSetupURL(t.Context(), registrationServicePrincipal(), IssueSetupHandoffInput{
		ProjectSlug: project.Slug, RegistrationID: uuid.NewString(), ProviderKey: remoteURLCatalogProvider, CatalogRef: "https://different.example.test/mcp",
	})

	require.ErrorIs(t, err, ErrCatalogRejected)
}

func TestRegistrationServiceBuildsInspectAuthorizationURLOnlyAfterAttachment(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	registrationID := uuid.New()
	store := &recordingRegistrationStore{
		project:   project,
		dashboard: RegistrationDashboardSetup{OrganizationSlug: "organization", MCPServerRoute: "server route"},
	}
	service := newRegistrationService(testCatalog{}, &testRegistrationGate{enabled: true}, store)
	service.WithDashboardURL(&url.URL{Scheme: "https", Host: "localhost:5173"})

	authorizationURL, err := service.DashboardAuthorizationURL(t.Context(), registrationServicePrincipal(), project.Slug, registrationID.String())

	require.NoError(t, err)
	require.Equal(t, "https://localhost:5173/organization/projects/project/mcp/x/server%20route/inspect", authorizationURL)
}

func TestRegistrationServiceStopsBeforeAttachmentWhenSetupBudgetDenies(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	store := &recordingRegistrationStore{project: project}
	gate := &testRegistrationGate{enabled: true}
	attachment := &recordingIdentityProviderAttachment{}
	denied := OperationBudget{
		Connection:   &recordingOperationLimiter{result: ratelimit.Result{Allowed: false}},
		Organization: allowOperationLimiter{},
	}
	service := newRegistrationService(testCatalog{}, gate, store).
		WithIdentityProviderAttachment(attachment).
		WithOperationBudgets(OperationBudgets{
			Catalog:      allowBudget(),
			Registration: allowBudget(),
			Handoff:      allowBudget(),
			SetupStart:   denied,
			Repair:       allowBudget(),
		})

	_, err := service.AttachDefaultIdentityProvider(t.Context(), registrationServicePrincipal(), project.Slug, uuid.NewString())

	require.ErrorIs(t, err, ErrOperationRateLimited)
	require.Zero(t, gate.calls)
	require.Zero(t, store.resolveCalls)
	require.Zero(t, attachment.calls)
}

func TestRegistrationServiceAttachesIdentityProviderForRemoteURLRegistration(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	store := &recordingRegistrationStore{
		project:   project,
		candidate: CatalogCandidate{ProviderKey: remoteURLCatalogProvider, CatalogRef: "https://remote.example.test/mcp"},
	}
	attachment := &recordingIdentityProviderAttachment{result: CatalogIdentityProviderAttachmentResult{Attached: true, ProviderURL: "https://identity.example"}}
	service := newRegistrationService(testCatalog{}, &testRegistrationGate{enabled: true}, store).WithIdentityProviderAttachment(attachment)

	result, err := service.AttachDefaultIdentityProvider(t.Context(), registrationServicePrincipal(), project.Slug, uuid.NewString())

	require.NoError(t, err, "a remote URL registration takes the live-discovery attachment path")
	require.True(t, result.Attached)
	require.Equal(t, "https://identity.example", result.ProviderURL)
	require.Equal(t, 1, attachment.calls)
}

func TestRegistrationServiceRejectsIdentityProviderAttachmentForFixtureProviders(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	store := &recordingRegistrationStore{
		project:   project,
		candidate: CatalogCandidate{ProviderKey: "fixture", CatalogRef: "fixture/mcp"},
	}
	attachment := &recordingIdentityProviderAttachment{}
	service := newRegistrationService(testCatalog{}, &testRegistrationGate{enabled: true}, store).WithIdentityProviderAttachment(attachment)

	_, err := service.AttachDefaultIdentityProvider(t.Context(), registrationServicePrincipal(), project.Slug, uuid.NewString())

	require.ErrorIs(t, err, ErrIdentityProviderAttachmentUnsupported)
	require.Zero(t, attachment.calls)
}

func TestRegistrationServiceStopsBeforePersistenceWhenBudgetDenies(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	store := &recordingRegistrationStore{project: project}
	denied := OperationBudget{
		Connection:   &recordingOperationLimiter{result: ratelimit.Result{Allowed: false}},
		Organization: allowOperationLimiter{},
	}
	service := NewRegistrationService(
		testCatalog{details: CatalogDetails{CatalogCandidate: CatalogCandidate{ProviderKey: "provider", CatalogRef: "reviewed/mcp", SetupIntent: "authorize"}, Transport: "streamable-http"}},
		&testRegistrationGate{enabled: true},
		store,
	).WithOperationBudgets(OperationBudgets{Registration: denied, Catalog: denied, Handoff: denied, SetupStart: denied, Repair: denied})

	_, err := service.RegisterCatalogMCP(t.Context(), registrationServicePrincipal(), RegisterCatalogMCPInput{
		ProjectSlug: project.Slug, ProviderKey: "provider", CatalogRef: "reviewed/mcp", IdempotencyKey: "request-key",
	})

	require.ErrorIs(t, err, ErrOperationRateLimited)
	require.Zero(t, store.resolveCalls)
	require.Zero(t, store.beginCalls)
}

func TestRegistrationServiceReturnsActiveRegistrationCapConflict(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	store := &recordingRegistrationStore{
		project: project,
		begin: OperationReceipt{
			ID:         uuid.New(),
			Status:     receiptStatusSucceeded,
			ResultCode: receiptResultActiveCap,
			Replayed:   true,
		},
	}
	service := newRegistrationService(
		testCatalog{details: CatalogDetails{CatalogCandidate: CatalogCandidate{ProviderKey: "provider", CatalogRef: "reviewed/mcp", SetupIntent: "authorize"}, Transport: "streamable-http", remoteURL: "https://provider.test/mcp"}},
		&testRegistrationGate{enabled: true},
		store,
	)

	_, err := service.RegisterCatalogMCP(t.Context(), registrationServicePrincipal(), RegisterCatalogMCPInput{
		ProjectSlug: project.Slug, ProviderKey: "provider", CatalogRef: "reviewed/mcp", IdempotencyKey: "request-key",
	})

	require.ErrorIs(t, err, ErrRegistrationCap)
	require.Zero(t, store.convergeCalls)
	require.Zero(t, store.completeCalls)
}

func TestRegistrationServiceReplayReturnsPersistedSecretSetupState(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	registrationID := uuid.New()
	pendingSecretFields := []CatalogConfigurationField{{Key: "header:x-api-key", Kind: "header", Name: "X-API-Key", Required: true, Secret: true}}
	store := &recordingRegistrationStore{
		project:             project,
		pendingSecretFields: pendingSecretFields,
		begin: OperationReceipt{
			ID:             uuid.New(),
			RegistrationID: uuid.NullUUID{UUID: registrationID, Valid: true},
			Status:         receiptStatusSucceeded,
			Replayed:       true,
		},
	}
	service := newRegistrationService(
		testCatalog{details: CatalogDetails{
			CatalogCandidate: CatalogCandidate{ProviderKey: "provider", CatalogRef: "reviewed/mcp", SetupIntent: "dashboard_source_settings"},
			Transport:        "streamable-http",
			remoteURL:        "https://provider.test/mcp",
			Configuration: []CatalogConfigurationField{{
				Key: "header:x-api-key", Kind: "header", Name: "X-API-Key", Required: true, Secret: true,
			}},
		}},
		&testRegistrationGate{enabled: true},
		store,
	)

	result, err := service.RegisterCatalogMCP(t.Context(), registrationServicePrincipal(), RegisterCatalogMCPInput{
		ProjectSlug: project.Slug, ProviderKey: "provider", CatalogRef: "reviewed/mcp", IdempotencyKey: "request-key",
	})

	require.NoError(t, err)
	require.Equal(t, pendingSecretFields, result.SecretFieldsPending)
	require.Equal(t, 1, store.pendingSecretFieldsCalls)
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
	service := newRegistrationService(
		testCatalog{details: CatalogDetails{CatalogCandidate: CatalogCandidate{ProviderKey: "provider", CatalogRef: "reviewed/mcp", SetupIntent: "authorize"}, Transport: "streamable-http", remoteURL: "https://provider.test/mcp"}},
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
	project                  ResolvedProject
	begin                    OperationReceipt
	converged                OperationReceipt
	completed                OperationReceipt
	err                      error
	eligible                 bool
	eligibilitySet           bool
	eligibilityErr           error
	request                  CatalogRegistrationRequest
	configuration            resolvedCatalogConfiguration
	resolveCalls             int
	beginCalls               int
	convergeCalls            int
	completeCalls            int
	completeRemoteCalls      int
	remoteURL                string
	remoteDisplayName        string
	handoffCalls             int
	candidate                CatalogCandidate
	dashboard                RegistrationDashboardSetup
	pendingSecretFields      []CatalogConfigurationField
	pendingSecretFieldsCalls int
}

func (s *recordingRegistrationStore) ResolveProject(context.Context, string, string) (ResolvedProject, error) {
	s.resolveCalls++
	return s.project, s.err
}

func (s *recordingRegistrationStore) EligibleCatalogRegistrationTarget(_ context.Context, _ string, _ ResolvedProject) (bool, error) {
	if !s.eligibilitySet {
		return true, s.eligibilityErr
	}
	return s.eligible, s.eligibilityErr
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

func (s *recordingRegistrationStore) CompleteRegistration(_ context.Context, _ Principal, _ ResolvedProject, request CatalogRegistrationRequest, _ OperationReceipt, configuration resolvedCatalogConfiguration) (OperationReceipt, error) {
	s.completeCalls++
	s.request = request
	s.configuration = configuration
	return s.completed, s.err
}

func (s *recordingRegistrationStore) CompleteRegistrationWithRemoteURL(_ context.Context, _ Principal, _ ResolvedProject, request CatalogRegistrationRequest, _ OperationReceipt, remoteURL string, displayName ...string) (OperationReceipt, error) {
	s.completeRemoteCalls++
	s.request = request
	s.remoteURL = remoteURL
	if len(displayName) > 0 {
		s.remoteDisplayName = displayName[0]
	}
	return s.completed, s.err
}

func (s *recordingRegistrationStore) ResolveRegistrationPendingSecretFields(_ context.Context, _ Principal, _ ResolvedProject, _ uuid.UUID, _ []CatalogConfigurationField) ([]CatalogConfigurationField, error) {
	s.pendingSecretFieldsCalls++
	return append([]CatalogConfigurationField(nil), s.pendingSecretFields...), s.err
}

func (s *recordingRegistrationStore) ResolveRegistrationCatalogIdentity(_ context.Context, _ Principal, _ ResolvedProject, _ uuid.UUID) (CatalogCandidate, error) {
	if s.candidate.ProviderKey != "" {
		return s.candidate, s.err
	}
	return CatalogCandidate{ProviderKey: "provider", CatalogRef: "reviewed/mcp"}, s.err
}

func (s *recordingRegistrationStore) ResolveRegistrationDashboardSetup(_ context.Context, _ Principal, _ ResolvedProject, _ uuid.UUID) (RegistrationDashboardSetup, error) {
	if s.dashboard.OrganizationSlug != "" {
		return s.dashboard, s.err
	}
	return RegistrationDashboardSetup{OrganizationSlug: "organization", MCPServerRoute: "server"}, s.err
}

func (s *recordingRegistrationStore) IssueSetupHandoff(_ context.Context, _ Principal, binding SetupHandoffBinding, _ time.Time) (IssuedSetupHandoff, error) {
	s.handoffCalls++
	return IssuedSetupHandoff{SetupHandoff: SetupHandoff{ProjectID: binding.ProjectID, RegistrationID: binding.RegistrationID, ProviderKey: binding.ProviderKey, Intent: binding.Intent}}, s.err
}

type recordingIdentityProviderAttachment struct {
	calls  int
	result CatalogIdentityProviderAttachmentResult
	err    error
}

func (a *recordingIdentityProviderAttachment) Attach(_ context.Context, _ Principal, _ ResolvedProject, _ uuid.UUID) (CatalogIdentityProviderAttachmentResult, error) {
	a.calls++
	return a.result, a.err
}

type allowOperationLimiter struct{}

func (allowOperationLimiter) Allow(context.Context, string) (ratelimit.Result, error) {
	return ratelimit.Result{Allowed: true}, nil
}

func allowBudget() OperationBudget {
	return OperationBudget{Connection: allowOperationLimiter{}, Organization: allowOperationLimiter{}}
}

func newRegistrationService(catalog Catalog, gate CatalogRegistrationGateChecker, store RegistrationPersistence) *RegistrationService {
	budget := allowBudget()
	return NewRegistrationService(catalog, gate, store).WithOperationBudgets(OperationBudgets{
		Catalog:      budget,
		Registration: budget,
		Handoff:      budget,
		SetupStart:   budget,
		Repair:       budget,
	})
}

func registrationServicePrincipal() Principal {
	return Principal{UserID: "user", OrganizationID: "organization", ConnectionID: uuid.NewString(), Generation: uuid.NewString(), ClientID: "client"}
}

func TestRegistrationServiceReturnsCatalogInspectionErrors(t *testing.T) {
	t.Parallel()

	upstreamErr := errors.New("catalog unavailable")
	service := newRegistrationService(testCatalog{err: upstreamErr}, &testRegistrationGate{enabled: true}, &recordingRegistrationStore{})

	_, err := service.RegisterCatalogMCP(t.Context(), registrationServicePrincipal(), RegisterCatalogMCPInput{
		ProjectSlug: "project", ProviderKey: "provider", CatalogRef: "reviewed/mcp", IdempotencyKey: "request-key",
	})

	require.ErrorIs(t, err, upstreamErr)
}
