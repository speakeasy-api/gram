//nolint:exhaustruct // MCP manifests and service payloads use optional zero values.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	srv "github.com/speakeasy-api/gram/server/gen/okta_resource_connections"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	idpc "github.com/speakeasy-api/gram/server/internal/identityproviderconnections"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oauth/protectedresource"
	"github.com/speakeasy-api/gram/server/internal/oktaresourceconnections"
	oktarepo "github.com/speakeasy-api/gram/server/internal/oktaresourceconnections/repo"
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

	// emaEnabled and servers report vendors the Okta connection does not
	// describe: orgs without one, and servers Speakeasy may vouch for.
	emaEnabled FeatureChecker
	servers    xaaServerReader
}

// xaaServer is one eligible server and whether Speakeasy vouches for it.
type xaaServer struct {
	issuerID        uuid.UUID
	resource        string
	advertisesIDJAG bool
	speakeasy       bool
}

// xaaServerReader reads one server identity chaining could serve, in the
// caller's organization, that the caller may read. False when there is none.
type xaaServerReader interface {
	Server(ctx context.Context, organizationID string, projectID, serverID uuid.UUID) (xaaServer, bool, error)
}

// Provider-agnostic readiness states, reported whenever the Okta resource
// connection does not describe the vendor's EMA provider.
const (
	xaaStateNotApplicable = "not_applicable"
	xaaStateReady         = "ready"
	xaaStateNotReady      = "not_ready"
)

// xaaIdentityInspector reads identity chaining bindings and the federated
// sign-in callback for one server the List snapshot already authorized.
type xaaIdentityInspector interface {
	Inspect(ctx context.Context, organizationID string, projectID, serverID, remoteSessionIssuerID uuid.UUID, resource string) (XAAIdentityChaining, *string, error)
}

// WithXAAReadiness reuses the dashboard service's org-admin authorization,
// MCP visibility filtering and readiness derivation. List itself is not gated,
// so the agent surface checks the same rollout flag before reading it.
func (r *PostgresReader) WithXAAReadiness(connections xaaConnectionsReader, signIn oktaSignInReader, flags feature.Provider) *PostgresReader {
	flagChecker := func(flag feature.Flag) FeatureChecker {
		return func(ctx context.Context, orgID string) (bool, error) {
			if flags == nil {
				return false, ErrUnavailable
			}
			slug, err := NewPostgresOrganizationSlugResolver(r.db).OrganizationSlug(ctx, orgID)
			if err != nil {
				return false, err
			}
			return flags.IsFlagEnabled(ctx, flag, orgID, feature.OrgProjectGroups(slug, ""))
		}
	}
	r.xaaReadiness = &xaaReadinessService{logger: r.logger, connections: connections, signIn: signIn, enabled: flagChecker(feature.FlagOktaConnections), identity: nil, emaEnabled: flagChecker(feature.FlagEMAProviders), servers: nil}
	return r
}

// WithXAAIdentityChaining adds identity chaining bindings and the federated
// sign-in callback to XAA readiness. outbound is the pinned callback origin
// for clients that recorded none.
func (r *PostgresReader) WithXAAIdentityChaining(governor IdentityChainingGovernor, outbound *url.URL) *PostgresReader {
	if r.xaaReadiness != nil && r.db != nil {
		r.xaaReadiness.identity = &postgresXAAIdentityInspector{logger: r.logger, db: r.db, governor: governor, origins: remotesessions.CallbackOrigins{Outbound: outbound, Registration: nil}}
		r.xaaReadiness.servers = &postgresXAAServerReader{reader: r}
	}
	return r
}

// postgresXAAServerReader reads the authorization engine at call time, since
// WithAuthorization may compose after WithXAAIdentityChaining.
type postgresXAAServerReader struct {
	reader *PostgresReader
}

