// Live validation end to end: one SDK negotiation probe (server/discover, initialize, initialized, tools/list), closed afterwards, verdict stored and shown.

package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	mcpendpoints_repo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotemcp_repo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// validationProbeTimeout is generous enough for a real handshake under a parallel suite; hangs still end with it.
const validationProbeTimeout = 2 * time.Second

// validationMemberMode scripts what the fake member does with a probe.
type validationMemberMode string

const (
	// memberAccepts answers 200 and mints a session.
	memberAccepts validationMemberMode = "accepts"
	// memberStateless answers 200 without minting a session.
	memberStateless           validationMemberMode = "stateless"
	memberStatelessRejectsAck validationMemberMode = "stateless-rejects-ack"
	memberMissingVersion      validationMemberMode = "missing-version"
	memberWrongVersion        validationMemberMode = "wrong-version"
	// memberRejects answers 401 to everything.
	memberRejects validationMemberMode = "rejects"
	// memberForbids answers 403 to everything.
	memberForbids validationMemberMode = "forbids"
	// memberRejectsAck mints a session on initialize, then answers 401 to notifications/initialized.
	memberRejectsAck validationMemberMode = "rejects-ack"
	// The list rejection modes complete the handshake, then reject tools/list.
	memberRejectsList validationMemberMode = "rejects-list"
	memberForbidsList validationMemberMode = "forbids-list"
	// memberErrorsList completes the handshake, then answers tools/list with a JSON-RPC error.
	memberErrorsList validationMemberMode = "errors-list"
	// The discovery-forbidden modes reject only the SDK's discovery attempt, then exercise the MCP handshake.
	memberDiscoveryForbiddenAccepts   validationMemberMode = "discovery-forbidden-accepts"
	memberDiscoveryForbiddenListFails validationMemberMode = "discovery-forbidden-list-fails"
	memberDiscoveryForbiddenListHangs validationMemberMode = "discovery-forbidden-list-hangs"
	// memberFailsClose accepts, then answers 500 to the DELETE.
	memberFailsClose validationMemberMode = "fails-close"
	// memberHangs never answers, until the probe gives up.
	memberHangs               validationMemberMode = "hangs"
	memberHangsAck            validationMemberMode = "hangs-ack"
	memberHangsClose          validationMemberMode = "hangs-close"
	memberHangsInitializeJSON validationMemberMode = "hangs-initialize-json"
	memberHangsInitializeSSE  validationMemberMode = "hangs-initialize-sse"
	// memberErrors answers 200 with a JSON-RPC error instead of an initialize result.
	memberErrors validationMemberMode = "errors"
	// memberGarbles answers 200 with a body that is not JSON-RPC at all.
	memberGarbles validationMemberMode = "garbles"
)

// validationMemberRequest is one request as it arrived at the fake member.
type validationMemberRequest struct {
	method    string
	jsonrpc   string
	rpcMethod string
	auth      string
	session   string
	version   string
}

// validationMember is a scripted MCP upstream recording every request it receives on the wire.
type validationMember struct {
	url      string
	mu       sync.Mutex
	mode     validationMemberMode
	requests []validationMemberRequest
	sessions int
	// onInitialize runs before initialize is answered; its error is kept for the test goroutine.
	onInitialize func() error
	hookErr      error
}

func newValidationMember(t *testing.T) *validationMember {
	t.Helper()
	m := &validationMember{url: "", mu: sync.Mutex{}, mode: memberAccepts, requests: nil, sessions: 0, onInitialize: nil, hookErr: nil}
	srv := httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(srv.Close)
	m.url = srv.URL
	return m
}

func (m *validationMember) set(mode validationMemberMode) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mode = mode
}

// drain returns the requests since the last drain.
func (m *validationMember) drain() []validationMemberRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	got := m.requests
	m.requests = nil
	return got
}

