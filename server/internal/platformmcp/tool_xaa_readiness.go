//nolint:exhaustruct // MCP manifests and service payloads use optional zero values.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	srv "github.com/speakeasy-api/gram/server/gen/okta_resource_connections"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

type xaaConnectionsReader interface {
	List(context.Context, *srv.ListPayload) (*srv.ListOktaResourceConnectionsResult, error)
}

type xaaReadinessService struct {
	connections xaaConnectionsReader
	enabled     FeatureChecker
	identity    xaaIdentityInspector
}

// xaaIdentityInspector reads identity chaining bindings and the federated
// sign-in callback for one server the List snapshot already authorized.
type xaaIdentityInspector interface {
	Inspect(ctx context.Context, organizationID string, projectID, serverID, remoteSessionIssuerID uuid.UUID, resource string) (XAAIdentityChaining, *string, error)
}

// WithXAAReadiness reuses the dashboard service's org-admin authorization,
// MCP visibility filtering and readiness derivation. List itself is not gated,
// so the agent surface checks the same rollout flag before reading it.
func (r *PostgresReader) WithXAAReadiness(connections xaaConnectionsReader, flags feature.Provider) *PostgresReader {
	r.xaaReadiness = &xaaReadinessService{connections: connections, enabled: func(ctx context.Context, orgID string) (bool, error) {
		if flags == nil {
			return false, ErrUnavailable
		}
		slug, err := NewPostgresOrganizationSlugResolver(r.db).OrganizationSlug(ctx, orgID)
		if err != nil {
			return false, err
		}
		return flags.IsFlagEnabled(ctx, feature.FlagOktaConnections, orgID, feature.OrgProjectGroups(slug, ""))
	}}
	return r
}

// WithXAAIdentityChaining adds identity chaining bindings and the federated
// sign-in callback to XAA readiness. outbound is the pinned callback origin
// for clients that recorded none.
func (r *PostgresReader) WithXAAIdentityChaining(governor IdentityChainingGovernor, outbound *url.URL) *PostgresReader {
	if r.xaaReadiness != nil && r.db != nil {
		r.xaaReadiness.identity = &postgresXAAIdentityInspector{db: r.db, governor: governor, origins: remotesessions.CallbackOrigins{Outbound: outbound, Registration: nil}}
	}
	return r
}

type GetXAAReadinessInput struct {
	ProjectID   string `json:"project_id" jsonschema:"exact project ID that owns the MCP server"`
	MCPServerID string `json:"mcp_server_id" jsonschema:"exact MCP server ID, not a Platform MCP registration or legacy toolset ID"`
}

// Deliberately omit organization totals, other servers, provider app IDs,
// client IDs, URLs and administrator-entered labels from List.
type GetXAAReadinessOutput struct {
	ProjectID           string  `json:"project_id"`
	MCPServerID         string  `json:"mcp_server_id"`
	State               string  `json:"state"`
	Pending             bool    `json:"pending"`
	NotApplicableReason *string `json:"not_applicable_reason,omitempty"`
	BrokenReason        *string `json:"broken_reason,omitempty"`
	ObservedResult      *string `json:"observed_result,omitempty"`
	ObservedAt          *string `json:"observed_at,omitempty"`
	// Absent when this deployment cannot inspect identity chaining.
	IdentityChaining     *XAAIdentityChaining `json:"identity_chaining,omitempty"`
	FederatedCallbackURL *string              `json:"federated_callback_url,omitempty" jsonschema:"redirect URI to register in the identity provider sign-in app for this server's user sign-in"`
}

type XAAIdentityChaining struct {
	Served   bool                         `json:"served" jsonschema:"true when exactly one ready binding serves this server's upstream at runtime"`
	Bindings []XAAIdentityChainingBinding `json:"bindings"`
}

type XAAIdentityChainingBinding struct {
	State            string   `json:"state"`
	Stage            string   `json:"stage"`
	Remediation      string   `json:"remediation,omitempty"`
	Retryable        bool     `json:"retryable"`
	GrantSource      string   `json:"grant_source"`
	RequestedScopes  []string `json:"requested_scopes" jsonschema:"scopes identity chaining requests, after dropping OpenID Connect scopes"`
	ConfiguredScopes []string `json:"configured_scopes" jsonschema:"scopes configured on the binding"`
}

