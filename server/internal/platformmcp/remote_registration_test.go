package platformmcp

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/ratelimit"
)

const remoteRegistrationTestKey = "remote-registration-test-key"

var remoteRegistrationTestNow = time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)

type recordingRemoteApprovalChecker struct {
	state          RemoteMCPApprovalState
	err            error
	calls          int
	organizationID string
	projectID      uuid.UUID
	remoteURL      string
}

func (c *recordingRemoteApprovalChecker) CheckRemoteMCPApproval(_ context.Context, organizationID string, projectID uuid.UUID, remoteURL string) (RemoteMCPApprovalState, error) {
	c.calls++
	c.organizationID = organizationID
	c.projectID = projectID
	c.remoteURL = remoteURL
	return c.state, c.err
}

func mintProbeReceipt(t *testing.T, keyMaterial string, principal Principal, normalizedURL string, now time.Time) string {
	t.Helper()

	codec, err := newProbeReceiptCodec(keyMaterial)
	require.NoError(t, err)
	value, err := codec.Encode(principal, normalizedURL, "probe-digest", now)
	require.NoError(t, err)
	return value
}

// remoteRegistrationStore builds the recording fake with the receipt sequence
// a fresh, successful registration walks through.
func remoteRegistrationStore(project ResolvedProject, registrationID uuid.UUID) *recordingRegistrationStore {
	return &recordingRegistrationStore{
		project: project,
		begin:   OperationReceipt{ID: uuid.New()},
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
}

func newRemoteRegistrationService(gate CatalogRegistrationGateChecker, store RegistrationPersistence, approvals RemoteMCPApprovalChecker) *RegistrationService {
	service := newRegistrationService(testCatalog{}, gate, store).WithRemoteRegistration(remoteRegistrationTestKey, approvals)
	service.now = func() time.Time { return remoteRegistrationTestNow }
	return service
}

func TestRegistrationServiceRegistersProbedRemoteURL(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Name: "Project", Slug: "project"}
	registrationID := uuid.New()
	store := remoteRegistrationStore(project, registrationID)
	checker := &recordingRemoteApprovalChecker{}
	service := newRemoteRegistrationService(&testRegistrationGate{enabled: true}, store, checker)
	principal := registrationServicePrincipal()
	normalizedURL := "https://remote.example.test/mcp"

	result, err := service.RegisterRemoteMCP(t.Context(), principal, RegisterRemoteMCPInput{
		ProjectSlug:    project.Slug,
		ProbeReceipt:   mintProbeReceipt(t, remoteRegistrationTestKey, principal, normalizedURL, remoteRegistrationTestNow),
		IdempotencyKey: "request-key",
	})

	require.NoError(t, err)
	require.Equal(t, project, result.Project)
	require.Equal(t, normalizedURL, result.RemoteURL)
	require.Equal(t, registrationID.String(), result.Registration)
	require.False(t, result.BlockedPendingApproval)
	require.Empty(t, result.DashboardApprovalsURL)

	require.Equal(t, remoteURLSourceKind, store.request.SourceKind)
	require.Equal(t, remoteURLCatalogProvider, store.request.CatalogProvider)
	require.Equal(t, normalizedURL, store.request.CatalogReference)
	require.Equal(t, catalogRegistrationInputHash(project.Slug, remoteURLSourceKind, remoteURLCatalogProvider, normalizedURL), store.request.InputHash)
	require.Equal(t, normalizedURL, store.remoteURL)
	require.Equal(t, "remote.example.test", store.remoteDisplayName, "an omitted display name derives from the remote host")
	require.Equal(t, 1, store.beginCalls)
	require.Equal(t, 1, store.convergeCalls)
	require.Equal(t, 1, store.completeRemoteCalls)
	require.Zero(t, store.completeCalls, "the remote path never resolves a catalogue configuration")

	require.Equal(t, 1, checker.calls)
	require.Equal(t, principal.OrganizationID, checker.organizationID)
	require.Equal(t, project.ID, checker.projectID)
	require.Equal(t, normalizedURL, checker.remoteURL)
}

