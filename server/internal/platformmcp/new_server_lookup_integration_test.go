package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
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
	// ttl is how long a check stays fresh; zero means one minute.
	ttl time.Duration
}

func (p *countingCatalogReadinessProber) ProbeCatalogReadiness(_ context.Context, principal Principal, _, registrationID, _, _, _, _ uuid.UUID) (ProviderReadinessProbeResult, error) {
	p.calls.Add(1)
	if p.err != nil {
		return ProviderReadinessProbeResult{}, p.err
	}
	now := time.Now().UTC()
	ttl := p.ttl
	if ttl == 0 {
		ttl = time.Minute
	}
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
		ExpiresAt:    now.Add(ttl),
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

// registrationOfMCP returns the Platform setup record behind an MCP server
// the caller added. No tool returns it, so tests that inspect it look it up.
func registrationOfMCP(t *testing.T, ctx context.Context, conn *pgxpool.Pool, principal Principal, project ResolvedProject, mcpID string) platformrepo.PlatformMcpCatalogRegistration {
	t.Helper()

	store, err := NewRegistrationStore(conn)
	require.NoError(t, err)
	registrationID, err := store.RegistrationIDForMCP(ctx, principal, project.Slug, mcpID, "test")
	require.NoError(t, err)
	registration, err := platformrepo.New(conn).GetPlatformMCPCatalogRegistrationByID(ctx, platformrepo.GetPlatformMCPCatalogRegistrationByIDParams{
		ID:             uuid.MustParse(registrationID),
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
	})
	require.NoError(t, err)
	return registration
}

// registeredServerAddress returns the name and slug of an added MCP server,
// and the slug of the endpoint address its registration created.
func registeredServerAddress(t *testing.T, ctx context.Context, conn *pgxpool.Pool, principal Principal, project ResolvedProject, mcpID string) (string, string, string) {
	t.Helper()

	registration := registrationOfMCP(t, ctx, conn, principal, project, mcpID)
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

	name, serverSlug, endpointSlug := registeredServerAddress(t, ctx, conn, principal, project, remote.MCPID)
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

	_, serverSlug, endpointSlug := registeredServerAddress(t, ctx, conn, principal, project, remote.MCPID)
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

	_, firstSlug, firstEndpoint := registeredServerAddress(t, ctx, conn, principal, project, first.MCPID)
	require.Equal(t, "linear", firstSlug)
	require.Equal(t, orgSlug+"-linear", firstEndpoint)

	_, secondSlug, secondEndpoint := registeredServerAddress(t, ctx, conn, principal, project, second.MCPID)
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
	require.Equal(t, first.MCPID, retry.MCPID)
	require.Equal(t, first.ReceiptID, retry.ReceiptID)
	name, serverSlug, _ := registeredServerAddress(t, ctx, conn, principal, project, retry.MCPID)
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
	mcpServerID := uuid.MustParse(remote.MCPID)

	candidates, err := platformrepo.New(conn).ListPlatformMCPInventoryAuthorizationCandidatePage(ctx, platformrepo.ListPlatformMCPInventoryAuthorizationCandidatePageParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      uuid.NullUUID{},
		AfterMcpID:     uuid.NullUUID{},
		QueryText:      normalizeInventoryQuery("https://remote.example.test/mcp"),
		LimitValue:     10,
	})
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, mcpServerID, candidates[0].ID)
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
	return callCtx, reg, project.Slug, remote.MCPID
}

