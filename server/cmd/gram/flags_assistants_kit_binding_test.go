package gram

import (
	"context"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/assistants"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/require"
)

type kitBindingDB struct {
	toolsetsrepo.DBTX
	endpoint     mcpendpointsrepo.McpEndpoint
	server       mcpserversrepo.McpServer
	meta         metamcprepo.MetaMcpServer
	issuer       usersessionsrepo.UserSessionIssuer
	organization string
	toolset      *toolsetsrepo.Toolset
	domain       *customdomainsrepo.CustomDomain
}

type kitBindingRow struct {
	values []any
	err    error
}

func (r kitBindingRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, value := range r.values {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(value))
	}
	return nil
}
func kitBindingValues(row any) []any {
	value := reflect.ValueOf(row)
	out := make([]any, value.NumField())
	for i := range out {
		out[i] = value.Field(i).Interface()
	}
	return out
}
func (db *kitBindingDB) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	switch {
	case strings.Contains(query, "-- name: GetMCPEndpointByProjectAndCustomDomainAndSlug"):
		if db.toolset != nil {
			return kitBindingRow{err: pgx.ErrNoRows}
		}
		return kitBindingRow{values: kitBindingValues(db.endpoint)}
	case strings.Contains(query, "-- name: GetMetaMCPServerByIDAndProjectID"):
		return kitBindingRow{values: kitBindingValues(db.meta)}
	case strings.Contains(query, "-- name: GetMCPServerByIDAndProjectID"):
		return kitBindingRow{values: kitBindingValues(db.server)}
	case strings.Contains(query, "-- name: GetToolsetByMcpSlugAndProjectAndCustomDomain"):
		if db.toolset != nil {
			return kitBindingRow{values: kitBindingValues(*db.toolset)}
		}
		return kitBindingRow{err: pgx.ErrNoRows}
	case strings.Contains(query, "-- name: GetCustomDomainByDomainAndOrganization"):
		if db.domain != nil {
			return kitBindingRow{values: kitBindingValues(*db.domain)}
		}
		return kitBindingRow{err: pgx.ErrNoRows}
	case strings.Contains(query, "-- name: GetKitUserSessionIssuerByID"):
		if db.organization == "other-org" {
			return kitBindingRow{err: pgx.ErrNoRows}
		}
		return kitBindingRow{values: kitBindingValues(db.issuer)}
	default:
		return kitBindingRow{err: pgx.ErrNoRows}
	}
}
func kitBindingURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	value, err := url.Parse(raw)
	require.NoError(t, err)
	return value
}