func TestRegistrationServiceRemoteRegistrationPassesThroughDisplayName(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	store := remoteRegistrationStore(project, uuid.New())
	service := newRemoteRegistrationService(&testRegistrationGate{enabled: true}, store, &recordingRemoteApprovalChecker{})
	principal := registrationServicePrincipal()

	_, err := service.RegisterRemoteMCP(t.Context(), principal, RegisterRemoteMCPInput{
		ProjectSlug:    project.Slug,
		ProbeReceipt:   mintProbeReceipt(t, remoteRegistrationTestKey, principal, "https://remote.example.test/mcp", remoteRegistrationTestNow),
		IdempotencyKey: "request-key",
		DisplayName:    "  Vendor MCP  ",
	})

	require.NoError(t, err)
	require.Equal(t, "Vendor MCP", store.remoteDisplayName)
}

// The display name lands in persisted component names, so it is bounded to
// maxRemoteDisplayNameLength bytes and must be a single line: an embedded
// control character or line separator would carry a caller-controlled break
// or escape sequence into every surface rendering the name.
func TestRegistrationServiceRemoteRegistrationBoundsDisplayName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		displayName string
		rejected    bool
	}{
		{name: "name at the byte bound is accepted", displayName: strings.Repeat("n", maxRemoteDisplayNameLength)},
		{name: "name one byte over the bound is rejected", displayName: strings.Repeat("n", maxRemoteDisplayNameLength+1), rejected: true},
		// The bound is measured in bytes, not runes: two-byte runes reach it
		// at half the count, and a 256-rune name overflows through one
		// multibyte rune.
		{name: "multibyte name at the byte bound is accepted", displayName: strings.Repeat("ñ", maxRemoteDisplayNameLength/2)},
		{name: "name over the bound through a multibyte rune is rejected", displayName: strings.Repeat("n", maxRemoteDisplayNameLength-1) + "ñ", rejected: true},
		{name: "embedded carriage return is rejected", displayName: "Vendor\rMCP", rejected: true},
		{name: "embedded line feed is rejected", displayName: "Vendor\nMCP", rejected: true},
		{name: "embedded ANSI escape is rejected", displayName: "Vendor\x1b[2KMCP", rejected: true},
		{name: "embedded unicode line separator is rejected", displayName: "Vendor\u2028MCP", rejected: true},
	}
	for _, test := range tests {
		project := ResolvedProject{ID: uuid.New(), Slug: "project"}
		store := remoteRegistrationStore(project, uuid.New())
		service := newRemoteRegistrationService(&testRegistrationGate{enabled: true}, store, &recordingRemoteApprovalChecker{})
		principal := registrationServicePrincipal()

		_, err := service.RegisterRemoteMCP(t.Context(), principal, RegisterRemoteMCPInput{
			ProjectSlug:    project.Slug,
			ProbeReceipt:   mintProbeReceipt(t, remoteRegistrationTestKey, principal, "https://remote.example.test/mcp", remoteRegistrationTestNow),
			IdempotencyKey: "request-key",
			DisplayName:    test.displayName,
		})

		if test.rejected {
			require.ErrorIs(t, err, ErrRegistrationInvalid, test.name)
			require.Zero(t, store.beginCalls, "%s: a rejected name must not reach persistence", test.name)
			continue
		}
		require.NoError(t, err, test.name)
		require.Equal(t, test.displayName, store.remoteDisplayName, test.name)
	}
}

func TestRegistrationServiceRemoteRegistrationUnavailableUntilConfigured(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	store := remoteRegistrationStore(project, uuid.New())
	gate := &testRegistrationGate{enabled: true}
	service := newRegistrationService(testCatalog{}, gate, store)
	principal := registrationServicePrincipal()

	_, err := service.RegisterRemoteMCP(t.Context(), principal, RegisterRemoteMCPInput{
		ProjectSlug:    project.Slug,
		ProbeReceipt:   mintProbeReceipt(t, remoteRegistrationTestKey, principal, "https://remote.example.test/mcp", remoteRegistrationTestNow),
		IdempotencyKey: "request-key",
	})

	require.ErrorIs(t, err, ErrRegistrationUnavailable)
	require.Zero(t, gate.calls)
	require.Zero(t, store.beginCalls)
}

