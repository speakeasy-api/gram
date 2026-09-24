package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

type MCPConnectionSettings struct {
	ProjectID         string                          `json:"project_id"`
	TargetKind        MCPConnectionSettingsTargetKind `json:"target_kind"`
	TargetID          string                          `json:"target_id"`
	Name              string                          `json:"name"`
	Visibility        string                          `json:"visibility"`
	NetworkMode       string                          `json:"network_mode"`
	Endpoints         []MCPConnectionEndpoint         `json:"endpoints"`
	Ingress           *MCPConnectionIngress           `json:"ingress,omitempty"`
	PluginMemberships []MCPConnectionPluginMembership `json:"plugin_memberships"`
}

type MCPConnectionSettingsService struct {
	queries *platformrepo.Queries
}

func NewMCPConnectionSettingsService(db platformrepo.DBTX) *MCPConnectionSettingsService {
	if db == nil {
		return nil
	}
	return &MCPConnectionSettingsService{queries: platformrepo.New(db)}
}

func (s *MCPConnectionSettingsService) Get(ctx context.Context, principal Principal, input GetMCPConnectionSettingsInput) (MCPConnectionSettings, error) {
	if s == nil || s.queries == nil {
		return MCPConnectionSettings{}, ErrUnavailable
	}
	return s.get(ctx, s.queries, principal, input)
}

// GetInTx returns the same connection-settings snapshot through the caller's
// transaction. It does not imply concurrency safety for a later mutation: the
// endpoint, ingress, and plugin-membership writers do not share a lock/version
// protocol with this read.
func (s *MCPConnectionSettingsService) GetInTx(ctx context.Context, tx pgx.Tx, principal Principal, input GetMCPConnectionSettingsInput) (MCPConnectionSettings, error) {
	if s == nil || s.queries == nil || tx == nil {
		return MCPConnectionSettings{}, ErrUnavailable
	}
	return s.get(ctx, s.queries.WithTx(tx), principal, input)
}

func (s *MCPConnectionSettingsService) get(ctx context.Context, queries *platformrepo.Queries, principal Principal, input GetMCPConnectionSettingsInput) (MCPConnectionSettings, error) {
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
	row, err := queries.GetPlatformMCPConnectionSettings(ctx, platformrepo.GetPlatformMCPConnectionSettingsParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      projectID,
		TargetKind:     string(input.TargetKind),
		TargetID:       targetID,
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
	return MCPConnectionSettings{
		ProjectID: projectID.String(), TargetKind: input.TargetKind, TargetID: targetID.String(),
		Name: row.Name, Visibility: row.Visibility, NetworkMode: row.NetworkMode,
		Endpoints: endpoints, Ingress: ingress, PluginMemberships: memberships,
	}, nil
}

var (
	ErrMCPConnectionSettingsInvalid  = errors.New("invalid MCP connection settings target")
	ErrMCPConnectionSettingsNotFound = errors.New("MCP connection settings target not found")
)