func (s *postgresXAAServerReader) Server(ctx context.Context, organizationID string, projectID, serverID uuid.UUID) (xaaServer, bool, error) {
	var none xaaServer
	engine := s.reader.authz
	if engine == nil {
		return none, false, ErrUnavailable
	}
	row, err := oktarepo.New(s.reader.db).GetEligibleServer(ctx, oktarepo.GetEligibleServerParams{OrganizationID: organizationID, McpServerID: serverID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return none, false, nil
	case err != nil:
		return none, false, fmt.Errorf("load XAA readiness server: %w", err)
	case row.ProjectID != projectID:
		return none, false, nil
	}
	// org:admin does not imply mcp:read, as in the dashboard's readiness list.
	allowed, err := engine.Filter(ctx, []authz.Check{authz.MCPCheck(authz.ScopeMCPRead, serverID.String(), projectID.String())})
	if err != nil {
		return none, false, fmt.Errorf("filter XAA readiness server: %w", err)
	}
	if !slices.Contains(allowed, serverID.String()) {
		return none, false, nil
	}
	vendor, err := remotesessions.NormalizeVendorIssuer(row.Issuer)
	if err != nil {
		vendor = row.Issuer
	}
	// Only the order's kinds are read here; Inspect reports live availability.
	q := remotesessionsrepo.New(s.reader.db)
	order, err := remotesessions.EndpointEMAProviders(ctx, q, organizationID, row.UserSessionIssuerID.UUID, vendor, false)
	if err != nil {
		return none, false, fmt.Errorf("resolve XAA readiness EMA provider order: %w", err)
	}
	federated := true
	_, err = q.GetEMAChainingUserIssuer(ctx, remotesessionsrepo.GetEMAChainingUserIssuerParams{ID: row.UserSessionIssuerID.UUID, OrganizationID: conv.ToPGText(organizationID)})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		federated = false
	case err != nil:
		return none, false, fmt.Errorf("read XAA readiness sign-in trust: %w", err)
	}
	return xaaServer{
		issuerID:        row.IssuerID,
		resource:        oktaresourceconnections.ResourceIndicator(oktarepo.ListEligibleServersRow(row)),
		advertisesIDJAG: oktaresourceconnections.AdvertisesIDJAG(row.GrantTypesSupported, row.AuthorizationGrantProfilesSupported),
		speakeasy:       speakeasyReadinessView(order, federated),
	}, true, nil
}

// speakeasyReadinessView reports whether identity chaining, not the Okta
// resource connection, describes the server's readiness: its order names the
// Speakeasy issuer, unless the sign-in is federated and the order also names
// an external issuer, the only kind that can vouch for a federated sign-in.
func speakeasyReadinessView(order remotesessions.EMAProviderOrder, federated bool) bool {
	speakeasy := slices.ContainsFunc(order.Providers, func(p remotesessions.EMAProvider) bool {
		return p.Kind == remotesessions.EMAProviderSpeakeasy
	})
	external := slices.ContainsFunc(order.Providers, func(p remotesessions.EMAProvider) bool {
		return p.Kind == remotesessions.EMAProviderExternal && p.RemoteSessionIssuerID != uuid.Nil
	})
	return speakeasy && (!federated || !external)
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
	State               string  `json:"state" jsonschema:"when the organization has an Okta connection and the vendor's order names no Speakeasy issuer, or this server's sign-in is federated and the order also names an external issuer, the Okta resource connection state: not_applicable, needs_agent, needs_connection, broken, connected or verified. Otherwise, including whenever the order names the Speakeasy issuer, unless this server's sign-in is federated and the order also names an external issuer: not_applicable, ready (identity chaining serves this server now) or not_ready (identity_chaining says why)"`
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
	Served          bool                         `json:"served" jsonschema:"true when exactly one ready binding serves this server's upstream at runtime and at least one EMA provider in the vendor's order can serve now"`
	NotServedReason *string                      `json:"not_served_reason,omitempty" jsonschema:"why served is false: binding_not_ready (no single ready binding), provider_order_unavailable (the vendor's EMA provider order could not be read; retry), provider_unavailable (no EMA provider in the order can serve now; each provider's unavailable_reason says why), resource_unavailable (resource metadata could not be validated; check the endpoint and its protected-resource metadata, then retry), or not_routable (identity chaining has no route to this server: it has no sign-in issuer, no upstream authorization server or resource, or is tunneled without its issuer); omitted when served, and also when provider availability could not be inspected"`
	Providers       []XAAEMAProvider             `json:"providers,omitempty" jsonschema:"the EMA providers the vendor accepts, in their stored order: by default the endpoint's trusted external issuer alone, or the organization's Speakeasy issuer alone when the endpoint trusts none, or else the administrator's saved list. Each endpoint can use only the provider its sign-in goes through, so the saved list acts as the set of accepted providers; providers that cannot serve are skipped. Omitted when no single ready binding serves the upstream"`
	Bindings        []XAAIdentityChainingBinding `json:"bindings"`
}

