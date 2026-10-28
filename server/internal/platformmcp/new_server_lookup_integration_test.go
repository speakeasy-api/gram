package platformmcp

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type countingCatalogReadinessProber struct {
	calls atomic.Int32
	err   error
}

func (p *countingCatalogReadinessProber) ProbeCatalogReadiness(_ context.Context, principal Principal, _, registrationID, _, _, _, _ uuid.UUID) (ProviderReadinessProbeResult, error) {
	p.calls.Add(1)
	if p.err != nil {
		return ProviderReadinessProbeResult{}, p.err
	}
	now := time.Now().UTC()
	return ProviderReadinessProbeResult{
		AuthorizationIdentity: ProviderAuthorizationIdentity{
			OrganizationID: principal.OrganizationID,
			Subject:        urn.NewUserSubject(principal.UserID),
			RegistrationID: registrationID,
			Absence:        "no_client",
		},
		State:        ReadinessNeedsConfiguration,
		EvidenceCode: "upstream_identity_provider_not_configured",
		CheckedAt:    now,
		ExpiresAt:    now.Add(time.Minute),
	}, nil
}

// newRemoteRegistrar returns a registrar whose register_remote_mcp tool
// resolves every URL to canonicalURL.
func newRemoteRegistrar(store *RegistrationStore, gate *testRegistrationGate, canonicalURL string) *Registrar {
	return newInspectedRemoteRegistrar(store, gate, namedRemoteInspector(canonicalURL, ""))
}

// newInspectedRemoteRegistrar returns a registrar whose register_remote_mcp
// tool uses the given inspector, so a test can set or change the upstream's
// own server name.
func newInspectedRemoteRegistrar(store *RegistrationStore, gate *testRegistrationGate, inspector *testDirectRemoteInspector) *Registrar {
	registrations := newRegistrationService(testCatalog{}, gate, store).WithDirectRemoteInspector(inspector)
	reg := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "platform-mcp-test"}, nil))
	registerRemoteRegistrationTool(reg, registrations, nil)
	return reg
}

func namedRemoteInspector(canonicalURL, serverName string) *testDirectRemoteInspector {
	return &testDirectRemoteInspector{inspection: DirectRemoteInspection{CanonicalURL: canonicalURL, Transport: "streamable-http", Trust: "user_supplied_unreviewed", ServerName: serverName}}
}

// registeredServerAddress returns the name and slug of the MCP server a
// registration created, and the slug of its endpoint address.
func registeredServerAddress(t *testing.T, ctx context.Context, conn *pgxpool.Pool, principal Principal, project ResolvedProject, registrationID string) (string, string, string) {
	t.Helper()

	id, err := uuid.Parse(registrationID)
	require.NoError(t, err)
	registration, err := platformrepo.New(conn).GetPlatformMCPCatalogRegistrationByID(ctx, platformrepo.GetPlatformMCPCatalogRegistrationByIDParams{
		ID:             id,
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
	})
	require.NoError(t, err)
	server, err := mcpserversrepo.New(conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: registration.McpServerID.UUID, ProjectID: project.ID})
	require.NoError(t, err)
	endpoint, err := mcpendpointsrepo.New(conn).GetMCPEndpointByID(ctx, mcpendpointsrepo.GetMCPEndpointByIDParams{ID: registration.McpEndpointID.UUID, ProjectID: project.ID})
	require.NoError(t, err)
	return server.Name.String, server.Slug.String, endpoint.Slug
}

func organizationSlug(t *testing.T, ctx context.Context, conn *pgxpool.Pool, principal Principal) string {
	t.Helper()

	organization, err := organizationsrepo.New(conn).GetOrganizationMetadata(ctx, principal.OrganizationID)
	require.NoError(t, err)
	return organization.Slug
}

// A server added by URL without a name takes the upstream's own name, and its
// MCP server slug and endpoint address are derived from that name.
func TestRemoteRegistrationUsesTheServerNameForItsAddress(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_remote_readable_address")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn)
	require.NoError(t, err)
	reg := newInspectedRemoteRegistrar(store, &testRegistrationGate{enabled: true}, namedRemoteInspector("https://remote.example.test/mcp", "Linear"))
	callCtx := ContextWithPrincipal(ctx, principal)

	remote, err := invokeTool[RegisterRemoteMCPToolOutput](t, callCtx, reg, "register_remote_mcp", map[string]any{
		"project_slug":    project.Slug,
		"remote_url":      "https://remote.example.test/mcp",
		"idempotency_key": "readable-address",
	})
	require.NoError(t, err)

	name, serverSlug, endpointSlug := registeredServerAddress(t, ctx, conn, principal, project, remote.RegistrationID)
	require.Equal(t, "Linear", name)
	require.Equal(t, "linear", serverSlug)
	require.Equal(t, organizationSlug(t, ctx, conn, principal)+"-linear", endpointSlug)
}

