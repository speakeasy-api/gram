package mcp_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	authclientfixtures "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
)

// An approved gateway connection records each member's configuration hash, so
// any change the hash covers asks the user for consent again. Route and
// credential-slot changes must; credential values and settings enforced live
// on every request must not.
func TestGatewayMemberConfigurationHashCoversRouteNotSecretValues(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	auth, _ := contextvalues.GetAuthContext(ctx)
	projectID := *auth.ProjectID
	_, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, auth.ActiveOrganizationID, "catalog-hash-"+uuid.NewString(), issuer.ID)
	headers := remotemcp.NewHeaders(ti.logger, ti.conn, ti.enc)

	updateHeader := func(header remotemcprepo.RemoteMcpServerHeader, name string, secret, setValue bool, value string) error {
		_, err := headers.UpdateServerHeader(ctx, remotemcprepo.UpdateServerHeaderParams{
			Name: name, Description: pgtype.Text{}, IsRequired: header.IsRequired, IsSecret: secret,
			SetValue: setValue, Value: pgtype.Text{String: value, Valid: setValue}, ValueFromRequestHeader: pgtype.Text{},
			ID: header.ID, ProjectID: projectID,
		})
		if err != nil {
			return fmt.Errorf("update header: %w", err)
		}
		return nil
	}
	updateRemote := func(remote remotemcprepo.RemoteMcpServer, transport, url string) error {
		_, err := remotemcprepo.New(ti.conn).UpdateServer(ctx, remotemcprepo.UpdateServerParams{
			ID: remote.ID, ProjectID: remote.ProjectID, Name: remote.Name, Slug: remote.Slug, TransportType: transport, Url: url,
		})
		if err != nil {
			return fmt.Errorf("update remote server: %w", err)
		}
		return nil
	}
	updateServer := func(server mcpserversrepo.McpServer, visibility string, network pgtype.Text) error {
		_, err := mcpserversrepo.New(ti.conn).UpdateMCPServer(ctx, mcpserversrepo.UpdateMCPServerParams{
			Name: server.Name, Slug: server.Slug, EnvironmentID: server.EnvironmentID, UserSessionIssuerID: server.UserSessionIssuerID,
			RemoteMcpServerID: server.RemoteMcpServerID, TunneledMcpServerID: server.TunneledMcpServerID, ToolsetID: server.ToolsetID,
			UnproxiedMcpServerID: server.UnproxiedMcpServerID, ToolVariationsGroupID: server.ToolVariationsGroupID,
			Visibility: visibility, NetworkAccessModeSet: true, NetworkAccessMode: network, ID: server.ID, ProjectID: server.ProjectID,
		})
		if err != nil {
			return fmt.Errorf("update server: %w", err)
		}
		return nil
	}
	hash := func(t *testing.T, serverID uuid.UUID) string {
		t.Helper()
		rows, err := mcpserversrepo.New(ti.conn).ListMCPServerConsentConfigurations(ctx, mcpserversrepo.ListMCPServerConsentConfigurationsParams{
			OrganizationID: auth.ActiveOrganizationID, McpServerIds: []uuid.UUID{serverID},
		})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.NotEmpty(t, rows[0].ConfigurationHash)
		return rows[0].ConfigurationHash
	}

	cases := []struct {
		name    string
		changes bool
		mutate  func(m hashMember) error
	}{
		{name: "rotate secret header value", changes: false, mutate: func(m hashMember) error {
			return updateHeader(m.secret, m.secret.Name, true, true, "rotated")
		}},
		{name: "re-save unchanged secret header value", changes: false, mutate: func(m hashMember) error {
			return updateHeader(m.secret, m.secret.Name, true, true, "first")
		}},
		{name: "change visibility", changes: false, mutate: func(m hashMember) error {
			visibility := "public"
			if m.server.Visibility == visibility {
				visibility = "private"
			}
			return updateServer(m.server, visibility, m.server.NetworkAccessMode)
		}},
		{name: "change network access", changes: false, mutate: func(m hashMember) error {
			mode := "private_only"
			if m.server.NetworkAccessMode.String == mode {
				mode = "dual"
			}
			return updateServer(m.server, m.server.Visibility, pgtype.Text{String: mode, Valid: true})
		}},
		{name: "change plain header value", changes: true, mutate: func(m hashMember) error {
			return updateHeader(m.plain, m.plain.Name, false, true, "beta")
		}},
		{name: "rename secret header", changes: true, mutate: func(m hashMember) error {
			return updateHeader(m.secret, "X-Other-Key", true, false, "")
		}},
		{name: "mark plain header secret", changes: true, mutate: func(m hashMember) error {
			return updateHeader(m.plain, m.plain.Name, true, true, "alpha")
		}},
		{name: "add header", changes: true, mutate: func(m hashMember) error {
			_, err := headers.CreateServerHeader(ctx, remotemcprepo.CreateServerHeaderParams{
				Name: "X-Region", Description: pgtype.Text{}, IsRequired: false, IsSecret: false,
				Value: pgtype.Text{String: "eu", Valid: true}, ValueFromRequestHeader: pgtype.Text{},
				RemoteMcpServerID: m.remote.ID, ProjectID: projectID,
			})
			if err != nil {
				return fmt.Errorf("create header: %w", err)
			}
			return nil
		}},
		{name: "change URL", changes: true, mutate: func(m hashMember) error {
			return updateRemote(m.remote, m.remote.TransportType, m.remote.Url+"/v2")
		}},
		{name: "change transport", changes: true, mutate: func(m hashMember) error {
			return updateRemote(m.remote, "sse", m.remote.Url)
		}},
		{name: "change upstream auth requirement", changes: true, mutate: func(m hashMember) error {
			return authclientfixtures.New(ti.conn).SetRequiresUpstreamAuthFixture(ctx, authclientfixtures.SetRequiresUpstreamAuthFixtureParams{
				RequiresUpstreamAuth: pgtype.Bool{Bool: !m.server.RequiresUpstreamAuth.Bool, Valid: true}, ID: m.server.ID,
			})
		}},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			slug := fmt.Sprintf("member-%d", i)
			serverID := seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, gateway.ID, slug, slug, int32(i+1), "https://upstream.example.test/"+slug)
			m := seedHashMember(t, ctx, ti, headers, projectID, serverID)
			before := hash(t, serverID)
			require.NoError(t, tc.mutate(m))
			if tc.changes {
				require.NotEqual(t, before, hash(t, serverID))
			} else {
				require.Equal(t, before, hash(t, serverID))
			}
		})
	}
}

