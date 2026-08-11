package telemetry

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

type ShadowMCPInventoryURL struct {
	GramProjectID string
	ServerURL     shadowmcp.InventoryURL
	ServerName    string
	SeenAt        time.Time
}

type BackfillShadowMCPInventoryURLsParams struct {
	GramProjectID      string
	Limit              int
	HostedMCPHostnames []string
}

type BackfillShadowMCPInventoryURLsResult struct {
	InventoryURLCount int
}

func (l *Logger) UpsertShadowMCPInventoryURLs(ctx context.Context, inventoryURLs []ShadowMCPInventoryURL) error {
	if len(inventoryURLs) == 0 || l.chConn == nil {
		return nil
	}

	params := make([]repo.UpsertShadowMCPInventoryURLParams, 0, len(inventoryURLs))
	for _, inventoryURL := range inventoryURLs {
		if inventoryURL.GramProjectID == "" || inventoryURL.ServerURL.CanonicalURL == "" {
			continue
		}

		seenAt := inventoryURL.SeenAt
		if seenAt.IsZero() {
			seenAt = time.Now()
		}

		params = append(params, repo.UpsertShadowMCPInventoryURLParams{
			GramProjectID:      inventoryURL.GramProjectID,
			CanonicalServerURL: inventoryURL.ServerURL.CanonicalURL,
			URLHost:            inventoryURL.ServerURL.URLHost,
			ServerName:         inventoryURL.ServerName,
			SeenAt:             seenAt,
			FirstSeen:          time.Time{},
			LastSeen:           time.Time{},
			UpdatedAt:          time.Now(),
		})
	}

	if len(params) == 0 {
		return nil
	}

	// This currently issues one ClickHouse point-SELECT per URL plus the
	// final insert (see repo.UpsertShadowMCPInventoryURLs); each call gets
	// its own connection-level span and duration sample (DNO-606/DNO-602).
	err := repo.New(l.chConn).UpsertShadowMCPInventoryURLs(l.detachedWriteContext(ctx), params)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "upsert shadow mcp inventory urls")
	}

	return nil
}

func (s *Service) BackfillShadowMCPInventoryURLs(ctx context.Context, params BackfillShadowMCPInventoryURLsParams) (BackfillShadowMCPInventoryURLsResult, error) {
	if params.GramProjectID == "" {
		return BackfillShadowMCPInventoryURLsResult{InventoryURLCount: 0}, nil
	}
	hostedHostnames, err := s.shadowMCPHostedHostnames(ctx, params.GramProjectID, params.HostedMCPHostnames)
	if err != nil {
		return BackfillShadowMCPInventoryURLsResult{InventoryURLCount: 0}, err
	}

	usageRows, err := s.chRepo.ListShadowMCPInventoryUsage(ctx, repo.ListShadowMCPInventoryUsageParams{
		GramProjectID:       params.GramProjectID,
		CanonicalServerURLs: nil,
		Limit:               params.Limit,
	})
	if err != nil {
		return BackfillShadowMCPInventoryURLsResult{InventoryURLCount: 0}, oops.E(oops.CodeUnexpected, err, "list shadow mcp inventory usage for backfill")
	}

	upserts := make([]repo.UpsertShadowMCPInventoryURLParams, 0, len(usageRows))
	now := time.Now()
	for _, usageRow := range usageRows {
		invURL, ok := shadowmcp.CanonicalizeInventoryURL(usageRow.CanonicalServerURL)
		if !ok || usageRow.FirstCalled == nil || usageRow.LastCalled == nil {
			continue
		}
		if shadowMCPInventoryURLHosted(invURL, hostedHostnames) {
			continue
		}

		upserts = append(upserts, repo.UpsertShadowMCPInventoryURLParams{
			GramProjectID:      params.GramProjectID,
			CanonicalServerURL: invURL.CanonicalURL,
			URLHost:            invURL.URLHost,
			ServerName:         usageRow.ServerName,
			SeenAt:             time.Time{},
			FirstSeen:          *usageRow.FirstCalled,
			LastSeen:           *usageRow.LastCalled,
			UpdatedAt:          now,
		})
	}

	if len(upserts) == 0 {
		return BackfillShadowMCPInventoryURLsResult{InventoryURLCount: 0}, nil
	}

	if err := s.chRepo.UpsertShadowMCPInventoryURLs(ctx, upserts); err != nil {
		return BackfillShadowMCPInventoryURLsResult{InventoryURLCount: 0}, oops.E(oops.CodeUnexpected, err, "upsert shadow mcp inventory urls")
	}

	return BackfillShadowMCPInventoryURLsResult{InventoryURLCount: len(upserts)}, nil
}

func (s *Service) shadowMCPHostedHostnames(ctx context.Context, projectID string, configuredHostnames []string) (map[string]struct{}, error) {
	hostnames := make(map[string]struct{}, len(configuredHostnames)+1)
	for _, hostname := range configuredHostnames {
		if normalized := normalizeShadowMCPHostedHostname(hostname); normalized != "" {
			hostnames[normalized] = struct{}{}
		}
	}

	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return hostnames, nil
	}
	project, err := s.projectsRepo.GetProjectByID(ctx, projectUUID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load project for shadow mcp hosted hostname exclusions")
	}

	customDomain, err := customdomainsrepo.New(s.db).GetCustomDomainByOrganization(ctx, project.OrganizationID)
	switch {
	case err == nil:
		if normalized := normalizeShadowMCPHostedHostname(customDomain.Domain); normalized != "" {
			hostnames[normalized] = struct{}{}
		}
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return nil, oops.E(oops.CodeUnexpected, err, "load custom domain for shadow mcp hosted hostname exclusions")
	}

	return hostnames, nil
}

func shadowMCPInventoryURLHosted(invURL shadowmcp.InventoryURL, hostedHostnames map[string]struct{}) bool {
	if len(hostedHostnames) == 0 {
		return false
	}
	hostname := normalizeShadowMCPHostedHostname(invURL.URLHost)
	if hostname == "" {
		parsed, err := url.Parse(invURL.CanonicalURL)
		if err != nil {
			return false
		}
		hostname = normalizeShadowMCPHostedHostname(parsed.Hostname())
	}
	_, ok := hostedHostnames[hostname]
	return ok
}

func normalizeShadowMCPHostedHostname(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Hostname() != "" {
		value = parsed.Hostname()
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	value = strings.Trim(value, "[]")
	value = strings.TrimSuffix(value, ".")
	return value
}