func TestRegistrationServiceRemoteRegistrationFailsClosedWhenGateIsDisabled(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	store := remoteRegistrationStore(project, uuid.New())
	gate := &testRegistrationGate{}
	service := newRemoteRegistrationService(gate, store, &recordingRemoteApprovalChecker{})
	principal := registrationServicePrincipal()

	_, err := service.RegisterRemoteMCP(t.Context(), principal, RegisterRemoteMCPInput{
		ProjectSlug:    project.Slug,
		ProbeReceipt:   mintProbeReceipt(t, remoteRegistrationTestKey, principal, "https://remote.example.test/mcp", remoteRegistrationTestNow),
		IdempotencyKey: "request-key",
	})

	require.ErrorIs(t, err, ErrRegistrationUnavailable)
	require.Equal(t, 1, gate.calls)
	require.Zero(t, store.resolveCalls)
	require.Zero(t, store.beginCalls)
}

func TestRegistrationServiceRemoteRegistrationStopsBeforeGateWhenBudgetDenies(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	store := remoteRegistrationStore(project, uuid.New())
	gate := &testRegistrationGate{enabled: true}
	denied := OperationBudget{
		Connection:   &recordingOperationLimiter{result: ratelimit.Result{Allowed: false}},
		Organization: allowOperationLimiter{},
	}
	service := NewRegistrationService(testCatalog{}, gate, store).
		WithOperationBudgets(OperationBudgets{Registration: denied, Catalog: denied, Handoff: denied, SetupStart: denied, Repair: denied}).
		WithRemoteRegistration(remoteRegistrationTestKey, &recordingRemoteApprovalChecker{})
	principal := registrationServicePrincipal()

	_, err := service.RegisterRemoteMCP(t.Context(), principal, RegisterRemoteMCPInput{
		ProjectSlug:    project.Slug,
		ProbeReceipt:   mintProbeReceipt(t, remoteRegistrationTestKey, principal, "https://remote.example.test/mcp", time.Now()),
		IdempotencyKey: "request-key",
	})

	require.ErrorIs(t, err, ErrOperationRateLimited)
	require.Zero(t, gate.calls)
	require.Zero(t, store.resolveCalls)
	require.Zero(t, store.beginCalls)
}

func TestRegistrationServiceRemoteRegistrationRejectsForgedReceipt(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	store := remoteRegistrationStore(project, uuid.New())
	service := newRemoteRegistrationService(&testRegistrationGate{enabled: true}, store, &recordingRemoteApprovalChecker{})
	principal := registrationServicePrincipal()

	_, err := service.RegisterRemoteMCP(t.Context(), principal, RegisterRemoteMCPInput{
		ProjectSlug:    project.Slug,
		ProbeReceipt:   mintProbeReceipt(t, "a-different-signing-key", principal, "https://remote.example.test/mcp", remoteRegistrationTestNow),
		IdempotencyKey: "request-key",
	})

	require.ErrorIs(t, err, ErrProbeReceiptInvalid)
	require.Zero(t, store.resolveCalls)
	require.Zero(t, store.beginCalls)
}

func TestRegistrationServiceRemoteRegistrationRejectsExpiredReceipt(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	store := remoteRegistrationStore(project, uuid.New())
	service := newRemoteRegistrationService(&testRegistrationGate{enabled: true}, store, &recordingRemoteApprovalChecker{})
	principal := registrationServicePrincipal()

	_, err := service.RegisterRemoteMCP(t.Context(), principal, RegisterRemoteMCPInput{
		ProjectSlug:    project.Slug,
		ProbeReceipt:   mintProbeReceipt(t, remoteRegistrationTestKey, principal, "https://remote.example.test/mcp", remoteRegistrationTestNow.Add(-probeReceiptTTL)),
		IdempotencyKey: "request-key",
	})

	require.ErrorIs(t, err, ErrProbeReceiptExpired)
	require.Zero(t, store.beginCalls)
}

func TestRegistrationServiceRemoteRegistrationRejectsForeignConnectionReceipt(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	store := remoteRegistrationStore(project, uuid.New())
	service := newRemoteRegistrationService(&testRegistrationGate{enabled: true}, store, &recordingRemoteApprovalChecker{})
	prober := registrationServicePrincipal()
	redeemer := registrationServicePrincipal()
	redeemer.OrganizationID = prober.OrganizationID

	_, err := service.RegisterRemoteMCP(t.Context(), redeemer, RegisterRemoteMCPInput{
		ProjectSlug:    project.Slug,
		ProbeReceipt:   mintProbeReceipt(t, remoteRegistrationTestKey, prober, "https://remote.example.test/mcp", remoteRegistrationTestNow),
		IdempotencyKey: "request-key",
	})

	require.ErrorIs(t, err, ErrProbeReceiptContextMismatch)
	require.Zero(t, store.beginCalls)
}