// A name that fits the address limit keeps its full length. Only a suffixed
// slug is cut shorter to make room for the suffix.
func TestRemoteRegistrationKeepsTheWholeNameWhenItFits(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_remote_readable_full_name")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn)
	require.NoError(t, err)
	orgSlug := organizationSlug(t, ctx, conn, principal)
	name := strings.Repeat("a", maxPlatformEndpointSlugLength-len(orgSlug)-len("-"))
	reg := newInspectedRemoteRegistrar(store, &testRegistrationGate{enabled: true}, namedRemoteInspector("https://remote.example.test/mcp", name))

	remote, err := invokeTool[RegisterRemoteMCPToolOutput](t, ContextWithPrincipal(ctx, principal), reg, "register_remote_mcp", map[string]any{
		"project_slug":    project.Slug,
		"remote_url":      "https://remote.example.test/mcp",
		"idempotency_key": "readable-full-name",
	})
	require.NoError(t, err)

	_, serverSlug, endpointSlug := registeredServerAddress(t, ctx, conn, principal, project, remote.RegistrationID)
	require.Equal(t, name, serverSlug)
	require.Equal(t, orgSlug+"-"+name, endpointSlug)
}

// A second server whose name gives the same slug gets a short random suffix
// on both its MCP server slug and its endpoint address.
func TestRemoteRegistrationAddsASuffixWhenTheReadableAddressIsTaken(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_remote_readable_collision")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn)
	require.NoError(t, err)
	gate := &testRegistrationGate{enabled: true}
	callCtx := ContextWithPrincipal(ctx, principal)
	orgSlug := organizationSlug(t, ctx, conn, principal)

	first, err := invokeTool[RegisterRemoteMCPToolOutput](t, callCtx, newInspectedRemoteRegistrar(store, gate, namedRemoteInspector("https://remote.example.test/mcp", "Linear")), "register_remote_mcp", map[string]any{
		"project_slug":    project.Slug,
		"remote_url":      "https://remote.example.test/mcp",
		"idempotency_key": "readable-collision-first",
	})
	require.NoError(t, err)
	second, err := invokeTool[RegisterRemoteMCPToolOutput](t, callCtx, newInspectedRemoteRegistrar(store, gate, namedRemoteInspector("https://mirror.example.test/mcp", "linear")), "register_remote_mcp", map[string]any{
		"project_slug":    project.Slug,
		"remote_url":      "https://mirror.example.test/mcp",
		"idempotency_key": "readable-collision-second",
	})
	require.NoError(t, err)

	_, firstSlug, firstEndpoint := registeredServerAddress(t, ctx, conn, principal, project, first.RegistrationID)
	require.Equal(t, "linear", firstSlug)
	require.Equal(t, orgSlug+"-linear", firstEndpoint)

	_, secondSlug, secondEndpoint := registeredServerAddress(t, ctx, conn, principal, project, second.RegistrationID)
	require.Regexp(t, `^linear-[a-z0-9]{5}$`, secondSlug)
	require.Equal(t, orgSlug+"-"+secondSlug, secondEndpoint)
}

// A retry with the same key and URL returns the same registration even when
// the upstream reports a different name on the retry.
func TestRemoteRegistrationRetryIgnoresAChangedServerName(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_remote_readable_retry")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn)
	require.NoError(t, err)
	inspector := namedRemoteInspector("https://remote.example.test/mcp", "Linear")
	reg := newInspectedRemoteRegistrar(store, &testRegistrationGate{enabled: true}, inspector)
	callCtx := ContextWithPrincipal(ctx, principal)
	args := map[string]any{
		"project_slug":    project.Slug,
		"remote_url":      "https://remote.example.test/mcp",
		"idempotency_key": "readable-retry",
	}

	first, err := invokeTool[RegisterRemoteMCPToolOutput](t, callCtx, reg, "register_remote_mcp", args)
	require.NoError(t, err)
	inspector.inspection.ServerName = "Linear (renamed upstream)"
	retry, err := invokeTool[RegisterRemoteMCPToolOutput](t, callCtx, reg, "register_remote_mcp", args)
	require.NoError(t, err)

	require.True(t, retry.Replayed)
	require.Equal(t, first.RegistrationID, retry.RegistrationID)
	require.Equal(t, first.ReceiptID, retry.ReceiptID)
	name, serverSlug, _ := registeredServerAddress(t, ctx, conn, principal, project, retry.RegistrationID)
	require.Equal(t, "Linear", name)
	require.Equal(t, "linear", serverSlug)
}

