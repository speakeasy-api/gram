// The gateway's connection_status tool and the MCP Apps view that renders it.

package mcp

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// handleMetaConnectionStatusCall reports stored consent outcomes for cached
// connections, otherwise the credentials currently available for each member.
// It does not attempt identity chaining or probe upstream servers.
func (s *Service) handleMetaConnectionStatusCall(
	ctx context.Context,
	logger *slog.Logger,
	gate *metaGateContext,
	members []metaMember,
	req *rawRequest,
) (json.RawMessage, error) {
	servers := make([]metamcp.ConnectionStatusServer, 0, len(members)+len(gate.unapprovedMembers)+len(gate.hiddenMembers))
	requiresAuth := s.memberAuthRequirements(gate.organizationID, members)
	for _, member := range members {
		memberCtx := contextvalues.WithConnectionReferenceSlot(ctx)
		row := s.memberSignInStatus(memberCtx, logger, gate, member, requiresAuth)
		row.Slug, row.Name = member.slug, member.name
		withStatusReference(memberCtx, &row)
		servers = append(servers, row)
	}
	// A cached connection reports members its consent did not approve,
	// whether unavailable at consent or added since, so the person knows
	// renewed consent would add them.
	if gate.catalog != nil && gate.catalogRef != nil && gate.catalogRef.Mode == toolfilter.CatalogModeCached {
		for _, member := range gate.unapprovedMembers {
			servers = append(servers, metamcp.ConnectionStatusServer{
				Slug:      member.slug,
				Name:      member.name,
				Status:    metamcp.SignInNeedsConsent,
				Detail:    catalogMemberNotApproved,
				Reconnect: false,
				Outage:    nil,
			})
		}
	}

	for _, member := range gate.hiddenMembers {
		servers = append(servers, metamcp.ConnectionStatusServer{
			Slug:      member.slug,
			Name:      member.name,
			Status:    metamcp.SignInHidden,
			Detail:    "",
			Reconnect: false,
			Outage:    nil,
		})
	}

	payload := metamcp.ConnectionStatusResult{ConnectURL: gate.connectURL, Audience: metamcp.ConnectionStatusAudienceCaller, Servers: servers}
	if gate.agentFix != nil {
		payload.Audience = metamcp.ConnectionStatusAudienceAgentEditors
		payload.ConnectURL = agentConnectURL(*gate, members, servers)
	}
	structured, err := json.Marshal(payload)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "serialize connection_status result").LogError(ctx, logger)
	}

	chunk, err := json.Marshal(contentChunk[string, json.RawMessage]{
		Type:     "text",
		MimeType: nil,
		Text:     payload.Text(),
		Data:     nil,
		Meta:     nil,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "serialize connection_status content").LogError(ctx, logger)
	}
	bs, err := json.Marshal(&result[toolCallResult]{
		ID: req.ID,
		Result: toolCallResult{
			Content:           []json.RawMessage{chunk},
			StructuredContent: structured,
			IsError:           false,
		},
		serverIdentity: serverInfoMetaServer,
		cacheHints:     nil,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize tools/call response").LogError(ctx, logger)
	}
	return bs, nil
}

// withStatusReference adds the reference of a row this check stored to a member reporting a
// problem. Most problems here are read from stored state and store no row, so show none.
func withStatusReference(ctx context.Context, row *metamcp.ConnectionStatusServer) {
	line := contextvalues.ConnectionReferenceLine(ctx)
	if line == "" || row.Status == metamcp.SignInConnected || row.Status == metamcp.SignInNotRequired {
		return
	}
	row.Detail = withReferenceSentence(row.Detail, line)
	if row.Outage != nil {
		row.Outage.Message = withReferenceSentence(row.Outage.Message, line)
	}
}

// withReferenceSentence ends a status text with the reference, as one more sentence.
func withReferenceSentence(text, line string) string {
	if text == "" {
		return line
	}
	return strings.TrimSuffix(text, ".") + ". " + line
}

// agentConnectURL is the agent's sign-in step for the first row it fixes, or
// empty when no row needs it or the page could not be built.
func agentConnectURL(gate metaGateContext, members []metaMember, servers []metamcp.ConnectionStatusServer) string {
	for i, member := range members {
		if servers[i].Reconnect {
			return gate.memberFix(member).url
		}
	}
	return ""
}