func (m *validationMember) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var rpc struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
	}
	_ = json.Unmarshal(body, &rpc)

	m.mu.Lock()
	m.requests = append(m.requests, validationMemberRequest{
		method:    r.Method,
		jsonrpc:   rpc.JSONRPC,
		rpcMethod: rpc.Method,
		auth:      r.Header.Get("Authorization"),
		session:   r.Header.Get("Mcp-Session-Id"),
		version:   r.Header.Get("MCP-Protocol-Version"),
	})
	mode := m.mode
	hook := m.onInitialize
	m.mu.Unlock()

	// A real member answers the id it was asked with.
	id := string(rpc.ID)
	if id == "" {
		id = "null"
	}
	answer := func(status int, payload string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,%s}`, id, payload)
	}
	reject := func(status int) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="member"`)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":"invalid_token","error_description":"secret upstream detail"}`))
	}

	if r.Method == http.MethodDelete {
		if mode == memberHangsClose {
			<-r.Context().Done()
			return
		}
		if mode == memberFailsClose {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}
	if mode == memberHangs {
		<-r.Context().Done()
		return
	}
	if mode == memberRejects || mode == memberForbids {
		status := http.StatusUnauthorized
		if mode == memberForbids {
			status = http.StatusForbidden
		}
		reject(status)
		return
	}
	switch rpc.Method {
	case "server/discover":
		// The client falls back to initialize when discovery is unavailable or forbidden.
		switch mode {
		case memberDiscoveryForbiddenAccepts, memberDiscoveryForbiddenListFails, memberDiscoveryForbiddenListHangs:
			reject(http.StatusForbidden)
		default:
			answer(http.StatusNotFound, `"error":{"code":-32601,"message":"method not found"}`)
		}
	case "initialize":
		if hook != nil {
			if err := hook(); err != nil {
				m.mu.Lock()
				m.hookErr = err
				m.mu.Unlock()
			}
		}
		switch mode {
		case memberStateless, memberStatelessRejectsAck:
			answer(http.StatusOK, `"result":{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"validation-member","version":"1"}}`)
			return
		case memberMissingVersion, memberWrongVersion:
			version := ""
			if mode == memberWrongVersion {
				version = `"jsonrpc":"1.0",`
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Mcp-Session-Id", "malformed-session")
			_, _ = io.WriteString(w, `data: {`+version+`"id":`+id+`,"result":{}}`+"\n\n")
			return
		case memberErrors:
			answer(http.StatusOK, `"error":{"code":-32603,"message":"secret upstream detail"}`)
			return
		case memberGarbles:
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<html><body>secret upstream detail</body></html>`))
			return
		case memberHangsInitializeJSON, memberHangsInitializeSSE:
			contentType, body := "application/json", "{"
			if mode == memberHangsInitializeSSE {
				contentType, body = "text/event-stream", ": pending\n\n"
			}
			w.Header().Set("Content-Type", contentType)
			w.Header().Set("Mcp-Session-Id", "malformed-session")
			_, _ = io.WriteString(w, body)
			if err := http.NewResponseController(w).Flush(); err != nil {
				return
			}
			<-r.Context().Done()
			return
		case memberAccepts, memberRejects, memberForbids, memberRejectsAck, memberRejectsList, memberForbidsList, memberErrorsList, memberFailsClose, memberHangs, memberHangsAck, memberHangsClose, memberDiscoveryForbiddenAccepts, memberDiscoveryForbiddenListFails, memberDiscoveryForbiddenListHangs:
		}
		m.mu.Lock()
		m.sessions++
		session := m.sessions
		m.mu.Unlock()
		w.Header().Set("Mcp-Session-Id", "member-session-"+uuid.NewString()[:8]+"-"+strings.Repeat("x", session))
		answer(http.StatusOK, `"result":{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"validation-member","version":"1"}}`)
	case "notifications/initialized":
		if mode == memberHangsAck {
			<-r.Context().Done()
			return
		}
		if mode == memberRejectsAck || mode == memberStatelessRejectsAck {
			reject(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		switch mode {
		case memberRejectsList:
			reject(http.StatusUnauthorized)
		case memberForbidsList:
			reject(http.StatusForbidden)
		case memberErrorsList:
			answer(http.StatusOK, `"error":{"code":-32603,"message":"secret upstream detail"}`)
		case memberDiscoveryForbiddenListFails:
			answer(http.StatusInternalServerError, `"error":{"code":-32603,"message":"secret upstream detail"}`)
		case memberDiscoveryForbiddenListHangs:
			<-r.Context().Done()
			return
		default:
			answer(http.StatusOK, `"result":{"tools":[]}`)
		}
	default:
		answer(http.StatusOK, `"result":{}`)
	}
}

// mintFirstPartyConsentState stores a first-party connect challenge, so no MCP client row is needed to render.
func mintFirstPartyConsentState(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, orgID string, shared uuid.UUID, slug string) (*mcp.ResolvedMcpEndpoint, string, urn.SessionSubject) {
	t.Helper()

	endpoint := &mcp.ResolvedMcpEndpoint{
		AudienceURN:          urn.NewUserSessionIssuer(shared).String(),
		CIMDAdmissionModeRaw: pgtype.Text{String: "", Valid: false},
		CustomDomainID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		IsPublic:             true,
		McpServerID:          uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:       orgID,
		ProjectID:            projectID,
		RouteBase:            "mcp",
		Slug:                 slug,
		ToolsetID:            uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UpstreamResource:     "",
		UserSessionIssuerID:  shared,
	}

	subject := urn.NewUserSubject(uuid.NewString())
	stateID := uuid.NewString()
	require.NoError(t, ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  stateID,
		UserSessionIssuerID: shared,
		Endpoint: mcp.EndpointRef{
			McpSlug:        endpoint.Slug,
			RouteBase:      endpoint.RouteBase,
			CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		},
		ClientID:            "",
		RedirectURI:         "",
		CodeChallenge:       "",
		CodeChallengeMethod: "",
		CSRFToken:           "csrf-token",
		Subject:             &subject,
		FirstParty:          true,
		CreatedAt:           time.Now(),
	}))
	return endpoint, stateID, subject
}

// createPublicRemoteMcpServer binds a public remote-backed mcp_server to issuerID fronting upstreamURL.
func createPublicRemoteMcpServer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, issuerID uuid.UUID, slug, upstreamURL string) uuid.UUID {
	t.Helper()
	remoteServer, err := remotemcp_repo.New(conn).CreateServer(ctx, remotemcp_repo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     projectID,
		TransportType: "streamable-http",
		Url:           upstreamURL,
	})
	require.NoError(t, err)
	server, err := mcpservers_repo.New(conn).CreateMCPServer(ctx, mcpservers_repo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                conv.ToPGText(slug),
		Slug:                conv.ToPGText(slug),
		RemoteMcpServerID:   conv.ToNullUUID(remoteServer.ID),
		Visibility:          "public",
		UserSessionIssuerID: conv.ToNullUUID(issuerID),
	})
	require.NoError(t, err)
	return server.ID
}

// validationFixture is one consent challenge with a connected card whose credential routes to member.
type validationFixture struct {
	ti       *testInstance
	reader   *sdkmetric.ManualReader
	endpoint *mcp.ResolvedMcpEndpoint
	stateID  string
	subject  urn.SessionSubject
	member   *validationMember
	clientID uuid.UUID
	// name is what a verdict calls the member.
	name string
}

func newValidationMeterProvider() (*sdkmetric.ManualReader, metric.MeterProvider) {
	reader := sdkmetric.NewManualReader()
	return reader, sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
}

// seedMetaValidationFixture: a meta MCP with one remote member fronting the fake upstream, and a grant qualified to it.
func seedMetaValidationFixture(t *testing.T, prefix string) (context.Context, validationFixture) {
	t.Helper()

	reader, provider := newValidationMeterProvider()
	ctx, ti := newTestMCPServiceWithValidationTimeout(t, provider, validationProbeTimeout)
	projectID, orgID := consentTestTenant(t, ctx)

	shared := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaServerID := createMetaServer(t, ctx, ti.conn, projectID, orgID, prefix+"-gw", shared)
	endpoint, stateID, subject := mintFirstPartyConsentState(t, ctx, ti, projectID, orgID, shared, prefix+"-gw")
	endpoint.MetaMcpServerID = conv.ToNullUUID(metaServerID)

	member := newValidationMember(t)
	clientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, prefix, "", []uuid.UUID{shared})
	issuerID := clientRemoteIssuerID(t, ctx, ti.conn, projectID, orgID, clientID)
	createMetaMember(t, ctx, ti.conn, projectID, metaServerID, prefix+"-member", member.url, conv.ToNullUUID(issuerID), 0)
	insertQualifiedRemoteSessionToken(t, ctx, ti, shared, clientID, subject, "token-"+prefix, member.url)

	return ctx, validationFixture{
		ti:       ti,
		reader:   reader,
		endpoint: endpoint,
		stateID:  stateID,
		subject:  subject,
		member:   member,
		clientID: clientID,
		name:     prefix + "-member",
	}
}

// seedStandaloneValidationFixture: a proxied endpoint whose own backend is the fake upstream, and a grant naming it.
func seedStandaloneValidationFixture(t *testing.T, prefix string) (context.Context, validationFixture) {
	t.Helper()
	return seedStandaloneValidationFixtureWith(t, prefix, mcp.MetaRuntimeConfig{MemberCallTimeout: 0, ValidationTimeout: validationProbeTimeout})
}

// seedStandaloneValidationFixtureWith is seedStandaloneValidationFixture under the given probe budgets.
func seedStandaloneValidationFixtureWith(t *testing.T, prefix string, metaRuntime mcp.MetaRuntimeConfig) (context.Context, validationFixture) {
	t.Helper()

	reader, provider := newValidationMeterProvider()
	ctx, ti := newTestMCPServiceWithMetaRuntime(t, provider, metaRuntime)
	projectID, orgID := consentTestTenant(t, ctx)

	shared := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	member := newValidationMember(t)
	serverID := createPublicRemoteMcpServer(t, ctx, ti.conn, projectID, shared, prefix+"-server", member.url)
	endpoint, stateID, subject := mintFirstPartyConsentState(t, ctx, ti, projectID, orgID, shared, prefix+"-server")
	endpoint.McpServerID = conv.ToNullUUID(serverID)
	endpoint.UpstreamResource = member.url
	// The endpoint row and the server-keyed ref let a committed grant be placed from the challenge alone, as the callback does.
	_, err := mcpendpoints_repo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpoints_repo.CreateMCPEndpointParams{
		ProjectID:       projectID,
		CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:     conv.ToNullUUID(serverID),
		MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Slug:            endpoint.Slug,
	})
	require.NoError(t, err)
	challengeState, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	require.NoError(t, err)
	challengeState.Endpoint.McpServerID = conv.ToNullUUID(serverID)
	require.NoError(t, ti.authnChallengeCache.Store(ctx, challengeState))

	clientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, prefix, "", []uuid.UUID{shared})
	insertQualifiedRemoteSessionToken(t, ctx, ti, shared, clientID, subject, "token-"+prefix, member.url)

	return ctx, validationFixture{
		ti:       ti,
		reader:   reader,
		endpoint: endpoint,
		stateID:  stateID,
		subject:  subject,
		member:   member,
		clientID: clientID,
		name:     prefix + "-server",
	}
}

// postValidate drives the validate action for one client.
func postValidate(t *testing.T, fx validationFixture, clientID uuid.UUID) (*httptest.ResponseRecorder, error) {
	t.Helper()
	form := url.Values{}
	form.Set("state", fx.stateID)
	form.Set("csrf_token", "csrf-token")
	form.Set("action", "validate")
	form.Set("client_id", clientID.String())
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.endpoint.Slug+"/connect/remote-session", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	if err := fx.ti.service.ServeConsentAction(w, req, fx.endpoint); err != nil {
		return w, fmt.Errorf("serve consent action: %w", err)
	}
	return w, nil
}

func requireValidated(t *testing.T, fx validationFixture) {
	t.Helper()
	w, err := postValidate(t, fx, fx.clientID)
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, w.Code)
	require.Equal(t, "/mcp/"+fx.endpoint.Slug+"/connect?state="+url.QueryEscape(fx.stateID), w.Header().Get("Location"))
}

// renderConsent renders the page after an action, normalized for copy assertions.
func renderConsent(t *testing.T, fx validationFixture) string {
	t.Helper()
	return renderConsentAt(t, fx, "/mcp/"+fx.endpoint.Slug+"/connect?state="+url.QueryEscape(fx.stateID))
}

func renderConsentAt(t *testing.T, fx validationFixture, target string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	w := httptest.NewRecorder()
	require.NoError(t, fx.ti.service.ServeConsent(w, req, fx.endpoint))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return strings.Join(strings.Fields(w.Body.String()), " ")
}

func storedSession(t *testing.T, ctx context.Context, fx validationFixture) remotesessions_repo.RemoteSession {
	t.Helper()
	sess, err := remotesessions_repo.New(fx.ti.conn).GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{
		SubjectUrn:            fx.subject,
		RemoteSessionClientID: fx.clientID,
	})
	require.NoError(t, err)
	return sess
}

// validationCounts reads gram.remote_session.validation back by outcome.
func validationCounts(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	counts := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "gram.remote_session.validation" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, dp := range sum.DataPoints {
				outcome, found := dp.Attributes.Value(attr.OutcomeKey)
				require.True(t, found)
				issuer, found := dp.Attributes.Value(attr.OAuthIssuerKey)
				require.True(t, found)
				require.NotEmpty(t, issuer.AsString(), "the series names the credential's issuer")
				counts[outcome.AsString()] += dp.Value
			}
		}
	}
	return counts
}

// requireProbe asserts one SDK negotiation probe's wire shape through the
// card's own credential, server/discover then the initialize handshake, with
// tools/list once the handshake holds, and a DELETE for every minted session.
func requireProbe(t *testing.T, requests []validationMemberRequest, bearer string, expectAck, expectClose bool) {
	t.Helper()
	discovers := 0
	initializes := 0
	acks := 0
	deletes := 0
	session := ""
	for _, req := range requests {
		require.Equal(t, "Bearer "+bearer, req.auth, "every probe request carries the card's own credential: %+v", req)
		if req.method == http.MethodPost {
			require.Equal(t, "2.0", req.jsonrpc, "every protocol message is JSON-RPC 2.0: %+v", req)
		}
		switch {
		case req.method == http.MethodDelete:
			deletes++
			if expectAck {
				require.Equal(t, session, req.session, "the close names the session the handshake minted")
			} else {
				require.Equal(t, "malformed-session", req.session, "an invalid initialize still closes its minted session")
			}
		case req.rpcMethod == "server/discover":
			discovers++
			require.Empty(t, req.session, "discovery rides no session: %+v", req)
		case req.rpcMethod == "initialize":
			initializes++
			require.Equal(t, 1, discovers, "the handshake is the fallback after server/discover: %+v", requests)
		case req.rpcMethod == "notifications/initialized":
			acks++
			session = req.session
			if expectClose {
				require.NotEmpty(t, session)
			}
		}
	}
	require.Equal(t, 1, discovers, "every probe starts with server/discover: %+v", requests)
	require.Equal(t, 1, initializes, "the credential is presented once more on the handshake: %+v", requests)
	if expectAck {
		require.Equal(t, 1, acks, "a successful initialize is acknowledged: %+v", requests)
	} else {
		require.Zero(t, acks, "an unsuccessful initialize is not acknowledged: %+v", requests)
	}
	if expectClose {
		require.Equal(t, 1, deletes, "a DELETE follows each minted session: %+v", requests)
	} else {
		require.Zero(t, deletes, "a stateless or unsuccessful initialize has nothing to close: %+v", requests)
	}
	if expectAck && !expectClose {
		require.Empty(t, session, "a stateless initialize is acknowledged without a session header")
	}
}

// requireProbeUnanswered asserts a member that never answered: the opening request with the bearer, then at most the client's cancellations.
func requireProbeUnanswered(t *testing.T, requests []validationMemberRequest, bearer string) {
	t.Helper()
	require.NotEmpty(t, requests)
	require.Equal(t, "server/discover", requests[0].rpcMethod, "every probe opens with server/discover: %+v", requests)
	for i, req := range requests {
		require.Equal(t, "Bearer "+bearer, req.auth, "%+v", req)
		require.Equal(t, http.MethodPost, req.method, "%+v", req)
		if i > 0 {
			require.Equal(t, "notifications/cancelled", req.rpcMethod, "nothing but cancellations follows an unanswered opening: %+v", requests)
		}
	}
}

func TestServeConsentAction_ValidateMetaMember_RecordsWhatTheMemberAnswered(t *testing.T) {
	t.Parallel()

	ctx, fx := seedMetaValidationFixture(t, "aim204-meta")
	bearer := "token-aim204-meta"

	page := renderConsent(t, fx)
	require.Contains(t, page, `data-validation="none"`)
	require.Contains(t, page, "Not yet verified")
	require.Contains(t, page, `data-validate-link > Verify`)

	// 200: the member accepts the credential.
	fx.member.set(memberAccepts)
	requireValidated(t, fx)
	requireProbe(t, fx.member.drain(), bearer, true, true)
	sess := storedSession(t, ctx, fx)
	require.Equal(t, "valid", sess.ValidationStatus.String)
	require.False(t, sess.ValidationReason.Valid)
	require.True(t, sess.LastValidatedAt.Valid)
	verifiedAt := sess.LastValidatedAt.Time
	page = renderConsent(t, fx)
	require.Contains(t, page, `data-validation="valid"`)
	require.Contains(t, page, "Verified <time datetime=")
	require.Contains(t, page, ">just now</time>")
	require.Equal(t, map[string]int64{"valid": 1}, validationCounts(t, fx.reader))

	// Timeout: the member never answers, and the stored valid is not downgraded.
	fx.member.set(memberHangs)
	requireValidated(t, fx)
	requireProbeUnanswered(t, fx.member.drain(), bearer)
	sess = storedSession(t, ctx, fx)
	require.Equal(t, "valid", sess.ValidationStatus.String)
	require.Equal(t, verifiedAt, sess.LastValidatedAt.Time, "an inconclusive probe leaves the stored verdict untouched")
	page = renderConsent(t, fx)
	require.Contains(t, page, `data-validation="valid"`)
	require.Equal(t, map[string]int64{"valid": 1, "unknown": 1}, validationCounts(t, fx.reader))

	// 401: the member rejects the credential. Its body never reaches the row.
	fx.member.set(memberRejects)
	requireValidated(t, fx)
	requireProbe(t, fx.member.drain(), bearer, false, false)
	sess = storedSession(t, ctx, fx)
	require.Equal(t, "rejected_by_member", sess.ValidationStatus.String)
	require.Equal(t, "Rejected by "+fx.name, sess.ValidationReason.String)
	require.True(t, sess.LastValidatedAt.Time.After(verifiedAt))
	page = renderConsent(t, fx)
	require.Contains(t, page, `data-validation="rejected"`)
	require.Contains(t, page, "Rejected by "+fx.name+" — reconnect to continue")
	require.Contains(t, page, `data-connect-link > Reconnect`)
	require.NotContains(t, page, "secret upstream detail")
	require.Equal(t, map[string]int64{"valid": 1, "unknown": 1, "rejected_by_member": 1}, validationCounts(t, fx.reader))

	// Timeout again: a stored rejection is not a valid, so the inconclusive probe is recorded.
	fx.member.set(memberHangs)
	requireValidated(t, fx)
	requireProbeUnanswered(t, fx.member.drain(), bearer)
	sess = storedSession(t, ctx, fx)
	require.Equal(t, "unknown", sess.ValidationStatus.String)
	require.Equal(t, fx.name+" did not answer in time", sess.ValidationReason.String)
	page = renderConsent(t, fx)
	require.Contains(t, page, `data-validation="unknown"`)
	require.Contains(t, page, `data-validation="unknown" > · Checked <time datetime=`)
	require.Contains(t, page, `data-validation-reason >`+fx.name+" did not answer in time")
	require.NotContains(t, page, "Reconnect")
	require.Equal(t, map[string]int64{"valid": 1, "unknown": 2, "rejected_by_member": 1}, validationCounts(t, fx.reader))
}

// A standalone proxied endpoint validates through its own backend builder, routed as a runtime request is.
func TestServeConsentAction_ValidateStandaloneRemoteBackend(t *testing.T) {
	t.Parallel()

	ctx, fx := seedStandaloneValidationFixture(t, "aim204-solo")
	bearer := "token-aim204-solo"

	fx.member.set(memberAccepts)
	requireValidated(t, fx)
	requireProbe(t, fx.member.drain(), bearer, true, true)
	sess := storedSession(t, ctx, fx)
	require.Equal(t, "valid", sess.ValidationStatus.String)
	page := renderConsent(t, fx)
	require.Contains(t, page, `data-validation="valid"`)
	require.Contains(t, page, `data-validate-link > Verify`)

	fx.member.set(memberRejects)
	requireValidated(t, fx)
	requireProbe(t, fx.member.drain(), bearer, false, false)
	sess = storedSession(t, ctx, fx)
	require.Equal(t, "rejected_by_member", sess.ValidationStatus.String)
	require.Equal(t, "Rejected by "+fx.name, sess.ValidationReason.String)
	page = renderConsent(t, fx)
	require.Contains(t, page, "Rejected by "+fx.name+" — reconnect to continue")
	require.Equal(t, map[string]int64{"valid": 1, "rejected_by_member": 1}, validationCounts(t, fx.reader))
}

// A card without a live grant, or one no member would be handed, is refused before dialing.
func TestServeConsentAction_ValidateRefusesUnconnectedAndUnroutable(t *testing.T) {
	t.Parallel()

	ctx, fx := seedMetaValidationFixture(t, "aim204-refuse")
	projectID, orgID := consentTestTenant(t, ctx)

	unconnected := createConsentRemoteClient(t, ctx, fx.ti.conn, projectID, orgID, "aim204-refuse-unconnected", "", []uuid.UUID{fx.endpoint.UserSessionIssuerID})
	w, err := postValidate(t, fx, unconnected)
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.ErrorContains(t, err, "Connect this service before verifying it.")
	require.Empty(t, w.Header().Get("Location"))

	// A grant qualified to another upstream is unroutable on this gateway.
	elsewhere := createConsentRemoteClient(t, ctx, fx.ti.conn, projectID, orgID, "aim204-refuse-elsewhere", "", []uuid.UUID{fx.endpoint.UserSessionIssuerID})
	elsewhereIssuer := clientRemoteIssuerID(t, ctx, fx.ti.conn, projectID, orgID, elsewhere)
	createMetaMember(t, ctx, fx.ti.conn, projectID, fx.endpoint.MetaMcpServerID.UUID, "aim204-refuse-claimant", "https://claimed.example.com/mcp", conv.ToNullUUID(elsewhereIssuer), 1)
	insertQualifiedRemoteSessionToken(t, ctx, fx.ti, fx.endpoint.UserSessionIssuerID, elsewhere, fx.subject, "token-elsewhere", "https://elsewhere.example.com/mcp")
	_, err = postValidate(t, fx, elsewhere)
	require.Error(t, err)
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.ErrorContains(t, err, "cannot be verified")

	require.Empty(t, fx.member.drain(), "no probe reached the member")
	require.Empty(t, validationCounts(t, fx.reader))
}

// Protocol outcomes share the same end-to-end setup while pinning their distinct wire and stored results.
func TestServeConsentAction_ValidateProtocolOutcomes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, prefix, status string
		mode                 validationMemberMode
		expectAck, close     bool
		reasonFormat         string
		reconnect            bool
		contains, excludes   string
	}{
		{name: "stateless initialize", prefix: "aim204-stateless", mode: memberStateless, status: "valid", expectAck: true, contains: `data-validation="valid"`},
		{name: "initialized rejects", prefix: "aim204-ack401", mode: memberRejectsAck, status: "rejected_by_member", expectAck: true, close: true, reasonFormat: "Rejected by %s", contains: `data-validation="rejected"`},
		{name: "stateless initialized rejects", prefix: "stateless-ack401", mode: memberStatelessRejectsAck, status: "rejected_by_member", expectAck: true, reasonFormat: "Rejected by %s", reconnect: true},
		{name: "missing JSON-RPC version", prefix: "missing-version", mode: memberMissingVersion, status: "unknown", close: true, reasonFormat: "Unexpected answer from %s"},
		{name: "wrong JSON-RPC version", prefix: "wrong-version", mode: memberWrongVersion, status: "unknown", close: true, reasonFormat: "Unexpected answer from %s"},
		{name: "close fails", prefix: "aim204-close500", mode: memberFailsClose, status: "valid", expectAck: true, close: true},
		{name: "initialize forbidden", prefix: "aim204-403", mode: memberForbids, status: "rejected_by_member", reasonFormat: "Rejected by %s", reconnect: true, excludes: "secret upstream detail"},
		{name: "tools/list rejects", prefix: "aim204-list401", mode: memberRejectsList, status: "rejected_by_member", expectAck: true, close: true, reasonFormat: "Rejected by %s", reconnect: true, excludes: "secret upstream detail"},
		{name: "tools/list forbidden", prefix: "aim204-list403", mode: memberForbidsList, status: "rejected_by_member", expectAck: true, close: true, reasonFormat: "Rejected by %s", reconnect: true, excludes: "secret upstream detail"},
		{name: "tools/list errors", prefix: "aim204-listerr", mode: memberErrorsList, status: "unknown", expectAck: true, close: true, reasonFormat: "Unexpected answer from %s", excludes: "secret upstream detail"},
		{name: "discovery forbidden then accepts", prefix: "discovery403-ok", mode: memberDiscoveryForbiddenAccepts, status: "valid", expectAck: true, close: true},
		{name: "discovery forbidden then tools/list fails", prefix: "discovery403-list500", mode: memberDiscoveryForbiddenListFails, status: "unknown", expectAck: true, close: true, reasonFormat: "Unexpected answer from %s", excludes: "secret upstream detail"},
		{name: "discovery forbidden then tools/list hangs", prefix: "discovery403-list-timeout", mode: memberDiscoveryForbiddenListHangs, status: "unknown", expectAck: true, close: true, reasonFormat: "%s did not answer in time"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, fx := seedMetaValidationFixture(t, tc.prefix)
			fx.member.set(tc.mode)
			requireValidated(t, fx)
			requireProbe(t, fx.member.drain(), "token-"+tc.prefix, tc.expectAck, tc.close)

			sess := storedSession(t, ctx, fx)
			require.Equal(t, tc.status, sess.ValidationStatus.String)
			if tc.reasonFormat != "" {
				require.Equal(t, fmt.Sprintf(tc.reasonFormat, fx.name), sess.ValidationReason.String)
			} else {
				require.False(t, sess.ValidationReason.Valid)
			}
			page := renderConsent(t, fx)
			if tc.contains != "" {
				require.Contains(t, page, tc.contains)
			}
			if tc.reconnect {
				require.Contains(t, page, "Rejected by "+fx.name+" — reconnect to continue")
			}
			if tc.excludes != "" {
				require.NotContains(t, page, tc.excludes)
			}
			require.Equal(t, map[string]int64{tc.status: 1}, validationCounts(t, fx.reader))
		})
	}
}

// A 200 that is not an initialize result proves nothing: recorded unknown, never valid, and never over a stored valid.
func TestServeConsentAction_ValidateMalformedOKRecordsUnknown(t *testing.T) {
	t.Parallel()

	ctx, fx := seedMetaValidationFixture(t, "aim204-malformed")
	bearer := "token-aim204-malformed"

	// 200 carrying a JSON-RPC error.
	fx.member.set(memberErrors)
	requireValidated(t, fx)
	requireProbe(t, fx.member.drain(), bearer, false, false)
	sess := storedSession(t, ctx, fx)
	require.Equal(t, "unknown", sess.ValidationStatus.String)
	require.Equal(t, "Unexpected answer from "+fx.name, sess.ValidationReason.String)
	page := renderConsent(t, fx)
	require.Contains(t, page, `data-validation="unknown"`)
	require.NotContains(t, page, "secret upstream detail")

	// 200 with a body that is not JSON-RPC.
	fx.member.set(memberGarbles)
	requireValidated(t, fx)
	requireProbe(t, fx.member.drain(), bearer, false, false)
	sess = storedSession(t, ctx, fx)
	require.Equal(t, "unknown", sess.ValidationStatus.String)
	require.Equal(t, "Unexpected answer from "+fx.name, sess.ValidationReason.String)
	require.Equal(t, map[string]int64{"unknown": 2}, validationCounts(t, fx.reader))

	// Once valid, a malformed answer is inconclusive and leaves the verdict alone.
	fx.member.set(memberAccepts)
	requireValidated(t, fx)
	requireProbe(t, fx.member.drain(), bearer, true, true)
	require.Equal(t, "valid", storedSession(t, ctx, fx).ValidationStatus.String)
	fx.member.set(memberErrors)
	requireValidated(t, fx)
	requireProbe(t, fx.member.drain(), bearer, false, false)
	require.Equal(t, "valid", storedSession(t, ctx, fx).ValidationStatus.String)
	require.Equal(t, map[string]int64{"unknown": 3, "valid": 1}, validationCounts(t, fx.reader))
}

// A tunneled member is routed as dispatch routes it: its own issuer's grant, unqualified or naming its identifier.
// A grant qualified elsewhere is unroutable and never dialed.
func TestServeConsentAction_ValidateTunneledMetaMemberRoutesAsDispatchDoes(t *testing.T) {
	t.Parallel()

	reader, provider := newValidationMeterProvider()
	ctx, ti := newTestMCPServiceWithValidationTimeout(t, provider, validationProbeTimeout)
	projectID, orgID := consentTestTenant(t, ctx)
	shared := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaServerID := createMetaServer(t, ctx, ti.conn, projectID, orgID, "aim204-tunnel-gw", shared)
	endpoint, stateID, subject := mintFirstPartyConsentState(t, ctx, ti, projectID, orgID, shared, "aim204-tunnel-gw")
	endpoint.MetaMcpServerID = conv.ToNullUUID(metaServerID)

	clientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "aim204-tunnel-gw", "", []uuid.UUID{shared})
	issuerID := clientRemoteIssuerID(t, ctx, ti.conn, projectID, orgID, clientID)
	member := createTunneledMetaMember(t, ctx, ti.conn, projectID, metaServerID, "aim204-tunnel-member", "", conv.ToNullUUID(issuerID), 0)
	serverRow, err := mcpservers_repo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpservers_repo.GetMCPServerByIDAndProjectIDParams{ID: member.mcpServerID, ProjectID: projectID})
	require.NoError(t, err)

	gateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-1", backendSessionID: "backend-secret-session", legacy: false, dead: false, busy: false, challenge: "", mu: sync.Mutex{}, forwards: nil, forwardBodies: nil}
	gatewayServer := httptest.NewServer(gateway)
	t.Cleanup(gatewayServer.Close)
	require.NoError(t, ti.tunnelRoutes.Publish(ctx, serverRow.TunneledMcpServerID.UUID.String(), gatewayServer.URL, time.Hour))

	fx := validationFixture{ti: ti, reader: reader, endpoint: endpoint, stateID: stateID, subject: subject, member: nil, clientID: clientID, name: "aim204-tunnel-member"}

	// A resource-qualified grant is audience-bound elsewhere: unroutable, never dialed.
	insertQualifiedRemoteSessionToken(t, ctx, ti, shared, clientID, subject, "token-aim204-tunnel-qualified", "https://elsewhere.example.com/mcp")
	_, err = postValidate(t, fx, clientID)
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.ErrorContains(t, err, "cannot be verified")
	headers, _ := tunnelForwards(gateway)
	require.Empty(t, headers, "no probe reached the tunnel")
	require.Empty(t, validationCounts(t, fx.reader))
	require.False(t, storedSession(t, ctx, fx).ValidationStatus.Valid)

	// An unqualified grant under the member's own issuer rides the tunnel, as dispatch sends it.
	insertQualifiedRemoteSessionToken(t, ctx, ti, shared, clientID, subject, "token-aim204-tunnel-plain", "")
	requireValidated(t, fx)
	headers, bodies := tunnelForwards(gateway)
	requireTunnelProbe(t, headers, bodies, "token-aim204-tunnel-plain")
	require.Equal(t, "valid", storedSession(t, ctx, fx).ValidationStatus.String)
	require.Equal(t, map[string]int64{"valid": 1}, validationCounts(t, fx.reader))
}

// requireTunnelProbe asserts the dry run rode the tunnel on the given bearer: server/discover first, the handshake and tools/list on the backend's session, and its DELETE last.
func requireTunnelProbe(t *testing.T, headers []http.Header, bodies []string, bearer string) {
	t.Helper()
	require.GreaterOrEqual(t, len(headers), 5, "server/discover, initialize, initialized, tools/list, DELETE: %v", bodies)
	require.Contains(t, bodies[0], `"server/discover"`)
	require.Equal(t, "2026-07-28", headers[0].Get("MCP-Protocol-Version"), "the client opens on the latest revision")
	require.Empty(t, headers[0].Get("Mcp-Session-Id"))
	seen := map[string]bool{}
	for i, body := range bodies {
		require.Equal(t, "Bearer "+bearer, headers[i].Get("Authorization"), "the tunnel's own keyed token rides every probe request")
		for _, method := range []string{`"initialize"`, `"notifications/initialized"`, `"tools/list"`} {
			if strings.Contains(body, method) {
				seen[method] = true
				if method != `"initialize"` {
					require.Equal(t, "backend-secret-session", headers[i].Get("Mcp-Session-Id"), "%s rides the session the backend minted", method)
				}
			}
		}
	}
	require.Len(t, seen, 3, "the handshake and the tool list all reached the backend: %v", bodies)
	last := len(bodies) - 1
	require.Empty(t, bodies[last], "the DELETE closes the probe: %v", bodies)
	require.Equal(t, "backend-secret-session", headers[last].Get("Mcp-Session-Id"), "the close names the session the backend minted")
}

// tunnelForwards snapshots the fake gateway's forwards as (headers, body) pairs.
func tunnelForwards(g *fakeTunnelGateway) ([]http.Header, []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]http.Header(nil), g.forwards...), append([]string(nil), g.forwardBodies...)
}

// A standalone tunneled endpoint probes through the tunnel gateway with the token keyed by its own derived issuer.
func TestServeConsentAction_ValidateStandaloneTunneledBackend(t *testing.T) {
	t.Parallel()

	reader, provider := newValidationMeterProvider()
	ctx, ti := newTestMCPServiceWithValidationTimeout(t, provider, validationProbeTimeout)
	projectID, orgID := consentTestTenant(t, ctx)
	shared := createUserSessionIssuer(t, ctx, ti.conn, projectID)

	tunneledID, err := uuid.NewV7()
	require.NoError(t, err)
	tunneledServer, err := tunneledmcprepo.New(ti.conn).CreateServer(ctx, tunneledmcprepo.CreateServerParams{
		ID:                 tunneledID,
		ProjectID:          projectID,
		Name:               "aim204-tunnel-" + uuid.NewString()[:8],
		KeyHash:            uuid.NewString(),
		KeyPrefix:          "gram_tunnel_test",
		ResourceIdentifier: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	server, err := mcpservers_repo.New(ti.conn).CreateMCPServer(ctx, mcpservers_repo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                conv.ToPGText("aim204-tunnel"),
		Slug:                conv.ToPGText("aim204-tunnel"),
		TunneledMcpServerID: conv.ToNullUUID(tunneledServer.ID),
		Visibility:          "public",
		UserSessionIssuerID: conv.ToNullUUID(shared),
	})
	require.NoError(t, err)

	endpoint, stateID, subject := mintFirstPartyConsentState(t, ctx, ti, projectID, orgID, shared, "aim204-tunnel")
	endpoint.McpServerID = conv.ToNullUUID(server.ID)

	clientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "aim204-tunnel", "", []uuid.UUID{shared})
	require.NoError(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, ti.conn, orgID, projectID, []uuid.UUID{shared}))
	insertQualifiedRemoteSessionToken(t, ctx, ti, shared, clientID, subject, "token-aim204-tunnel", "")

	gateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-1", backendSessionID: "backend-secret-session", legacy: false, dead: false, busy: false, challenge: "", mu: sync.Mutex{}, forwards: nil, forwardBodies: nil}
	gatewayServer := httptest.NewServer(gateway)
	t.Cleanup(gatewayServer.Close)
	require.NoError(t, ti.tunnelRoutes.Publish(ctx, tunneledServer.ID.String(), gatewayServer.URL, time.Hour))

	fx := validationFixture{ti: ti, reader: reader, endpoint: endpoint, stateID: stateID, subject: subject, member: nil, clientID: clientID, name: "aim204-tunnel"}
	require.Contains(t, renderConsent(t, fx), `data-validate-link > Verify`)
	requireValidated(t, fx)

	headers, bodies := tunnelForwards(gateway)
	requireTunnelProbe(t, headers, bodies, "token-aim204-tunnel")

	sess := storedSession(t, ctx, fx)
	require.Equal(t, "valid", sess.ValidationStatus.String)
	require.Contains(t, renderConsent(t, fx), `data-validation="valid"`)
	require.Equal(t, map[string]int64{"valid": 1}, validationCounts(t, fx.reader))

	// The route goes away: an inconclusive probe, and the stored valid survives it.
	gateway.dead = true
	requireValidated(t, fx)
	sess = storedSession(t, ctx, fx)
	require.Equal(t, "valid", sess.ValidationStatus.String)
	require.Equal(t, map[string]int64{"valid": 1, "unknown": 1}, validationCounts(t, fx.reader))
}

// createOrgLevelConsentRemoteClient mints an organization-level issuer and client (project_id NULL) on the given user session issuers.
func createOrgLevelConsentRemoteClient(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID, slug string, userSessionIssuerIDs []uuid.UUID) uuid.UUID {
	t.Helper()
	asBase := "https://" + slug + "-as.example.com"
	q := remotesessions_repo.New(conn)
	rsi, err := q.CreateRemoteSessionIssuer(ctx, remotesessions_repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:                    conv.ToPGText(organizationID),
		Slug:                              slug + "-rsi",
		Issuer:                            asBase,
		AuthorizationEndpoint:             conv.ToPGText(asBase + "/authorize"),
		TokenEndpoint:                     conv.ToPGText(asBase + "/token"),
		ScopesSupported:                   []string{},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
		CodeChallengeMethodsSupported:     []string{"S256"},
	})
	require.NoError(t, err)
	rsc, err := q.CreateRemoteSessionClient(ctx, remotesessions_repo.CreateRemoteSessionClientParams{
		ProjectID:             uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:        conv.ToPGText(organizationID),
		RemoteSessionIssuerID: rsi.ID,
		ClientID:              slug + "-external-client",
		ClientIDIssuedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	require.NoError(t, err)
	for _, usi := range userSessionIssuerIDs {
		require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessions_repo.AttachRemoteSessionClientToUserSessionIssuerParams{
			RemoteSessionClientID: rsc.ID,
			UserSessionIssuerID:   usi,
		}))
	}
	return rsc.ID
}

// An organization-level client (project_id NULL) validates like a project one.
func TestServeConsentAction_ValidateOrgLevelClient(t *testing.T) {
	t.Parallel()

	reader, provider := newValidationMeterProvider()
	ctx, ti := newTestMCPServiceWithValidationTimeout(t, provider, validationProbeTimeout)
	projectID, orgID := consentTestTenant(t, ctx)
	shared := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaServerID := createMetaServer(t, ctx, ti.conn, projectID, orgID, "aim204-org-gw", shared)
	endpoint, stateID, subject := mintFirstPartyConsentState(t, ctx, ti, projectID, orgID, shared, "aim204-org-gw")
	endpoint.MetaMcpServerID = conv.ToNullUUID(metaServerID)

	member := newValidationMember(t)
	clientID := createOrgLevelConsentRemoteClient(t, ctx, ti.conn, orgID, "aim204-org", []uuid.UUID{shared})
	issuerID := clientRemoteIssuerID(t, ctx, ti.conn, projectID, orgID, clientID)
	createMetaMember(t, ctx, ti.conn, projectID, metaServerID, "aim204-org-member", member.url, conv.ToNullUUID(issuerID), 0)
	insertQualifiedRemoteSessionToken(t, ctx, ti, shared, clientID, subject, "token-aim204-org", member.url)

	fx := validationFixture{ti: ti, reader: reader, endpoint: endpoint, stateID: stateID, subject: subject, member: member, clientID: clientID, name: "aim204-org-member"}
	requireValidated(t, fx)
	requireProbe(t, member.drain(), "token-aim204-org", true, true)
	require.Equal(t, "valid", storedSession(t, ctx, fx).ValidationStatus.String)
	require.Contains(t, renderConsent(t, fx), `data-validation="valid"`)
	require.Equal(t, map[string]int64{"valid": 1}, validationCounts(t, fx.reader))
}

func TestServeConsentAction_ValidatePersistsVerdictAfterRefresh(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		expiresIn   time.Duration
		priorValid  bool
		memberMode  validationMemberMode
		wantVerdict string
		expectClose bool
	}{
		{name: "inside refresh window", expiresIn: 5 * time.Second, memberMode: memberAccepts, wantVerdict: "valid", expectClose: true},
		{name: "expired but refreshable", expiresIn: -time.Minute, memberMode: memberAccepts, wantVerdict: "valid", expectClose: true},
		{name: "previously valid refreshed token is unknown", expiresIn: -time.Minute, priorValid: true, memberMode: memberGarbles, wantVerdict: "unknown", expectClose: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, fx := seedMetaValidationFixture(t, "aim204-refresh-"+uuid.NewString()[:8])
			projectID, orgID := consentTestTenant(t, ctx)
			expiresAt := time.Now().Add(tc.expiresIn)
			tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					http.Error(w, "invalid form", http.StatusBadRequest)
					return
				}
				if r.Form.Get("grant_type") != "refresh_token" {
					http.Error(w, "unexpected grant type", http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"access_token":"refreshed-validation-token","token_type":"Bearer","expires_in":3600,"refresh_token":"rotated-refresh-token"}`)
			}))
			t.Cleanup(tokenServer.Close)

			rows, err := remotesessions_repo.New(fx.ti.conn).ForceRemoteSessionIssuerTokenEndpointFixture(ctx, remotesessions_repo.ForceRemoteSessionIssuerTokenEndpointFixtureParams{
				TokenEndpoint:         conv.ToPGText(tokenServer.URL),
				RemoteSessionClientID: fx.clientID,
				ProjectID:             projectID,
				OrganizationID:        orgID,
			})
			require.NoError(t, err)
			require.EqualValues(t, 1, rows)

			accessEncrypted, err := fx.ti.enc.Encrypt([]byte("stale-validation-token"))
			require.NoError(t, err)
			refreshEncrypted, err := fx.ti.enc.Encrypt([]byte("refresh-validation-token"))
			require.NoError(t, err)
			configured, err := remotesessions_repo.New(fx.ti.conn).UpsertRemoteSession(ctx, remotesessions_repo.UpsertRemoteSessionParams{
				SubjectUrn:            fx.subject,
				UserSessionIssuerID:   fx.endpoint.UserSessionIssuerID,
				RemoteSessionClientID: fx.clientID,
				AccessTokenEncrypted:  accessEncrypted,
				AccessExpiresAt:       pgtype.Timestamptz{Time: expiresAt, Valid: true, InfinityModifier: pgtype.Finite},
				RefreshTokenEncrypted: conv.ToPGText(refreshEncrypted),
				RefreshExpiresAt:      pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true, InfinityModifier: pgtype.Finite},
				Scopes:                []string{},
				Resource:              conv.ToPGText(fx.member.url),
			})
			require.NoError(t, err)
			if tc.priorValid {
				rows, err = remotesessions_repo.New(fx.ti.conn).SetRemoteSessionValidation(ctx, remotesessions_repo.SetRemoteSessionValidationParams{
					LastValidatedAt:       pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true},
					ValidationStatus:      "valid",
					ID:                    configured.ID,
					SubjectUrn:            fx.subject,
					RemoteSessionClientID: fx.clientID,
					ExpectedUpdatedAt:     configured.UpdatedAt,
					ProjectID:             projectID,
					OrganizationID:        orgID,
				})
				require.NoError(t, err)
				require.EqualValues(t, 1, rows)
			}
			fx.member.set(tc.memberMode)

			require.Contains(t, renderConsent(t, fx), `data-validate-link > Verify`)
			requireValidated(t, fx)
			requireProbe(t, fx.member.drain(), "refreshed-validation-token", tc.expectClose, tc.expectClose)
			after := storedSession(t, ctx, fx)
			require.True(t, after.UpdatedAt.Time.After(configured.UpdatedAt.Time), "refresh rotates the grant CAS token")
			require.Equal(t, tc.wantVerdict, after.ValidationStatus.String, "the verdict lands against the refreshed grant snapshot")
			require.True(t, after.LastValidatedAt.Valid)
		})
	}
}