// A server named after its upstream can still be found by its source URL.
func TestInventorySearchFindsAServerNamedByItsUpstreamBySourceURL(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_inventory_upstream_name")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn)
	require.NoError(t, err)
	reg := newInspectedRemoteRegistrar(store, &testRegistrationGate{enabled: true}, namedRemoteInspector("https://remote.example.test/mcp", "Linear"))
	callCtx := ContextWithPrincipal(ctx, principal)

	remote, err := invokeTool[RegisterRemoteMCPToolOutput](t, callCtx, reg, "register_remote_mcp", map[string]any{
		"project_slug":    project.Slug,
		"remote_url":      "https://remote.example.test/mcp",
		"idempotency_key": "inventory-upstream-name",
	})
	require.NoError(t, err)
	registrationID, err := uuid.Parse(remote.RegistrationID)
	require.NoError(t, err)
	registration, err := platformrepo.New(conn).GetPlatformMCPCatalogRegistrationByID(ctx, platformrepo.GetPlatformMCPCatalogRegistrationByIDParams{
		ID:             registrationID,
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
	})
	require.NoError(t, err)

	candidates, err := platformrepo.New(conn).ListPlatformMCPInventoryAuthorizationCandidatePage(ctx, platformrepo.ListPlatformMCPInventoryAuthorizationCandidatePageParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      uuid.NullUUID{},
		AfterMcpID:     uuid.NullUUID{},
		QueryText:      normalizeInventoryQuery("https://remote.example.test/mcp"),
		LimitValue:     10,
	})
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, registration.McpServerID.UUID, candidates[0].ID)
}

// newNeverCheckedServer adds a server by URL and returns a registrar with the
// readiness tools wired to the given probe limiter and prober.
func newNeverCheckedServer(t *testing.T, dbName string, limiter Limiter, prober CatalogReadinessProber) (context.Context, *Registrar, string, string) {
	t.Helper()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, dbName)
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn)
	require.NoError(t, err)
	gate := &testRegistrationGate{enabled: true}
	reg := newRemoteRegistrar(store, gate, "https://remote.example.test/mcp")
	registerReadinessTools(reg, NewReadinessService(store, gate, NewProviderAdapters(nil), limiter, allowBudget(), prober))
	callCtx := ContextWithPrincipal(ctx, principal)

	remote, err := invokeTool[RegisterRemoteMCPToolOutput](t, callCtx, reg, "register_remote_mcp", map[string]any{
		"project_slug":    project.Slug,
		"remote_url":      "https://remote.example.test/mcp",
		"idempotency_key": dbName,
	})
	require.NoError(t, err)
	return callCtx, reg, project.Slug, remote.RegistrationID
}

// A server that has never been checked reports its real state on the first
// readiness read instead of a degraded "temporarily unavailable" placeholder.
// Later reads use the stored result rather than checking again.
func TestReadinessToolChecksAServerThatHasNeverBeenChecked(t *testing.T) {
	t.Parallel()

	prober := &countingCatalogReadinessProber{}
	callCtx, reg, projectSlug, registrationID := newNeverCheckedServer(t, "platform_mcp_readiness_first_probe", allowOperationLimiter{}, prober)
	args := map[string]any{"project_slug": projectSlug, "registration_id": registrationID}

	first, err := invokeTool[GetMCPReadinessToolOutput](t, callCtx, reg, "get_mcp_readiness", args)
	require.NoError(t, err)
	require.Equal(t, ReadinessNeedsConfiguration, first.State)
	require.Equal(t, "fresh", first.Freshness)
	require.Equal(t, int32(1), prober.calls.Load())

	second, err := invokeTool[GetMCPReadinessToolOutput](t, callCtx, reg, "get_mcp_readiness", args)
	require.NoError(t, err)
	require.Equal(t, ReadinessNeedsConfiguration, second.State)
	require.Equal(t, int32(1), prober.calls.Load(), "a stored result must be read, not checked again")
}

// When the first check is refused by the probe limit or fails, the read still
// succeeds and reports the server as unchecked.
func TestReadinessToolReportsUncheckedWhenTheFirstCheckCannotRun(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		limiter   Limiter
		prober    *countingCatalogReadinessProber
		wantCalls int32
	}{
		{name: "refused", limiter: denyOperationLimiter{}, prober: &countingCatalogReadinessProber{}, wantCalls: 0},
		{name: "failed", limiter: allowOperationLimiter{}, prober: &countingCatalogReadinessProber{err: errors.New("provider unreachable")}, wantCalls: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			callCtx, reg, projectSlug, registrationID := newNeverCheckedServer(t, "platform_mcp_readiness_first_probe_"+tc.name, tc.limiter, tc.prober)

			out, err := invokeTool[GetMCPReadinessToolOutput](t, callCtx, reg, "get_mcp_readiness", map[string]any{"project_slug": projectSlug, "registration_id": registrationID})
			require.NoError(t, err)
			require.Equal(t, ReadinessDegraded, out.State)
			require.Equal(t, "readiness_unavailable", out.EvidenceCode)
			require.Equal(t, "unavailable", out.Freshness)
			require.Equal(t, tc.wantCalls, tc.prober.calls.Load())
		})
	}
}

