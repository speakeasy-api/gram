package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const chainedMemberToken = "chained-member-token"

// singleUpstreamChainer serves exactly one upstream, and Acquire succeeds
// for it with a fixed token.
type singleUpstreamChainer struct {
	issuer   uuid.UUID
	upstream string
	usable   bool
	mu       sync.Mutex
	acquired int
}

func (c *singleUpstreamChainer) match(req identitychaining.Request) bool {
	return strings.TrimRight(req.UpstreamResource, "/") == strings.TrimRight(c.upstream, "/")
}

func (c *singleUpstreamChainer) Governs(_ context.Context, req identitychaining.Request) bool {
	return c.match(req)
}

func (c *singleUpstreamChainer) Serves(_ context.Context, req identitychaining.Request) (uuid.UUID, bool) {
	return c.issuer, c.match(req)
}

func (c *singleUpstreamChainer) Configured(context.Context, string, uuid.UUID, uuid.UUID) bool {
	return true
}

func (c *singleUpstreamChainer) HasUsableCredential(_ context.Context, req identitychaining.Request) bool {
	return c.usable && c.match(req)
}

func (c *singleUpstreamChainer) CheckConsent(context.Context, identitychaining.Request, uuid.UUID) identitychaining.Outcome {
	return identitychaining.Outcome{Stage: identitychaining.StageSelection, Reason: identitychaining.ReasonSuccess}
}

func (c *singleUpstreamChainer) Acquire(_ context.Context, req identitychaining.Request) (identitychaining.Token, identitychaining.Outcome) {
	c.mu.Lock()
	c.acquired++
	c.mu.Unlock()
	if !c.match(req) {
		return identitychaining.Token{}, identitychaining.Outcome{Stage: identitychaining.StageSelection, Reason: identitychaining.ReasonNotApplicable}
	}
	return identitychaining.NewTokenForTest(chainedMemberToken, time.Now().Add(time.Hour)), identitychaining.Outcome{Stage: identitychaining.StageRedemption, Reason: identitychaining.ReasonSuccess}
}

func (c *singleUpstreamChainer) acquireCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.acquired
}

// chainedOnlyUpstream fronts a recording upstream that accepts only the
// chained token, like a member whose authorization server trusts only the
// Speakeasy-signed identity assertion.
type chainedOnlyUpstream struct {
	url   string
	inner *recordingUpstream
}

func newChainedOnlyUpstream(t *testing.T) *chainedOnlyUpstream {
	t.Helper()
	inner := newRecordingUpstream(t, "note")
	target, err := url.Parse(inner.url)
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(target)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+chainedMemberToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return &chainedOnlyUpstream{url: srv.URL, inner: inner}
}

func (u *chainedOnlyUpstream) toolCalls() int {
	n := 0
	for _, req := range u.inner.journal() {
		if req.rpcMethod == mcpversions.MethodToolsCall {
			n++
		}
	}
	return n
}