func TestRegistrationServiceRemoteRegistrationReturnsActiveRegistrationCapConflict(t *testing.T) {
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
	checker := &recordingRemoteApprovalChecker{}
	service := newRemoteRegistrationService(&testRegistrationGate{enabled: true}, store, checker)
	principal := registrationServicePrincipal()

	_, err := service.RegisterRemoteMCP(t.Context(), principal, RegisterRemoteMCPInput{
		ProjectSlug:    project.Slug,
		ProbeReceipt:   mintProbeReceipt(t, remoteRegistrationTestKey, principal, "https://remote.example.test/mcp", remoteRegistrationTestNow),
		IdempotencyKey: "request-key",
	})

	require.ErrorIs(t, err, ErrRegistrationCap)
	require.Zero(t, store.convergeCalls)
	require.Zero(t, store.completeRemoteCalls)
	require.Zero(t, checker.calls)
}

func TestRegistrationServiceRemoteRegistrationRejectsIneligibleTargetBeforeReceipt(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "legacy-project"}
	store := remoteRegistrationStore(project, uuid.New())
	store.eligibilitySet = true
	service := newRemoteRegistrationService(&testRegistrationGate{enabled: true}, store, &recordingRemoteApprovalChecker{})
	principal := registrationServicePrincipal()

	_, err := service.RegisterRemoteMCP(t.Context(), principal, RegisterRemoteMCPInput{
		ProjectSlug:    project.Slug,
		ProbeReceipt:   mintProbeReceipt(t, remoteRegistrationTestKey, principal, "https://remote.example.test/mcp", remoteRegistrationTestNow),
		IdempotencyKey: "request-key",
	})

	require.ErrorIs(t, err, ErrTargetIneligible)
	require.Zero(t, store.beginCalls)
}

func TestRegistrationServiceRemoteRegistrationReplayReturnsOriginal(t *testing.T) {
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
	checker := &recordingRemoteApprovalChecker{}
	service := newRemoteRegistrationService(&testRegistrationGate{enabled: true}, store, checker)
	principal := registrationServicePrincipal()

	result, err := service.RegisterRemoteMCP(t.Context(), principal, RegisterRemoteMCPInput{
		ProjectSlug:    project.Slug,
		ProbeReceipt:   mintProbeReceipt(t, remoteRegistrationTestKey, principal, "https://remote.example.test/mcp", remoteRegistrationTestNow),
		IdempotencyKey: "request-key",
	})

	require.NoError(t, err)
	require.Equal(t, registrationID.String(), result.Registration)
	require.Zero(t, store.convergeCalls)
	require.Zero(t, store.completeRemoteCalls)
	require.Equal(t, 1, checker.calls, "a replay still consults current enforcement state")
}

func TestRegistrationServiceRemoteRegistrationReportsBlockedPendingApproval(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	store := remoteRegistrationStore(project, uuid.New())
	checker := &recordingRemoteApprovalChecker{state: RemoteMCPApprovalState{EnforcementActive: true, Approved: false}}
	service := newRemoteRegistrationService(&testRegistrationGate{enabled: true}, store, checker)
	principal := registrationServicePrincipal()

	result, err := service.RegisterRemoteMCP(t.Context(), principal, RegisterRemoteMCPInput{
		ProjectSlug:    project.Slug,
		ProbeReceipt:   mintProbeReceipt(t, remoteRegistrationTestKey, principal, "https://remote.example.test/mcp", remoteRegistrationTestNow),
		IdempotencyKey: "request-key",
	})

	require.NoError(t, err, "enforcement blocks distribution, not the registration itself")
	require.True(t, result.BlockedPendingApproval)
	require.Equal(t, "/organization/projects/project/shadow-mcp", result.DashboardApprovalsURL)
}

