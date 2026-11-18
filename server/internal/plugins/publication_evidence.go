package plugins

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/plugins/naming"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

// PublicationEvidence contains non-secret evidence for one selected plugin
// package. Fresh is nil when it is unknown: the project has no usable
// published fingerprint and the plugin's auto sync is on.
type PublicationEvidence struct {
	PluginSlug    string
	NotConfigured bool
	Fresh         *bool
	Packages      []PublicationPackageAddress

	// AwaitingManualSync is true when the plugin's auto sync is off and its
	// own package has changes waiting, or was never published, so only a
	// manual sync publishes it.
	AwaitingManualSync bool
}

// ErrPublicationEvidenceUnavailable is why a service can't resolve publication
// evidence: it has no authorization engine to check the caller with, as a
// publisher built by NewPublisher does not.
var ErrPublicationEvidenceUnavailable = errors.New("plugins service cannot authorize publication evidence")

// CanResolvePublicationEvidence reports whether ResolvePublicationEvidence can
// answer, so a composition root can refuse a service that would only ever
// report evidence as unavailable.
func (s *Service) CanResolvePublicationEvidence() error {
	if s == nil || s.authz == nil {
		return ErrPublicationEvidenceUnavailable
	}
	return nil
}

// PublicationPackageAddress is the resolved address used by a plugin package.
type PublicationPackageAddress struct {
	ServerName string
	MCPURL     string
}

// ResolvePublicationEvidence returns freshness and selected package addresses
// for exact plugin slugs in a project. Freshness is per plugin, exactly as the
// dashboard's publish status reports it, so another plugin's held change never
// marks this one stale. It reads Speakeasy's current package inputs and stored
// publication fingerprints, and the manifests of plugins whose auto sync is off
// and whose changes are held, which it reads from the repo once per published
// state. It does not expose the marketplace bearer URL or mint credentials.
func (s *Service) ResolvePublicationEvidence(ctx context.Context, organizationID string, projectID uuid.UUID, pluginSlugs []string) ([]PublicationEvidence, error) {
	if err := s.CanResolvePublicationEvidence(); err != nil || organizationID == "" {
		return nil, fmt.Errorf("publication evidence authorization unavailable")
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: organizationID, Dimensions: nil}); err != nil {
		return nil, fmt.Errorf("authorize publication evidence: %w", err)
	}
	project, err := projectsrepo.New(s.db).GetProjectWithOrganizationMetadata(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("resolve publication evidence project: %w", err)
	}
	if project.ID != organizationID {
		return nil, fmt.Errorf("project %s does not belong to organization %s", projectID, organizationID)
	}

	allInfos, serverURL, err := s.resolvePluginInfos(ctx, projectID)
	if err != nil {
		return nil, err
	}
	selected := make(map[string]struct{}, len(pluginSlugs))
	for _, slug := range pluginSlugs {
		if _, exists := selected[slug]; exists {
			return nil, fmt.Errorf("duplicate plugin slug %q", slug)
		}
		selected[slug] = struct{}{}
	}

	selectedInfos := make([]PluginInfo, 0, len(selected))
	for _, info := range allInfos {
		if _, ok := selected[info.Slug]; ok {
			selectedInfos = append(selectedInfos, info)
			delete(selected, info.Slug)
		}
	}
	if len(selected) > 0 {
		for slug := range selected {
			return nil, fmt.Errorf("plugin %q is not active in project %s", slug, projectID)
		}
	}

	conn, err := s.repo.GetGitHubConnection(ctx, projectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return evidenceWithAddresses(selectedInfos, nil, true), nil
		}
		return nil, fmt.Errorf("read project publication fingerprints: %w", err)
	}

	if len(conn.PublishedMcpFingerprints) == 0 {
		// A connection that predates fingerprinting can't say whether a
		// plugin with auto sync on is current. One with auto sync off records
		// no fingerprint, so an automatic publish leaves it out: it is not
		// current, and only a manual sync publishes it.
		pausedSlugs, err := s.repo.ListAutoSyncPausedPluginSlugs(ctx, projectID)
		if err != nil {
			return nil, fmt.Errorf("list plugins with auto sync off: %w", err)
		}
		paused := make(map[string]bool, len(pausedSlugs))
		for _, slug := range pausedSlugs {
			paused[slug] = true
		}
		cfg := s.generateConfig(ctx, project.ID, project.Slug, project.ProjectSlug, projectID, serverURL, conn.PublishedHooksConfig)
		if publishedName := naming.PublishedHooksOrgName(conn.PublishedHooksConfig); publishedName != "" && hooksPackagesCollide(allInfos, cfg.OrgName) && !hooksPackagesCollide(allInfos, publishedName) {
			cfg.HooksOrgName = publishedName
		}
		if err := requirePausedPluginPackagesFree(allInfos, paused, cfg); err != nil {
			return nil, err
		}
		result := evidenceWithAddresses(selectedInfos, nil, false)
		for i := range result {
			if paused[result[i].PluginSlug] {
				result[i].Fresh = new(false)
				result[i].AwaitingManualSync = true
			}
		}
		return result, nil
	}
	observabilityEnabled, err := s.projectObservabilityEnabled(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("read observability plugin setting: %w", err)
	}
	freshness, err := s.resolveMarketplaceFreshness(ctx, project.ID, project.Slug, project.ProjectSlug, projectID, allInfos, serverURL, conn, observabilityEnabled)
	if err != nil {
		return nil, fmt.Errorf("compare with the published marketplace: %w", err)
	}
	result := make([]PublicationEvidence, 0, len(selectedInfos))
	for _, info := range selectedInfos {
		result = append(result, PublicationEvidence{
			PluginSlug:         info.Slug,
			NotConfigured:      false,
			Fresh:              new(freshness.Plugins[info.Slug]),
			Packages:           publicationAddresses(info),
			AwaitingManualSync: freshness.AwaitingManualSync(info.Slug),
		})
	}
	return result, nil
}

func evidenceWithAddresses(infos []PluginInfo, fresh *bool, notConfigured bool) []PublicationEvidence {
	result := make([]PublicationEvidence, 0, len(infos))
	for _, info := range infos {
		result = append(result, PublicationEvidence{
			PluginSlug:         info.Slug,
			NotConfigured:      notConfigured,
			Fresh:              fresh,
			Packages:           publicationAddresses(info),
			AwaitingManualSync: false,
		})
	}
	return result
}

func publicationAddresses(info PluginInfo) []PublicationPackageAddress {
	packages := make([]PublicationPackageAddress, 0, len(info.Servers))
	for _, server := range info.Servers {
		packages = append(packages, PublicationPackageAddress{
			ServerName: server.DisplayName,
			MCPURL:     server.MCPURL,
		})
	}
	return packages
}
