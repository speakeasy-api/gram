// Per-member upstream calls for the meta MCP runtime. Each call is one
// synthesized JSON-RPC exchange driven through the same proxy machinery a
// direct proxied endpoint uses (SSRF policy, billing, telemetry identity, and
// per-tool RBAC ride along via ProxyManager.BuildTarget), with the response
// captured and parsed by the meta MCP rather than relayed to the client.
package mcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mv/orgaccount"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	remotemcp_repo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// routeMetaMemberToken selects the bearer forwarded to one meta MCP member.
// No lone-token fallback on either arm: mcp:write can attach a member
// pointing anywhere, and forwarding an unmatched credential would hand it a
// sibling's bearer. No match means an anonymous call, never a mismatched one.
//
// A remote member matches on recorded RFC 8707 resource across the whole map,
// because its routing key is its upstream URL — the same address the proxy
// dials, so a credential that matches is being returned to the audience it
// names.
//
// A self client's credential routes to a remote member by identity too: the
// issuer it is keyed under must be the member's own derived
// remote_session_issuer (upstreamTokenRoutes). One whose credential could not
// be obtained, or was requested for another resource, answers with the
// member-scoped remedy rather than an anonymous call.
//
// A subject's connection held without a token returns its entry with the
// member-scoped failure: an outage answers retryably, a sign-in that can't be
// read asks for a reconnect, and a refused renewal names the administrator.
// The entry is returned for chaining. A usable credential for the same
// upstream outranks it, and of several held entries an outage answers first.
//
// A tunneled member is routed by identity alone: only the entry keyed by its
// own derived remote_session_issuer (mcpserverissuersync.go), accepted when
// that grant is unqualified or names this member's recorded resource
// identifier. The identifier cannot select across issuers the way a remote
// URL does, because a tunnel's dial target is decoupled from the resource it
// claims — an operator-supplied identifier that collided with a sibling's
// upstream would otherwise deliver that sibling's bearer to the tunnel.
func routeMetaMemberToken(tokens map[uuid.UUID]remotesessions.UpstreamToken, member metaMember, upstreamResource string) (remotesessions.UpstreamToken, error) {
	var none remotesessions.UpstreamToken
	want := strings.TrimRight(upstreamResource, "/")
	if member.tunneledServerID.Valid {
		routed, err := tunneledIssuerToken(tokens, member.remoteSessionIssuerID, want)
		if err != nil {
			if entry := tokens[member.remoteSessionIssuerID.UUID]; entry.HeldWithoutToken() {
				return entry, metaMemberClientCredentialError(member, err)
			}
			return none, metaMemberClientCredentialError(member, err)
		}
		return routed, nil
	}
	if want == "" {
		return none, nil
	}
	var matched, held remotesessions.UpstreamToken
	var heldIssuer uuid.UUID
	found := 0
	for issuerID, entry := range tokens {
		if entry.HeldWithoutToken() {
			// A held grant on the member's own issuer answers for it even when its resource
			// no longer matches: it carries no bearer, and the alternative is an anonymous call.
			routes := upstreamTokenRoutes(issuerID, entry, want, member.remoteSessionIssuerID) || (member.remoteSessionIssuerID.Valid && issuerID == member.remoteSessionIssuerID.UUID)
			if routes && heldOutranks(entry, issuerID, held, heldIssuer) {
				held, heldIssuer = entry, issuerID
			}
			continue
		}
		if upstreamTokenRoutes(issuerID, entry, want, member.remoteSessionIssuerID) {
			matched = entry
			found++
		}
	}
	switch found {
	case 0:
		if held.HeldWithoutToken() {
			return held, metaMemberClientCredentialError(member, held.ClientCredentialErr)
		}
		if member.remoteSessionIssuerID.Valid {
			if entry, ok := tokens[member.remoteSessionIssuerID.UUID]; ok && entry.CredentialOwner == remotesessions.CredentialOwnerSelf {
				return none, &metaMemberError{message: fmt.Sprintf("server %q has an upstream credential issued for a different resource; contact the MCP server administrator", member.slug)}
			}
		}
		return none, nil
	case 1:
		if err := matched.ClientCredentialErr; err != nil {
			return none, metaMemberClientCredentialError(member, err)
		}
		return matched, nil
	default:
		// Several credentials claim the same upstream, so forwarding any one
		// would be a guess. Name the duplication rather than the symptom.
		return none, fmt.Errorf("%w: %w", errAmbiguousMemberCredential, &metaMemberError{message: fmt.Sprintf("server %q has %d upstream credentials recorded for the same upstream, so none can be chosen; disconnect the duplicates from this gateway's sign-in and reconnect once", member.slug, found)})
	}
}

// errAmbiguousMemberCredential marks several stored credentials claiming one member's upstream.
var errAmbiguousMemberCredential = errors.New("ambiguous member credential")

// memberProxyBuilder yields a fresh proxy per upstream exchange, since a
// Proxy is a one-request value. It takes the exchange's own context so a
// detached close is not built on an expired call context.
type memberProxyBuilder func(ctx context.Context) (*proxy.Proxy, error)