func TestRegistrationServiceRemoteRegistrationBuildsAbsoluteApprovalsURLWithDashboardOrigin(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	store := remoteRegistrationStore(project, uuid.New())
	checker := &recordingRemoteApprovalChecker{state: RemoteMCPApprovalState{EnforcementActive: true, Approved: false}}
	service := newRemoteRegistrationService(&testRegistrationGate{enabled: true}, store, checker)
	service.WithDashboardURL(&url.URL{Scheme: "https", Host: "localhost:5173"})
	principal := registrationServicePrincipal()

	result, err := service.RegisterRemoteMCP(t.Context(), principal, RegisterRemoteMCPInput{
		ProjectSlug:    project.Slug,
		ProbeReceipt:   mintProbeReceipt(t, remoteRegistrationTestKey, principal, "https://remote.example.test/mcp", remoteRegistrationTestNow),
		IdempotencyKey: "request-key",
	})

	require.NoError(t, err)
	require.True(t, result.BlockedPendingApproval)
	require.Equal(t, "https://localhost:5173/organization/projects/project/shadow-mcp", result.DashboardApprovalsURL)
}

func TestRegistrationServiceRemoteRegistrationDoesNotBlockApprovedServerUnderEnforcement(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	store := remoteRegistrationStore(project, uuid.New())
	checker := &recordingRemoteApprovalChecker{state: RemoteMCPApprovalState{EnforcementActive: true, Approved: true}}
	service := newRemoteRegistrationService(&testRegistrationGate{enabled: true}, store, checker)
	principal := registrationServicePrincipal()

	result, err := service.RegisterRemoteMCP(t.Context(), principal, RegisterRemoteMCPInput{
		ProjectSlug:    project.Slug,
		ProbeReceipt:   mintProbeReceipt(t, remoteRegistrationTestKey, principal, "https://remote.example.test/mcp", remoteRegistrationTestNow),
		IdempotencyKey: "request-key",
	})

	require.NoError(t, err)
	require.False(t, result.BlockedPendingApproval)
	require.Empty(t, result.DashboardApprovalsURL)
}

func TestRegistrationServiceRemoteEnforcementStatusReportsBlockedPendingApproval(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	store := remoteRegistrationStore(project, uuid.New())
	store.candidate = CatalogCandidate{ProviderKey: remoteURLCatalogProvider, CatalogRef: "https://remote.example.test/mcp"}
	checker := &recordingRemoteApprovalChecker{state: RemoteMCPApprovalState{EnforcementActive: true, Approved: false}}
	service := newRemoteRegistrationService(&testRegistrationGate{enabled: true}, store, checker)
	principal := registrationServicePrincipal()

	status, err := service.RemoteRegistrationEnforcementStatus(t.Context(), principal, project, uuid.New())

	require.NoError(t, err)
	require.True(t, status.BlockedPendingApproval)
	require.Equal(t, "/organization/projects/project/shadow-mcp", status.DashboardApprovalsURL)
	require.Equal(t, 1, checker.calls)
	require.Equal(t, principal.OrganizationID, checker.organizationID)
	require.Equal(t, project.ID, checker.projectID)
	require.Equal(t, "https://remote.example.test/mcp", checker.remoteURL, "the consult uses the persisted registration URL, never caller input")
}

func TestRegistrationServiceRemoteEnforcementStatusBuildsAbsoluteURLWithDashboardOrigin(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	store := remoteRegistrationStore(project, uuid.New())
	store.candidate = CatalogCandidate{ProviderKey: remoteURLCatalogProvider, CatalogRef: "https://remote.example.test/mcp"}
	checker := &recordingRemoteApprovalChecker{state: RemoteMCPApprovalState{EnforcementActive: true, Approved: false}}
	service := newRemoteRegistrationService(&testRegistrationGate{enabled: true}, store, checker)
	service.WithDashboardURL(&url.URL{Scheme: "https", Host: "localhost:5173"})

	status, err := service.RemoteRegistrationEnforcementStatus(t.Context(), registrationServicePrincipal(), project, uuid.New())

	require.NoError(t, err)
	require.True(t, status.BlockedPendingApproval)
	require.Equal(t, "https://localhost:5173/organization/projects/project/shadow-mcp", status.DashboardApprovalsURL)
}

