// Per-member upstream calls for the meta MCP runtime. Each call is one
// synthesized JSON-RPC exchange driven through the same proxy machinery a
// direct proxied endpoint uses (SSRF policy, billing, telemetry identity, and
// per-tool RBAC ride along via ProxyManager.BuildTarget), with the response
// captured and parsed by the meta MCP rather than relayed to the client.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
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
			return none, metaMemberClientCredentialError(member, err)
		}
		return routed, nil
	}
	if want == "" {
		return none, nil
	}
	var matched remotesessions.UpstreamToken
	found := 0
	for issuerID, entry := range tokens {
		if upstreamTokenRoutes(issuerID, entry, want, member.remoteSessionIssuerID) {
			matched = entry
			found++
		}
	}
	switch found {
	case 0:
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
}

// memberAuthFailure names the member-scoped meaning of an upstream 401/403.
func memberAuthFailure(member metaMember, dial memberDial) error {
	switch {
	case dial.anonymous:
		return &metaMemberError{message: fmt.Sprintf("server %q requires authentication and this gateway holds no credential that routes to it; connect it from this gateway's sign-in page", member.slug)}
	case dial.clientCredential:
		return metaMemberClientCredentialError(member, remotesessions.ErrClientCredentialMisconfigured)
	}
	return &metaMemberError{message: fmt.Sprintf("server %q rejected the stored credential; reconnect it from this gateway's sign-in page", member.slug)}
}