func registerXAAReadinessTool(reg *Registrar, service *xaaReadinessService) {
	addTool(reg, &mcp.Tool{
		Name: "get_xaa_readiness", Title: "Check Cross-App Access Readiness",
		Description: "Inspect stored Okta Cross-App Access (XAA) readiness for one exact project and MCP server. Reports the dashboard's derived state and exchange evidence; does not probe, connect or change anything. Connected means administrator-confirmed, not verified. identity_chaining lists this server's identity-chaining bindings with state, remediation, configured scopes and the scopes actually requested after dropping OpenID Connect scopes; served is true only when exactly one ready binding serves the upstream at runtime. federated_callback_url is the redirect URI to register in the identity provider sign-in app for this server's user sign-in. Changing bindings is not available here. Requires organization administration and read access to the server. Unlike get_mcp_readiness, this checks organization XAA configuration, not a registration's provider readiness.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, DiscoveryScopes: discoveryMCPRead, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetXAAReadinessInput) (*mcp.CallToolResult, GetXAAReadinessOutput, error) {
		// External-only: the service reads an org-wide snapshot under a live member
		// user context. Project assistants have no reviewed org-admin authority for
		// that service. Never expose that snapshot to a managed assistant.
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, GetXAAReadinessOutput{}, err
		}
		if principal.surface() == SurfaceProjectAssistant {
			return xaaReadinessRefusal("forbidden", "XAA readiness requires an external organization administrator.")
		}
		projectID, err := uuid.Parse(input.ProjectID)
		if err != nil || projectID == uuid.Nil {
			return xaaReadinessRefusal("invalid_request", "Provide an exact project ID and MCP server ID.")
		}
		serverID, err := uuid.Parse(input.MCPServerID)
		if err != nil || serverID == uuid.Nil {
			return xaaReadinessRefusal("invalid_request", "Provide an exact project ID and MCP server ID.")
		}
		if service == nil || service.connections == nil || service.enabled == nil {
			return xaaReadinessRefusal(unavailableCode, "XAA readiness is unavailable on this server.")
		}
		enabled, err := service.enabled(ctx, principal.OrganizationID)
		if err != nil {
			return xaaReadinessRefusal(unavailableCode, "XAA readiness is temporarily unavailable.")
		}
		if !enabled {
			return xaaReadinessRefusal(unavailableCode, "XAA readiness is not enabled for this organization.")
		}
		result, err := service.connections.List(ctx, &srv.ListPayload{IncludeAll: true})
		if err != nil {
			if shareable, ok := errors.AsType[*oops.ShareableError](err); ok {
				if shareable.Code == oops.CodeForbidden || shareable.Code == oops.CodeUnauthorized {
					return xaaReadinessRefusal("forbidden", "XAA readiness requires organization administration and server read access.")
				}
				if shareable.Code == oops.CodeFailedPrecondition {
					return xaaReadinessRefusal("setup_required", "Connect an identity provider before checking XAA readiness.")
				}
			}
			return xaaReadinessRefusal(unavailableCode, "XAA readiness is temporarily unavailable.")
		}
		if result != nil {
			for _, row := range result.Servers {
				if row != nil && row.ProjectID == projectID.String() && row.McpServerID == serverID.String() {
					output := GetXAAReadinessOutput{ProjectID: row.ProjectID, MCPServerID: row.McpServerID, State: row.State, Pending: row.Pending, NotApplicableReason: row.NotApplicableReason, BrokenReason: row.BrokenReason, ObservedResult: row.ObservedResult, ObservedAt: row.ObservedAt}
					if service.identity != nil {
						issuerID := uuid.Nil
						if row.IssuerID != nil {
							issuerID, _ = uuid.Parse(*row.IssuerID)
						}
						chaining, callback, err := service.identity.Inspect(ctx, principal.OrganizationID, projectID, serverID, issuerID, row.ResourceIndicator)
						if err != nil {
							return xaaReadinessRefusal(unavailableCode, "XAA readiness is temporarily unavailable.")
						}
						output.IdentityChaining, output.FederatedCallbackURL = &chaining, callback
					}
					return nil, output, nil
				}
			}
		}
		return xaaReadinessRefusal("not_found", "That project or eligible MCP server is not available to you.")
	})
}