// memberDial is a routed member's proxy builder plus what routing found, so a
// 401 can name the gateway's gap or the credential's owner, not just a
// rejected token.
type memberDial struct {
	build     memberProxyBuilder
	anonymous bool

	// clientCredential marks a member called with the credential a self
	// client holds for itself, which nobody reconnects.
	clientCredential bool

	// fix resolves who fixes the member's connection, and where, when a
	// rejection needs it; nil reads as the zero memberFix.
	fix func() memberFix

	// oauthSignIn reports, on an agent gateway, that the agent reached the
	// member through an OAuth connection of its own, which its sign-in step
	// connects. Without one, its credential is configured on the server, or
	// it needs none. Only agent paths read it. See agentOAuthSignIn.
	oauthSignIn bool

	// sessionKey fingerprints the route and credential the build sends, so
	// a shared member session never outlives either.
	sessionKey string

	// credential names the auth client the build signs the member in
	// upstream with; the zero value when routing recorded none.
	credential contextvalues.UpstreamCredential

	// tunneled marks a member reached through a tunnel.
	tunneled bool

	// configuredHeaders marks a member sent the headers an admin configured on it.
	configuredHeaders bool

	// provider is the member's upstream URL, empty for a tunnel's internal gateway address.
	provider string
}

func serverHeadersHaveCredential(headers []remotemcp_repo.RemoteMcpServerHeader) bool {
	return slices.ContainsFunc(headers, func(header remotemcp_repo.RemoteMcpServerHeader) bool {
		return header.Value.String != "" && header.ValueFromRequestHeader.String == "" &&
			proxy.IsCredentialHeader(header.Name, header.IsSecret, false)
	})
}

func (d memberDial) upstreamCredential() remotemcp.UpstreamCredentialSent {
	return remotemcp.UpstreamCredentialSent{Attached: !d.anonymous, Self: d.clientCredential, Configured: d.configuredHeaders}
}

// sentCredential is what of the dialed credentials the final request to the member still carried.
func (d memberDial) sentCredential(answer memberRejection) remotemcp.UpstreamCredentialSent {
	return remotemcp.SentCredential(answer.authorizationSent, answer.configuredSent, !d.anonymous, d.clientCredential)
}

// memberSendsConfiguredCredential reports an admin-set credential among the headers a member's
// build sends. Build drops the server's credential headers once the client's headers are selected.
func memberSendsConfiguredCredential(headers []remotemcp_repo.RemoteMcpServerHeader, clientHeaders []proxy.ConfiguredHeader, clientSelected bool) bool {
	if clientSelected {
		return remotemcp.HasConfiguredCredential(clientHeaders)
	}
	return serverHeadersHaveCredential(headers)
}