// agentDetailNeedsReauth is the row detail of an agent's member whose
// connection is missing or unusable. The status hint carries who reconnects
// it and where, so the row does not repeat them.
const agentDetailNeedsReauth = "needs re-authentication"

// signInRow is a member's connection_status row, named by the caller.
func signInRow(status, detail string, outage *metamcp.ConnectionStatusOutage) metamcp.ConnectionStatusServer {
	return metamcp.ConnectionStatusServer{Slug: "", Name: "", Status: status, Detail: detail, Reconnect: false, Outage: outage}
}

// agentDetailConfiguredOnServer is the row detail of an agent's member that
// needs a credential configured on the server, which the agent's sign-in
// step does not set (serverCredentialMissing).
const agentDetailConfiguredOnServer = "needs a credential configured for it in Speakeasy; ask an admin of the server's project"

// notConnectedRow is a member the caller holds no credential for. On an
// agent gateway, the agent's sign-in step connects one with an OAuth sign-in;
// any other is the server's to fix, which the row says.
func notConnectedRow(gate *metaGateContext, oauthSignIn bool) metamcp.ConnectionStatusServer {
	row := signInRow(metamcp.SignInNotConnected, "", nil)
	if gate.agentFix == nil {
		return row
	}
	row.Reconnect = oauthSignIn
	if !oauthSignIn {
		row.Detail = agentDetailConfiguredOnServer
	}
	return row
}

// memberSignInStatus classifies one member by its own routing rule. A member
// that receives no credential and is bound to no sign-in provider (no derived
// remote_session_issuer) needs no sign-in. A remote member can still receive
// a grant routed by its upstream resource without a derived issuer, so
// routing runs before that shortcut.
func (s *Service) memberSignInStatus(ctx context.Context, logger *slog.Logger, gate *metaGateContext, member metaMember, requiresAuth func(context.Context, metaMember) (bool, error)) metamcp.ConnectionStatusServer {
	if gate.catalog != nil {
		// Live discovery checks the approval too, so a refused call never reads as ready.
		approval := s.checkCatalogApproval(ctx, logger, gate, member)
		if approval.changed {
			if approval.remedy == staleChoiceAskAdmin {
				return signInRow(metamcp.SignInActionRequired, approval.message, nil)
			}
			return signInRow(metamcp.SignInNeedsConsent, approval.message, nil)
		}
		if gate.catalogRef.Mode == toolfilter.CatalogModeCached {
			if approval.err != nil {
				if _, stale := errors.AsType[memberStaleChoiceError](approval.err); stale {
					return s.staleMemberStatus(ctx, logger, gate, member)
				}
				return signInRow(metamcp.SignInNeedsConsent, memberErrorMessage(approval.err), nil)
			}
			for _, outcome := range gate.catalog.MemberOutcomes {
				if outcome.Slug == member.slug {
					return signInRow(metamcp.SignInVerifiedAtConsent, outcome.Label+". Tool calls check current authorization.", nil)
				}
			}
			return signInRow(metamcp.SignInNeedsConsent, "No recorded consent outcome.", nil)
		}
	}
	if member.backend == metaMemberBackendHosted {
		if !member.remoteSessionIssuerID.Valid {
			return signInRow(metamcp.SignInNotRequired, "", nil)
		}
		gateTokens, err := s.resolveMemberTokens(ctx, gate, member)
		if err != nil {
			return memberFailureSignIn(ctx, *gate, member, err)
		}
		tokens, err := hostedMemberTokens(gateTokens, member)
		if err != nil {
			return memberFailureSignIn(ctx, *gate, member, withMemberConnectLink(err, member, gate.memberCallerURL(member)))
		}
		if len(tokens) == 0 {
			return notConnectedRow(gate, true)
		}
		return signInRow(metamcp.SignInConnected, "", nil)
	}

	probe := *gate
	probe.chainUpstream = nil
	dial, _, err := s.routeMetaMember(ctx, logger, probe, member, "", false)
	if err != nil {
		if _, stale := errors.AsType[memberStaleChoiceError](err); stale {
			return s.staleMemberStatus(ctx, logger, gate, member)
		}
		return memberFailureSignIn(ctx, *gate, member, err)
	}
	if !dial.anonymous {
		return signInRow(metamcp.SignInConnected, "", nil)
	}
	if member.remoteSessionIssuerID.Valid {
		return notConnectedRow(gate, dial.oauthSignIn)
	}
	if member.upstreamNoAuth {
		return signInRow(metamcp.SignInNotRequired, "", nil)
	}
	// A member with no derived provider can still require a credential
	// through an enabled auth client, such as a static credential. A call
	// that sends none is refused upstream, so it is not ready.
	required, err := requiresAuth(ctx, member)
	if err != nil {
		logger.ErrorContext(ctx, "check member upstream authentication", attr.SlogError(err))
		return signInRow(metamcp.SignInError, "could not check this server", nil)
	}
	if required {
		return notConnectedRow(gate, dial.oauthSignIn)
	}
	return signInRow(metamcp.SignInNotRequired, "", nil)
}

