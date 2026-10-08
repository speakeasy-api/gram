//nolint:exhaustruct // MCP manifests and service payloads use optional zero values.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	srv "github.com/speakeasy-api/gram/server/gen/okta_resource_connections"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	idpc "github.com/speakeasy-api/gram/server/internal/identityproviderconnections"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

type xaaConnectionsReader interface {
	List(context.Context, *srv.ListPayload) (*srv.ListOktaResourceConnectionsResult, error)
}

type oktaSignInReader interface {
	Read(ctx context.Context, organizationID string) (*idpc.SignInSetup, error)
}

type xaaReadinessService struct {
	logger      *slog.Logger
	connections xaaConnectionsReader
	signIn      oktaSignInReader
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
func (r *PostgresReader) WithXAAReadiness(connections xaaConnectionsReader, signIn oktaSignInReader, flags feature.Provider) *PostgresReader {
	r.xaaReadiness = &xaaReadinessService{logger: r.logger, connections: connections, signIn: signIn, enabled: func(ctx context.Context, orgID string) (bool, error) {
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
// client IDs, scopes, URLs and administrator-entered labels from List.
// Okta sign-in carries only the public values Okta must be given.
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
	OktaSignIn           *OktaSignInStatus    `json:"okta_sign_in,omitempty" jsonschema:"organization Okta sign-in setup, which XAA relies on; omitted when it could not be read"`
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

type OktaSignInStatus struct {
	NextStep             string             `json:"next_step" jsonschema:"one of verify_connection, record_ai_agent, await_provisioning, resolve_duplicate_sign_in_clients, set_up_sign_in, trust_sign_in, register_in_okta; register_in_okta means Speakeasy's side is done and the Okta-side steps cannot be observed"`
	NextStepGuidance     string             `json:"next_step_guidance"`
	AgentRecorded        bool               `json:"agent_recorded" jsonschema:"whether the Okta AI agent ID is recorded on the connection"`
	ClientRegistered     bool               `json:"sign_in_client_registered" jsonschema:"whether the agent's linked app is registered as the organization's sign-in client"`
	DuplicateClients     int                `json:"duplicate_sign_in_clients,omitempty" jsonschema:"number of organization sign-in clients claiming the agent ID when more than one; none is chosen until the extras are deleted"`
	ClientReady          bool               `json:"sign_in_client_ready" jsonschema:"whether that client uses a signing key with the scopes sign-in needs"`
	RedirectURI          *string            `json:"redirect_uri,omitempty" jsonschema:"sign-in redirect URI to add to the linked app in Okta"`
	KeyURL               *string            `json:"key_url,omitempty" jsonschema:"key URL (JWKS URI) that publishes the sign-in client's public keys; Okta's agent Credentials take the pasted public_jwk, not this URL"`
	KeyID                *string            `json:"key_id,omitempty" jsonschema:"kid of the active signing key"`
	PublicJWK            map[string]any     `json:"public_jwk,omitempty" jsonschema:"active public signing key (public members only) to paste in the agent's Credentials in Okta and Activate; re-register it after rotating the key set or publishing a new key"`
	TrustingIssuers      []OktaSignInIssuer `json:"trusting_issuers" jsonschema:"organization sign-in issuers that trust the agent's sign-in client"`
	StaleTrustingIssuers []OktaSignInIssuer `json:"stale_trusting_issuers" jsonschema:"organization sign-in issuers that still trust a sign-in client for a previous agent ID or a deleted client"`
	ChecklistNextStep    *OktaChecklistStep `json:"checklist_next_step,omitempty" jsonschema:"first Okta setup checklist step not yet confirmed done; once Speakeasy's side is done, steps Speakeasy cannot observe are skipped"`
}

type OktaSignInIssuer struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}

type OktaChecklistStep struct {
	Key       string `json:"key"`
	Title     string `json:"title"`
	Completed *bool  `json:"completed,omitempty" jsonschema:"false when observed not done; omitted when Speakeasy cannot observe the step"`
}

func registerXAAReadinessTool(reg *Registrar, service *xaaReadinessService) {
	addTool(reg, &mcp.Tool{
		Name: "get_xaa_readiness", Title: "Check Cross-App Access Readiness",
		Description: "Inspect stored Okta Cross-App Access (XAA) readiness for one exact project and MCP server. Reports the dashboard's derived state and exchange evidence; does not probe, connect or change anything. Connected means administrator-confirmed, not verified. identity_chaining lists this server's identity-chaining bindings with state, remediation, configured scopes and the scopes actually requested after dropping OpenID Connect scopes; served is true only when exactly one ready binding serves the upstream at runtime. federated_callback_url is the redirect URI to register in the identity provider sign-in app for this server's user sign-in. okta_sign_in reports the organization's Okta sign-in setup: whether the agent's sign-in client exists, the public key to paste in the agent's Credentials in Okta and Activate (re-register it after rotating or publishing a new key), the redirect URI for the linked app, which sign-in issuers trust it or still trust a previous agent's client, and the next setup step. Changing bindings or setting up sign-in is not available here; sign-in setup needs a signing key choice and a confirmed trust change, so send the administrator to the dashboard for it. Requires organization administration and read access to the server. Unlike get_mcp_readiness, this checks organization XAA configuration, not a registration's provider readiness.",
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
					output := GetXAAReadinessOutput{ProjectID: row.ProjectID, MCPServerID: row.McpServerID, State: row.State, Pending: row.Pending, NotApplicableReason: row.NotApplicableReason, BrokenReason: row.BrokenReason, ObservedResult: row.ObservedResult, ObservedAt: row.ObservedAt, OktaSignIn: service.readSignIn(ctx, principal.OrganizationID)}
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

// readSignIn is advisory: a failed read omits it rather than failing readiness.
func (s *xaaReadinessService) readSignIn(ctx context.Context, organizationID string) *OktaSignInStatus {
	if s.signIn == nil {
		return nil
	}
	setup, err := s.signIn.Read(ctx, organizationID)
	if err != nil {
		if !errors.Is(err, idpc.ErrConnectionNotFound) && s.logger != nil {
			s.logger.ErrorContext(ctx, "read okta sign-in setup", attr.SlogError(err))
		}
		return nil
	}
	if setup == nil {
		return nil
	}
	status := &OktaSignInStatus{
		NextStep:             setup.NextStep,
		NextStepGuidance:     oktaSignInGuidance[setup.NextStep],
		AgentRecorded:        setup.AgentRecorded,
		ClientRegistered:     setup.ClientRegistered,
		DuplicateClients:     setup.DuplicateClients,
		ClientReady:          setup.ClientReady,
		RedirectURI:          conv.PtrEmpty(setup.RedirectURI),
		KeyURL:               conv.PtrEmpty(setup.JWKSURL),
		KeyID:                conv.PtrEmpty(setup.ActiveKeyID),
		PublicJWK:            setup.PublicJWK,
		TrustingIssuers:      signInIssuers(setup.TrustingIssuers),
		StaleTrustingIssuers: signInIssuers(setup.StaleTrustingIssuers),
		ChecklistNextStep:    nil,
	}
	speakeasyDone := setup.NextStep == idpc.SignInStepRegisterInOkta
	for _, item := range setup.Checklist {
		if item.Completed == nil && speakeasyDone {
			continue
		}
		if item.Completed == nil || !*item.Completed {
			status.ChecklistNextStep = &OktaChecklistStep{Key: item.Key, Title: item.Title, Completed: item.Completed}
			break
		}
	}
	return status
}

func signInIssuers(issuers []idpc.SignInIssuer) []OktaSignInIssuer {
	out := make([]OktaSignInIssuer, 0, len(issuers))
	for _, issuer := range issuers {
		out = append(out, OktaSignInIssuer{ID: issuer.ID, Slug: issuer.Slug})
	}
	return out
}

const oktaSignInDashboard = " in the dashboard (IDP and SSO > Identity providers > Okta); Platform MCP cannot make this change."

var oktaSignInGuidance = map[string]string{
	idpc.SignInStepVerifyConnection:  "Finish and verify the Okta connection" + oktaSignInDashboard,
	idpc.SignInStepRecordAgent:       "Register the Okta AI agent and record its ID" + oktaSignInDashboard,
	idpc.SignInStepAwaitProvision:    "Speakeasy is still provisioning the connection; check again shortly.",
	idpc.SignInStepResolveDuplicates: "Several organization sign-in clients use the agent ID under the Okta connection's issuer, so none is used. Delete the extras under Remote identity providers" + oktaSignInDashboard,
	idpc.SignInStepSetUpSignIn:       "Use Set up Okta sign-in, which needs customer-managed encryption keys enabled for the organization and a Google Cloud KMS key to sign with," + oktaSignInDashboard,
	idpc.SignInStepTrustSignIn:       "Confirm which sign-in issuer trusts Okta sign-in" + oktaSignInDashboard,
	idpc.SignInStepRegisterInOkta:    "In Okta, paste public_jwk in the agent's Credentials and click Activate, then enable Authorization Code and Refresh Token on the linked app and add redirect_uri. Rotating the signing key set or publishing a new key needs the new public key pasted in Okta again. Speakeasy cannot observe these Okta settings.",
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
