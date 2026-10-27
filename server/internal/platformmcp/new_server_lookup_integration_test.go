package platformmcp

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type countingCatalogReadinessProber struct {
	calls atomic.Int32
}

func (p *countingCatalogReadinessProber) ProbeCatalogReadiness(_ context.Context, principal Principal, _, registrationID, _, _, _, _ uuid.UUID) (ProviderReadinessProbeResult, error) {
	p.calls.Add(1)
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

// A server that has never been checked reports its real state on the first
// readiness read instead of a degraded "temporarily unavailable" placeholder.
// Later reads use the stored result rather than checking again.
func TestReadinessToolChecksAServerThatHasNeverBeenChecked(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_readiness_first_probe")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn)
	require.NoError(t, err)
	gate := &testRegistrationGate{enabled: true}
	registrations := newRegistrationService(testCatalog{}, gate, store).
		WithDirectRemoteInspector(&testDirectRemoteInspector{inspection: DirectRemoteInspection{CanonicalURL: "https://remote.example.test/mcp", Transport: "streamable-http", Trust: "user_supplied_unreviewed"}})
	prober := &countingCatalogReadinessProber{}

	reg := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "platform-mcp-test"}, nil))
	registerRemoteRegistrationTool(reg, registrations, nil)
	registerReadinessTools(reg, NewReadinessService(store, gate, NewProviderAdapters(nil), allowOperationLimiter{}, allowBudget(), prober))
	callCtx := ContextWithPrincipal(ctx, principal)

	remote, err := invokeTool[RegisterRemoteMCPToolOutput](t, callCtx, reg, "register_remote_mcp", map[string]any{
		"project_slug":    project.Slug,
		"remote_url":      "https://remote.example.test/mcp",
		"idempotency_key": "readiness-first-probe",
	})
	require.NoError(t, err)

	first, err := invokeTool[GetMCPReadinessToolOutput](t, callCtx, reg, "get_mcp_readiness", map[string]any{"project_slug": project.Slug, "registration_id": remote.RegistrationID})
	require.NoError(t, err)
	require.Equal(t, ReadinessNeedsConfiguration, first.State)
	require.Equal(t, "fresh", first.Freshness)
	require.Equal(t, int32(1), prober.calls.Load())

	second, err := invokeTool[GetMCPReadinessToolOutput](t, callCtx, reg, "get_mcp_readiness", map[string]any{"project_slug": project.Slug, "registration_id": remote.RegistrationID})
	require.NoError(t, err)
	require.Equal(t, ReadinessNeedsConfiguration, second.State)
	require.Equal(t, int32(1), prober.calls.Load(), "a stored result must be read, not checked again")
}

// A server added by URL can be found by that URL after it is given its own
// name, both when choosing candidates for authorization and in the inventory.
func TestInventorySearchMatchesTheSourceURL(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_inventory_source_url")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn)
	require.NoError(t, err)
	registrations := newRegistrationService(testCatalog{}, &testRegistrationGate{enabled: true}, store).
		WithDirectRemoteInspector(&testDirectRemoteInspector{inspection: DirectRemoteInspection{CanonicalURL: "https://remote.example.test/mcp", Transport: "streamable-http", Trust: "user_supplied_unreviewed"}})
	reg := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "platform-mcp-test"}, nil))
	registerRemoteRegistrationTool(reg, registrations, nil)

	remote, err := invokeTool[RegisterRemoteMCPToolOutput](t, ContextWithPrincipal(ctx, principal), reg, "register_remote_mcp", map[string]any{
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
	require.Len(t, candidates, 1)
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
	require.Len(t, rows, 1)
	require.Equal(t, mcpServerID, rows[0].McpServerID)
	require.Equal(t, "Renamed server", rows[0].McpName.String)

	mcps := []MCP{mcpFromInventoryRow(rows[0], nil)}
	require.Len(t, inventoryQueryResult(mcps, query, 10), 1)
}
