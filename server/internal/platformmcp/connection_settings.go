package platformmcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/authclients"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

type MCPConnectionSettingsTargetKind string

const (
	MCPConnectionSettingsMCPServer MCPConnectionSettingsTargetKind = "mcp_server"
	MCPConnectionSettingsGateway   MCPConnectionSettingsTargetKind = "gateway"
)

type GetMCPConnectionSettingsInput struct {
	ProjectID  string                          `json:"project_id" jsonschema:"project ID that owns the target"`
	TargetKind MCPConnectionSettingsTargetKind `json:"target_kind" jsonschema:"exact target type: mcp_server or gateway"`
	TargetID   string                          `json:"target_id" jsonschema:"ID of the exact MCP server or gateway"`
}

type MCPConnectionEndpoint struct {
	ID             string `json:"id"`
	Slug           string `json:"slug"`
	CustomDomainID string `json:"custom_domain_id,omitempty"`
	Domain         string `json:"domain,omitempty"`
	IsDomainRoot   bool   `json:"is_domain_root"`
}

type MCPConnectionIngress struct {
	Enabled        bool   `json:"enabled"`
	NamespaceKind  string `json:"namespace_kind"`
	Hostname       string `json:"hostname"`
	CustomDomainID string `json:"custom_domain_id,omitempty"`
	Status         string `json:"status"`
	DNSName        string `json:"dns_name,omitempty"`
}

type MCPConnectionPluginMembership struct {
	ID          string `json:"id"`
	PluginID    string `json:"plugin_id"`
	PluginSlug  string `json:"plugin_slug"`
	DisplayName string `json:"display_name"`
	Policy      string `json:"policy"`
	SortOrder   int32  `json:"sort_order"`
}

// MCPUpstreamAuthClient is one way the server authenticates to its upstream.
// Stored values are never returned.
type MCPUpstreamAuthClient struct {
	ID                    string `json:"id"`
	Name                  string `json:"name"`
	ClientType            string `json:"client_type" jsonschema:"remote_session, service_account, static_credential, pass_through or ema"`
	Enabled               bool   `json:"enabled"`
	Personal              bool   `json:"personal" jsonschema:"each caller uses their own connection; a grant never lets anyone use another user's connection"`
	OtherServerCount      int    `json:"other_server_count" jsonschema:"other servers offering the same client; changing or disabling it changes them too"`
	OfferedOnPublicServer bool   `json:"offered_on_public_server"`
}

// MCPUpstreamAuthentication says whether the server's upstream needs
// authentication and which clients it offers.
type MCPUpstreamAuthentication struct {
	Status               string                  `json:"status" jsonschema:"configured, setup_incomplete (authentication required but no enabled client), no_auth_required, or undecided"`
	RequiresUpstreamAuth *bool                   `json:"requires_upstream_auth,omitempty"`
	Clients              []MCPUpstreamAuthClient `json:"clients"`
}

type MCPConnectionSettings struct {
	// DiscoveryMode is a gateway's stored default discovery mode. A connection
	// records the default at consent and keeps it until it reconnects.
	DiscoveryMode string `json:"discovery_mode,omitempty"`

	// EffectiveDiscoveryMode is the mode the gateway default serves now: a
	// stored code_mode serves progressive while Code Mode is unavailable to
	// the organization or the gateway does not require sign-in, and a mode
	// this build does not support serves progressive.
	EffectiveDiscoveryMode string `json:"effective_discovery_mode,omitempty"`

	// CodeModeAvailable reports, for a gateway, whether the code runtime is
	// available and the organization has Code Mode.
	CodeModeAvailable *bool `json:"code_mode_available,omitempty"`

	ProjectID         string                          `json:"project_id"`
	TargetKind        MCPConnectionSettingsTargetKind `json:"target_kind"`
	TargetID          string                          `json:"target_id"`
	Name              string                          `json:"name"`
	Visibility        string                          `json:"visibility"`
	NetworkMode       string                          `json:"network_mode"`
	Version           string                          `json:"version"`
	Endpoints         []MCPConnectionEndpoint         `json:"endpoints"`
	Ingress           *MCPConnectionIngress           `json:"ingress,omitempty"`
	PluginMemberships []MCPConnectionPluginMembership `json:"plugin_memberships"`
	// UpstreamAuthentication is set for MCP servers, not gateways.
	UpstreamAuthentication *MCPUpstreamAuthentication `json:"upstream_authentication,omitempty"`
}