func memberSessionKey(route any, token string) string {
	encoded, _ := json.Marshal([]any{route, token})
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// memberFix resolves who fixes the dialed member's connection, and where.
func (d memberDial) memberFix() memberFix {
	if d.fix == nil {
		return memberFix{agent: false, url: "", accessURL: ""}
	}
	return d.fix()
}

// memberAuthFailure names the member-scoped meaning of an upstream 401/403.
//
// A person connects the member on its own page, so the message gives them
// that page's URL to open; an agent shows it to them rather than retrying.
// An agent cannot sign in itself, so its message asks someone who can edit
// it to fix the connection at the agent's sign-in step. Without a page, an
// administrator fixes it.
func memberAuthFailure(member metaMember, dial memberDial, status int) error {
	fix := dial.memberFix()
	// A self client's credential is the administrator's, even an agent's.
	agent := fix.agent && !dial.clientCredential
	page := fix.callerURL()
	switch {
	// With no agent token sent, a server that has no OAuth sign-in, or one
	// whose configured credential was sent, refused the server's own
	// credential. The agent's sign-in step does not set that, so the fix is
	// the server's.
	case agent && dial.anonymous && dial.configuredHeaders:
		return memberCredentialError{&metaMemberError{message: fmt.Sprintf("server %q rejected the credential configured for it in Speakeasy, such as an API key. This agent cannot change it. Ask an admin of the server's project to update the server's credential, then retry", member.slug)}}
	// An agent with a connection of its own never dials anonymously: routing
	// refuses the member first (agentUnroutedError).
	case agent && dial.anonymous && !dial.oauthSignIn:
		return memberCredentialError{serverCredentialMissing(member.slug)}
	// A 403 to a credential the upstream accepted is the upstream account's
	// permissions. Signing in again as the same account does not change them.
	case agent && fix.url != "" && status == http.StatusForbidden:
		return memberCredentialError{&metaMemberError{message: fmt.Sprintf("server %q answered 403 Forbidden: the upstream account this agent's connection signs in as may not do this, and signing in again as the same account does not change that. Ask %s to check which account it connects with at %s, or to get that account access upstream, then retry", member.slug, agentEditors, fix.url)}}
	case agent && fix.url != "":
		return memberCredentialError{&metaMemberError{message: fmt.Sprintf("server %q rejected this agent's connection, so it needs re-authentication. %s", member.slug, fix.askEditorToReconnect())}}
	case dial.anonymous && page != "":
		return memberCredentialError{&metaMemberError{message: fmt.Sprintf("server %q is not connected for this user. Ask the user to open %s to connect it, then retry", member.slug, page)}}
	case dial.anonymous:
		return memberCredentialError{&metaMemberError{message: fmt.Sprintf("server %q requires authentication and has no credential for this caller. Ask your administrator to connect it", member.slug)}}
	case dial.clientCredential:
		return metaMemberClientCredentialError(member, remotesessions.ErrClientCredentialMisconfigured)
	case page != "":
		return memberCredentialError{&metaMemberError{message: fmt.Sprintf("server %q rejected the user's credential. Ask the user to open %s to reconnect it, then retry", member.slug, page)}}
	}
	return memberCredentialError{&metaMemberError{message: fmt.Sprintf("server %q rejected the stored credential. Ask your administrator to reconnect it", member.slug)}}
}

// memberCredentialError is a member failure the caller fixes by connecting or
// reconnecting the member, as opposed to one a retry might clear.
type memberCredentialError struct{ *metaMemberError }

func (e memberCredentialError) Unwrap() error { return e.metaMemberError }

// memberUnavailableError is an outage: discovery records it like any member it couldn't list.
type memberUnavailableError struct {
	memberDiscoveryError

	// session is the refresh failure when the outage hit a person's connection.
	session error
}

func (e memberUnavailableError) Unwrap() error { return e.memberDiscoveryError }

func newMemberUnavailableError(message string, session error) memberUnavailableError {
	return memberUnavailableError{
		memberDiscoveryError: memberDiscoveryError{metaMemberError: &metaMemberError{message: message}, status: 0, retryable: false, retryAfter: 0},
		session:              session,
	}
}

// memberCredentialLoadError says retry only when a retry can help: a broken query needs a fix.
func memberCredentialLoadError(member metaMember, err error) error {
	// A caller that went away says nothing about the member.
	code := mcpconnectionattempts.Code{}
	if !errors.Is(err, context.Canceled) {
		code = resolveFailureHappened(err)
	}
	if oops.IsInfrastructureFailure(err) || errors.Is(err, context.Canceled) {
		return memberConnectionError{newMemberUnavailableError(fmt.Sprintf("server %q could not load its connection credentials. Retry shortly. If it keeps happening, ask your administrator to contact Speakeasy support", member.slug), nil), code, ""}
	}
	return memberConnectionError{memberDiscoveryError{metaMemberError: &metaMemberError{message: fmt.Sprintf("server %q could not load its connection credentials. If it keeps happening, ask your administrator to contact Speakeasy support", member.slug)}, status: 0, retryable: false, retryAfter: 0}, code, ""}
}

// chainMetaMember chains a member with no routed credential or one held without
// a token, as a direct server does. A chained token is a verified credential for
// the same person, never an anonymous call; when chaining yields none, the held
// failure stands, so an unreadable sign-in still asks for a reconnect.
func chainMetaMember(ctx context.Context, gate metaGateContext, member metaMember, resource string, routed remotesessions.UpstreamToken, routeErr error) (remotesessions.UpstreamToken, string, error) {
	held := routed.HeldWithoutToken()
	switch {
	case routed.SignInUnreadable():
		routeErr = newMemberSignInUnreadableError(member, gate.memberReconnectAsk(member), routed.ClientCredentialErr)
	case held:
		routeErr = withMemberConnectLink(routeErr, member, gate.memberCallerURL(member))
		if routeErr != nil {
			routeErr = memberConnectionError{routeErr, resolveFailureHappened(routed.ClientCredentialErr), remotesessions.RefreshIssuerURL(routed.ClientCredentialErr)}
		}
	}
	if (routeErr != nil && !held) || (routed.Token != "") || gate.chainUpstream == nil || member.upstreamNoAuth || (member.authClient != nil && member.authClient.outcome != authClientLegacy) {
		return routed, routed.Token, routeErr
	}
	chained, err := gate.chainUpstream(ctx, member, resource)
	if held && chained == "" {
		return routed, "", routeErr
	}
	var none remotesessions.UpstreamToken
	return none, chained, err
}

// recordDialFailure records a member that could not be dialed for a call.
// A saved stale choice is recorded with its refusal message instead.
func (s *Service) recordDialFailure(ctx context.Context, gate *metaGateContext, member metaMember, err error) {
	if stale, ok := errors.AsType[memberStaleChoiceError](err); ok && stale.saved {
		return
	}
	var opts []mcpconnectionattempts.Option
	if id, ok := remotesessions.UnreadableRemoteSessionID(err); ok {
		opts = append(opts, mcpconnectionattempts.RemoteSession(id))
	}
	if _, credential := errors.AsType[memberCredentialError](err); credential {
		code := mcpconnectionattempts.CodeInvalidRemoteSession
		if failure, ok := errors.AsType[memberConnectionError](err); ok && failure.code != (mcpconnectionattempts.Code{}) {
			code = failure.code
			opts = append(opts, mcpconnectionattempts.Provider(failure.provider))
		}
		s.recordMemberOutcome(ctx, gate, member, code, opts...)
		return
	}
	if failure, ok := errors.AsType[memberConnectionError](err); ok {
		s.recordMemberOutcome(ctx, gate, member, failure.code, append(opts, mcpconnectionattempts.Provider(failure.provider))...)
	}
}

// memberStaleChoiceError is a member whose no-auth choice predates its sign-in setup.
type memberStaleChoiceError struct {
	memberCredentialError
	reason string

	// saved is a choice the caller saved; a proposed one is a first connect, not a setup change.
	saved bool
}

func (e memberStaleChoiceError) Unwrap() error { return e.memberCredentialError }

// dialMetaMember routes a member for dispatch and counts the routing outcome.
func (s *Service) dialMetaMember(
	ctx context.Context,
	logger *slog.Logger,
	gate metaGateContext,
	member metaMember,
	callerIdentity string,
) (memberDial, error) {
	dial, backend, err := s.routeMetaMember(ctx, logger, gate, member, callerIdentity, true)
	dial.fix = func() memberFix { return gate.memberFix(member) }
	switch {
	case errors.Is(err, errAmbiguousMemberCredential):
		s.metrics.RecordMetaMemberDispatch(ctx, backend, mcpmetrics.MetaDispatchAmbiguous)
	case err == nil:
		s.metrics.RecordMetaMemberDispatch(ctx, backend, dispatchOutcome(dial.anonymous))
	}
	return dial, err
}

// routeMetaMember loads the member's backend rows, routes its credential
// strictly, and returns the per-exchange proxy builder with the routing
// outcome and the backend kind. The snapshot already
// enforced mcp:connect for private members with the same key
// authorizeProxyBackendAccess uses; per-tool RBAC for private members
// attaches inside the proxy build. Uncounted, so probes can use it; a
// dispatch sets countParity to also compare the member's upstream issuer
// sources.
func (s *Service) routeMetaMember(
	ctx context.Context,
	logger *slog.Logger,
	gate metaGateContext,
	member metaMember,
	callerIdentity string,
	countParity bool,
) (memberDial, string, error) {
	serverRow, err := mcpservers_repo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpservers_repo.GetMCPServerByIDAndProjectIDParams{
		ID:        member.serverID,
		ProjectID: member.projectID,
	})
	if err != nil {
		return memberDial{}, "", fmt.Errorf("load meta MCP member server: %w", err)
	}
	// Collect what an agent's member authorization served, so the routed
	// token can be named on the call's log row.
	gate.servedCredentials = map[uuid.UUID]contextvalues.UpstreamCredential{}
	tokens, skipped, err := s.resolveMemberTokensSkipping(ctx, &gate, member)
	if err != nil {
		return memberDial{}, "", err
	}

	switch {
	case member.remoteServerID.Valid:
		remoteServer, rerr := remotemcp_repo.New(s.db).GetServerByID(ctx, remotemcp_repo.GetServerByIDParams{
			ID:        member.remoteServerID.UUID,
			ProjectID: member.projectID,
		})
		if errors.Is(rerr, pgx.ErrNoRows) {
			// The snapshot query does not join the backend source tables, so a
			// soft-deleted upstream still yields a member. Isolate it rather
			// than failing every member of the gateway.
			return memberDial{}, "remote", &metaMemberError{message: fmt.Sprintf("server %q is not currently servable", member.slug)}
		}
		if rerr != nil {
			return memberDial{}, "remote", fmt.Errorf("load meta MCP member upstream: %w", rerr)
		}
		headers, herr := remotemcp.NewHeaders(s.logger, s.db, s.enc).ListHeaders(ctx, remoteServer.ID, false)
		if herr != nil {
			return memberDial{}, "remote", fmt.Errorf("load meta MCP member upstream headers: %w", herr)
		}
		routed, terr := routeMetaMemberToken(tokens, member, strings.TrimRight(remoteServer.Url, "/"))
		if countParity {
			s.compareMemberRouting(ctx, logger, gate.organizationID, tokens, member, strings.TrimRight(remoteServer.Url, "/"), routed, terr)
		}
		upstreamToken := routed.Token
		// A resolved agent credential that does not route to this member is
		// not permission to dial it anonymously. Header/no-auth members have
		// no token map and retain their existing path.
		if terr == nil && gate.agentID != uuid.Nil && upstreamToken == "" && len(tokens) != 0 && !member.upstreamNoAuth {
			return memberDial{}, "remote", gate.agentUnroutedError(member, tokens, skipped)
		}
		routed, upstreamToken, terr = chainMetaMember(ctx, gate, member, remoteServer.Url, routed, terr)
		if terr != nil {
			return memberDial{}, "remote", terr
		}
		options := []remotemcp.BuildOption{remotemcp.WithoutToolsCallIdentityCoverage(), remotemcp.WithMetaMCPServerID(gate.metaServerID.String()), s.memberConnectionOption(ctx, &gate, member)}
		clientSelected := member.authClient != nil && (member.authClient.outcome != authClientLegacy || member.authClient.sendsClientHeaders())
		var clientHeaders []proxy.ConfiguredHeader
		if clientSelected {
			clientHeaders = member.authClient.headers
			options = append(options, remotemcp.WithAuthClientHeaders(clientHeaders))
		}
		var authHeaders []proxy.ConfiguredHeader
		if member.authClient != nil && member.authClient.outcome != authClientLegacy {
			authHeaders = member.authClient.headers
		}
		return memberDial{anonymous: upstreamToken == "", clientCredential: routed.CredentialOwner == remotesessions.CredentialOwnerSelf, fix: nil, oauthSignIn: agentOAuthSignIn(member, tokens), sessionKey: memberSessionKey([]any{remoteServer.Url, remoteServer.TransportType, headers, authHeaders}, upstreamToken), credential: memberServedCredential(gate, member, routed), tunneled: false, configuredHeaders: memberSendsConfiguredCredential(headers, clientHeaders, clientSelected), provider: remoteServer.Url, build: func(context.Context) (*proxy.Proxy, error) {
			// No WWW-Authenticate relay: a member's auth challenge must not
			// invite the client to re-authenticate against the meta MCP.
			p := s.remoteProxyManager.Build(logger, &remoteServer, member.serverID.String(), headers, member.visibility, gate.organizationID, member.projectID.String(), upstreamToken, "", nil, options...)
			// Meta-MCP-synthesized initializes are not client sessions.
			p.InitializeRequestInterceptors = nil
			renewal := s.renewClientCredentialOnRejection(p, logger, routed)
			renewal.preserveFailure(p)
			return p, nil
		}}, "remote", nil

	case member.tunneledServerID.Valid:
		routed, terr := routeMetaMemberToken(tokens, member, strings.TrimRight(member.tunneledResourceIdentifier, "/"))
		if countParity {
			s.compareMemberRouting(ctx, logger, gate.organizationID, tokens, member, strings.TrimRight(member.tunneledResourceIdentifier, "/"), routed, terr)
		}
		upstreamToken := routed.Token
		// A resolved agent credential that does not route to this member is
		// not permission to dial it anonymously. Header/no-auth members have
		// no token map and retain their existing path.
		if terr == nil && gate.agentID != uuid.Nil && upstreamToken == "" && len(tokens) != 0 && !member.upstreamNoAuth {
			return memberDial{}, "tunneled", gate.agentUnroutedError(member, tokens, skipped)
		}
		routed, upstreamToken, terr = chainMetaMember(ctx, gate, member, member.tunneledResourceIdentifier, routed, terr)
		if terr != nil {
			return memberDial{}, "tunneled", terr
		}
		// Per-member namespace so one caller's handshake, calls, and DELETE
		// land on one tunnel gateway.
		affinity := tunnelrouting.HashedClientAffinityKey("meta:"+member.serverID.String(), callerIdentity)
		return memberDial{anonymous: upstreamToken == "", clientCredential: routed.CredentialOwner == remotesessions.CredentialOwnerSelf, fix: nil, oauthSignIn: agentOAuthSignIn(member, tokens), sessionKey: memberSessionKey([]any{member.tunneledResourceIdentifier, affinity}, upstreamToken), credential: memberServedCredential(gate, member, routed), tunneled: true, configuredHeaders: false, provider: "", build: func(ctx context.Context) (*proxy.Proxy, error) {
			p, berr := s.tunnelManager.buildProxy(ctx, logger, buildProxyParams{
				ClientAffinityKey:  affinity,
				ProjectID:          member.projectID,
				OrganizationID:     gate.organizationID,
				MCPServer:          &serverRow,
				ResourceIdentifier: member.tunneledResourceIdentifier,
				UpstreamAuth:       upstreamToken,
				WWWAuthenticate:    "",
				Selection:          nil,
				Request:            nil,
			}, remotemcp.WithoutToolsCallIdentityCoverage(), remotemcp.WithMetaMCPServerID(gate.metaServerID.String()), s.memberConnectionOption(ctx, &gate, member))
			if berr != nil {
				return nil, fmt.Errorf("build tunnel proxy: %w", berr)
			}
			p.InitializeRequestInterceptors = nil
			renewal := s.renewClientCredentialOnRejection(p, logger, routed)
			renewal.preserveFailure(p)
			return p, nil
		}}, "tunneled", nil

	default:
		return memberDial{}, "", &metaMemberError{message: fmt.Sprintf("server %q is not currently servable", member.slug)}
	}
}