// An expired grant has no credential to present: refused before dialing.
func TestServeConsentAction_ValidateRefusesExpiredGrant(t *testing.T) {
	t.Parallel()

	ctx, fx := seedMetaValidationFixture(t, "aim204-expired")
	projectID, _ := consentTestTenant(t, ctx)
	sess := storedSession(t, ctx, fx)
	require.NoError(t, remotesessions_repo.New(fx.ti.conn).SetRemoteSessionAccessExpiresAt(ctx, remotesessions_repo.SetRemoteSessionAccessExpiresAtParams{
		AccessExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true, InfinityModifier: pgtype.Finite},
		ID:              sess.ID,
		ProjectID:       conv.ToNullUUID(projectID),
	}))

	_, err := postValidate(t, fx, fx.clientID)
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.ErrorContains(t, err, "Connect this service before verifying it.")
	require.Empty(t, fx.member.drain(), "no probe reached the member")
	require.Empty(t, validationCounts(t, fx.reader))
}

// A toolset-backed endpoint forwards no credential upstream: no Verify, and a posted validate is refused.
func TestServeConsentAction_ToolsetEndpointOffersNoRecheck(t *testing.T) {
	t.Parallel()

	reader, provider := newValidationMeterProvider()
	ctx, ti := newTestMCPServiceWithValidationTimeout(t, provider, validationProbeTimeout)
	projectID, orgID := consentTestTenant(t, ctx)
	shared := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	endpoint, stateID, subject := mintFirstPartyConsentState(t, ctx, ti, projectID, orgID, shared, "aim204-toolset")
	endpoint.ToolsetID = conv.ToNullUUID(uuid.New())

	member := newValidationMember(t)
	clientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "aim204-toolset", "", []uuid.UUID{shared})
	insertQualifiedRemoteSessionToken(t, ctx, ti, shared, clientID, subject, "token-aim204-toolset", member.url)

	fx := validationFixture{ti: ti, reader: reader, endpoint: endpoint, stateID: stateID, subject: subject, member: member, clientID: clientID, name: ""}
	page := renderConsent(t, fx)
	require.Contains(t, page, "Connected")
	require.NotContains(t, page, "Verify")
	require.NotContains(t, page, "data-validation")

	_, err := postValidate(t, fx, clientID)
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.ErrorContains(t, err, "cannot be verified")
	require.Empty(t, member.drain())
	require.Empty(t, validationCounts(t, fx.reader))
}