type hashMember struct {
	server mcpserversrepo.McpServer
	remote remotemcprepo.RemoteMcpServer
	secret remotemcprepo.RemoteMcpServerHeader
	plain  remotemcprepo.RemoteMcpServerHeader
}

// seedHashMember gives a member one secret and one plain header. Both are
// optional: a required secret header would make the member need sign-in.
func seedHashMember(t *testing.T, ctx context.Context, ti *testInstance, headers *remotemcp.Headers, projectID, serverID uuid.UUID) hashMember {
	t.Helper()
	server, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: projectID})
	require.NoError(t, err)
	remote, err := remotemcprepo.New(ti.conn).GetServerByID(ctx, remotemcprepo.GetServerByIDParams{ID: server.RemoteMcpServerID.UUID, ProjectID: projectID})
	require.NoError(t, err)
	create := func(name, value string, secret bool) remotemcprepo.RemoteMcpServerHeader {
		header, err := headers.CreateServerHeader(ctx, remotemcprepo.CreateServerHeaderParams{
			Name: name, Description: pgtype.Text{}, IsRequired: false, IsSecret: secret,
			Value: pgtype.Text{String: value, Valid: true}, ValueFromRequestHeader: pgtype.Text{},
			RemoteMcpServerID: remote.ID, ProjectID: projectID,
		})
		require.NoError(t, err)
		return header
	}
	return hashMember{server: server, remote: remote, secret: create("X-Api-Key", "first", true), plain: create("X-Tenant", "alpha", false)}
}

// Rotating a member's secret header keeps an approved connection usable: the
// next call runs against the upstream instead of asking for consent again.
func TestConnectionCatalogSecretRotationKeepsApproval(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	auth, _ := contextvalues.GetAuthContext(ctx)
	projectID := *auth.ProjectID
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "catalog-rotation-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, auth.ActiveOrganizationID, slug, issuer.ID)
	upstream := newCatalogUpstream(t, 1)
	memberID := seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, gateway.ID, "Source", "source", 1, upstream.url)
	server, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: memberID, ProjectID: projectID})
	require.NoError(t, err)

	// The member signs in with a static credential whose secret header lives
	// on the remote server.
	headers := remotemcp.NewHeaders(ti.logger, ti.conn, ti.enc)
	secret, err := headers.CreateServerHeader(ctx, remotemcprepo.CreateServerHeaderParams{
		Name: "X-Api-Key", Description: pgtype.Text{}, IsRequired: false, IsSecret: true,
		Value: pgtype.Text{String: "first", Valid: true}, ValueFromRequestHeader: pgtype.Text{},
		RemoteMcpServerID: server.RemoteMcpServerID.UUID, ProjectID: projectID,
	})
	require.NoError(t, err)
	fixtures := authclientfixtures.New(ti.conn)
	authClientID, err := fixtures.SeedAuthClientFixture(ctx, authclientfixtures.SeedAuthClientFixtureParams{
		OrganizationID: auth.ActiveOrganizationID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true},
		ClientType: "static_credential", Source: "admin", RemoteSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	require.NoError(t, fixtures.SeedAuthClientHeaderFixture(ctx, authclientfixtures.SeedAuthClientHeaderFixtureParams{
		AuthClientID: authClientID, OrganizationID: auth.ActiveOrganizationID, Name: "X-Api-Key", Value: pgtype.Text{String: "first", Valid: true},
	}))
	require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientfixtures.SeedServerAuthClientLinkFixtureParams{
		ProjectID: projectID, OrganizationID: auth.ActiveOrganizationID, McpServerID: memberID, AuthClientID: authClientID,
	}))
	ti.service.manual = true
	connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeDirect, nil)

	_, err = headers.UpdateServerHeader(ctx, remotemcprepo.UpdateServerHeaderParams{
		Name: secret.Name, Description: pgtype.Text{}, IsRequired: secret.IsRequired, IsSecret: true,
		SetValue: true, Value: pgtype.Text{String: "rotated", Valid: true}, ValueFromRequestHeader: pgtype.Text{},
		ID: secret.ID, ProjectID: projectID,
	})
	require.NoError(t, err)

	body := callGatewayTool(t, ti, slug, connection.token, "source--tool_000", map[string]any{})
	require.NotContains(t, body, "configuration changed")
	require.Contains(t, body, "executed live")
	require.EqualValues(t, 1, upstream.calls.Load())
}