// CodeModeAvailability reports whether an organization can use Code Mode now:
// the code runtime is available and the organization has the entitlement.
type CodeModeAvailability func(ctx context.Context, organizationID string) bool

type MCPConnectionSettingsService struct {
	db      platformrepo.DBTX
	queries *platformrepo.Queries
	// codeModeAvailable is nil when the deployment has no code runtime,
	// which reads as unavailable.
	codeModeAvailable CodeModeAvailability
}

func NewMCPConnectionSettingsService(db platformrepo.DBTX) *MCPConnectionSettingsService {
	if db == nil {
		return nil
	}
	return &MCPConnectionSettingsService{db: db, queries: platformrepo.New(db), codeModeAvailable: nil}
}

// WithCodeModeAvailability lets gateway settings report the discovery mode
// the gateway actually serves.
func (s *MCPConnectionSettingsService) WithCodeModeAvailability(check CodeModeAvailability) *MCPConnectionSettingsService {
	if s != nil {
		s.codeModeAvailable = check
	}
	return s
}

func (s *MCPConnectionSettingsService) Get(ctx context.Context, principal Principal, input GetMCPConnectionSettingsInput) (MCPConnectionSettings, error) {
	if s == nil || s.queries == nil {
		return MCPConnectionSettings{}, ErrUnavailable
	}
	return s.get(ctx, s.db, s.queries, principal, input)
}

// GetInTx returns the settings snapshot through the caller transaction. Version
// covers network mode and endpoint IDs, slugs, and domain IDs. It deliberately
// excludes ingress observations, domain names, membership, and root selection.
func (s *MCPConnectionSettingsService) GetInTx(ctx context.Context, tx pgx.Tx, principal Principal, input GetMCPConnectionSettingsInput) (MCPConnectionSettings, error) {
	if s == nil || s.queries == nil || tx == nil {
		return MCPConnectionSettings{}, ErrUnavailable
	}
	return s.get(ctx, tx, s.queries.WithTx(tx), principal, input)
}

