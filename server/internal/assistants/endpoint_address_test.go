package assistants

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func (f hostedAttachmentFixture) customDomain(t *testing.T, orgID, domain string, verified, activated bool) uuid.UUID {
	t.Helper()
	q := customdomainsrepo.New(f.db)
	created, err := q.CreateCustomDomain(t.Context(), customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: orgID, Domain: domain, IngressName: pgtype.Text{}, CertSecretName: pgtype.Text{},
		ProvisionerKind: "ingress", IpAllowlist: []string{},
	})
	require.NoError(t, err)
	_, err = q.UpdateCustomDomain(t.Context(), customdomainsrepo.UpdateCustomDomainParams{
		Verified: verified, Activated: activated,
		IngressName: pgtype.Text{}, CertSecretName: pgtype.Text{}, ProvisionerKind: "ingress", ID: created.ID,
	})
	require.NoError(t, err)
	return created.ID
}

// moveHostedToDomain re-addresses the hosted toolset onto a custom domain, as a toolset save does.
func (f hostedAttachmentFixture) moveHostedToDomain(t *testing.T, domainID uuid.UUID) {
	t.Helper()
	ts, err := toolsetsrepo.New(f.db).UpdateToolset(t.Context(), toolsetsrepo.UpdateToolsetParams{
		Name: f.hosted.Name, DefaultEnvironmentSlug: f.hosted.DefaultEnvironmentSlug, McpSlug: f.hosted.McpSlug,
		McpIsPublic: f.hosted.McpIsPublic, CustomDomainID: uuid.NullUUID{UUID: domainID, Valid: true}, McpEnabled: true,
		ToolSelectionMode: f.hosted.ToolSelectionMode, Slug: f.hosted.Slug, ProjectID: f.projectID,
	})
	require.NoError(t, err)
	tx := testenv.BeginTx(t, t.Context(), f.db)
	_, err = hostedmcp.BackfillSync(t.Context(), tx, newTestAuditLogger(), hostedmcp.SystemActor("test"), ts)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
}

func (f hostedAttachmentFixture) retirePlatformEndpoints(t *testing.T) {
	t.Helper()
	_, err := f.db.Exec(t.Context(), `UPDATE mcp_endpoints SET deleted_at = clock_timestamp() WHERE mcp_server_id = $1 AND project_id = $2`, f.remote.ID, f.projectID) //nolint:glint // notestingrawsql: retire the platform endpoint
	require.NoError(t, err)
}

func (f hostedAttachmentFixture) addRemoteEndpoint(t *testing.T, domainID uuid.NullUUID, slug string) {
	t.Helper()
	_, err := mcpendpointsrepo.New(f.db).CreateMCPEndpoint(t.Context(), mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: f.projectID, CustomDomainID: domainID, McpServerID: uuid.NullUUID{UUID: f.remote.ID, Valid: true}, Slug: slug,
	})
	require.NoError(t, err)
}

// A hosted server on a live custom domain is dialed at its own endpoint, not the platform alias.
func TestAssistantHostedServerOnCustomDomainUsesEndpointURL(t *testing.T) {
	t.Parallel()
	f := newHostedAttachmentFixture(t, "endpoint_addr_hosted_domain")
	f.moveHostedToDomain(t, f.customDomain(t, "org-test", "mcp.example.test", true, true))
	f.addServerRow(t, f.hosted.ID, uuid.NullUUID{})

	require.Equal(t, []runtimeMCPServer{{
		ID:      "billing",
		URL:     "https://mcp.example.test/mcp/org-test-billing-x1",
		Headers: map[string]string{"Gram-Environment": "default-env"},
	}}, f.runtime(t))

	record, err := f.core.CreateAssistant(t.Context(), "org-test", f.projectID, "user-1", "Second", "openai/gpt-4o-mini", "", nil,
		[]*types.AssistantMCPServerRef{{McpServerSlug: "org-test-billing-x1"}}, 300, 1, StatusActive, false, allowAllMCPEnable)
	require.NoError(t, err, "a custom-domain endpoint is a Speakeasy-hosted endpoint")
	f.assistant = record.ID
	require.Equal(t, "https://mcp.example.test/mcp/org-test-billing-x1", f.runtime(t)[0].URL)
}

// A hosted server whose domain is not live keeps its legacy alias, but new attachments need a live endpoint.
func TestAssistantHostedServerOnInactiveDomainKeepsLegacyAlias(t *testing.T) {
	t.Parallel()
	f := newHostedAttachmentFixture(t, "endpoint_addr_hosted_inactive")
	f.moveHostedToDomain(t, f.customDomain(t, "org-test", "mcp.example.test", true, false))
	f.addServerRow(t, f.hosted.ID, uuid.NullUUID{})

	servers := f.runtime(t)
	require.Len(t, servers, 1)
	require.Equal(t, "https://gram.test/mcp/org-test-billing-x1", servers[0].URL)

	_, err := f.core.CreateAssistant(t.Context(), "org-test", f.projectID, "user-1", "Second", "openai/gpt-4o-mini", "", nil,
		[]*types.AssistantMCPServerRef{{McpServerSlug: "org-test-billing-x1"}}, 300, 1, StatusActive, false, allowAllMCPEnable)
	require.ErrorContains(t, err, "has no Speakeasy-hosted MCP endpoint")
}