func TestRegistrationServiceRemoteEnforcementStatusReportsUnblockedStates(t *testing.T) {
	t.Parallel()

	for _, state := range []RemoteMCPApprovalState{
		{EnforcementActive: true, Approved: true},
		{EnforcementActive: false, Approved: false},
	} {
		project := ResolvedProject{ID: uuid.New(), Slug: "project"}
		store := remoteRegistrationStore(project, uuid.New())
		store.candidate = CatalogCandidate{ProviderKey: remoteURLCatalogProvider, CatalogRef: "https://remote.example.test/mcp"}
		service := newRemoteRegistrationService(&testRegistrationGate{enabled: true}, store, &recordingRemoteApprovalChecker{state: state})

		status, err := service.RemoteRegistrationEnforcementStatus(t.Context(), registrationServicePrincipal(), project, uuid.New())

		require.NoError(t, err, "state %+v", state)
		require.False(t, status.BlockedPendingApproval, "state %+v", state)
		require.Empty(t, status.DashboardApprovalsURL, "state %+v", state)
	}
}

func TestRegistrationServiceEnforcementStatusSkipsCatalogueRegistrations(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	store := remoteRegistrationStore(project, uuid.New())
	store.candidate = CatalogCandidate{ProviderKey: "provider", CatalogRef: "reviewed/mcp"}
	checker := &recordingRemoteApprovalChecker{state: RemoteMCPApprovalState{EnforcementActive: true, Approved: false}}
	service := newRemoteRegistrationService(&testRegistrationGate{enabled: true}, store, checker)

	status, err := service.RemoteRegistrationEnforcementStatus(t.Context(), registrationServicePrincipal(), project, uuid.New())

	require.NoError(t, err)
	require.False(t, status.BlockedPendingApproval)
	require.Zero(t, checker.calls, "catalogue registrations are never consulted against remote enforcement")
}

func TestRegistrationServiceEnforcementStatusWorksWithoutRemotePathForCatalogueRegistrations(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	store := remoteRegistrationStore(project, uuid.New())
	store.candidate = CatalogCandidate{ProviderKey: "provider", CatalogRef: "reviewed/mcp"}
	service := newRegistrationService(testCatalog{}, &testRegistrationGate{enabled: true}, store)

	status, err := service.RemoteRegistrationEnforcementStatus(t.Context(), registrationServicePrincipal(), project, uuid.New())

	require.NoError(t, err, "catalogue onboarding status must not depend on the remote URL rollout")
	require.False(t, status.BlockedPendingApproval)
}

func TestRegistrationServiceRemoteEnforcementStatusFailsClosedWithoutChecker(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	store := remoteRegistrationStore(project, uuid.New())
	store.candidate = CatalogCandidate{ProviderKey: remoteURLCatalogProvider, CatalogRef: "https://remote.example.test/mcp"}
	service := newRegistrationService(testCatalog{}, &testRegistrationGate{enabled: true}, store)

	_, err := service.RemoteRegistrationEnforcementStatus(t.Context(), registrationServicePrincipal(), project, uuid.New())

	require.ErrorIs(t, err, ErrRegistrationUnavailable, "a remote URL registration with no checker must not read as unblocked")
}

func TestRegistrationServiceRemoteEnforcementStatusFailsClosedOnConsultError(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	store := remoteRegistrationStore(project, uuid.New())
	store.candidate = CatalogCandidate{ProviderKey: remoteURLCatalogProvider, CatalogRef: "https://remote.example.test/mcp"}
	consultErr := errors.New("grant store unavailable")
	service := newRemoteRegistrationService(&testRegistrationGate{enabled: true}, store, &recordingRemoteApprovalChecker{err: consultErr})

	_, err := service.RemoteRegistrationEnforcementStatus(t.Context(), registrationServicePrincipal(), project, uuid.New())

	require.ErrorIs(t, err, consultErr)
}

func TestRegistrationServiceRemoteRegistrationFailsClosedWhenApprovalConsultFails(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	store := remoteRegistrationStore(project, uuid.New())
	consultErr := errors.New("grant store unavailable")
	checker := &recordingRemoteApprovalChecker{err: consultErr}
	service := newRemoteRegistrationService(&testRegistrationGate{enabled: true}, store, checker)
	principal := registrationServicePrincipal()

	_, err := service.RegisterRemoteMCP(t.Context(), principal, RegisterRemoteMCPInput{
		ProjectSlug:    project.Slug,
		ProbeReceipt:   mintProbeReceipt(t, remoteRegistrationTestKey, principal, "https://remote.example.test/mcp", remoteRegistrationTestNow),
		IdempotencyKey: "request-key",
	})

	require.ErrorIs(t, err, consultErr, "an unknown enforcement state must not read as unblocked")
}
