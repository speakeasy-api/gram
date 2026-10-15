//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/attr"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

const getNetworkIngressToolName = "get_network_ingress"

// Next actions returned by get_network_ingress. Each names the one step that
// stands between the organization and private MCP access; every step except
// waiting happens in the dashboard or with Speakeasy, never through this server.
const (
	networkIngressNextNone               = "none"
	networkIngressNextRequestEntitlement = "request_private_networking"
	networkIngressNextConfigure          = "configure_ingress_in_dashboard"
	networkIngressNextAddCredentials     = "add_credentials_in_dashboard"
	networkIngressNextEnable             = "enable_ingress_in_dashboard"
	networkIngressNextWait               = "wait_for_ingress"
	networkIngressNextRepair             = "repair_ingress_in_dashboard"
)

// networkIngressErrorCodePattern admits only the short reconcile codes the
// ingress lifecycle stores (for example provider_error). Anything else is
// withheld rather than returned, so free text can never reach a client.
var networkIngressErrorCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type NetworkIngressStatusService struct {
	logger       *slog.Logger
	queries      *platformrepo.Queries
	db           *pgxpool.Pool
	dashboardURL *url.URL
}

// WithNetworkIngressStatus enables the read-only organization private network
// ingress summary on this reader.
func (r *PostgresReader) WithNetworkIngressStatus(dashboardURL *url.URL) *PostgresReader {
	if r.db != nil && validDashboardURL(dashboardURL) {
		copyURL := *dashboardURL
		r.networkIngress = &NetworkIngressStatusService{logger: r.logger, queries: platformrepo.New(r.db), db: r.db, dashboardURL: &copyURL}
	}
	return r
}

type NetworkIngressSummary struct {
	Provider              string `json:"provider"`
	Hostname              string `json:"hostname"`
	NamespaceKind         string `json:"namespace_kind"`
	CustomDomainID        string `json:"custom_domain_id,omitempty"`
	Enabled               bool   `json:"enabled"`
	IdentityRequired      bool   `json:"identity_required"`
	CredentialsConfigured bool   `json:"credentials_configured"`
	Status                string `json:"status"`
	DNSName               string `json:"dns_name,omitempty"`
	LastError             string `json:"last_error,omitempty"`
	HealthCheckedAt       string `json:"health_checked_at,omitempty"`
	ConnectedSince        string `json:"connected_since,omitempty"`
}

type GetNetworkIngressOutput struct {
	// Entitled reports whether private networking is switched on for the
	// organization. Without it no MCP can leave public_only.
	Entitled   bool `json:"entitled"`
	Configured bool `json:"configured"`
	// ReadyForPrivateAccess mirrors the dashboard: an enabled, online ingress
	// with an observed private DNS name. It is a precondition for dual or
	// private_only, not proof that any client can reach the tailnet.
	ReadyForPrivateAccess bool                   `json:"ready_for_private_access"`
	Ingress               *NetworkIngressSummary `json:"ingress,omitempty"`
	NextAction            string                 `json:"next_action"`
	SetupURL              string                 `json:"setup_url"`
}

func (s *NetworkIngressStatusService) Get(ctx context.Context, principal Principal) (GetNetworkIngressOutput, error) {
	if s == nil || s.queries == nil || principal.OrganizationID == "" {
		return GetNetworkIngressOutput{}, ErrUnavailable
	}
	organization, err := organizationsrepo.New(s.db).GetOrganizationMetadata(ctx, principal.OrganizationID)
	if err != nil {
		return GetNetworkIngressOutput{}, fmt.Errorf("resolve network ingress organization: %w", err)
	}
	entitled, err := s.queries.GetPlatformMCPNetworkIngressEntitlement(ctx, platformrepo.GetPlatformMCPNetworkIngressEntitlementParams{
		OrganizationID: principal.OrganizationID,
		FeatureName:    string(productfeatures.FeatureNetworkIngress),
	})
	if err != nil {
		return GetNetworkIngressOutput{}, fmt.Errorf("check network ingress entitlement: %w", err)
	}
	output := GetNetworkIngressOutput{
		Entitled:   entitled,
		SetupURL:   s.dashboardURL.JoinPath(organization.Slug, "domains").String(),
		NextAction: networkIngressNextConfigure,
	}
	row, err := s.queries.GetPlatformMCPActiveNetworkIngress(ctx, principal.OrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return GetNetworkIngressOutput{}, fmt.Errorf("get active network ingress: %w", err)
	default:
		output.Configured = true
		output.Ingress = &NetworkIngressSummary{
			Provider:              row.Provider,
			Hostname:              row.Hostname,
			NamespaceKind:         row.EndpointNamespaceKind,
			CustomDomainID:        uuidString(row.CustomDomainID),
			Enabled:               row.Enabled,
			IdentityRequired:      row.IdentityRequired,
			CredentialsConfigured: row.CredentialsConfigured,
			Status:                row.Status,
			DNSName:               row.DnsName.String,
			LastError:             safeNetworkIngressErrorCode(row.LastError.String),
			HealthCheckedAt:       timestampString(row.HealthCheckedAt.Time, row.HealthCheckedAt.Valid),
			ConnectedSince:        timestampString(row.ConnectedSince.Time, row.ConnectedSince.Valid),
		}
	}
	output.NextAction = networkIngressNextAction(output)
	output.ReadyForPrivateAccess = output.NextAction == networkIngressNextNone
	return output, nil
}