type XAAEMAProvider struct {
	Kind                  string  `json:"kind" jsonschema:"external (an external issuer such as an Okta authorization server) or speakeasy (the organization's internal session issuer)"`
	RemoteSessionIssuerID *string `json:"remote_session_issuer_id,omitempty" jsonschema:"the external provider's remote session issuer, when it names one"`
	UnavailableReason     *string `json:"unavailable_reason,omitempty" jsonschema:"why the provider cannot serve now, such as not_trusted_by_endpoint (the endpoint's sign-in does not go through this provider), sign_in_enforcement_required, signing_paused, keyless, verified_domain_required or readiness_unknown; omitted when it can"`
	LastAcceptedAt        *string `json:"last_accepted_at,omitempty" jsonschema:"when the vendor last accepted an assertion from this exact provider anywhere in the organization, from cached credentials that still hold an access token; organization-wide evidence that the vendor trusts the provider, not proof for this server. The vendor is read through its current issuer record, so after that record's URL changes, older acceptance may be credited to the new vendor. Omitted when none has been accepted, and also when acceptance could not be read, so absence is not proof the vendor never accepted the provider"`
}

type XAAIdentityChainingBinding struct {
	State            string   `json:"state"`
	Stage            string   `json:"stage"`
	Remediation      string   `json:"remediation,omitempty"`
	Retryable        bool     `json:"retryable"`
	GrantSource      string   `json:"grant_source"`
	RequestedScopes  []string `json:"requested_scopes" jsonschema:"scopes identity chaining requests, after dropping OpenID Connect scopes"`
	ConfiguredScopes []string `json:"configured_scopes" jsonschema:"the whole scope set recorded on the binding when identity chaining was turned on, OpenID Connect scopes included; when requested_scopes is empty, an ID-JAG naming a scope outside this set is rejected"`
	ConnectionScopes []string `json:"connection_scopes,omitzero" jsonschema:"scopes preparing identity chaining again would record now, comparable to configured_scopes: what an interactive sign-in through the bound client requests for this server, or the recorded scopes when the binding has no server connection to follow or the connection names only OpenID Connect scopes while the recorded ones name others; an empty array when a bound client records none, omitted when no client is bound"`
	ScopesOutOfDate  bool     `json:"scopes_out_of_date" jsonschema:"true when connection_scopes differs from configured_scopes; prepareEMA with the current expected_generation applies the connection's scopes"`
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
		Description: "Inspect stored Cross-App Access (XAA) readiness, the enterprise-managed authorization path where an EMA provider (an external identity provider such as Okta, or the organization's Speakeasy issuer) issues ID-JAGs, for one exact project and MCP server. When the organization has an Okta connection and the vendor's order names no Speakeasy issuer, or this server's sign-in is federated and the order also names an external issuer, reports the dashboard's derived Okta resource connection state and exchange evidence (connected means administrator-confirmed, not verified). Otherwise, including for any vendor whose order names the Speakeasy issuer (unless this server's sign-in is federated through an external issuer and the order also names one) and in organizations with no Okta connection, state comes from identity chaining alone: ready, not_ready or not_applicable, without the Okta fields. May fetch read-only OAuth resource metadata to resolve the endpoint; does not exchange tokens, connect or change configuration. identity_chaining lists this server's identity-chaining bindings with state, remediation, configured scopes (recorded when identity chaining was prepared), the scopes actually requested after dropping OpenID Connect scopes, and whether the server connection's scopes have changed since (prepareEMA with the current expected_generation applies them); served is true only when exactly one ready binding serves the upstream at runtime and at least one EMA provider in the vendor's order can serve now; providers lists the providers the vendor accepts, stored as an ordered list: by default the endpoint's trusted external issuer alone, or the organization's Speakeasy issuer when the endpoint trusts none, or else the administrator's saved list, each with unavailable_reason when it cannot serve and last_accepted_at when the vendor has accepted that exact provider anywhere in the organization (omitted both when it never has and when acceptance could not be read, so absence is not proof the vendor never accepted it). Identity chaining skips providers that cannot serve, including providers the endpoint's sign-in does not go through (not_trusted_by_endpoint: an external issuer other than the sign-in's trusted issuer, or the Speakeasy issuer on a sign-in federated through an external issuer, since Speakeasy vouches only for servers that use Speakeasy's own sign-in). Each server can use only the provider its sign-in goes through, so the saved list acts as the set of providers the vendor accepts and its order does not change who vouches; not_served_reason says why served is false. The Speakeasy issuer serves only while the organization's sign-in enforcement is on. Changing which providers a vendor accepts is not available here; organization admins choose them on the dashboard's Organization → IDP and SSO → Identity providers → Services page for the vendor, or with the organizationRemoteSessionIssuers.setEMAProviders management API (saving a provider list needs the ema-providers rollout; restoring the default does not). federated_callback_url is the redirect URI to register in the identity provider sign-in app for this server's user sign-in. okta_sign_in reports the organization's Okta sign-in setup: whether the agent's sign-in client exists, the public key to paste in the agent's Credentials in Okta and Activate (re-register it after rotating or publishing a new key), the redirect URI for the linked app, which sign-in issuers trust it or still trust a previous agent's client, and the next setup step. Changing bindings or setting up sign-in is not available here; sign-in setup needs a signing key choice and a confirmed trust change, so send the administrator to the dashboard for it. Requires organization administration and read access to the server. Unlike get_mcp_readiness, this checks organization XAA configuration, not a registration's provider readiness.",
		Annotations: readOnlyOpenWorldAnnotations(),
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
		if service == nil {
			return xaaReadinessRefusal(unavailableCode, "XAA readiness is unavailable on this server.")
		}
		return service.read(ctx, principal.OrganizationID, projectID, serverID)
	})
}