// TestGatewayConsentNamesChainedMemberAsManaged: a member reached only by
// identity chaining cannot be listed at consent, because its token is bound
// to the session grant consent has yet to create. It is named as managed by
// the identity provider rather than as not connected, and a Specific-tools
// approval still leaves it out of the policy's members.
func TestGatewayConsentNamesChainedMemberAsManaged(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "gateway-chained-consent-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, slug, issuer.ID)

	chained := newChainedOnlyUpstream(t)
	connected := newRecordingUpstream(t, "search_docs")
	seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, gateway.ID, "Chained", "chained-"+uuid.NewString()[:8], 1, chained.url)
	docsSlug := "docs-" + uuid.NewString()[:8]
	docsID := seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, gateway.ID, "Docs", docsSlug, 2, connected.url)

	chainedClient := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "chained-"+uuid.NewString()[:8], "", []uuid.UUID{issuer.ID})
	docsClient := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "docs-"+uuid.NewString()[:8], "", []uuid.UUID{issuer.ID})
	insertQualifiedRemoteSessionToken(t, ctx, ti, issuer.ID, docsClient, urn.NewUserSubject(mockidp.MockUserID), "token-docs", connected.url)
	chainer := &singleUpstreamChainer{issuer: clientRemoteIssuerID(t, ctx, ti.conn, projectID, orgID, chainedClient), upstream: chained.url, usable: false, mu: sync.Mutex{}, acquired: 0}
	ti.service.SetIdentityChainer(chainer)

	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "mcp")
	require.NoError(t, err)

	type listing struct {
		names []string
		meta  map[string]any
		code  string
	}
	consent := func() listing {
		stateID, csrfToken := seedGatewayConsentChallenge(t, ctx, ti, issuer.ID, client, gateway.ID, slug)
		consentGetPageWithContext(t, ctx, ti, slug, stateID)
		attempt := uuid.NewString()
		w := serveConsentMCPRequest(t, ctx, ti, endpoint, stateID, csrfToken, attempt, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, nil)
		var listed struct {
			Result struct {
				Tools []struct {
					Name string `json:"name"`
				} `json:"tools"`
				Meta map[string]any `json:"_meta"`
			} `json:"result"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listed), w.Body.String())
		names := []string{}
		for _, tool := range listed.Result.Tools {
			names = append(names, tool.Name)
		}
		form := url.Values{}
		form.Set("state", stateID)
		form.Set("csrf_token", csrfToken)
		form.Set("action", "approve")
		form.Set("tool_inventory_id", attempt)
		form.Set("tool_filtering", "on")
		for _, n := range names {
			form.Add("tools", n)
		}
		req := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/connect", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		approve := httptest.NewRecorder()
		require.NoError(t, ti.service.ServeConsent(approve, req.WithContext(ctx), endpoint))
		require.Equal(t, http.StatusSeeOther, approve.Code, approve.Body.String())
		location, err := url.Parse(approve.Header().Get("Location"))
		require.NoError(t, err)
		return listing{names: names, meta: listed.Result.Meta, code: location.Query().Get("code")}
	}

	first := consent()
	require.Equal(t, []string{docsSlug + "--search_docs"}, first.names)
	require.Equal(t, []any{"Chained"}, first.meta["gram.dev/managedSources"])
	require.NotContains(t, first.meta, "gram.dev/unavailableSources", "a chained member is not reported as unconnected")
	require.Zero(t, chainer.acquireCount(), "consent never acquires a chained token")
	require.Zero(t, chained.toolCalls())

	grantCache := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	grant, err := grantCache.Get(ctx, "gatewayUserSessionGrant:"+issuer.ID.String()+":"+first.code)
	require.NoError(t, err)
	require.NotNil(t, grant.GatewayPolicy)
	require.Equal(t, map[string]uuid.UUID{docsSlug: docsID}, grant.GatewayPolicy.Members)

	// A usable stored chained credential changes nothing at consent.
	chainer.usable = true
	again := consent()
	require.Equal(t, []string{docsSlug + "--search_docs"}, again.names)
	require.Equal(t, []any{"Chained"}, again.meta["gram.dev/managedSources"])
	require.Zero(t, chainer.acquireCount())
}

// TestGatewaySelectionEnforcedOnChainedMember: a chained member gets its
// token by identity chaining on tools/call. All tools reaches it; a
// selection that leaves it out refuses it before chaining or dialing; a
// selection naming its tool allows only that tool.
func TestGatewaySelectionEnforcedOnChainedMember(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := *authCtx.ProjectID
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	slug := "gateway-chained-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, authCtx.ActiveOrganizationID, slug, issuerID)

	chained := newChainedOnlyUpstream(t)
	chainedSlug := "chained-" + uuid.NewString()[:8]
	chainedID := seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, gateway.ID, "Chained", chainedSlug, 1, chained.url)
	docs := newRecordingUpstream(t, "search_docs")
	docsSlug := "docs-" + uuid.NewString()[:8]
	docsID := seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, gateway.ID, "Docs", docsSlug, 2, docs.url)

	chainer := &singleUpstreamChainer{issuer: uuid.New(), upstream: chained.url, usable: false, mu: sync.Mutex{}, acquired: 0}
	ti.service.SetIdentityChainer(chainer)

	resource := "meta_mcp_server:" + gateway.ID.String()
	direct := metamcp.DiscoveryModeDirect
	mint := func(policy *toolfilter.SessionPolicy) string {
		return mintGatewaySelectionToken(t, ti, issuerID, slug, authCtx.UserID, policy)
	}
	call := func(token, name string) string {
		return callGatewayTool(t, ti, slug, token, name, map[string]any{})
	}
	requireRefused := func(res string) {
		t.Helper()
		require.Contains(t, res, `"isError":true`)
		require.Contains(t, res, remotemcp.ToolNotApprovedMessage)
		require.NotContains(t, res, "rejected the call", "the gateway refused the call, not the upstream")
	}

	// All tools, progressive default: no policy at all.
	allProgressive := mint(nil)
	res := callGatewayTool(t, ti, slug, allProgressive, "execute_tool", map[string]any{"name": chainedSlug + "--note", "arguments": map[string]any{}})
	require.Contains(t, res, "pong from note")
	require.Equal(t, 1, chained.toolCalls())
	chained.inner.requireToolCallAuth(t, "Bearer "+chainedMemberToken)
	require.NotZero(t, chainer.acquireCount())

	// All tools with Direct mode: a nil selection.
	allDirect := mint(&toolfilter.SessionPolicy{Resource: resource, Selection: nil, Gateway: &toolfilter.GatewayOptions{DiscoveryMode: &direct}, Members: nil})
	require.Contains(t, call(allDirect, chainedSlug+"--note"), "pong from note")
	require.Equal(t, 2, chained.toolCalls())

	// Specific tools from a consent that could not list the chained member.
	live := toolfilter.AnnotationModeLive
	specific, err := toolfilter.NewSessionSelection(resource, uuid.New(), []toolfilter.AllowEntry{
		{Type: toolfilter.AllowTypeTool, Name: metamcp.QualifyName(docsSlug, "search_docs"), Mode: nil, Tools: nil},
		{Type: toolfilter.AllowTypeAnnotation, Name: toolfilter.AnnotationReadOnly, Mode: &live, Tools: nil},
	})
	require.NoError(t, err)
	specificToken := mint(&toolfilter.SessionPolicy{Resource: resource, Selection: specific, Gateway: &toolfilter.GatewayOptions{DiscoveryMode: &direct}, Members: map[string]uuid.UUID{docsSlug: docsID}})
	acquired, requests := chainer.acquireCount(), chained.inner.requestCount()
	requireRefused(call(specificToken, chainedSlug+"--note"))
	require.Equal(t, acquired, chainer.acquireCount(), "a refused call does not chain")
	require.Equal(t, requests, chained.inner.requestCount(), "a refused call does not dial the member")
	require.Contains(t, call(specificToken, docsSlug+"--search_docs"), "pong from search_docs")

	// A selection naming the tool of a member absent from Members.
	named, err := toolfilter.NewSessionSelection(resource, uuid.New(), []toolfilter.AllowEntry{
		{Type: toolfilter.AllowTypeTool, Name: metamcp.QualifyName(chainedSlug, "note"), Mode: nil, Tools: nil},
		{Type: toolfilter.AllowTypeTool, Name: metamcp.QualifyName(docsSlug, "search_docs"), Mode: nil, Tools: nil},
	})
	require.NoError(t, err)
	namedToken := mint(&toolfilter.SessionPolicy{Resource: resource, Selection: named, Gateway: &toolfilter.GatewayOptions{DiscoveryMode: &direct}, Members: map[string]uuid.UUID{docsSlug: docsID}})
	requests = chained.inner.requestCount()
	requireRefused(call(namedToken, chainedSlug+"--note"))
	require.Equal(t, requests, chained.inner.requestCount())

	// A selection naming the chained tool when the member was consented.
	chosen, err := toolfilter.NewSessionSelection(resource, uuid.New(), []toolfilter.AllowEntry{
		{Type: toolfilter.AllowTypeTool, Name: metamcp.QualifyName(chainedSlug, "note"), Mode: nil, Tools: nil},
	})
	require.NoError(t, err)
	chosenToken := mint(&toolfilter.SessionPolicy{Resource: resource, Selection: chosen, Gateway: &toolfilter.GatewayOptions{DiscoveryMode: &direct}, Members: map[string]uuid.UUID{chainedSlug: chainedID, docsSlug: docsID}})
	require.Contains(t, call(chosenToken, chainedSlug+"--note"), "pong from note")
	docsRequests := docs.requestCount()
	requireRefused(call(chosenToken, docsSlug+"--search_docs"))
	require.Equal(t, docsRequests, docs.requestCount(), "an unselected sibling is refused without dialing it")
}