// A reconnect landing mid-probe rotates the grant; the verdict on the old token is dropped, not stamped on the new one.
func TestServeConsentAction_ValidateReconnectMidProbeLeavesNewGrantUnvalidated(t *testing.T) {
	t.Parallel()

	ctx, fx := seedMetaValidationFixture(t, "aim204-cas")
	before := storedSession(t, ctx, fx)
	fx.member.mu.Lock()
	fx.member.onInitialize = func() error {
		encrypted, err := fx.ti.enc.Encrypt([]byte("token-aim204-cas-new"))
		if err != nil {
			return fmt.Errorf("encrypt: %w", err)
		}
		_, err = remotesessions_repo.New(fx.ti.conn).UpsertRemoteSession(ctx, remotesessions_repo.UpsertRemoteSessionParams{
			SubjectUrn:            fx.subject,
			UserSessionIssuerID:   fx.endpoint.UserSessionIssuerID,
			RemoteSessionClientID: fx.clientID,
			AccessTokenEncrypted:  encrypted,
			AccessExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true, InfinityModifier: pgtype.Finite},
			RefreshTokenEncrypted: pgtype.Text{String: "", Valid: false},
			RefreshExpiresAt:      pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite},
			Scopes:                []string{},
			Resource:              conv.ToPGText(fx.member.url),
		})
		if err != nil {
			return fmt.Errorf("reconnect mid-probe: %w", err)
		}
		return nil
	}
	fx.member.mu.Unlock()

	fx.member.set(memberAccepts)
	requireValidated(t, fx)
	require.NoError(t, fx.member.hookErr)
	requireProbe(t, fx.member.drain(), "token-aim204-cas", true, true)

	after := storedSession(t, ctx, fx)
	require.Equal(t, before.ID, after.ID, "the reconnect replaced the grant in place")
	require.True(t, after.UpdatedAt.Time.After(before.UpdatedAt.Time))
	require.False(t, after.ValidationStatus.Valid, "the old token's verdict never lands on the new grant")
	require.False(t, after.LastValidatedAt.Valid)
	require.Contains(t, renderConsent(t, fx), "Not yet verified")
	require.Equal(t, map[string]int64{"valid": 1}, validationCounts(t, fx.reader), "the probe itself still counts")
}