func dispatchOutcome(anonymous bool) mcpmetrics.MetaDispatchOutcome {
	if anonymous {
		return mcpmetrics.MetaDispatchAnonymous
	}
	return mcpmetrics.MetaDispatchCredentialed
}

// memberServerContext names the member server's own project as the owner its
// proxied tool call is metered and logged under, when the caller's
// authentication names no project to fall back to. An agent gateway's key is
// scoped to its organization, not a project, so without this the remotemcp
// interceptors find no owner and the call is neither metered nor logged.
// Best effort: a lookup failure leaves the context as it was.
func (s *Service) memberServerContext(ctx context.Context, logger *slog.Logger, gate *metaGateContext, member metaMember) context.Context {
	if serverCtx, ok := remotemcp.GetServerContext(ctx); ok && serverCtx.OrganizationID != "" && serverCtx.ProjectID != uuid.Nil {
		return ctx
	}
	project, err := projectsrepo.New(s.db).GetProjectWithOrganizationMetadata(ctx, member.projectID)
	if err != nil {
		logger.WarnContext(ctx, "resolve meta MCP member owner", attr.SlogError(err))
		return ctx
	}
	// A member is always in the gateway's organization; one that is not is
	// never charged or logged under another tenant.
	if project.ID != gate.organizationID {
		return ctx
	}
	accountType, _ := orgaccount.ResolveTier(ctx, logger, s.billingRepository, project.ID, billing.Tier(project.GramAccountType))
	return remotemcp.WithServerContext(ctx, remotemcp.ServerContext{
		OrganizationID:   project.ID,
		OrganizationSlug: project.Slug,
		ProjectID:        project.ProjectID,
		ProjectSlug:      project.ProjectSlug,
		AccountType:      string(accountType),
	})
}

