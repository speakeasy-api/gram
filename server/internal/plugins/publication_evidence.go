package plugins

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

// PublicationEvidence contains non-secret evidence for one selected plugin
// package. Fresh is nil when the project has no usable published fingerprint.
type PublicationEvidence struct {
	PluginSlug    string
	NotConfigured bool
	Fresh         *bool
	Packages      []PublicationPackageAddress
}

// PublicationPackageAddress is the resolved address used by a plugin package.
type PublicationPackageAddress struct {
	ServerName string
	MCPURL     string
}

// ResolvePublicationEvidence returns freshness and selected package addresses
// for exact plugin slugs in a project. It only reads Gram's current package
// inputs and stored publication fingerprints. It does not contact GitHub, expose
// the marketplace bearer URL, or mint credentials.
func (s *Service) ResolvePublicationEvidence(ctx context.Context, organizationID string, projectID uuid.UUID, pluginSlugs []string) ([]PublicationEvidence, error) {
	project, err := projectsrepo.New(s.db).GetProjectWithOrganizationMetadata(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("resolve publication evidence project: %w", err)
	}
	if project.ID != organizationID {
		return nil, fmt.Errorf("project %s does not belong to organization %s", projectID, organizationID)
	}

	allInfos, err := s.resolvePluginInfos(ctx, projectID)
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

	observabilityEnabled, err := s.projectObservabilityEnabled(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("read observability plugin setting: %w", err)
	}
	cfg := s.generateConfig(ctx, project.ID, project.Slug, project.ProjectSlug, projectID)
	fingerprints, err := MCPFingerprints(allInfos, cfg, observabilityEnabled)
	if err != nil {
		return nil, fmt.Errorf("compute publication fingerprints: %w", err)
	}
	result := make([]PublicationEvidence, 0, len(selectedInfos))
	for _, info := range selectedInfos {
		result = append(result, PublicationEvidence{
			PluginSlug:    info.Slug,
			NotConfigured: false,
			Fresh:         publicationFreshness(conn.PublishedMcpFingerprints, fingerprints, info.Slug),
			Packages:      publicationAddresses(info),
		})
	}
	return result, nil
}

func publicationFreshness(raw []byte, current map[string]string, slug string) *bool {
	if len(raw) == 0 {
		return nil
	}

	published := decodeMCPFingerprints(raw)
	currentPlugin, hasCurrentPlugin := current[slug]
	publishedPlugin, hasPublishedPlugin := published[slug]
	currentShared, hasCurrentShared := current[mcpSharedFingerprintKey]
	publishedShared, hasPublishedShared := published[mcpSharedFingerprintKey]
	fresh := hasCurrentPlugin && hasPublishedPlugin && hasCurrentShared && hasPublishedShared &&
		currentPlugin == publishedPlugin && currentShared == publishedShared
	return &fresh
}

func evidenceWithAddresses(infos []PluginInfo, fresh *bool, notConfigured bool) []PublicationEvidence {
	result := make([]PublicationEvidence, 0, len(infos))
	for _, info := range infos {
		result = append(result, PublicationEvidence{
			PluginSlug:    info.Slug,
			NotConfigured: notConfigured,
			Fresh:         fresh,
			Packages:      publicationAddresses(info),
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
