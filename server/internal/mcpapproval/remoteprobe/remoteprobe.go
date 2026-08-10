// Package remoteprobe gathers what a remote MCP server publishes about
// itself, for the approval evidence document: its OAuth metadata through the
// standard well-known endpoints, and its tool declarations through an
// unauthenticated tools/list.
//
// Both are declarations by the server, gathered without any credential — the
// probe never authenticates, so a server that answers only to authenticated
// callers yields nothing here, which the evidence records as a gap rather
// than as an absence of authority.
package remoteprobe

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	externalmcptypes "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/authority"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/capability"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

type Probe struct {
	logger   *slog.Logger
	guardian *guardian.Policy
}

func New(logger *slog.Logger, guardianPolicy *guardian.Policy) *Probe {
	return &Probe{
		logger:   logger.With(attr.SlogComponent("mcpapproval-remoteprobe")),
		guardian: guardianPolicy,
	}
}

// DiscoverAuthority reads the server's published OAuth metadata via the
// RFC 9728 protected-resource and RFC 8414 authorization-server well-known
// endpoints.
//
// A nil declaration with a nil error means discovery ran and the server
// publishes no OAuth metadata — a real outcome the caller must keep distinct
// from a failed probe. Nothing about credentials it might demand at install
// (headers, environment variables) is knowable this way; those come from
// registry declarations, not from the server.
func (p *Probe) DiscoverAuthority(ctx context.Context, serverURL string) (*authority.Declaration, error) {
	result, err := externalmcp.DiscoverOAuthMetadata(ctx, p.logger, p.guardian, "", serverURL)
	if err != nil {
		return nil, fmt.Errorf("discover oauth metadata: %w", err)
	}

	// Version "none" with no endpoints is discovery finding nothing, not the
	// server declaring it needs nothing. Returning a declaration here would
	// summarise as "no credential requirement published", which overstates a
	// pair of 404s on well-known URLs.
	if result == nil || (result.Version == "none" && result.RegistrationEndpoint == "" && len(result.ScopesSupported) == 0) {
		// Finding nothing because the probes could not run — unreachable
		// host, TLS failure, 5xx — is a failed probe, not an absence of
		// published metadata; it must land in the document's gaps.
		if result != nil && result.ProbeIncomplete {
			return nil, errors.New("oauth discovery probes did not complete")
		}
		return nil, nil
	}

	return &authority.Declaration{
		Transport:            "http",
		RequiresOAuth:        result.Version != "none",
		OAuthVersion:         result.Version,
		RegistrationEndpoint: result.RegistrationEndpoint,
		Scopes:               result.ScopesSupported,
		Credentials:          nil,
		UnauthenticatedTools: nil,
	}, nil
}

// ListToolDeclarations connects to the server without credentials and asks it
// to list its tools.
//
// The declarations are the server's own words about itself — annotations and
// schemas, per the capability package's framing — and a server that refuses
// unauthenticated callers yields an error, which the evidence records as
// could-not-consult rather than as a clean empty list.
//
// One transport caveat: the MCP SDK carries readOnlyHint and idempotentHint
// as plain booleans, so a hint the server omitted arrives as false — which is
// also what the MCP spec defines as their default. A tool that publishes an
// annotations object therefore always carries all four hints here, with
// omitted ones resolved to their spec defaults; only a tool with no
// annotations at all surfaces as undeclared. The registry catalog path reads
// raw JSON and preserves true field presence.
func (p *Probe) ListToolDeclarations(ctx context.Context, serverURL string) ([]capability.Declaration, error) {
	// Retries are disabled: the assembler bounds this source with its own
	// deadline, and an unreachable host should spend one connection attempt,
	// not several.
	client, err := externalmcp.NewClient(ctx, p.logger, p.guardian, serverURL, externalmcptypes.TransportTypeStreamableHTTP, &externalmcp.ClientOptions{
		Authorization:  "",
		Headers:        nil,
		DisableRetries: true,
	})
	if err != nil {
		return nil, fmt.Errorf("connect for tool declarations: %w", err)
	}
	defer o11y.NoLogDefer(client.Close)

	tools, err := client.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tool declarations: %w", err)
	}

	declarations := make([]capability.Declaration, 0, len(tools))
	for _, tool := range tools {
		declaration := capability.Declaration{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: string(tool.Schema),
			ReadOnly:    nil,
			Destructive: nil,
			Idempotent:  nil,
			OpenWorld:   nil,
		}
		if tool.Annotations != nil {
			declaration.ReadOnly = tool.Annotations.ReadOnlyHint
			declaration.Destructive = tool.Annotations.DestructiveHint
			declaration.Idempotent = tool.Annotations.IdempotentHint
			declaration.OpenWorld = tool.Annotations.OpenWorldHint
		}
		declarations = append(declarations, declaration)
	}

	return declarations, nil
}