// A server that has never been checked reports its real state on the first
// readiness read instead of a degraded "temporarily unavailable" placeholder.
// Later reads use the stored result rather than checking again.
func TestReadinessToolChecksAServerThatHasNeverBeenChecked(t *testing.T) {
	t.Parallel()

	prober := &countingCatalogReadinessProber{}
	callCtx, reg, projectSlug, mcpID := newNeverCheckedServer(t, "platform_mcp_readiness_first_probe", allowOperationLimiter{}, prober)
	args := map[string]any{"project_slug": projectSlug, "mcp_id": mcpID}

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

			callCtx, reg, projectSlug, mcpID := newNeverCheckedServer(t, "platform_mcp_readiness_first_probe_"+tc.name, tc.limiter, tc.prober)

			out, err := invokeTool[GetMCPReadinessToolOutput](t, callCtx, reg, "get_mcp_readiness", map[string]any{"project_slug": projectSlug, "mcp_id": mcpID})
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
	callCtx, reg, projectSlug, mcpID := newNeverCheckedServer(t, "platform_mcp_repair_plan_no_probe", allowOperationLimiter{}, prober)

	plan, err := invokeTool[GetMCPRepairPlanToolOutput](t, callCtx, reg, "get_mcp_repair_plan", map[string]any{"project_slug": projectSlug, "mcp_id": mcpID})
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
	mcpServerID := uuid.MustParse(remote.MCPID)

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

func TestRemoteRegistrationToolReportsAServerAlreadyInTheProject(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_remote_already_registered")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn)
	require.NoError(t, err)
	reg := newRemoteRegistrar(store, &testRegistrationGate{enabled: true}, "https://remote.example.test/mcp")
	callCtx := ContextWithPrincipal(ctx, principal)
	register := func(key string) RegisterRemoteMCPToolOutput {
		out, err := invokeTool[RegisterRemoteMCPToolOutput](t, callCtx, reg, "register_remote_mcp", map[string]any{
			"project_slug":    project.Slug,
			"remote_url":      "https://remote.example.test/mcp",
			"idempotency_key": key,
		})
		require.NoError(t, err)
		return out
	}

	first := register("remote-already-first")
	require.False(t, first.AlreadyRegistered)

	again := register("remote-already-second")
	require.Equal(t, first.MCPID, again.MCPID)
	require.True(t, again.AlreadyRegistered)
}

// Once the stored check has expired, a readiness read checks the server again
// instead of reporting the expired result as stale. If that check cannot run,
// the read still succeeds with the stale result.
func TestReadinessToolRechecksAServerWhoseCheckHasExpired(t *testing.T) {
	t.Parallel()

	prober := &countingCatalogReadinessProber{ttl: time.Second}
	callCtx, reg, projectSlug, mcpID := newNeverCheckedServer(t, "platform_mcp_readiness_refresh_probe", allowOperationLimiter{}, prober)
	args := map[string]any{"project_slug": projectSlug, "mcp_id": mcpID}
	read := func() GetMCPReadinessToolOutput {
		out, err := invokeTool[GetMCPReadinessToolOutput](t, callCtx, reg, "get_mcp_readiness", args)
		require.NoError(t, err)
		return out
	}
	// The last check expires one ttl after it ran; wait for that without reading,
	// since a read would itself check again.
	var checkedBy time.Time
	waitUntilExpired := func() {
		require.Eventually(t, func() bool { return time.Now().After(checkedBy.Add(prober.ttl)) }, 5*time.Second, 20*time.Millisecond)
	}

	require.Equal(t, "fresh", read().Freshness)
	checkedBy = time.Now()
	require.Equal(t, int32(1), prober.calls.Load())

	waitUntilExpired()
	refreshed := read()
	require.Equal(t, "fresh", refreshed.Freshness)
	require.Equal(t, ReadinessNeedsConfiguration, refreshed.State)
	require.Equal(t, int32(2), prober.calls.Load(), "an expired check is run again")
	checkedBy = time.Now()

	waitUntilExpired()
	prober.err = errors.New("provider unreachable")
	stale := read()
	require.Equal(t, "stale", stale.Freshness)
	require.Equal(t, ReadinessNeedsConfiguration, stale.State)
	require.Equal(t, int32(3), prober.calls.Load())
}

// Every tool names an MCP server by mcp_id and resolves its Platform setup
// record itself. A server built outside Platform setup, one another member
// added, and an unknown ID are each refused with their own reason, which
// names the refused tool, and nothing is changed.
func TestMCPIDToolsRefuseServersTheCallerCannotActOn(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_mcp_id_refusals")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn)
	require.NoError(t, err)
	gate := &testRegistrationGate{enabled: true}
	attachment := &projectRecordingAttachment{}
	dashboardURL, err := url.Parse("https://dashboard.example.test")
	require.NoError(t, err)
	registrations := newRegistrationService(testCatalog{}, gate, store).
		WithDirectRemoteInspector(namedRemoteInspector("https://remote.example.test/mcp", "")).
		WithIdentityProviderAttachment(attachment).
		WithDashboardURL(dashboardURL)
	reg := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "platform-mcp-test"}, nil))
	registerRemoteRegistrationTool(reg, registrations, nil)
	registerIdentityProviderTool(reg, registrations)
	registerReadinessTools(reg, NewReadinessService(store, gate, NewProviderAdapters(nil), allowOperationLimiter{}, allowBudget(), &countingCatalogReadinessProber{}))

	remote, err := invokeTool[RegisterRemoteMCPToolOutput](t, ContextWithPrincipal(ctx, principal), reg, "register_remote_mcp", map[string]any{
		"project_slug":    project.Slug,
		"remote_url":      "https://remote.example.test/mcp",
		"idempotency_key": "mcp-id-refusals",
	})
	require.NoError(t, err)
	require.NotEmpty(t, remote.MCPID)

	// The seeded server was built in the dashboard, so it has no setup record.
	rows, err := platformrepo.New(conn).ListPlatformMCPInventoryAuthorizationCandidates(ctx, principal.OrganizationID)
	require.NoError(t, err)
	var dashboardMCPID string
	for _, row := range rows {
		if row.ProjectID == project.ID && row.ID.String() != remote.MCPID {
			dashboardMCPID = row.ID.String()
			break
		}
	}
	require.NotEmpty(t, dashboardMCPID)

	otherMember := principal
	otherMember.UserID = "user_" + uuid.NewString()

	type refusal struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	refuse := func(caller Principal, tool, mcpID string) refusal {
		t.Helper()
		arguments := map[string]any{"project_slug": project.Slug, "mcp_id": mcpID}
		if tool == "attach_platform_mcp_identity_provider" {
			arguments["confirmed"] = true
		}
		encoded, err := json.Marshal(arguments)
		require.NoError(t, err)
		_, err = descriptorByName(t, reg, tool).Invoke(ContextWithPrincipal(ctx, caller), encoded)
		refused, ok := errors.AsType[*ToolRefusalError](err)
		require.True(t, ok, "%s: expected a refusal, got %v", tool, err)
		var payload refusal
		require.NoError(t, json.Unmarshal([]byte(refused.Payload), &payload), refused.Payload)
		return payload
	}

	for _, tool := range []string{"get_mcp_readiness", "get_mcp_repair_plan", "attach_platform_mcp_identity_provider"} {
		notAdded := refuse(principal, tool, dashboardMCPID)
		require.Equal(t, "not_added_through_setup", notAdded.Code, tool)
		require.Contains(t, notAdded.Message, "not added through Platform setup, so "+tool+" does not apply", tool)

		otherMembers := refuse(otherMember, tool, remote.MCPID)
		require.Equal(t, "added_by_another_member", otherMembers.Code, tool)
		require.Contains(t, otherMembers.Message, "added by another member; only the member who added it can use "+tool, tool)

		require.Equal(t, "mcp_not_found", refuse(principal, tool, uuid.NewString()).Code, tool)
		require.Equal(t, "mcp_not_found", refuse(principal, tool, "not-an-id").Code, tool)
	}
	require.Empty(t, attachment.attachedProjects(), "no refused call connects a sign-in provider")

	// The member who added the server acts on it by the same mcp_id.
	readiness, err := invokeTool[GetMCPReadinessToolOutput](t, ContextWithPrincipal(ctx, principal), reg, "get_mcp_readiness", map[string]any{"project_slug": project.Slug, "mcp_id": remote.MCPID})
	require.NoError(t, err)
	require.Equal(t, remote.MCPID, readiness.MCPID)
	attached, err := invokeTool[AttachPlatformMCPIdentityProviderToolOutput](t, ContextWithPrincipal(ctx, principal), reg, "attach_platform_mcp_identity_provider", map[string]any{"project_slug": project.Slug, "mcp_id": remote.MCPID, "confirmed": true})
	require.NoError(t, err)
	require.Equal(t, remote.MCPID, attached.MCPID)
	require.Equal(t, []string{project.Slug}, attachment.attachedProjects())
}
