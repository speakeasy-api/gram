package platformmcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
)

func TestDeleteMCPToolIsExternalOrgAdminAndKeepsItsSchemaWhenUnavailable(t *testing.T) {
	t.Parallel()

	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "delete-mcp-live", Version: "0.0.1"}, nil))
	unavailable := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "delete-mcp-unavailable", Version: "0.0.1"}, nil))
	registerLifecycleDeleteTool(live, &LifecycleDeleteService{})
	registerLifecycleDeleteTool(unavailable, nil)

	liveDescriptor := descriptorByName(t, live, deleteMCPToolName)
	unavailableDescriptor := descriptorByName(t, unavailable, deleteMCPToolName)
	require.JSONEq(t, string(liveDescriptor.InputSchema), string(unavailableDescriptor.InputSchema))
	require.Equal(t, liveDescriptor.Meta, unavailableDescriptor.Meta)
	require.Equal(t, ExternalAuthorizationOrgAdmin, liveDescriptor.Meta.Authorization)
	require.Equal(t, externalOnly, liveDescriptor.Meta.Audiences, "the managed assistant has no confirmation flow for deleting another person's server")
	require.Equal(t, ProjectScopeExplicit, liveDescriptor.Meta.ProjectScope)
	for _, field := range []string{"project_id", "mcp_id", "expected_version", "idempotency_key", "confirmed"} {
		require.Contains(t, string(liveDescriptor.InputSchema), field)
	}

	_, err := unavailableDescriptor.Invoke(t.Context(), []byte(`{"project_id":"p","mcp_id":"m","expected_version":"v","idempotency_key":"k","confirmed":true}`))
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, unavailableCode, refusal.Code)
}

func TestDeleteMCPRefusesAnUnconfirmedCallBeforeReadingAnything(t *testing.T) {
	t.Parallel()

	// The pool and engine are never opened: an unconfirmed call is refused
	// before any read, authorization check, or delete.
	deleted := false
	service := &LifecycleDeleteService{db: new(pgxpool.Pool), authorizer: new(authz.Engine), deleter: func(context.Context, uuid.UUID) error {
		deleted = true
		return nil
	}, key: []byte("k"), now: nil}
	_, err := service.Delete(t.Context(), Principal{UserID: "user", OrganizationID: "org"}, DeleteMCPInput{ProjectID: uuid.NewString(), MCPID: uuid.NewString(), ExpectedVersion: "v", IdempotencyKey: "k", Confirmed: false})
	var mutation *RiskMutationError
	require.ErrorAs(t, err, &mutation)
	require.Equal(t, "invalid_request", mutation.Code)
	require.ErrorIs(t, err, ErrLifecycleDeleteInvalid)
	require.False(t, deleted)

	var uncomposed *LifecycleDeleteService
	_, err = uncomposed.Delete(t.Context(), Principal{UserID: "user", OrganizationID: "org"}, DeleteMCPInput{Confirmed: true})
	require.ErrorAs(t, err, &mutation)
	require.Equal(t, unavailableCode, mutation.Code)

	result, ok := deleteMCPToolResult(deleteMCPInvalid("Confirm deleting this exact server."))
	require.True(t, ok)
	require.True(t, result.IsError)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, text.Text, `"code":"invalid_request"`)

	result, ok = deleteMCPToolResult(ErrLifecycleDeleteMissing)
	require.True(t, ok)
	text, ok = result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, text.Text, `"code":"not_found"`)
}

// find_mcp and get_mcp must hand out the exact version delete_mcp checks, for
// every server rather than only Platform registrations.
func TestInventoryVersionMatchesTheDeleteVersionForUnregisteredServers(t *testing.T) {
	t.Parallel()

	reader := new(PostgresReader)
	reader.metadataVersionKey = lifecycleMetadataVersionKey("shared-key")
	service := &LifecycleDeleteService{db: nil, authorizer: nil, deleter: nil, key: lifecycleMetadataVersionKey("shared-key"), now: nil}

	server := mcpserversrepo.McpServer{ID: uuid.New(), ProjectID: uuid.New(), Name: conv.ToPGText("Dashboard Slack"), Slug: conv.ToPGText("dashboard-slack"), Visibility: "private"}
	inventory := MCP{ID: server.ID.String(), ProjectID: server.ProjectID.String(), Name: "Dashboard Slack", Slug: "dashboard-slack", Visibility: "private", Registration: nil}
	reader.setInventoryVersion(&inventory)

	require.NotEmpty(t, inventory.Version)
	require.Equal(t, service.version(server), inventory.Version)
}