// memberAttributionContext gives member exchanges the identity billing and
// telemetry read: the caller's own when authenticated, else the endpoint's
// organization. Best effort: attribution must never fail the call.
func (s *Service) memberAttributionContext(ctx context.Context, logger *slog.Logger, gate *metaGateContext) context.Context {
	if _, ok := contextvalues.GetAuthContext(ctx); ok {
		return ctx
	}
	orgMetadata, err := orgaccount.Describe(ctx, s.logger, s.orgsRepo, s.billingRepository, gate.organizationID)
	if err != nil {
		logger.WarnContext(ctx, "attribute meta MCP member call", attr.SlogError(err))
		return ctx
	}
	projectID := gate.projectID
	sessionID := gate.sessionID
	return contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID:  gate.organizationID,
		ProjectID:             &projectID,
		UserID:                "",
		ExternalUserID:        "",
		APIKeyID:              "",
		APIKeyName:            "",
		OrgWidePluginHooksKey: false,
		SessionID:             &sessionID,
		OrganizationSlug:      orgMetadata.Slug,
		Email:                 nil,
		AccountType:           orgMetadata.GramAccountType,
		HasActiveSubscription: orgMetadata.HasActiveSubscription,
		Whitelisted:           orgMetadata.Whitelisted,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
		IsAdmin:               false,
		SupportOrganizationID: "",
	})
}