func xaaReadinessRefusal(code, message string) (*mcp.CallToolResult, GetXAAReadinessOutput, error) {
	payload, err := json.Marshal(featureUnavailableResult{Code: code, Feature: "xaa_readiness", Message: message})
	if err != nil {
		return nil, GetXAAReadinessOutput{}, fmt.Errorf("marshal XAA readiness refusal: %w", err)
	}
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}}}, GetXAAReadinessOutput{}, nil
}

type postgresXAAIdentityInspector struct {
	db       *pgxpool.Pool
	governor IdentityChainingGovernor
	origins  remotesessions.CallbackOrigins
}

func (i *postgresXAAIdentityInspector) Inspect(ctx context.Context, organizationID string, projectID, serverID, remoteSessionIssuerID uuid.UUID, resource string) (XAAIdentityChaining, *string, error) {
	out := XAAIdentityChaining{Served: false, Bindings: []XAAIdentityChainingBinding{}}
	server, err := mcpserversrepo.New(i.db).GetMCPServerByLiveProjectForOrganization(ctx, mcpserversrepo.GetMCPServerByLiveProjectForOrganizationParams{OrganizationID: organizationID, ID: serverID, ProjectID: projectID})
	if err != nil {
		return out, nil, fmt.Errorf("load XAA readiness server: %w", err)
	}
	if !server.UserSessionIssuerID.Valid {
		return out, nil, nil
	}
	userIssuerID := server.UserSessionIssuerID.UUID
	var callback *string
	q := remotesessionsrepo.New(i.db)
	trusted, err := q.GetEMAChainingUserIssuer(ctx, remotesessionsrepo.GetEMAChainingUserIssuerParams{ID: userIssuerID, OrganizationID: conv.ToPGText(organizationID)})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return out, nil, fmt.Errorf("load XAA readiness sign-in issuer: %w", err)
	default:
		client, err := q.GetRemoteSessionClientByID(ctx, remotesessionsrepo.GetRemoteSessionClientByIDParams{ID: trusted.TrustedRemoteSessionClientID.UUID, ProjectID: projectID, OrganizationID: organizationID})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return out, nil, fmt.Errorf("load XAA readiness sign-in client: %w", err)
		default:
			if origin := i.origins.ForClient(client.RemoteSessionClient.CallbackBaseUrl); origin != nil {
				callback = new(remotesessions.FederatedIDPCallbackURL(origin, client.RemoteSessionClient.ID))
			}
		}
	}
	if remoteSessionIssuerID == uuid.Nil || resource == "" {
		return out, callback, nil
	}
	results, err := remotesessions.InspectIdentityChainingForTenant(ctx, i.db, projectID, organizationID, userIssuerID, remoteSessionIssuerID, resource)
	if err != nil {
		return out, nil, fmt.Errorf("inspect XAA readiness identity chaining: %w", err)
	}
	for _, r := range results {
		out.Bindings = append(out.Bindings, XAAIdentityChainingBinding{State: r.State, Stage: r.Stage, Remediation: r.Remediation, Retryable: r.Retryable, GrantSource: r.GrantSource, RequestedScopes: append([]string{}, identitychaining.GrantScopes(r.Scopes)...), ConfiguredScopes: append([]string{}, r.Scopes...)})
	}
	if i.governor != nil {
		if req, ok := identitychaining.NewUpstreamRequest(organizationID, projectID, userIssuerID, resource, server.TunneledMcpServerID.Valid, server.RemoteSessionIssuerID); ok {
			_, out.Served = i.governor.Serves(ctx, req)
		}
	}
	return out, callback, nil
}