// Only a verified, activated, live domain addresses a server.
func TestAssistantServerDomainGate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                      string
		verified, activated, drop bool
		wantURL                   string
	}{
		{name: "live", verified: true, activated: true, wantURL: "https://mcp.example.test/mcp/remote-on-domain"},
		{name: "unverified", verified: false, activated: true},
		{name: "inactive", verified: true, activated: false},
		{name: "deleted", verified: true, activated: true, drop: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newHostedAttachmentFixture(t, "endpoint_addr_gate_"+tc.name)
			domainID := f.customDomain(t, "org-test", "mcp.example.test", tc.verified, tc.activated)
			f.retirePlatformEndpoints(t)
			f.addRemoteEndpoint(t, uuid.NullUUID{UUID: domainID, Valid: true}, "remote-on-domain")
			if tc.drop {
				_, err := f.db.Exec(t.Context(), `UPDATE custom_domains SET deleted_at = clock_timestamp() WHERE id = $1`, domainID) //nolint:glint // notestingrawsql: soft-delete the domain
				require.NoError(t, err)
			}

			_, err := f.core.CreateAssistant(t.Context(), "org-test", f.projectID, "user-1", "Second", "openai/gpt-4o-mini", "", nil,
				[]*types.AssistantMCPServerRef{{McpServerSlug: "remote"}}, 300, 1, StatusActive, false, allowAllMCPEnable)
			if tc.wantURL == "" {
				require.ErrorContains(t, err, "has no Speakeasy-hosted MCP endpoint")
				f.addServerRow(t, f.remote.ID, uuid.NullUUID{})
				require.Empty(t, f.runtime(t))
				return
			}
			require.NoError(t, err)
			f.addServerRow(t, f.remote.ID, uuid.NullUUID{})
			servers := f.runtime(t)
			require.Len(t, servers, 1)
			require.Equal(t, tc.wantURL, servers[0].URL)
		})
	}
}

// A platform endpoint wins over a custom-domain one, so existing URLs do not move.
func TestAssistantServerPrefersPlatformEndpoint(t *testing.T) {
	t.Parallel()
	f := newHostedAttachmentFixture(t, "endpoint_addr_prefers_platform")
	domainID := f.customDomain(t, "org-test", "mcp.example.test", true, true)
	f.addRemoteEndpoint(t, uuid.NullUUID{UUID: domainID, Valid: true}, "remote-on-domain")
	_, err := f.db.Exec(t.Context(), `UPDATE mcp_endpoints SET created_at = created_at - interval '1 day' WHERE slug = 'remote-on-domain' AND project_id = $1`, f.projectID) //nolint:glint // notestingrawsql: make the domain endpoint the oldest
	require.NoError(t, err)
	f.addServerRow(t, f.remote.ID, uuid.NullUUID{})

	servers := f.runtime(t)
	require.Len(t, servers, 1)
	require.Equal(t, "https://gram.test/mcp/team-remote", servers[0].URL)
	view := f.view(t)
	require.Len(t, view.McpServers, 1)
	require.Equal(t, "team-remote", *view.McpServers[0].EndpointSlug)
}

// A server addressed only on a custom domain is dialed there; the API keeps
// endpoint_slug for platform addresses only.
func TestAssistantServerOnlyOnCustomDomain(t *testing.T) {
	t.Parallel()
	f := newHostedAttachmentFixture(t, "endpoint_addr_remote_domain_only")
	domainID := f.customDomain(t, "org-test", "mcp.example.test", true, true)
	f.retirePlatformEndpoints(t)
	f.addRemoteEndpoint(t, uuid.NullUUID{UUID: domainID, Valid: true}, "remote-on-domain")
	f.addServerRow(t, f.remote.ID, uuid.NullUUID{})

	servers := f.runtime(t)
	require.Len(t, servers, 1)
	require.Equal(t, "https://mcp.example.test/mcp/remote-on-domain", servers[0].URL)
	view := f.view(t)
	require.Len(t, view.McpServers, 1)
	require.Nil(t, view.McpServers[0].EndpointSlug)
}

// Another organization's domain never addresses this project's server.
func TestAssistantServerIgnoresForeignDomain(t *testing.T) {
	t.Parallel()
	f := newHostedAttachmentFixture(t, "endpoint_addr_foreign_domain")
	domainID := f.customDomain(t, "org-other", "mcp.other.test", true, true)
	f.retirePlatformEndpoints(t)
	f.addRemoteEndpoint(t, uuid.NullUUID{UUID: domainID, Valid: true}, "remote-on-foreign")
	f.addServerRow(t, f.remote.ID, uuid.NullUUID{})

	require.Empty(t, f.runtime(t))
}
