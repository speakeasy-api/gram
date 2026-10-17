// Package migration switches hosted toolsets that proxy an external MCP
// attachment over to remote MCP servers, in place. The server row keeps its
// id, address, plugins, grants and user session issuer, and the remote
// session clients behind the issuer keep working, so people stay signed in;
// a migration never creates an issuer or a client and never registers one
// with the upstream.
package migration

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/migration/repo"
)

// Candidate is a live server backed by an external MCP attachment.
type Candidate struct {
	// Row is the candidate as stored.
	Row repo.ListCandidatesRow

	// RemoteURL is the attachment's upstream URL, or empty when the server
	// references several.
	RemoteURL string

	// ReplacementURL is the upstream URL the server dials once switched: the
	// one resource its signed-in sessions recorded, otherwise RemoteURL
	// without unfilled template query parameters. Empty when blocked before
	// it could be derived.
	ReplacementURL string

	// Blockers explain why the server cannot be migrated; empty when it can.
	Blockers []string

	// ToolCalls counts the server's tool calls over the reporting window; nil
	// when telemetry is unavailable.
	ToolCalls *uint64
}

const (
	blockerNoOrganization     = "its organization has no metadata record, so it is handled separately"
	blockerNoIssuer           = "has no sign-in provider to reuse"
	blockerExternalOAuth      = "uses an external OAuth server, which remote MCP servers do not support"
	blockerOAuthProxy         = "uses an OAuth proxy, which remote MCP servers do not support"
	blockerUserOAuthTokens    = "has stored OAuth proxy tokens, which remote MCP servers do not use" //nolint:gosec // a reason shown to staff, not a credential
	blockerUserHeaders        = "users send their own credentials with each request; handle this server manually"
	blockerManyAttachments    = "proxies more than one external MCP attachment"
	blockerManyURLs           = "proxies more than one upstream URL"
	blockerManyTransports     = "proxies upstreams over more than one transport"
	blockerOtherTools         = "serves tools or resources beyond the external MCP attachment, which the switch would drop"
	blockerVariations         = "uses tool variations, which the switch would drop"
	blockerPrompts            = "has prompts, which the switch would drop"
	blockerAssistants         = "is attached to an assistant through its toolset"
	blockerSlackApps          = "is attached to a Slack app through its toolset"
	blockerToolsetPlugin      = "is in a plugin through its toolset rather than its server"
	blockerCollections        = "is in an organization collection"
	blockerGateway            = "is a member of a gateway"
	blockerNoServer           = "has no server row; only the legacy toolset address serves it"
	blockerManyServers        = "has more than one server row"
	blockerEndpoints          = "does not have exactly one address"
	blockerServerIssuer       = "its server signs in through a different provider than its toolset"
	blockerNoUpstreamIssuer   = "its sign-in provider has no upstream client"
	blockerManyUpstreamIssuer = "its sign-in provider's clients use more than one upstream authorization server"
	blockerSelfOwnedClient    = "its sign-in provider has a client that holds its own credentials"
	blockerEMABindings        = "its sign-in provider has enterprise-managed authorization bindings"
	blockerManyResources      = "its signed-in sessions record more than one upstream resource"
	blockerSharedNonRemote    = "another server signs in through the same provider and is not a remote MCP server"
	blockerSharedOtherURL     = "another remote MCP server on the same sign-in provider dials a different upstream"
)

// ListCandidates returns every live external-MCP-backed server across all
// organizations, with its blockers.
func (m *Migrator) ListCandidates(ctx context.Context) ([]Candidate, error) {
	rows, err := repo.New(m.db).ListCandidates(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("list external MCP candidates: %w", err)
	}
	candidates := make([]Candidate, 0, len(rows))
	for _, row := range rows {
		candidates = append(candidates, assess(row))
	}
	if m.toolCalls != nil {
		counts, err := m.toolCalls.CountToolCalls(ctx, candidates)
		if err != nil {
			m.logger.WarnContext(ctx, "count external MCP candidate tool calls", attr.SlogError(err))
		} else {
			for i := range candidates {
				count := counts[candidates[i].Row.ToolsetID]
				candidates[i].ToolCalls = &count
			}
		}
	}
	return candidates, nil
}