func TestKitMCPAuthoritativeBinding(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"valid", "platform-host-case", "custom-host-case", "wrong-issuer-id", "wrong-issuer-url", "foreign-origin", "ungated", "disabled", "meta-private-network", "meta-invalid-network", "meta-dual-network", "private-network", "invalid-network", "dual-network", "public-tunnel", "foreign-project", "foreign-issuer-tenant", "shared", "wrong-shared-pin", "auth-host", "local-relay", "escaped-path", "legacy", "legacy-disabled", "custom-domain", "inactive-domain"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			project, issuer, serverID := uuid.New(), uuid.New(), uuid.New()
			request := assistants.KitMCPProvisionRequest{ProjectID: project, UserSessionIssuerID: issuer, OrganizationID: "test-org"}
			hosts := authserver.Hosts{ServerURL: "https://api.example.com", AuthenticationHostBaseURL: "https://auth.example.com"}
			db := &kitBindingDB{
				endpoint:     mcpendpointsrepo.McpEndpoint{ProjectID: project, McpServerID: uuid.NullUUID{UUID: serverID, Valid: true}, Slug: "attached"},
				server:       mcpserversrepo.McpServer{ID: serverID, ProjectID: project, UserSessionIssuerID: uuid.NullUUID{UUID: issuer, Valid: true}, Visibility: "private"},
				issuer:       usersessionsrepo.UserSessionIssuer{ID: issuer, ProjectID: uuid.NullUUID{UUID: project, Valid: true}, AuthorizationServerMode: "endpoint"},
				organization: request.OrganizationID,
			}
			resource := kitBindingURL(t, "https://api.example.com/mcp/attached")
			expected := resource
			var localOrigin *url.URL
			good := false
			switch scenario {
			case "valid":
				good = true
			case "platform-host-case":
				resource = kitBindingURL(t, "https://API.EXAMPLE.COM/mcp/attached")
				expected = resource
				good = true
			case "custom-host-case":
				resource = kitBindingURL(t, "https://MCP.CUSTOMER.EXAMPLE/mcp/attached")
				expected = resource
				db.domain = &customdomainsrepo.CustomDomain{ID: uuid.New(), OrganizationID: request.OrganizationID, Domain: "mcp.customer.example", Verified: true, Activated: true}
				good = true
			case "wrong-issuer-id":
				request.UserSessionIssuerID = uuid.New()
			case "wrong-issuer-url":
				expected = kitBindingURL(t, "https://wrong.example.com/mcp/attached")
			case "foreign-origin":
				resource = kitBindingURL(t, "https://wrong.example.com/mcp/attached")
			case "ungated":
				db.server.UserSessionIssuerID.Valid = false
			case "disabled":
				db.server.Visibility = "disabled"
			case "meta-private-network", "meta-invalid-network", "meta-dual-network":
				db.endpoint.McpServerID = uuid.NullUUID{}
				db.endpoint.MetaMcpServerID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
				db.meta.UserSessionIssuerID = db.server.UserSessionIssuerID
				db.meta.NetworkAccessMode = pgtype.Text{String: map[string]string{"meta-private-network": "private_only", "meta-invalid-network": "invalid", "meta-dual-network": "dual"}[scenario], Valid: true}
				good = scenario == "meta-dual-network"
			case "private-network", "invalid-network", "dual-network":
				db.server.NetworkAccessMode = pgtype.Text{String: map[string]string{"private-network": "private_only", "invalid-network": "invalid", "dual-network": "dual"}[scenario], Valid: true}
				good = scenario == "dual-network"
			case "public-tunnel":
				db.server.Visibility = "public"
				db.server.TunneledMcpServerID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
			case "foreign-project":
				db.endpoint.ProjectID = uuid.New()
			case "foreign-issuer-tenant":
				db.organization = "other-org"
			case "shared":
				db.issuer.AuthorizationServerMode = "shared"
				db.issuer.UseAuthenticationHost = true
				expected = kitBindingURL(t, hosts.AuthenticationHostBaseURL+authserver.SharedPath(issuer))
				good = true
			case "wrong-shared-pin":
				db.issuer.AuthorizationServerMode = "shared"
				db.issuer.PinnedIssuerUrl = pgtype.Text{String: "https://unserved.example.com" + authserver.SharedPath(issuer), Valid: true}
				expected = kitBindingURL(t, db.issuer.PinnedIssuerUrl.String)
			case "auth-host":
				db.issuer.UseAuthenticationHost = true
				expected = kitBindingURL(t, hosts.AuthenticationHostBaseURL+resource.Path)
				good = true
			case "local-relay":
				resource = kitBindingURL(t, "https://host.docker.internal:8443/mcp/attached")
				expected = resource
				localOrigin = kitBindingURL(t, "https://host.docker.internal:8443")
				good = true
			case "legacy", "legacy-disabled":
				db.toolset = &toolsetsrepo.Toolset{ProjectID: project, McpEnabled: scenario == "legacy", UserSessionIssuerID: uuid.NullUUID{UUID: issuer, Valid: true}}
				good = scenario == "legacy"
			case "custom-domain", "inactive-domain":
				resource = kitBindingURL(t, "https://mcp.customer.example/mcp/attached")
				expected = resource
				db.domain = &customdomainsrepo.CustomDomain{ID: uuid.New(), OrganizationID: request.OrganizationID, Domain: resource.Hostname(), Verified: true, Activated: scenario == "custom-domain"}
				db.issuer.UseAuthenticationHost = true
				good = scenario == "custom-domain"
			case "escaped-path":
				resource = kitBindingURL(t, "https://api.example.com/mcp/%61ttached")
			}
			err := validateKitMCPBinding(t.Context(), db, request, resource, expected, hosts, localOrigin)
			if good {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestKitMCPPlatformOrigin(t *testing.T) {
	t.Parallel()
	hosts := authserver.Hosts{ServerURL: "https://api.example.com", AuthenticationHostBaseURL: "https://auth.example.com", PlatformHosts: map[string]string{"ai.example.com": "https://ai.example.com"}}
	require.True(t, kitMCPPlatformOrigin("https://api.example.com", hosts, nil))
	require.True(t, kitMCPPlatformOrigin("https://ai.example.com", hosts, nil))
	require.False(t, kitMCPPlatformOrigin("https://auth.example.com", hosts, nil))
	require.False(t, kitMCPPlatformOrigin("https://api.example.com:444", hosts, nil))
	require.False(t, kitMCPPlatformOrigin("https://host.docker.internal:8443", hosts, nil))
}
