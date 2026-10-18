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

// Action is what staff can do with a candidate.
type Action string

const (
	// ActionMigrate switches the server in place to a remote MCP server.
	ActionMigrate Action = "migrate"

	// ActionDelete deletes the toolset and its server, for a toolset left with
	// nothing to migrate.
	ActionDelete Action = "delete"
)

// Candidate is a toolset backed by an external MCP attachment.
type Candidate struct {
	// Row is the candidate as stored.
	Row repo.ListCandidatesRow

	// Action is what running the candidate does.
	Action Action

	// DeleteReasons explain why the candidate is deleted rather than
	// migrated; empty when it migrates.
	DeleteReasons []string

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

	// Warnings name what the migration drops or cannot carry over. They do
	// not stop it.
	Warnings []string

	// ToolCalls counts the server's tool calls over the reporting window; nil
	// when telemetry is unavailable.
	ToolCalls *uint64
}

const (
	blockerNoOrganization     = "its organization has no metadata record, so it is handled separately"
	deleteNotLive             = "MCP is off and it has no server row, so nothing serves it"
	deleteMigrated            = "already migrated; its server now proxies a remote MCP server and keeps working"
	deleteNotOnDeployment     = "its external MCP attachment is no longer on the project's active deployment"
	deleteManyUpstreams       = "proxies more than one external MCP attachment, URL or transport"
	blockerTransport          = "uses a transport remote MCP servers do not support"
	blockerVariations         = "uses tool variations, which the switch would drop"
	blockerPrompts            = "has prompts, which the switch would drop"
	blockerAssistants         = "is attached to an assistant through its toolset"
	blockerSlackApps          = "is attached to a Slack app through its toolset"
	blockerToolsetPlugin      = "is in a plugin through its toolset rather than its server"
	blockerGateway            = "is a member of a gateway"
	blockerNoServer           = "has no server row; only the legacy toolset address serves it"
	blockerManyServers        = "has more than one server row"
	blockerEndpoints          = "does not have exactly one address"
	blockerServerIssuer       = "its server signs in through a different provider than its toolset"
	blockerManyUpstreamIssuer = "its sign-in provider's clients use more than one upstream authorization server"
	blockerSelfOwnedClient    = "its sign-in provider has a client that holds its own credentials"
	blockerEMABindings        = "its sign-in provider has enterprise-managed authorization bindings"
	warningExternalOAuth      = "uses an external OAuth server, which is not carried over; sign-in through it is dropped"
	warningOAuthProxy         = "has an OAuth proxy or its stored tokens, which are dropped; sign-in through the proxy stops"
	warningOtherTools         = "serves tools or resources beyond the external MCP attachment, which are dropped"
	blockerManyResources      = "its signed-in sessions record more than one upstream resource"
	blockerSharedNonRemote    = "another server signs in through the same provider and is not a remote MCP server"
	blockerSharedToolset      = "another enabled toolset signs in through the same provider"
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
	candidate := Candidate{Row: row, Action: ActionMigrate, DeleteReasons: nil, RemoteURL: "", ReplacementURL: "", Blockers: nil, Warnings: nil, ToolCalls: nil}
	block := func(reason string) { candidate.Blockers = append(candidate.Blockers, reason) }
	warn := func(reason string) { candidate.Warnings = append(candidate.Warnings, reason) }

	if reasons := deleteReasons(row); len(reasons) > 0 {
		candidate.Action = ActionDelete
		candidate.DeleteReasons = reasons
		if len(row.RemoteUrls) == 1 {
			candidate.RemoteURL = row.RemoteUrls[0]
		}
		// Deleting does not unlink a Slack app; that stays a dashboard job.
		if row.HasSlackApps {
			block(blockerSlackApps)
		}
		return candidate
	}

	if !row.HasOrganizationMetadata {
		block(blockerNoOrganization)
	}
	// Sign-in through an external OAuth server or an OAuth proxy does not carry
	// over. The switched server keeps its user session issuer, if any, and
	// reaches the upstream without upstream credentials.
	if row.ExternalOauthServerID.Valid {
		warn(warningExternalOAuth)
	}
	if row.OauthProxyServerID.Valid || row.HasUserOauthTokens {
		warn(warningOAuthProxy)
	}
	if row.ServesOtherTools {
		warn(warningOtherTools)
	}
	if !supportedTransport(row.TransportTypes[0]) {
		block(blockerTransport)
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
	// A server without a sign-in provider, or whose provider has no upstream
	// client, holds no upstream credentials, so there is nothing to carry over.
	if row.UserSessionIssuerID.Valid {
		if len(row.RemoteSessionIssuerIds) > 1 {
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
		if row.OtherToolsetsOnIssuer > 0 {
			block(blockerSharedToolset)
		}
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

// deleteReasons explain why a toolset is left with nothing to migrate. A
// candidate with none proxies exactly one attachment, URL and transport on
// its project's active deployment.
func deleteReasons(row repo.ListCandidatesRow) []string {
	var reasons []string
	switch {
	case row.MigratedInPlace:
		return []string{deleteMigrated}
	case !row.Live:
		reasons = append(reasons, deleteNotLive)
	}
	if !row.OnActiveDeployment {
		reasons = append(reasons, deleteNotOnDeployment)
	} else if row.AttachmentCount != 1 || len(row.RemoteUrls) != 1 || len(row.TransportTypes) != 1 {
		reasons = append(reasons, deleteManyUpstreams)
	}
	return reasons
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