// The repair plan reads the last stored check and never runs one, even for a
// server that has never been checked.
func TestRepairPlanDoesNotCheckAServerThatHasNeverBeenChecked(t *testing.T) {
	t.Parallel()

	prober := &countingCatalogReadinessProber{}
	callCtx, reg, projectSlug, registrationID := newNeverCheckedServer(t, "platform_mcp_repair_plan_no_probe", allowOperationLimiter{}, prober)

	plan, err := invokeTool[GetMCPRepairPlanToolOutput](t, callCtx, reg, "get_mcp_repair_plan", map[string]any{"project_slug": projectSlug, "registration_id": registrationID})
	require.NoError(t, err)
	require.Equal(t, ReadinessDegraded, plan.State)
	require.Equal(t, "unavailable", plan.Freshness)
	require.Equal(t, int32(0), prober.calls.Load())
}

// A server added by URL can be found by that URL after it is given its own
// name, both when choosing candidates for authorization and in the inventory.
// It ranks ahead of another server whose name only contains the URL.
func TestInventorySearchMatchesTheSourceURL(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_inventory_source_url")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn)
	require.NoError(t, err)
	gate := &testRegistrationGate{enabled: true}
	reg := newRemoteRegistrar(store, gate, "https://remote.example.test/mcp")
	mirrorReg := newRemoteRegistrar(store, gate, "https://mirror.example.test/mcp")
	callCtx := ContextWithPrincipal(ctx, principal)

	// Added first so that, without exact URL ranking, it would sort first.
	_, err = invokeTool[RegisterRemoteMCPToolOutput](t, callCtx, mirrorReg, "register_remote_mcp", map[string]any{
		"project_slug":    project.Slug,
		"remote_url":      "https://mirror.example.test/mcp",
		"display_name":    "https://remote.example.test/mcp mirror",
		"idempotency_key": "inventory-source-url-mirror",
	})
	require.NoError(t, err)
	remote, err := invokeTool[RegisterRemoteMCPToolOutput](t, callCtx, reg, "register_remote_mcp", map[string]any{
		"project_slug":    project.Slug,
		"remote_url":      "https://remote.example.test/mcp",
		"display_name":    "Renamed server",
		"idempotency_key": "inventory-source-url",
	})
	require.NoError(t, err)
	registrationID, err := uuid.Parse(remote.RegistrationID)
	require.NoError(t, err)
	registration, err := platformrepo.New(conn).GetPlatformMCPCatalogRegistrationByID(ctx, platformrepo.GetPlatformMCPCatalogRegistrationByIDParams{
		ID:             registrationID,
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
	})
	require.NoError(t, err)
	mcpServerID := registration.McpServerID.UUID

	query := normalizeInventoryQuery("https://remote.example.test/mcp")
	candidates, err := platformrepo.New(conn).ListPlatformMCPInventoryAuthorizationCandidatePage(ctx, platformrepo.ListPlatformMCPInventoryAuthorizationCandidatePageParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      uuid.NullUUID{},
		AfterMcpID:     uuid.NullUUID{},
		QueryText:      query,
		LimitValue:     10,
	})
	require.NoError(t, err)
	require.Len(t, candidates, 2)
	require.Equal(t, mcpServerID, candidates[0].ID)

	rows, err := platformrepo.New(conn).ListPlatformMCPInventory(ctx, platformrepo.ListPlatformMCPInventoryParams{
		OrganizationID:          principal.OrganizationID,
		ConnectionID:            uuid.NullUUID{UUID: connectionIDFromPrincipal(t, principal), Valid: true},
		ConnectionGeneration:    uuid.NullUUID{UUID: connectionIDFromPrincipalGeneration(t, principal), Valid: true},
		SkipAuthorizationFilter: true,
		UserID:                  pgtype.Text{},
		ActingSurface:           pgtype.Text{},
		ProjectID:               uuid.NullUUID{},
		AfterMcpID:              uuid.NullUUID{},
		QueryText:               query,
		ReadinessState:          pgtype.Text{},
		LimitValue:              10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, mcpServerID, rows[0].McpServerID)
	require.Equal(t, "Renamed server", rows[0].McpName.String)

	mcps := []MCP{mcpFromInventoryRow(rows[0], nil), mcpFromInventoryRow(rows[1], nil)}
	picked := inventoryQueryResult(mcps, query, 10)
	require.Len(t, picked, 1)
	require.Equal(t, mcpServerID.String(), picked[0].ID)
}