func (s *MCPConnectionSettingsService) get(ctx context.Context, db platformrepo.DBTX, queries *platformrepo.Queries, principal Principal, input GetMCPConnectionSettingsInput) (MCPConnectionSettings, error) {
	if principal.OrganizationID == "" {
		return MCPConnectionSettings{}, ErrUnavailable
	}
	projectID, err := uuid.Parse(input.ProjectID)
	if err != nil {
		return MCPConnectionSettings{}, ErrMCPConnectionSettingsInvalid
	}
	targetID, err := uuid.Parse(input.TargetID)
	if err != nil || (input.TargetKind != MCPConnectionSettingsMCPServer && input.TargetKind != MCPConnectionSettingsGateway) {
		return MCPConnectionSettings{}, ErrMCPConnectionSettingsInvalid
	}
	grants, ok := authz.GrantsFromContext(ctx)
	if !ok {
		return MCPConnectionSettings{}, ErrUnavailable
	}
	admin, err := authz.GrantsAuthorize(grants, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: principal.OrganizationID, Dimensions: nil})
	if err != nil {
		return MCPConnectionSettings{}, fmt.Errorf("authorize MCP connection settings admin: %w", err)
	}
	if !admin {
		return MCPConnectionSettings{}, ErrMCPConnectionSettingsNotFound
	}
	if input.TargetKind == MCPConnectionSettingsMCPServer {
		allowed, err := authz.GrantsAuthorize(grants, authz.MCPCheck(authz.ScopeMCPRead, targetID.String(), projectID.String()))
		if err != nil {
			return MCPConnectionSettings{}, fmt.Errorf("authorize MCP connection settings target: %w", err)
		}
		if !allowed {
			return MCPConnectionSettings{}, ErrMCPConnectionSettingsNotFound
		}
	}
	row, err := queries.GetPlatformMCPConnectionSettings(ctx, platformrepo.GetPlatformMCPConnectionSettingsParams{
		OrganizationID: principal.OrganizationID, ProjectID: projectID, TargetKind: string(input.TargetKind), TargetID: targetID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return MCPConnectionSettings{}, ErrMCPConnectionSettingsNotFound
	}
	if err != nil {
		return MCPConnectionSettings{}, fmt.Errorf("get exact MCP connection settings: %w", err)
	}
	var endpoints []MCPConnectionEndpoint
	if err := json.Unmarshal(row.Endpoints, &endpoints); err != nil {
		return MCPConnectionSettings{}, fmt.Errorf("decode MCP connection endpoints: %w", err)
	}
	var memberships []MCPConnectionPluginMembership
	if err := json.Unmarshal(row.PluginMemberships, &memberships); err != nil {
		return MCPConnectionSettings{}, fmt.Errorf("decode MCP connection plugin memberships: %w", err)
	}
	var ingress *MCPConnectionIngress
	if len(row.Ingress) > 0 && string(row.Ingress) != "null" {
		var value MCPConnectionIngress
		if err := json.Unmarshal(row.Ingress, &value); err != nil {
			return MCPConnectionSettings{}, fmt.Errorf("decode MCP connection ingress: %w", err)
		}
		ingress = &value
	}
	settings := MCPConnectionSettings{
		ProjectID: projectID.String(), TargetKind: input.TargetKind, TargetID: targetID.String(),
		Name: row.Name, Visibility: row.Visibility, NetworkMode: row.NetworkMode, DiscoveryMode: row.DiscoveryMode,
		EffectiveDiscoveryMode: "", CodeModeAvailable: nil,
		Version: "", Endpoints: endpoints, Ingress: ingress, PluginMemberships: memberships, UpstreamAuthentication: nil,
	}
	if input.TargetKind == MCPConnectionSettingsMCPServer {
		upstream, err := upstreamAuthentication(ctx, db, principal.OrganizationID, targetID)
		if err != nil {
			return MCPConnectionSettings{}, err
		}
		settings.UpstreamAuthentication = upstream
	}
	if input.TargetKind == MCPConnectionSettingsGateway {
		available := s.codeModeAvailable != nil && s.codeModeAvailable(ctx, principal.OrganizationID)
		settings.CodeModeAvailable = &available
		effective := metamcp.ResolveDiscoveryMode(row.DiscoveryMode)
		if effective == metamcp.DiscoveryModeCode && (!available || !row.GatewayRequiresSignIn) {
			effective = metamcp.DiscoveryModeProgressive
		}
		settings.EffectiveDiscoveryMode = string(effective)
	}
	version := connectionAddressVersion{ProjectID: settings.ProjectID, TargetKind: settings.TargetKind, TargetID: settings.TargetID, NetworkMode: settings.NetworkMode, Endpoints: nil}
	for _, endpoint := range endpoints {
		version.Endpoints = append(version.Endpoints, connectionVersionEndpoint{ID: endpoint.ID, Slug: endpoint.Slug, CustomDomainID: endpoint.CustomDomainID})
	}
	payload, err := json.Marshal(version)
	if err != nil {
		return MCPConnectionSettings{}, fmt.Errorf("version MCP connection settings: %w", err)
	}
	digest := sha256.Sum256(payload)
	settings.Version = hex.EncodeToString(digest[:])
	return settings, nil
}

type connectionAddressVersion struct {
	ProjectID   string                          `json:"project_id"`
	TargetKind  MCPConnectionSettingsTargetKind `json:"target_kind"`
	TargetID    string                          `json:"target_id"`
	NetworkMode string                          `json:"network_mode"`
	Endpoints   []connectionVersionEndpoint     `json:"endpoints"`
}

type connectionVersionEndpoint struct {
	ID             string `json:"id"`
	Slug           string `json:"slug"`
	CustomDomainID string `json:"custom_domain_id"`
}

var (
	ErrMCPConnectionSettingsInvalid  = errors.New("invalid MCP connection settings target")
	ErrMCPConnectionSettingsNotFound = errors.New("MCP connection settings target not found")
)

func upstreamAuthentication(ctx context.Context, db platformrepo.DBTX, organizationID string, serverID uuid.UUID) (*MCPUpstreamAuthentication, error) {
	view, err := authclients.ReadServerAuthentication(ctx, db, organizationID, serverID)
	if err != nil {
		return nil, fmt.Errorf("read MCP upstream authentication: %w", err)
	}
	clients := make([]MCPUpstreamAuthClient, 0, len(view.Clients))
	for _, client := range view.Clients {
		public := false
		for _, srv := range client.Servers {
			public = public || srv.Public
		}
		clients = append(clients, MCPUpstreamAuthClient{
			ID:                    client.ID,
			Name:                  client.Name,
			ClientType:            client.ClientType,
			Enabled:               client.Enabled,
			Personal:              client.Personal,
			OtherServerCount:      max(len(client.Servers)-1, 0),
			OfferedOnPublicServer: public,
		})
	}
	return &MCPUpstreamAuthentication{Status: view.Status, RequiresUpstreamAuth: view.RequiresUpstreamAuth, Clients: clients}, nil
}
