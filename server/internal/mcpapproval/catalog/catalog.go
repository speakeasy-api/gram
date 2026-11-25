// Package catalog matches a requested server URL against the research project's
// selected MCP catalog and reads the matched entry's provenance signals.
package catalog

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/provenance"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

// Match is one registry entry whose remote URL matches a requested server.
type Match struct {
	// Registry is the display name of the catalog that supplies the entry.
	Registry string

	// Specifier is the registry's identifier for the entry, e.g.
	// `io.github.user/server`.
	Specifier string

	// Provenance is the entry's maturity and popularity signals — the
	// registry's claims, per that package's caveats.
	Provenance provenance.Provenance
}

// Catalog selects exactly one organization catalog and its reviewed reader.
type Catalog interface {
	SelectedSource(context.Context, string, string) (externalmcp.CatalogSource, error)
	ReaderFor(externalmcp.CatalogSource) (externalmcp.RegistryReader, error)
}
type projectReader interface {
	GetProjectByID(context.Context, uuid.UUID) (projectrepo.Project, error)
}
type organizationReader interface {
	GetOrganizationMetadata(context.Context, string) (orgrepo.OrganizationMetadatum, error)
}

// Source looks up evidence only in the research project's selected catalog.
type Source struct {
	logger        *slog.Logger
	projects      projectReader
	organizations organizationReader
	catalog       Catalog
}

func New(logger *slog.Logger, db *pgxpool.Pool, catalog Catalog) *Source {
	return &Source{logger: logger.With(attr.SlogComponent("mcpapproval-catalog")), projects: projectrepo.New(db), organizations: orgrepo.New(db), catalog: catalog}
}

// Lookup resolves organization identity from the trusted research project, not
// HTTP auth context: workers and HTTP intake use the same routing boundary.
// A successful empty list means checked-and-absent. A selected-source failure
// is an evidence gap, never permission to consult a different catalog.
func (s *Source) Lookup(ctx context.Context, projectID uuid.UUID, serverURL string) (*Match, error) {
	canonical, ok := shadowmcp.CanonicalizeInventoryURL(serverURL)
	if !ok {
		return nil, nil
	}
	if projectID == uuid.Nil {
		return nil, fmt.Errorf("catalog evidence requires a research project")
	}
	project, err := s.projects.GetProjectByID(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("resolve catalog project: %w", err)
	}
	if project.OrganizationID == "" {
		return nil, fmt.Errorf("catalog project has no organization")
	}
	org, err := s.organizations.GetOrganizationMetadata(ctx, project.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("resolve catalog organization: %w", err)
	}
	source, err := s.catalog.SelectedSource(ctx, project.OrganizationID, org.Slug)
	if err != nil {
		return nil, fmt.Errorf("select evidence catalog: %w", err)
	}
	reader, err := s.catalog.ReaderFor(source)
	if err != nil {
		return nil, fmt.Errorf("resolve evidence catalog reader: %w", err)
	}
	var result externalmcp.ListServersResult
	if retained, ok := reader.(interface {
		ListEvidenceServers(context.Context, externalmcp.Registry) (externalmcp.ListServersResult, error)
	}); ok {
		result, err = retained.ListEvidenceServers(ctx, source.Registry)
	} else {
		result, err = reader.ListServers(ctx, source.Registry, externalmcp.ListServersParams{Search: nil})
	}
	if err != nil {
		return nil, fmt.Errorf("list catalog servers: %w", err)
	}
	for _, entry := range result.Servers {
		for _, remote := range entry.Remotes {
			remoteCanonical, ok := shadowmcp.CanonicalizeInventoryURL(remote.URL)
			if !ok || remoteCanonical.CanonicalURL != canonical.CanonicalURL {
				continue
			}
			return &Match{Registry: source.Name, Specifier: entry.RegistrySpecifier, Provenance: provenance.Read(entry.Meta)}, nil
		}
	}
	return nil, nil
}