// Verify is paced per challenge: past the limit the card gets a notice, no probe runs, nothing is written.
func TestServeConsentAction_ValidateRateLimited(t *testing.T) {
	t.Parallel()

	ctx, fx := seedMetaValidationFixture(t, "aim204-limit")
	fx.member.set(memberAccepts)
	for range 6 {
		requireValidated(t, fx)
		requireProbe(t, fx.member.drain(), "token-aim204-limit", true, true)
	}
	before := storedSession(t, ctx, fx)
	require.Equal(t, "valid", before.ValidationStatus.String)

	w, err := postValidate(t, fx, fx.clientID)
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, w.Code)
	location := w.Header().Get("Location")
	require.Equal(t, "/mcp/"+fx.endpoint.Slug+"/connect?state="+url.QueryEscape(fx.stateID)+"&validate_limited="+fx.clientID.String(), location)
	require.Empty(t, fx.member.drain(), "a limited verify never reaches the member")

	page := renderConsentAt(t, fx, location)
	require.Contains(t, page, `data-validation-notice >Try again in a moment`)
	require.Contains(t, page, `data-validation="valid"`, "the stored verdict still shows")
	require.NotContains(t, renderConsent(t, fx), "Try again in a moment", "the notice is not sticky")

	after := storedSession(t, ctx, fx)
	require.Equal(t, before.LastValidatedAt.Time, after.LastValidatedAt.Time, "nothing is written for a limited verify")
	require.Equal(t, map[string]int64{"valid": 6}, validationCounts(t, fx.reader))
}

