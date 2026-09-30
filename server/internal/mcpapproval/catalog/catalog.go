// Package catalog matches a requested server URL against the research project's
// selected MCP catalog and reads what the matched entry declares.
//
// This is the route to tool declarations that needs no connection to the
// server at all: the registry's entry carries complete tool definitions and
// the maturity signals the provenance package parses. Both are the registry's
// copy of the server's claims — one step further from the source than a
// direct tools/list, which is why the evidence document labels capabilities
// with where they came from. For OAuth-protected servers that refuse
// unauthenticated callers, this copy is the only one available before anyone
// consents.
package catalog

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/capability"
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

	// Tools is the entry's declared tool list. Nil when the details fetch
	// failed, and also when it succeeded without carrying tool metadata for
	// the matched remote — the registry not publishing declarations is
	// unknown, never "declared zero tools". A catalogued server whose
	// registry entry genuinely declared an empty tool list is an empty
	// slice.
	Tools []capability.Declaration
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
// Detail failure retains matched provenance while leaving tools unknown.
func (s *Source) Lookup(ctx context.Context, projectID uuid.UUID, serverURL string, includeTools bool) (*Match, error) {
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
			match := &Match{Registry: source.Name, Specifier: entry.RegistrySpecifier, Provenance: provenance.Read(entry.Meta), Tools: nil}
			if !includeTools {
				return match, nil
			}
			details, err := reader.GetServerDetails(ctx, source.Registry, entry.RegistrySpecifier, []string{remote.URL})
			if err != nil {
				s.logger.WarnContext(ctx, "catalog entry matched but details fetch failed", attr.SlogError(err))
				return match, nil
			}
			match.Tools = declarations(details)
			return match, nil
		}
	}
	return nil, nil
}

// declarations maps the registry's tool definitions onto the capability
// package's declaration shape. Absent annotations stay nil — an unannotated
// tool must never read as declared-safe.
//
// A nil details.Tools stays nil: the details fetch succeeds without tool
// metadata whenever the registry lacks the tools extension for the matched
// remote, and mapping that onto an empty slice would turn "the registry
// published no declarations" into "the registry declared zero tools". Only a
// genuinely-declared empty list comes back as an empty slice.
func declarations(details *externalmcp.ServerDetails) []capability.Declaration {
	if details.Tools == nil {
		return nil
	}

	out := make([]capability.Declaration, 0, len(details.Tools))
	for _, tool := range details.Tools {
		out = append(out, capability.Declaration{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: string(tool.InputSchema),
			ReadOnly:    annotationHint(tool.Annotations, "readOnlyHint"),
			Destructive: annotationHint(tool.Annotations, "destructiveHint"),
			Idempotent:  annotationHint(tool.Annotations, "idempotentHint"),
			OpenWorld:   annotationHint(tool.Annotations, "openWorldHint"),
		})
	}

	return out
}

// annotationHint reads one boolean hint from a tool's annotation map. A false
// hint is a real declaration and survives; only an absent or non-boolean value
// yields nil.
func annotationHint(annotations map[string]any, key string) *bool {
	value, ok := annotations[key].(bool)
	if !ok {
		return nil
	}

	return &value
}