func (s *xaaReadinessService) providerAgnostic() bool {
	return s.emaEnabled != nil && s.servers != nil && s.identity != nil
}

// read prefers the Okta resource connection when the organization has one;
// otherwise readiness comes from identity chaining for any EMA provider.
func (s *xaaReadinessService) read(ctx context.Context, organizationID string, projectID, serverID uuid.UUID) (*mcp.CallToolResult, GetXAAReadinessOutput, error) {
	okta := s.connections != nil && s.enabled != nil
	if !okta && !s.providerAgnostic() {
		return xaaReadinessRefusal(unavailableCode, "XAA readiness is unavailable on this server.")
	}
	oktaOn, emaOn := false, false
	if okta {
		on, err := s.enabled(ctx, organizationID)
		if err != nil {
			return xaaReadinessRefusal(unavailableCode, "XAA readiness is temporarily unavailable.")
		}
		oktaOn = on
	}
	if s.providerAgnostic() {
		on, err := s.emaEnabled(ctx, organizationID)
		if err != nil {
			return xaaReadinessRefusal(unavailableCode, "XAA readiness is temporarily unavailable.")
		}
		emaOn = on
	}
	if !oktaOn && !emaOn {
		return xaaReadinessRefusal(unavailableCode, "XAA readiness is not enabled for this organization.")
	}
	if !oktaOn {
		return s.readProviderAgnostic(ctx, organizationID, projectID, serverID)
	}
	result, err := s.connections.List(ctx, &srv.ListPayload{IncludeAll: true})
	if err != nil {
		shareable, ok := errors.AsType[*oops.ShareableError](err)
		switch {
		case ok && (shareable.Code == oops.CodeForbidden || shareable.Code == oops.CodeUnauthorized):
			return xaaReadinessRefusal("forbidden", "XAA readiness requires organization administration and server read access.")
		case ok && shareable.Code == oops.CodeFailedPrecondition && emaOn:
			return s.readProviderAgnostic(ctx, organizationID, projectID, serverID)
		case ok && shareable.Code == oops.CodeFailedPrecondition:
			return xaaReadinessRefusal("setup_required", "Connect an identity provider before checking XAA readiness.")
		}
		return xaaReadinessRefusal(unavailableCode, "XAA readiness is temporarily unavailable.")
	}
	if result != nil {
		for _, row := range result.Servers {
			if row != nil && row.ProjectID == projectID.String() && row.McpServerID == serverID.String() {
				return s.readOkta(ctx, organizationID, projectID, serverID, row, emaOn)
			}
		}
	}
	return xaaReadinessRefusal("not_found", "That project or eligible MCP server is not available to you.")
}

