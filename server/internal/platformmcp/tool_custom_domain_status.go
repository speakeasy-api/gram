package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
)

// CustomDomainStatus is the organization's saved domain diagnosis, not a live probe.
type CustomDomainStatus struct {
	// Configured distinguishes an absent domain from an observation that has not run.
	Configured bool `json:"configured"`

	// Domain is the organization's configured hostname.
	Domain string `json:"domain,omitempty"`

	// Verified is the ownership verification state.
	Verified bool `json:"verified"`

	// Activated reports whether serving is enabled.
	Activated bool `json:"activated"`

	// DNSConfigurationStatus is advisory; unconfirmed includes proxies and flattening.
	DNSConfigurationStatus string `json:"dns_configuration_status,omitempty"`

	// DNSRoutingMethod identifies only the publicly visible configuration.
	DNSRoutingMethod string `json:"dns_routing_method,omitempty"`

	// InfrastructureStatus describes resources and certificates, not public reachability.
	InfrastructureStatus string `json:"infrastructure_status,omitempty"`

	// InfrastructureIssue is the saved infrastructure diagnostic code.
	InfrastructureIssue string `json:"infrastructure_issue,omitempty"`

	// CheckedAt is the DNS observation time; an empty value means no saved DNS check.
	CheckedAt string `json:"checked_at,omitempty"`
}

func readCustomDomainStatus(ctx context.Context, db *pgxpool.Pool, organizationID string) (CustomDomainStatus, error) {
	var output CustomDomainStatus
	if db == nil || organizationID == "" {
		return output, ErrUnavailable
	}
	domain, err := customdomainsrepo.New(db).GetCustomDomainByOrganization(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return output, nil
	}
	if err != nil {
		return output, fmt.Errorf("read custom domain diagnosis: %w", err)
	}
	output.Configured = true
	output.Domain = domain.Domain
	output.Verified = domain.Verified
	output.Activated = domain.Activated
	output.DNSConfigurationStatus = domain.DnsConfigurationStatus.String
	output.DNSRoutingMethod = domain.DnsRoutingMethod.String
	output.InfrastructureStatus = domain.HealthStatus.String
	output.InfrastructureIssue = domain.HealthIssue.String
	output.CheckedAt = timestampString(domain.DnsCheckedAt.Time, domain.DnsCheckedAt.Valid)
	// A DNS-only health issue can coexist with newer DNS columns during a
	// rolling upgrade; it is never an infrastructure assessment.
	if !domain.DnsCheckedAt.Valid || !domain.HealthCheckedAt.Valid || !domain.DnsCheckedAt.Time.Equal(domain.HealthCheckedAt.Time) || customdomains.IsDNSHealthIssue(customdomains.HealthIssue(domain.HealthIssue.String)) {
		output.InfrastructureStatus = "unknown"
		output.InfrastructureIssue = ""
	}
	return output, nil
}

func registerCustomDomainStatusTool(reg *Registrar, db *pgxpool.Pool, logger *slog.Logger) {
	tool := new(mcp.Tool)
	tool.Name = "get_custom_domain_status"
	tool.Title = "Get Custom Domain Status"
	tool.Description = "Read the current organization's saved custom domain ownership, activation, advisory DNS configuration, and infrastructure health. Requires organization administrator access. DNS unconfirmed includes proxies and flattened records; neither a DNS match nor healthy infrastructure proves public endpoint availability. Health observations never automatically disable a domain. Check checked_at for freshness. Use the organization's Network Access settings to recheck or change configuration. Returns no verification tokens or credentials."
	tool.Annotations = readOnlyAnnotations()
	addTool(reg, tool, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeNone, DiscoveryScopes: nil}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, CustomDomainStatus, error) {
		var noOutput CustomDomainStatus
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, noOutput, err
		}
		output, err := readCustomDomainStatus(ctx, db, principal.OrganizationID)
		if err != nil {
			if logger != nil && !errors.Is(err, ErrUnavailable) {
				logger.ErrorContext(ctx, "read custom domain diagnosis", attr.SlogError(err))
			}
			result := new(mcp.CallToolResult)
			result.IsError = true
			result.Content = []mcp.Content{&mcp.TextContent{Text: "Custom domain status is temporarily unavailable. Retry or open the organization's Network Access settings.", Meta: nil, Annotations: nil}}
			return result, noOutput, nil
		}
		return nil, output, nil
	})
}