// dialMetaMember routes a member for dispatch and counts the routing outcome.
func (s *Service) dialMetaMember(
	ctx context.Context,
	logger *slog.Logger,
	gate metaGateContext,
	member metaMember,
	callerIdentity string,
) (memberDial, error) {
	dial, backend, err := s.routeMetaMember(ctx, logger, gate, member, callerIdentity)
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
// attaches inside the proxy build. Uncounted, so probes can use it.
func (s *Service) routeMetaMember(
	ctx context.Context,
	logger *slog.Logger,
	gate metaGateContext,
	member metaMember,
	callerIdentity string,
) (memberDial, string, error) {
	serverRow, err := mcpservers_repo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpservers_repo.GetMCPServerByIDAndProjectIDParams{
		ID:        member.serverID,
		ProjectID: member.projectID,
	})
	if err != nil {
		return memberDial{}, "", fmt.Errorf("load meta MCP member server: %w", err)
	}

	// One environment snapshot serves every exchange built for this member,
	// and a member whose environment headers cannot be sent is isolated
	// rather than failing the whole gateway.
	// Only proxied members carry environment headers.
	backend := ""
	switch {
	case member.remoteServerID.Valid:
		backend = "remote"
	case member.tunneledServerID.Valid:
		backend = "tunneled"
	default:
		return memberDial{}, "", &metaMemberError{message: fmt.Sprintf("server %q is not currently servable", member.slug)}
	}
	environment, err := readEnvironmentHeaders(ctx, s.environmentHeaders, member.projectID, serverRow.EnvironmentID)
	if err != nil {
		if isEnvironmentHeaderConfigError(err) {
			logger.WarnContext(ctx, "meta MCP member environment headers are misconfigured", attr.SlogError(err))
			return memberDial{}, backend, &metaMemberError{message: fmt.Sprintf("server %q has an invalid environment header configuration; contact the MCP server administrator", member.slug)}
		}
		return memberDial{}, backend, fmt.Errorf("load meta MCP member environment headers: %w", err)
	}

	// gate.toolSelection is provably nil today: meta endpoints mint no tool
	// selections. If they ever do, its names are meta-MCP-qualified and would
	// have to be translated before reaching a member proxy's strict filter.
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
		routed, terr := routeMetaMemberToken(gate.tokens, member, strings.TrimRight(remoteServer.Url, "/"))
		upstreamToken := routed.Token
		if terr == nil && upstreamToken == "" && gate.chainUpstream != nil {
			upstreamToken, terr = gate.chainUpstream(ctx, remoteServer.Url)
		}
		if terr != nil {
			return memberDial{}, "remote", terr
		}
		return memberDial{anonymous: upstreamToken == "", clientCredential: routed.CredentialOwner == remotesessions.CredentialOwnerSelf, build: func(context.Context) (*proxy.Proxy, error) {
			// No WWW-Authenticate relay: a member's auth challenge must not
			// invite the client to re-authenticate against the meta MCP.
			p := s.remoteProxyManager.Build(logger, &remoteServer, member.serverID.String(), headers, member.visibility, gate.organizationID, member.projectID.String(), upstreamToken, "", gate.toolSelection, remotemcp.WithoutToolsCallIdentityCoverage(), remotemcp.WithMetaMCPServerID(gate.metaServerID.String()), remotemcp.WithEnvironmentHeaders(environment.rows))
			// Meta-MCP-synthesized initializes are not client sessions.
			p.InitializeRequestInterceptors = nil
			renewal := s.renewClientCredentialOnRejection(p, logger, routed)
			renewal.preserveFailure(p)
			return p, nil
		}}, "remote", nil

	case member.tunneledServerID.Valid:
		routed, terr := routeMetaMemberToken(gate.tokens, member, strings.TrimRight(member.tunneledResourceIdentifier, "/"))
		upstreamToken := routed.Token
		if terr == nil && upstreamToken == "" && gate.chainUpstream != nil {
			upstreamToken, terr = gate.chainUpstream(ctx, member.tunneledResourceIdentifier)
		}
		if terr != nil {
			return memberDial{}, "tunneled", terr
		}
		// Per-member namespace so one caller's handshake, calls, and DELETE
		// land on one tunnel gateway.
		affinity := tunnelrouting.HashedClientAffinityKey("meta:"+member.serverID.String(), callerIdentity)
		return memberDial{anonymous: upstreamToken == "", clientCredential: routed.CredentialOwner == remotesessions.CredentialOwnerSelf, build: func(ctx context.Context) (*proxy.Proxy, error) {
			p, berr := s.tunnelManager.buildProxy(ctx, logger, buildProxyParams{
				ClientAffinityKey:  affinity,
				ProjectID:          member.projectID,
				OrganizationID:     gate.organizationID,
				MCPServer:          &serverRow,
				ResourceIdentifier: member.tunneledResourceIdentifier,
				UpstreamAuth:       upstreamToken,
				WWWAuthenticate:    "",
				Selection:          gate.toolSelection,
				EnvironmentHeaders: &environment,
			}, remotemcp.WithoutToolsCallIdentityCoverage(), remotemcp.WithMetaMCPServerID(gate.metaServerID.String()))
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

// memberAttributionContext gives member exchanges the identity billing and
// telemetry read: the caller's own when authenticated, else the endpoint's
// organization. Best effort: attribution must never fail the call.
func (s *Service) memberAttributionContext(ctx context.Context, logger *slog.Logger, gate *metaGateContext) context.Context {
	if _, ok := contextvalues.GetAuthContext(ctx); ok {
		return ctx
	}
	orgMetadata, err := mv.DescribeOrganization(ctx, s.logger, s.orgsRepo, s.billingRepository, gate.organizationID)
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
	// A proxied member's tool-call log is written by the remotemcp interceptor,
	// which never sees the gate, so carry the caller's identity down to it.
	clientIdentity, _ := resolveClientIdentity(ctx, logger, s.sessionClientInfo, &mcpInputs{ //nolint:exhaustruct // only the record's identity fields matter here
		// The member's project, not the gate's: an agent gateway's members can
		// sit in different projects, and this record belongs with the one the
		// call actually reached.
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
	// Only a tool call may acquire a downstream token by identity chaining.
	callGate := *gate
	callGate.chainUpstream = s.metaMemberChainer(gate, member)
	dial, err := s.dialMetaMember(ctx, logger, callGate, member, gate.callerIdentity())
	if err != nil {
		if memberErr, ok := errors.AsType[*metaMemberError](err); ok {
			return marshalMetaToolError(ctx, logger, req.ID, memberErr.message)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "dial meta MCP member").LogError(ctx, logger)
	}

	// The caller's _meta stays on our side of the wire: WireMeta is a lossy
	// observability parse (re-serializing it emits empty/null fields that
	// strict vendors reject with 400). The SDK supplies its own identity and
	// protocol metadata for the negotiated revision.
	ctx, cancel := context.WithTimeout(ctx, s.metaRuntime.MemberCallTimeout)
	defer cancel()
	session, rt, err := s.connectMetaMember(ctx, logger, dial.build, memberSessionCloseTimeout)
	if err != nil {
		return marshalMetaToolError(ctx, logger, req.ID, memberClientFailure(ctx, logger, rt, dial, member, err).Error())
	}
	defer o11y.LogDefer(ctx, logger, "close member session", session.Close)
	upstreamResult, err := callMemberTool(ctx, session, &mcp.CallToolParams{
		Name: toolName, Arguments: arguments, Meta: nil, InputResponses: nil, RequestState: "",
	})
	if err != nil {
		return marshalMetaToolError(ctx, logger, req.ID, memberClientFailure(ctx, logger, rt, dial, member, err).Error())
	}

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

// maxProxiedListPages bounds cursor-following on a member's tools/list.
const maxProxiedListPages = 8

// describeMetaMember reads one member's tool catalog through whichever
// path serves it: the hosted model view, or the member's own tools/list.
func (s *Service) describeMetaMember(ctx context.Context, logger *slog.Logger, gate *metaGateContext, member metaMember) (*memberCatalog, error) {
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
	dial, err := s.dialMetaMember(ctx, logger, *gate, member, gate.callerIdentity())
	if err != nil {
		// Member-scoped errors stay detectable through the %w chain.
		return nil, fmt.Errorf("dial meta MCP member: %w", err)
	}

	// One deadline and one upstream session cover the whole pagination.
	ctx, cancel := context.WithTimeout(ctx, s.metaRuntime.MemberCallTimeout)
	defer cancel()
	session, rt, err := s.connectMetaMember(ctx, logger, dial.build, memberSessionCloseTimeout)
	if err != nil {
		return nil, memberClientFailure(ctx, logger, rt, dial, member, err)
	}
	defer o11y.LogDefer(ctx, logger, "close member session", session.Close)

	entries := []*toolListEntry{}
	byName := map[string]*toolListEntry{}
	dropped := map[string]struct{}{}
	cursor := ""
	for page := 0; ; page++ {
		if page >= maxProxiedListPages {
			logger.WarnContext(ctx, "meta MCP member tool listing truncated at the page cap", attr.SlogMcpServerID(member.serverID.String()))
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
			return nil, &metaMemberError{message: fmt.Sprintf("server %q returned a tool listing the meta MCP could not read", member.slug)}
		}
		for _, entry := range listing.Tools {
			if entry == nil || entry.Name == "" {
				continue
			}
			entries = append(entries, entry)
			// Duplicates drop entirely, matching the hosted catalog's rule.
			if _, gone := dropped[entry.Name]; gone {
				continue
			}
			if _, dup := byName[entry.Name]; dup {
				delete(byName, entry.Name)
				dropped[entry.Name] = struct{}{}
				continue
			}
			byName[entry.Name] = entry
		}
		if listing.NextCursor == "" || listing.NextCursor == cursor {
			break
		}
		cursor = listing.NextCursor
	}

	// Rebuild entries from the kept set, matching the hosted path's output.
	catalog := &memberCatalog{entries: make([]*toolListEntry, 0, len(byName)), byName: byName}
	for _, entry := range entries {
		if kept, ok := byName[entry.Name]; ok && kept == entry {
			catalog.entries = append(catalog.entries, entry)
		}
	}
	return catalog, nil
}