// memberResponseRecorder captures a member upstream exchange instead of
// relaying it. Flush is the no-op the proxy's SSE relay requires; the byte
// cap turns an oversized member response into a member-scoped failure.
type memberResponseRecorder struct {
	header    http.Header
	status    int
	body      bytes.Buffer
	truncated bool
}

func newMemberResponseRecorder() *memberResponseRecorder {
	return &memberResponseRecorder{
		header:    make(http.Header),
		status:    0, // Remains unset until the upstream answers.
		body:      bytes.Buffer{},
		truncated: false,
	}
}

func (r *memberResponseRecorder) Header() http.Header { return r.header }

func (r *memberResponseRecorder) WriteHeader(status int) { r.status = status }

func (r *memberResponseRecorder) Write(p []byte) (int, error) {
	remaining := metamcp.MaxMemberResponseBytes - r.body.Len()
	if remaining <= 0 {
		r.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		r.truncated = true
		p = p[:remaining]
	}
	r.body.Write(p)
	return len(p), nil
}

func (r *memberResponseRecorder) Flush() {}

// callerIdentity is the stable per-caller key member dispatch pins tunnel
// affinity on; falls back through the identities the gate resolved.
func (g *metaGateContext) callerIdentity() string {
	for _, id := range []string{g.sessionID, g.userID, g.externalUserID, g.apiKeyID} {
		if id != "" {
			return id
		}
	}
	return "anon"
}