// readOkta reports the Okta resource connection, unless the EMA providers
// rollout is on and Speakeasy vouches for the server, whose readiness that
// connection does not describe.
func (s *xaaReadinessService) readOkta(ctx context.Context, organizationID string, projectID, serverID uuid.UUID, row *srv.OktaResourceConnectionServer, emaOn bool) (*mcp.CallToolResult, GetXAAReadinessOutput, error) {
	if emaOn {
		server, ok, err := s.servers.Server(ctx, organizationID, projectID, serverID)
		if err != nil {
			return xaaReadinessRefusal(unavailableCode, "XAA readiness is temporarily unavailable.")
		}
		if ok && server.speakeasy {
			return s.providerOutput(ctx, organizationID, projectID, serverID, server)
		}
	}
	output := GetXAAReadinessOutput{ProjectID: row.ProjectID, MCPServerID: row.McpServerID, State: row.State, Pending: row.Pending, NotApplicableReason: row.NotApplicableReason, BrokenReason: row.BrokenReason, ObservedResult: row.ObservedResult, ObservedAt: row.ObservedAt, OktaSignIn: s.readSignIn(ctx, organizationID)}
	if s.identity != nil {
		issuerID := uuid.Nil
		if row.IssuerID != nil {
			issuerID, _ = uuid.Parse(*row.IssuerID)
		}
		chaining, callback, err := s.identity.Inspect(ctx, organizationID, projectID, serverID, issuerID, row.ResourceIndicator)
		if err != nil {
			return xaaReadinessRefusal(unavailableCode, "XAA readiness is temporarily unavailable.")
		}
		output.IdentityChaining, output.FederatedCallbackURL = &chaining, callback
	}
	return nil, output, nil
}

func (s *xaaReadinessService) readProviderAgnostic(ctx context.Context, organizationID string, projectID, serverID uuid.UUID) (*mcp.CallToolResult, GetXAAReadinessOutput, error) {
	server, ok, err := s.servers.Server(ctx, organizationID, projectID, serverID)
	if err != nil {
		return xaaReadinessRefusal(unavailableCode, "XAA readiness is temporarily unavailable.")
	}
	if !ok {
		return xaaReadinessRefusal("not_found", "That project or eligible MCP server is not available to you.")
	}
	return s.providerOutput(ctx, organizationID, projectID, serverID, server)
}

// providerOutput derives readiness from identity chaining alone, so it holds
// for any EMA provider and needs no identity provider connection.
func (s *xaaReadinessService) providerOutput(ctx context.Context, organizationID string, projectID, serverID uuid.UUID, server xaaServer) (*mcp.CallToolResult, GetXAAReadinessOutput, error) {
	chaining, callback, err := s.identity.Inspect(ctx, organizationID, projectID, serverID, server.issuerID, server.resource)
	if err != nil {
		return xaaReadinessRefusal(unavailableCode, "XAA readiness is temporarily unavailable.")
	}
	output := GetXAAReadinessOutput{ProjectID: projectID.String(), MCPServerID: serverID.String(), State: xaaStateNotReady, Pending: true, IdentityChaining: &chaining, FederatedCallbackURL: callback}
	switch {
	case !server.advertisesIDJAG:
		output.State, output.Pending, output.NotApplicableReason = xaaStateNotApplicable, false, new(oktaresourceconnections.ReasonNoIDJAG)
	case chaining.Served:
		output.State, output.Pending = xaaStateReady, false
	}
	return nil, output, nil
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
	idpc.SignInStepResolveDuplicates: "Several organization sign-in clients use the agent ID under the Okta connection's issuer, so none is used. Delete the extras under Authorization Servers" + oktaSignInDashboard,
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
	logger   *slog.Logger
	db       *pgxpool.Pool
	governor IdentityChainingGovernor
	origins  remotesessions.CallbackOrigins
}

type identityChainingResourceResolver interface {
	ResolveResource(context.Context, identitychaining.Request) (protectedresource.EndpointResource, error)
}

// xaaNotRoutable explains not_ready for a server identity chaining can't
// route: no sign-in issuer, no upstream issuer or resource, or a tunneled
// server without its issuer.
const xaaNotRoutable = "not_routable"

