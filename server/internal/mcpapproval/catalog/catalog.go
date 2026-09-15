// Package catalog matches a requested server URL against the MCP registries
// this deployment already consults, and reads what the matched entry declares.
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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/capability"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/provenance"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

// Match is one registry entry whose remote URL matches a requested server.
type Match struct {
	// Registry is the display name of the registry that catalogues the entry.
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

// Source looks requested servers up in the configured MCP registries.
type Source struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	client *externalmcp.RegistryClient
}

func New(logger *slog.Logger, db *pgxpool.Pool, client *externalmcp.RegistryClient) *Source {
	return &Source{
		logger: logger.With(attr.SlogComponent("mcpapproval-catalog")),
		db:     db,
		client: client,
	}
}

// Lookup finds the registry entry whose remote endpoint matches serverURL.
//
// A nil match with a nil error means every registry answered and none
// catalogues the URL — checked-and-absent, distinct from a lookup failure. A
// matched entry whose details fetch fails still returns the match, with Tools
// nil: provenance comes from the already-fetched list entry and stands on its
// own. includeTools false skips the details fetch entirely, for callers that
// already have the server's own declarations and want provenance only.
func (s *Source) Lookup(ctx context.Context, serverURL string, includeTools bool) (*Match, error) {
	canonical, ok := shadowmcp.CanonicalizeInventoryURL(serverURL)
	if !ok {
		return nil, nil
	}

	registries, err := repo.New(s.db).ListMCPRegistries(ctx)
	if err != nil {
		return nil, fmt.Errorf("list mcp registries: %w", err)
	}

	var firstErr error
	for _, row := range registries {
		registry := externalmcp.Registry{ID: row.ID, URL: row.Url}
		result, err := s.client.ListServers(ctx, registry, externalmcp.ListServersParams{Search: nil})
		if err != nil {
			// Logged per registry: a later registry may still match, and a
			// match ends the loop with no error — this line is then the only
			// record that one catalog went unconsulted.
			s.logger.WarnContext(ctx, "catalog registry could not be listed", attr.SlogError(err))
			if firstErr == nil {
				firstErr = fmt.Errorf("list catalog servers: %w", err)
			}
			continue
		}

		for _, entry := range result.Servers {
			for _, remote := range entry.Remotes {
				remoteCanonical, ok := shadowmcp.CanonicalizeInventoryURL(remote.URL)
				if !ok || remoteCanonical.CanonicalURL != canonical.CanonicalURL {
					continue
				}

				match := &Match{
					Registry:   row.Name,
					Specifier:  entry.RegistrySpecifier,
					Provenance: provenance.Read(entry.Meta),
					Tools:      nil,
				}
				if !includeTools {
					return match, nil
				}

				details, err := s.client.GetServerDetails(ctx, registry, entry.RegistrySpecifier, []string{remote.URL})
				if err != nil {
					s.logger.WarnContext(ctx, "catalog entry matched but details fetch failed", attr.SlogError(err))
					return match, nil
				}
				match.Tools = declarations(details)

				return match, nil
			}
		}
	}

	// A failure on one registry with no match anywhere must read as a failed
	// lookup, not as checked-and-absent: the entry could live in the registry
	// that did not answer.
	if firstErr != nil {
		return nil, firstErr
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