// executeProxiedMemberTool forwards one tool call to a proxied member with
// the unqualified tool name — qualification is a meta MCP concept — and
// re-wraps the upstream result under the outer request id.
func (s *Service) executeProxiedMemberTool(
	ctx context.Context,
	logger *slog.Logger,
	gate *metaGateContext,
	member metaMember,
	req *rawRequest,
	toolName string,
	arguments json.RawMessage,
	meta *mcprequests.WireMeta,
) (json.RawMessage, error) {
	ctx = s.memberAttributionContext(ctx, logger, gate)
	ctx = s.memberServerContext(ctx, logger, gate, member)
	// A proxied member's tool-call log is written by the remotemcp interceptor,
	// which never sees the gate, so carry the caller's identity down to it.
	clientIdentity, _ := resolveClientIdentity(ctx, logger, s.sessionClientInfo, &mcpInputs{ //nolint:exhaustruct // only the record's identity fields matter here
		projectID:       member.projectID,
		sessionID:       gate.sessionID,
		clientInfoScope: metaClientInfoScope(gate.metaServerID),
	}, meta.Sanitize().ClientInfo)
	if clientIdentity.Name != "" {
		ctx = contextvalues.SetMCPClientInfo(ctx, contextvalues.MCPClientInfo{
			Name:    clientIdentity.Name,
			Version: clientIdentity.Version,
		})
	}
	// Execution resolves the member credential again before dialing.
	redial := func(ctx context.Context, gate *metaGateContext, member metaMember) (memberDial, error) {
		callGate := *gate
		callGate.chainUpstream = s.gatewayMemberChainer(gate, mcpidentity.KindUserSession, uuid.Nil)
		return s.dialMetaMember(ctx, logger, callGate, member, codeCallerIdentity(ctx, gate))
	}
	dial, err := redial(ctx, gate, member)
	if err != nil {
		s.recordDialFailure(ctx, gate, member, err)
		if _, ok := errors.AsType[*metaMemberError](err); ok {
			message := s.callFailureMessage(ctx, logger, gate, member, err)
			failMetaToolCall(ctx, metaMemberErrorStatus(err), message)
			return marshalMetaMemberFailure(ctx, logger, req.ID, err, message)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "dial meta MCP member").LogError(ctx, logger)
	}

	// The caller's _meta stays on our side of the wire: WireMeta is a lossy
	// observability parse (re-serializing it emits empty/null fields that
	// strict vendors reject with 400). The SDK supplies its own identity and
	// protocol metadata for the negotiated revision.
	ctx, cancel := context.WithTimeout(ctx, s.metaRuntime.MemberCallTimeout)
	defer cancel()
	upstreamResult, unsent, err := s.callOnMemberSession(ctx, logger, dial, redial, member, toolName, arguments)
	if unsent {
		// The shared session failed before this call was sent, so one retry cannot replay it.
		upstreamResult, _, err = s.callOnMemberSession(ctx, logger, dial, redial, member, toolName, arguments)
	}
	if err != nil {
		if failure, ok := errors.AsType[memberConnectionError](err); ok {
			s.recordMemberOutcome(ctx, gate, member, failure.code, mcpconnectionattempts.Provider(failure.provider))
		}
		// The gateway logs this failure only when the call never reached
		// the upstream interceptor.
		failMetaToolCall(ctx, metaMemberErrorStatus(err), err.Error())
		return marshalMetaMemberFailure(ctx, logger, req.ID, err, err.Error())
	}
	s.recordMemberOutcome(ctx, gate, member, mcpconnectionattempts.ResultServed, mcpconnectionattempts.Provider(dial.provider))

	bs, err := json.Marshal(&result[json.RawMessage]{
		ID:     req.ID,
		Result: upstreamResult,
		// The result's _meta identity stays the member's, as on hosted.
		serverIdentity: serverInfo{Name: member.slug, Version: "0.0.0"},
		cacheHints:     nil,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "serialize member tool result").LogError(ctx, logger)
	}
	return bs, nil
}

// callOnMemberSession runs one tools/call on the member's session. unsent
// reports a failure on a shared session before the call left the gateway.
func (s *Service) callOnMemberSession(ctx context.Context, logger *slog.Logger, dial memberDial, redial memberRedial, member metaMember, toolName string, arguments json.RawMessage) (json.RawMessage, bool, error) {
	ctx, ms, err := s.openMemberSession(ctx, logger, dial, redial, member)
	if err != nil {
		return nil, false, err
	}
	defer ms.release(ctx, logger)
	// The remotemcp interceptor writes this call's log row and never sees the
	// gate, so carry down which auth client signs the member in. A recheck
	// while waiting for a pooled session may have routed the member to another
	// credential, so name the route the session was opened on; a route that
	// recorded none clears any credential named before.
	ctx = contextvalues.SetUpstreamCredential(ctx, ms.dial.credential)
	upstreamResult, err := callMemberTool(ctx, ms.session, &mcp.CallToolParams{
		Name: toolName, Arguments: arguments, Meta: nil, InputResponses: nil, RequestState: "",
	})
	if err != nil {
		ms.fail(ctx)
		unsent := ms.unsent()
		if unsent && ctx.Err() == nil {
			return nil, true, err
		}
		// A call that left the gateway is logged by the upstream interceptor.
		if !unsent {
			markMetaToolCallProxied(ctx)
		}
		return nil, false, memberClientFailure(ctx, logger, ms.outcome, ms.dial, member, err)
	}
	// The upstream interceptor wrote this call's row.
	markMetaToolCallProxied(ctx)
	return upstreamResult, false, nil
}

// maxProxiedListPages bounds cursor-following on a member's tools/list.
const maxProxiedListPages = 8