// memberAuthRequirements reports whether a proxied member offers an enabled
// auth client, so a call carrying no credential cannot succeed. The first
// lookup reads every proxied member in one query; later lookups reuse it.
func (s *Service) memberAuthRequirements(organizationID string, members []metaMember) func(context.Context, metaMember) (bool, error) {
	var required map[uuid.UUID]bool
	var loadErr error
	loaded := false
	return func(ctx context.Context, member metaMember) (bool, error) {
		if member.backend != metaMemberBackendProxied {
			return false, nil
		}
		if !loaded {
			loaded = true
			ids := make([]uuid.UUID, 0, len(members))
			for _, m := range members {
				if m.backend == metaMemberBackendProxied {
					ids = append(ids, m.serverID)
				}
			}
			rows, err := upstreamclientsrepo.New(s.db).ListServerConsentAuthClients(ctx, upstreamclientsrepo.ListServerConsentAuthClientsParams{OrganizationID: organizationID, McpServerIds: ids})
			if err != nil {
				loadErr = fmt.Errorf("list member auth clients: %w", err)
			} else {
				required = make(map[uuid.UUID]bool, len(ids))
				for _, row := range rows {
					required[row.McpServerID] = required[row.McpServerID] || row.RequiresUpstreamAuth
				}
			}
		}
		if loadErr != nil {
			return false, loadErr
		}
		return required[member.serverID], nil
	}
}

// memberFailureSignIn keeps an outage from reading as a broken sign-in. On an
// agent gateway, a missing or unusable connection is a row the agent's
// sign-in step fixes.
func memberFailureSignIn(ctx context.Context, gate metaGateContext, member metaMember, err error) metamcp.ConnectionStatusServer {
	if failure, ok := errors.AsType[memberConnectionError](err); ok && failure.code == mcpconnectionattempts.CodeStoredSignInUnreadable {
		if gate.agentFix != nil {
			row := signInRow(metamcp.SignInError, agentDetailNeedsReauth, nil)
			row.Reconnect = true
			return row
		}
		return signInRow(metamcp.SignInNotConnected, memberErrorMessage(err), nil)
	}
	unavailable, ok := errors.AsType[memberUnavailableError](err)
	if refreshProvenance(err) || (ok && refreshProvenance(unavailable.session)) {
		contextvalues.ClaimRefreshConnectionReference(ctx)
	}
	if !ok {
		if _, reconnect := errors.AsType[agentCredentialError](err); reconnect && gate.agentFix != nil {
			row := signInRow(metamcp.SignInError, agentDetailNeedsReauth, nil)
			row.Reconnect = true
			return row
		}
		return signInRow(metamcp.SignInError, memberErrorMessage(err), nil)
	}
	name := cmp.Or(member.name, member.slug)
	if unavailable.session == nil {
		return signInRow(metamcp.SignInUnavailable, memberErrorMessage(err), &metamcp.ConnectionStatusOutage{Message: name + " isn't available right now. Try again shortly.", ReconnectURL: ""})
	}
	kind := remotesessions.OutageKindOf(unavailable.session)
	reconnectURL := ""
	if kind == remotesessions.RefreshOutageProvider {
		reconnectURL = gate.memberCallerURL(member)
	}
	return signInRow(metamcp.SignInUnavailable, memberErrorMessage(err), &metamcp.ConnectionStatusOutage{Message: connectionOutageMessage(name, kind, remotesessions.RetryAfter(unavailable.session)), ReconnectURL: reconnectURL})
}