func safeNetworkIngressErrorCode(value string) string {
	if !networkIngressErrorCodePattern.MatchString(value) {
		return ""
	}
	return value
}

func networkIngressNextAction(output GetNetworkIngressOutput) string {
	ingress := output.Ingress
	switch {
	case !output.Entitled:
		return networkIngressNextRequestEntitlement
	case ingress == nil:
		return networkIngressNextConfigure
	case !ingress.CredentialsConfigured:
		return networkIngressNextAddCredentials
	case !ingress.Enabled:
		return networkIngressNextEnable
	case ingress.Status == "error" || ingress.Status == "degraded":
		return networkIngressNextRepair
	case ingress.Status != "online" || ingress.DNSName == "":
		return networkIngressNextWait
	default:
		return networkIngressNextNone
	}
}

const getNetworkIngressDescription = "Read the organization's private network (Tailscale) ingress: whether private networking is switched on for the organization, whether an ingress is configured and enabled, its latest observed status and private DNS name, and the single next_action that stands between the organization and private MCP access. " +
	"ready_for_private_access is required before any MCP can use dual or private_only network access; it is not proof that a tailnet client can connect. " +
	"Creating, enabling, repairing, or supplying credentials for the ingress happens only in the dashboard at setup_url, because it requires Tailscale OAuth client credentials. Never returns credentials or provider resources."

func registerNetworkIngressTool(reg *Registrar, service *NetworkIngressStatusService) {
	addTool(reg, &mcp.Tool{
		Name:        getNetworkIngressToolName,
		Title:       "Get Private Network Ingress",
		Description: getNetworkIngressDescription,
		Annotations: readOnlyAnnotations(),
	}, networkIngressToolMeta(), func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, GetNetworkIngressOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, GetNetworkIngressOutput{}, err
		}
		output, err := service.Get(ctx, principal)
		if err != nil {
			// Database and lookup failures stay server-side: the SDK would
			// otherwise return the wrapped error text to the client.
			if !errors.Is(err, ErrUnavailable) && service != nil && service.logger != nil {
				service.logger.ErrorContext(ctx, "get network ingress status", attr.SlogError(err))
			}
			result, marshalErr := networkIngressUnavailableResult("Private network ingress status is temporarily unavailable.")
			return result, GetNetworkIngressOutput{}, marshalErr
		}
		return nil, output, nil
	})
}

func registerUnavailableNetworkIngressTool(reg *Registrar) {
	addTool(reg, &mcp.Tool{
		Name:        getNetworkIngressToolName,
		Title:       "Get Private Network Ingress",
		Description: "Read the organization's private network (Tailscale) ingress status. Private network ingress status is unavailable on this server.",
		Annotations: readOnlyAnnotations(),
	}, networkIngressToolMeta(), func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, featureUnavailableResult, error) {
		result, err := networkIngressUnavailableResult("Private network ingress status is unavailable on this server.")
		return result, featureUnavailableResult{}, err
	})
}

func networkIngressToolMeta() ToolMeta {
	return ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeNone}
}

func networkIngressUnavailableResult(message string) (*mcp.CallToolResult, error) {
	content, err := json.Marshal(featureUnavailableResult{Code: unavailableCode, Feature: "network_ingress", Message: message})
	if err != nil {
		return nil, fmt.Errorf("encode unavailable network ingress: %w", err)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, nil
}