func (i *postgresXAAIdentityInspector) Inspect(ctx context.Context, organizationID string, projectID, serverID, remoteSessionIssuerID uuid.UUID, resource string) (XAAIdentityChaining, *string, error) {
	out := XAAIdentityChaining{Served: false, NotServedReason: nil, Providers: nil, Bindings: []XAAIdentityChainingBinding{}}
	server, err := mcpserversrepo.New(i.db).GetMCPServerByLiveProjectForOrganization(ctx, mcpserversrepo.GetMCPServerByLiveProjectForOrganizationParams{OrganizationID: organizationID, ID: serverID, ProjectID: projectID})
	if err != nil {
		return out, nil, fmt.Errorf("load XAA readiness server: %w", err)
	}
	if !server.UserSessionIssuerID.Valid {
		out.NotServedReason = new(xaaNotRoutable)
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
		out.NotServedReason = new(xaaNotRoutable)
		return out, callback, nil
	}
	req, routable := identitychaining.NewUpstreamRequest(organizationID, projectID, userIssuerID, resource, server.TunneledMcpServerID.Valid, server.RemoteSessionIssuerID)
	scope := uuid.NullUUID{UUID: remoteSessionIssuerID, Valid: true}
	resources := []string{resource}
	discoveryFailed := false
	if routable {
		scope = req.RemoteSessionIssuerID
		if resolver, ok := i.governor.(identityChainingResourceResolver); ok {
			identity, err := resolver.ResolveResource(ctx, req)
			if err != nil {
				out.NotServedReason = new("resource_unavailable")
				discoveryFailed = true
				// Preserve diagnostics for ready canonical resources on this
				// origin without treating any candidate as authorization.
				candidates, readErr := q.ListEMAChainingResourceCandidates(ctx, remotesessionsrepo.ListEMAChainingResourceCandidatesParams{
					OrganizationID: organizationID, ProjectID: projectID, UserSessionIssuerID: userIssuerID, RemoteSessionIssuerID: scope,
				})
				if readErr != nil {
					return out, callback, fmt.Errorf("read XAA diagnostic candidates: %w", readErr)
				}
				for _, candidate := range candidates {
					if identitychaining.MatchesDiagnosticOrigin(resource, candidate) && !slices.Contains(resources, candidate) {
						resources = append(resources, candidate)
					}
				}
				if i.logger != nil {
					i.logger.WarnContext(ctx, "resolve XAA readiness resource", attr.SlogOrganizationID(organizationID), attr.SlogProjectID(projectID.String()), attr.SlogError(err))
				}
			} else {
				resource = identity.Resource
				resources = []string{resource}
			}
		}
	}
	for _, diagnosticResource := range resources {
		// Every deployment serving Platform MCP wires a protected resource
		// prober into logins, so their scope requests consult resource rows.
		results, err := remotesessions.InspectIdentityChainingForTenant(ctx, i.db, projectID, organizationID, userIssuerID, scope, diagnosticResource, true)
		if err != nil {
			return out, nil, fmt.Errorf("inspect XAA readiness identity chaining: %w", err)
		}
		for _, r := range results {
			out.Bindings = append(out.Bindings, XAAIdentityChainingBinding{State: r.State, Stage: r.Stage, Remediation: r.Remediation, Retryable: r.Retryable, GrantSource: r.GrantSource, RequestedScopes: append([]string{}, remotesessions.GrantScopes(r.Scopes)...), ConfiguredScopes: append([]string{}, r.Scopes...), ConnectionScopes: r.ConnectionScopes, ScopesOutOfDate: r.ScopesOutOfDate})
		}
	}
	if !routable {
		out.NotServedReason = new(xaaNotRoutable)
	}
	if i.governor != nil && routable && !discoveryFailed {
		// One read of binding and order, so served, providers and the
		// reason never disagree during a change.
		order, ok, orderErr := i.governor.ProviderOrder(ctx, req)
		if ok {
			// Acceptance is evidence, not readiness: a failed read leaves it out.
			if accepted, err := remotesessions.VendorEMAAcceptance(ctx, q, organizationID, order.VendorIssuer); err != nil {
				if i.logger != nil {
					i.logger.WarnContext(ctx, "read XAA readiness provider acceptance", attr.SlogOrganizationID(organizationID), attr.SlogError(err))
				}
			} else {
				order = remotesessions.StampEMAProviderLastAccepted(order, accepted)
			}
			out.Providers = make([]XAAEMAProvider, 0, len(order.Providers))
			for _, p := range order.Providers {
				provider := XAAEMAProvider{Kind: string(p.Kind), RemoteSessionIssuerID: nil, UnavailableReason: nil, LastAcceptedAt: nil}
				if p.RemoteSessionIssuerID != uuid.Nil {
					provider.RemoteSessionIssuerID = new(p.RemoteSessionIssuerID.String())
				}
				if p.Unavailable != "" {
					provider.UnavailableReason = new(string(p.Unavailable))
				}
				if p.LastAcceptedAt != nil {
					provider.LastAcceptedAt = new(p.LastAcceptedAt.UTC().Format(time.RFC3339))
				}
				out.Providers = append(out.Providers, provider)
			}
		}
		switch {
		case orderErr != nil:
			out.NotServedReason = new("provider_order_unavailable")
		case !ok:
			out.NotServedReason = new("binding_not_ready")
		case len(order.Available()) == 0:
			out.NotServedReason = new("provider_unavailable")
		default:
			out.Served = true
		}
	}
	return out, callback, nil
}