// describeMetaMember reads one member's tool catalog through whichever
// path serves it: the hosted model view, or the member's own tools/list.
func (s *Service) describeMetaMember(ctx context.Context, logger *slog.Logger, gate *metaGateContext, member metaMember) (*memberCatalog, error) {
	if err := s.validateCatalogMember(ctx, gate, member, catalogCheckRecorded); err != nil {
		return nil, err
	}
	if gate.catalog != nil && gate.catalogRef.Mode == toolfilter.CatalogModeCached {
		return s.describeCachedMember(ctx, gate, member)
	}
	if member.backend == metaMemberBackendHosted {
		return s.describeMemberToolset(ctx, logger, gate, member)
	}
	return s.describeProxiedMember(ctx, logger, gate, member)
}

// describeProxiedMember pages a proxied member's tools/list into a catalog,
// holding one upstream session across pages so session-scoped cursors stay
// valid. RBAC and session tool filtering already applied inside the proxy's
// tools/list interceptors.
func (s *Service) describeProxiedMember(ctx context.Context, logger *slog.Logger, gate *metaGateContext, member metaMember) (*memberCatalog, error) {
	ctx = s.memberAttributionContext(ctx, logger, gate)
	redial := func(ctx context.Context, gate *metaGateContext, member metaMember) (memberDial, error) {
		probe := *gate
		if gate.catalog != nil {
			probe.chainUpstream = s.gatewayMemberChainer(gate, mcpidentity.KindUserSession, uuid.Nil)
		}
		return s.dialMetaMember(ctx, logger, probe, member, codeCallerIdentity(ctx, gate))
	}
	dial, err := redial(ctx, gate, member)
	if err != nil {
		// Member-scoped errors stay detectable through the %w chain.
		return nil, fmt.Errorf("dial meta MCP member: %w", err)
	}

	// One deadline and one upstream session cover the whole pagination.
	ctx, cancel := context.WithTimeout(ctx, s.metaRuntime.MemberCallTimeout)
	defer cancel()
	sessionCtx, ms, err := s.openMemberSession(ctx, logger, dial, redial, member)
	if err != nil {
		return nil, err
	}
	ctx = sessionCtx
	defer ms.release(ctx, logger)
	catalog, err := listMemberSessionTools(ctx, logger, ms.session, ms.outcome, ms.dial, member)
	if err != nil {
		ms.fail(ctx)
		return nil, err
	}
	return catalog, nil
}

// listMemberSessionTools pages tools/list within one member session, so the
// proxy's interceptors filter each page. Pagination is bounded by
// maxProxiedListPages.
func listMemberSessionTools(ctx context.Context, logger *slog.Logger, session *mcp.ClientSession, rt memberOutcome, dial memberDial, member metaMember) (*memberCatalog, error) {
	entries := []*toolListEntry{}
	byName := map[string]*toolListEntry{}
	incomplete := false
	dropped := map[string]struct{}{}
	cursor := ""
	seenCursors := map[string]bool{}
	for page := 0; ; page++ {
		if page >= maxProxiedListPages {
			logger.WarnContext(ctx, "meta MCP member tool listing truncated at the page cap", attr.SlogMcpServerID(member.serverID.String()))
			incomplete = true
			break
		}
		pageResult, cerr := session.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor, Meta: nil})
		if cerr != nil {
			return nil, memberClientFailure(ctx, logger, rt, dial, member, cerr)
		}
		upstreamResult, merr := json.Marshal(pageResult)
		if merr != nil {
			return nil, fmt.Errorf("marshal member tool listing: %w", merr)
		}

		var listing struct {
			Tools      []*toolListEntry `json:"tools"`
			NextCursor string           `json:"nextCursor"`
		}
		if uerr := json.Unmarshal(upstreamResult, &listing); uerr != nil {
			return nil, memberDiscoveryError{metaMemberError: &metaMemberError{message: fmt.Sprintf("server %q returned a tool listing the meta MCP could not read", member.slug)}, status: 0, retryable: false, retryAfter: 0}
		}
		for _, entry := range listing.Tools {
			if entry == nil || entry.Name == "" {
				incomplete = true
				continue
			}
			entries = append(entries, entry)
			// Duplicates drop entirely, matching the hosted catalog's rule.
			if _, gone := dropped[entry.Name]; gone {
				continue
			}
			if _, dup := byName[entry.Name]; dup {
				incomplete = true
				delete(byName, entry.Name)
				dropped[entry.Name] = struct{}{}
				continue
			}
			byName[entry.Name] = entry
		}
		if listing.NextCursor == "" {
			break
		}
		if listing.NextCursor == cursor || seenCursors[listing.NextCursor] {
			return nil, memberDiscoveryError{metaMemberError: &metaMemberError{message: fmt.Sprintf("server %q returned a repeated tool listing cursor", member.slug)}, status: 0, retryable: false, retryAfter: 0}
		}
		seenCursors[listing.NextCursor] = true
		cursor = listing.NextCursor
	}

	// Rebuild entries from the kept set, matching the hosted path's output.
	catalog := &memberCatalog{entries: make([]*toolListEntry, 0, len(byName)), byName: byName, incomplete: incomplete}
	for _, entry := range entries {
		if kept, ok := byName[entry.Name]; ok && kept == entry {
			catalog.entries = append(catalog.entries, entry)
		}
	}
	return catalog, nil
}