// assess derives a candidate's upstream URLs and blockers from its row.
func assess(row repo.ListCandidatesRow) Candidate {
	candidate := Candidate{Row: row, RemoteURL: "", ReplacementURL: "", Blockers: nil, ToolCalls: nil}
	block := func(reason string) { candidate.Blockers = append(candidate.Blockers, reason) }

	if !row.HasOrganizationMetadata {
		block(blockerNoOrganization)
	}
	if !row.UserSessionIssuerID.Valid || !row.UserSessionIssuerSlug.Valid {
		block(blockerNoIssuer)
	}
	if row.ExternalOauthServerID.Valid {
		block(blockerExternalOAuth)
	}
	if row.OauthProxyServerID.Valid {
		block(blockerOAuthProxy)
	}
	if row.HasUserOauthTokens {
		block(blockerUserOAuthTokens)
	}
	if row.DeclaresHeaders && !row.RequiresOauth {
		block(blockerUserHeaders)
	}
	if row.AttachmentCount != 1 {
		block(blockerManyAttachments)
	}
	if len(row.TransportTypes) != 1 || !supportedTransport(row.TransportTypes[0]) {
		block(blockerManyTransports)
	}
	if row.ServesOtherTools {
		block(blockerOtherTools)
	}
	if row.ToolsetHasVariations || row.ServerHasVariations {
		block(blockerVariations)
	}
	if row.HasPrompts {
		block(blockerPrompts)
	}
	if row.HasAssistants {
		block(blockerAssistants)
	}
	if row.HasSlackApps {
		block(blockerSlackApps)
	}
	if row.HasToolsetPluginEntries {
		block(blockerToolsetPlugin)
	}
	if row.HasCollectionAttachments {
		block(blockerCollections)
	}
	if row.InGateway {
		block(blockerGateway)
	}
	switch {
	case row.ServerCount == 0:
		block(blockerNoServer)
	case row.ServerCount > 1:
		block(blockerManyServers)
	case row.EndpointCount != 1:
		block(blockerEndpoints)
	case !row.McpSlugIsEndpoint:
		block(fmt.Sprintf("its legacy address /mcp/%s is not one of its endpoints and would stop working", row.McpSlug.String))
	}
	if row.ServerUserSessionIssuerID.Valid && row.ServerUserSessionIssuerID != row.UserSessionIssuerID {
		block(blockerServerIssuer)
	}
	if row.UserSessionIssuerID.Valid {
		switch len(row.RemoteSessionIssuerIds) {
		case 0:
			block(blockerNoUpstreamIssuer)
		case 1:
		default:
			block(blockerManyUpstreamIssuer)
		}
		if row.HasSelfOwnedClients {
			block(blockerSelfOwnedClient)
		}
		if row.HasEmaBindings {
			block(blockerEMABindings)
		}
		if row.OtherNonRemoteServersOnIssuer > 0 {
			block(blockerSharedNonRemote)
		}
	}

	if len(row.RemoteUrls) != 1 {
		block(blockerManyURLs)
		return candidate
	}
	candidate.RemoteURL = row.RemoteUrls[0]
	replacement, err := replacementURL(candidate.RemoteURL, row.SessionResources)
	if err != nil {
		block(err.Error())
		return candidate
	}
	candidate.ReplacementURL = replacement
	for _, other := range row.OtherRemoteUrlsOnIssuer {
		if !sameUpstream(other, replacement) {
			block(blockerSharedOtherURL)
			break
		}
	}
	return candidate
}

// replacementURL is the URL the switched server dials. Existing upstream
// credentials only route to a remote backend whose URL equals their stored
// resource, so a single recorded resource wins over the attachment's URL.
func replacementURL(attachmentURL string, sessionResources []string) (string, error) {
	switch len(sessionResources) {
	case 0:
		return CleanRemoteURL(attachmentURL)
	case 1:
		if _, err := CleanRemoteURL(sessionResources[0]); err != nil {
			return "", fmt.Errorf("signed-in sessions record an unusable upstream resource: %w", err)
		}
		return sessionResources[0], nil
	default:
		return "", fmt.Errorf("%s", blockerManyResources)
	}
}

func supportedTransport(transport string) bool {
	return transport == "streamable-http" || transport == "sse"
}

// loadCandidate re-reads one candidate inside the migration transaction. It
// returns nil when the toolset is no longer a candidate.
func loadCandidate(ctx context.Context, tx pgx.Tx, toolsetID uuid.UUID) (*Candidate, error) {
	rows, err := repo.New(tx).ListCandidates(ctx, []uuid.UUID{toolsetID})
	if err != nil {
		return nil, fmt.Errorf("reload external MCP candidate: %w", err)
	}
	if len(rows) != 1 {
		return nil, nil
	}
	candidate := assess(rows[0])
	return &candidate, nil
}

// SignInProvider names the candidate's user session issuer and the upstream
// authorization servers its clients sign in through.
func (c Candidate) SignInProvider() string {
	if !c.Row.UserSessionIssuerSlug.Valid {
		return ""
	}
	if len(c.Row.RemoteIssuerNames) == 0 {
		return c.Row.UserSessionIssuerSlug.String
	}
	return c.Row.UserSessionIssuerSlug.String + " (" + strings.Join(c.Row.RemoteIssuerNames, ", ") + ")"
}