// refreshProvenance reports an error known to come from a refresh that wrote its own failed row.
func refreshProvenance(err error) bool {
	return remotesessions.RefreshFailureRecorded(err) || remotesessions.RefreshStepRecorded(err)
}

// connectionOutageMessage says what's down and when to retry, in words for a person.
func connectionOutageMessage(name string, kind remotesessions.RefreshOutageKind, wait time.Duration) string {
	retry := "Try again " + outageWaitWords(wait) + "."
	switch kind {
	case remotesessions.RefreshOutageProvider:
		possessive := name + "'s"
		if strings.HasSuffix(strings.ToLower(name), "s") {
			possessive = name + "'"
		}
		return possessive + " provider isn't available right now. " + retry
	case remotesessions.RefreshOutageTunnel:
		return name + " can't renew its sign-in right now. " + retry + " If it keeps happening, ask your administrator to check that the server and its sign-in provider are running."
	case remotesessions.RefreshOutageNone:
	}
	return name + " can't renew its sign-in right now. " + retry
}

// staleMemberStatus gives a stale choice the call's remedy; the connect page can't fix it.
func (s *Service) staleMemberStatus(ctx context.Context, logger *slog.Logger, gate *metaGateContext, member metaMember) metamcp.ConnectionStatusServer {
	message, remedy := s.staleChoiceMessage(ctx, logger, gate, member)
	if remedy == staleChoiceReconnect || remedy == staleChoiceReconnectThenAdmin {
		return signInRow(metamcp.SignInNeedsConsent, message, nil)
	}
	return signInRow(metamcp.SignInActionRequired, message, nil)
}

func memberErrorMessage(err error) string {
	if memberErr, ok := errors.AsType[*metaMemberError](err); ok {
		return memberErr.message
	}
	return "could not check this server"
}

// handleMetaResourcesList lists the gateway's single UI resource.
func handleMetaResourcesList(ctx context.Context, logger *slog.Logger, req *rawRequest) (json.RawMessage, error) {
	bs, err := json.Marshal(&result[resourcesListResult]{
		ID: req.ID,
		Result: resourcesListResult{Resources: []*resourceListEntry{{
			URI:         metamcp.ConnectionStatusResourceURI,
			Name:        "Gateway sign-in status",
			Description: conv.PtrEmpty("Which member servers you are signed in to."),
			MimeType:    conv.PtrEmpty(metamcp.MCPAppMimeType),
			Meta:        nil,
		}}},
		serverIdentity: serverInfoMetaServer,
		cacheHints:     cacheHintsCallerUniform,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize resources/list response").LogError(ctx, logger)
	}
	return bs, nil
}

// handleMetaResourcesRead serves the static connection_status view. The page
// carries no data; the host delivers the tool result to it.
func handleMetaResourcesRead(ctx context.Context, logger *slog.Logger, req *rawRequest) (json.RawMessage, error) {
	var params resourceReadParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "failed to parse resources/read request").LogError(ctx, logger)
	}
	if !metamcp.IsConnectionStatusResourceURI(params.URI) {
		return nil, oops.E(oops.CodeNotFound, nil, "unknown resource %q", params.URI).LogWarn(ctx, logger)
	}
	bs, err := json.Marshal(&result[resourceReadResult]{
		ID: req.ID,
		Result: resourceReadResult{Contents: []resourceContent{{
			URI:      params.URI,
			Name:     nil,
			MimeType: conv.PtrEmpty(metamcp.MCPAppMimeType),
			Text:     metamcp.ConnectionStatusHTML,
			Blob:     "",
			Meta:     map[string]any{"ui": map[string]any{"prefersBorder": true}},
		}}},
		serverIdentity: serverInfoMetaServer,
		cacheHints:     cacheHintsCallerUniform,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize resources/read response").LogError(ctx, logger)
	}
	return bs, nil
}
