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
	"unicode/utf8"

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
// as plain booleans, so those two hints — when the server published an
// annotations object at all — arrive as false when omitted, which is also
// what the MCP spec defines as their default. destructiveHint and
// openWorldHint are *bool passthrough and stay nil when omitted, preserving
// true field presence. A tool with no annotations object at all surfaces as
// undeclared on every hint. The registry catalog path reads raw JSON and
// preserves field presence for all four.
func (p *Probe) ListToolDeclarations(ctx context.Context, serverURL string) ([]capability.Declaration, error) {
	// Retries are disabled: the assembler bounds this source with its own
	// deadline, and an unreachable host should spend one connection attempt,
	// not several.
	client, err := externalmcp.NewClient(ctx, p.logger, p.guardian, serverURL, externalmcptypes.TransportTypeStreamableHTTP, &externalmcp.ClientOptions{
		Authorization:    "",
		Headers:          nil,
		DisableRetries:   true,
		MaxResponseBytes: 0,
	})
	if err != nil {
		return nil, fmt.Errorf("connect for tool declarations: %w", err)
	}
	defer o11y.NoLogDefer(client.Close)

	tools, err := client.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tool declarations: %w", err)
	}

	// The listing feeds a stored evidence document, so a hostile or broken
	// server must not be able to inflate it without bound. Exceeding the tool
	// cap fails the whole probe — the evidence then records a gap, which is
	// honest: a partial listing presented as complete would read as "the
	// server declared less".
	if len(tools) > maxToolDeclarations {
		return nil, fmt.Errorf("server declared %d tools, exceeding the %d-tool evidence cap", len(tools), maxToolDeclarations)
	}

	declarations := make([]capability.Declaration, 0, len(tools))
	for _, tool := range tools {
		declaration := capability.Declaration{
			Name:        boundField(tool.Name),
			Description: boundField(tool.Description),
			InputSchema: boundSchema(string(tool.Schema)),
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

// maxToolDeclarations bounds how many tools one probe carries into the
// evidence document. Far above any legitimate server; a listing past it is
// treated as a failed probe rather than truncated, so the cap can never make
// a server read as having declared fewer tools than it did.
const maxToolDeclarations = 500

// maxDeclarationFieldBytes bounds each declaration field carried into the
// evidence document.
const maxDeclarationFieldBytes = 4096

// truncationMarker flags a clipped field so a bounded value can never pass as
// the server's own words.
const truncationMarker = "…[truncated]"

// boundField clips one free-text declaration field at the byte cap, marking
// the cut explicitly. The marker fits inside the cap and the cut retreats to
// a rune boundary, so a bounded field never exceeds maxDeclarationFieldBytes
// and never carries a mangled half-rune into the stored document.
func boundField(value string) string {
	if len(value) <= maxDeclarationFieldBytes {
		return value
	}

	cut := maxDeclarationFieldBytes - len(truncationMarker)
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}

	return value[:cut] + truncationMarker
}

// boundSchema drops an oversized input schema entirely instead of clipping
// it: truncated JSON would surface only the schema-implied capabilities that
// happen to sit in its head, silently reading as "declared less". An empty
// schema is the already-documented unreadable-schema outcome — it implies
// nothing and proves nothing.
func boundSchema(schema string) string {
	if len(schema) <= maxDeclarationFieldBytes {
		return schema
	}

	return ""
}