// introspectionServer is a plain-http fake authorization server answering RFC 7662 with a scripted body.
func introspectionServer(t *testing.T, body *atomic.Pointer[string]) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.ParseForm() != nil || r.Form.Get("token") == "" {
			http.Error(w, "bad introspection request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, *body.Load())
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// Only introspection can tell a member's rejection from a token the provider no longer honours.
func TestServeConsentAction_ValidateRejectedThenIntrospectedAsInactive(t *testing.T) {
	t.Parallel()

	ctx, fx := seedMetaValidationFixture(t, "aim205-revoked")
	projectID, orgID := consentTestTenant(t, ctx)
	var body atomic.Pointer[string]
	body.Store(conv.PtrEmpty(`{"active":true,"sub":"user-123","username":"grant"}`))
	rows, err := remotesessions_repo.New(fx.ti.conn).ForceRemoteSessionIssuerEnrichmentEndpointsFixture(ctx, remotesessions_repo.ForceRemoteSessionIssuerEnrichmentEndpointsFixtureParams{
		UserinfoEndpoint:      pgtype.Text{String: "", Valid: false},
		IntrospectionEndpoint: conv.ToPGText(introspectionServer(t, &body)),
		JwksUri:               pgtype.Text{String: "", Valid: false},
		RemoteSessionClientID: fx.clientID,
		ProjectID:             projectID,
		OrganizationID:        orgID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)

	// The member rejects but the provider still honours the token: a member rejection.
	fx.member.set(memberRejects)
	requireValidated(t, fx)
	sess := storedSession(t, ctx, fx)
	require.Equal(t, "rejected_by_member", sess.ValidationStatus.String)
	require.Equal(t, "Rejected by "+fx.name, sess.ValidationReason.String)
	require.Equal(t, "user-123", sess.UpstreamSubject.String, "introspection still enriches the grant")
	require.Equal(t, remotesessions.IdentitySourceIntrospection, sess.IdentitySource.String)
	page := renderConsent(t, fx)
	require.Contains(t, page, `data-validation="rejected"`)
	require.Contains(t, page, "Authenticated as grant")

	// The provider reports the token dead: inactive.
	body.Store(conv.PtrEmpty(`{"active":false}`))
	requireValidated(t, fx)
	sess = storedSession(t, ctx, fx)
	require.Equal(t, "inactive", sess.ValidationStatus.String)
	require.Equal(t, "Inactive at aim205-revoked-rsi", sess.ValidationReason.String)
	page = renderConsent(t, fx)
	require.Contains(t, page, `data-validation="inactive"`)
	require.Contains(t, page, "Inactive at aim205-revoked-rsi — reconnect to continue")
	require.Contains(t, page, `data-connect-link > Reconnect`)
	require.Equal(t, map[string]int64{"rejected_by_member": 1, "inactive": 1}, validationCounts(t, fx.reader))

	// A member that accepts the token is trusted over a provider that says otherwise.
	fx.member.set(memberAccepts)
	requireValidated(t, fx)
	require.Equal(t, "valid", storedSession(t, ctx, fx).ValidationStatus.String)
}
