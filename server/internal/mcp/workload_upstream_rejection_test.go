package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// rejectedOwnerToken is the owner's stored upstream credential, which the
// rejecting upstream refuses.
const rejectedOwnerToken = "owner-token-the-upstream-refuses"

// rejection is how a rejecting upstream answers: its challenge, and the body
// it sends with contentType.
type rejection struct {
	challenge   string
	contentType string
	body        string
}

// expiredChallenge is a rejection whose challenge carries the reason.
var expiredChallenge = rejection{challenge: `Bearer error="invalid_token", error_description="credential expired"`, contentType: "", body: ""}

// rejectingUpstream answers every MCP request with a 401 as its rejection
// says, recording the Authorization header it was sent.
type rejectingUpstream struct {
	*httptest.Server

	mu   sync.Mutex
	seen []string
}

func newRejectingUpstream(t *testing.T, answer rejection) *rejectingUpstream {
	t.Helper()

	u := &rejectingUpstream{Server: nil, mu: sync.Mutex{}, seen: nil}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/.well-known/") {
			http.NotFound(w, r)
			return
		}

		u.mu.Lock()
		u.seen = append(u.seen, r.Header.Get("Authorization"))
		u.mu.Unlock()

		if answer.challenge != "" {
			w.Header().Set("WWW-Authenticate", answer.challenge)
		}
		if answer.contentType != "" {
			w.Header().Set("Content-Type", answer.contentType)
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(answer.body))
	}))
	t.Cleanup(u.Close)

	return u
}

func (u *rejectingUpstream) authorizations() []string {
	u.mu.Lock()
	defer u.mu.Unlock()

	return append([]string(nil), u.seen...)
}

// rejectedCredentialEndpoint is an issuer-gated remote endpoint whose owner
// has a stored upstream credential the upstream rejects, with bearers for a
// workload acting through the owner's agent and for the owner directly.
type rejectedCredentialEndpoint struct {
	slug          string
	workloadToken string
	ownerToken    string
	upstream      *rejectingUpstream
}

func newRejectedCredentialEndpoint(t *testing.T, ctx context.Context, ti *testInstance, answer rejection) rejectedCredentialEndpoint {
	t.Helper()

	fx := newWorkloadConsentFixture(t, ctx, ti)
	selector, err := authz.NewSelector(authz.ScopeProjectRead, fx.target.ProjectID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: fx.orgID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, fx.userID), Scope: string(authz.ScopeProjectRead), Selectors: selector})
	require.NoError(t, err)

	upstream := newRejectingUpstream(t, answer)
	slug := "workload-rejected-" + uuid.NewString()[:8]
	server, _ := createRemoteMcpEndpoint(t, ctx, ti.conn, fx.target.ProjectID, upstream.URL, slug, "private", fx.target.UserSessionIssuerID)
	fx.target.MCPResourceID = server.ID
	agent := createConsentAgent(t, ctx, ti, fx, "Rejected credential workload")
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), server.ID)
	issuer := seedWorkloadIssuer(t, ctx, ti, fx.orgID)
	assignAgentToWorkload(t, ctx, ti, fx.orgID, issuer, workloadSessionSubject, agent.ID)
	subject := urn.NewWorkloadSubject(issuer, workloadSessionSubject)
	session := seedWorkloadSession(t, ctx, ti, fx, subject)
	client := createConsentRemoteClient(t, ctx, ti.conn, fx.target.ProjectID, fx.orgID, "workload-rejected", "", []uuid.UUID{fx.target.UserSessionIssuerID})
	selection := selectionFixture{ti: ti, projectID: fx.target.ProjectID, orgID: fx.orgID}
	selection.associate(t, ctx, server.ID, client)
	policy, err := usersessionsrepo.New(ti.conn).GetUserSessionPolicyByJTI(ctx, usersessionsrepo.GetUserSessionPolicyByJTIParams{UserSessionIssuerID: fx.target.UserSessionIssuerID, Jti: session.Jti})
	require.NoError(t, err)
	_, err = usersessionsrepo.New(ti.conn).RecordUpstreamClientSelections(ctx, usersessionsrepo.RecordUpstreamClientSelectionsParams{UserSessionIssuerID: fx.target.UserSessionIssuerID, CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: policy.CredentialID, Source: upstreamclients.SelectionSourceIssuance, PickedMcpServerIds: []uuid.UUID{server.ID}, PickedClientIds: []uuid.UUID{client}})
	require.NoError(t, err)
	insertQualifiedRemoteSessionToken(t, ctx, ti, fx.target.UserSessionIssuerID, client, urn.NewUserSubject(fx.userID), rejectedOwnerToken, upstream.URL)

	signer := sessiontokens.NewSigner("test-jwt-secret")
	issuerURL := ti.serverURL.JoinPath("mcp", slug).String()
	audience := urn.NewUserSessionIssuer(fx.target.UserSessionIssuerID).String()
	workloadToken, _, err := signer.Mint(sessiontokens.MintParams{Subject: subject, Audience: audience, Issuer: issuerURL, ExpiresAt: &session.ExpiresAt.Time, ClientID: fx.client.ClientID, JTI: session.Jti})
	require.NoError(t, err)
	ownerToken, ownerJTI, err := signer.Mint(sessiontokens.MintParams{Subject: urn.NewUserSubject(fx.userID), Audience: audience, Issuer: issuerURL, Lifetime: time.Hour})
	require.NoError(t, err)
	persistTestUserSession(t, ti, fx.target.UserSessionIssuerID, urn.NewUserSubject(fx.userID), ownerJTI)
	ownerPolicy, err := usersessionsrepo.New(ti.conn).GetUserSessionPolicyByJTI(ctx, usersessionsrepo.GetUserSessionPolicyByJTIParams{UserSessionIssuerID: fx.target.UserSessionIssuerID, Jti: ownerJTI})
	require.NoError(t, err)
	_, err = usersessionsrepo.New(ti.conn).RecordUpstreamClientSelections(ctx, usersessionsrepo.RecordUpstreamClientSelectionsParams{UserSessionIssuerID: fx.target.UserSessionIssuerID, CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: ownerPolicy.CredentialID, Source: upstreamclients.SelectionSourceIssuance, PickedMcpServerIds: []uuid.UUID{server.ID}, PickedClientIds: []uuid.UUID{client}})
	require.NoError(t, err)

	return rejectedCredentialEndpoint{slug: slug, workloadToken: workloadToken, ownerToken: ownerToken, upstream: upstream}
}

func TestServePublic_WorkloadUpstreamCredentialRejectionNamesTheServer(t *testing.T) {
	t.Parallel()

	ctx, ti, _ := newTestMCPServiceWithDevIDP(t)
	ti.service.manual = true
	endpoint := newRejectedCredentialEndpoint(t, ctx, ti, expiredChallenge)

	for name, body := range map[string][]byte{
		"initialize": makeInitializeBody(),
		"tools/call": toolsCallBody(t),
	} {
		w, err := servePublicHTTP(t, context.Background(), ti, endpoint.slug, body, endpoint.workloadToken, nil)
		message := requireOopsCode(t, err, oops.CodeFailedPrecondition)
		require.NotEqual(t, http.StatusUnauthorized, w.Code, name)
		require.Empty(t, w.Header().Get("WWW-Authenticate"), "%s: no challenge sends the workload to exchange a token that is still valid", name)
		require.Equal(t, `The upstream rejected the credential this workload uses for MCP server "test mcp server" (HTTP 401, invalid_token: credential expired). The workload's Speakeasy token is still valid, so exchanging a new one will not help. The credential's owner must reconnect "test mcp server" in Speakeasy.`, message, name)
		require.NotContains(t, message, rejectedOwnerToken, name)
		require.NotContains(t, w.Body.String(), rejectedOwnerToken, name)
	}

	sent := endpoint.upstream.authorizations()
	require.NotEmpty(t, sent)
	for _, authorization := range sent {
		require.Equal(t, "Bearer "+rejectedOwnerToken, authorization, "the upstream only ever sees the owner's credential")
	}
}

func TestServePublic_WorkloadUpstreamCredentialRejectionIsRecordedForTheServer(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]func(t *testing.T) []byte{
		"initialize": func(*testing.T) []byte { return makeInitializeBody() },
		"tools/call": toolsCallBody,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx, ti, _ := newTestMCPServiceWithDevIDP(t)
			ti.service.manual = true
			endpoint := newRejectedCredentialEndpoint(t, ctx, ti, expiredChallenge)
			rec := recordConnections(t, ti)

			_, err := servePublicHTTP(t, context.Background(), ti, endpoint.slug, body(t), endpoint.workloadToken, nil)
			requireOopsCode(t, err, oops.CodeFailedPrecondition)

			rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeProviderRejected)
		})
	}
}

func TestServePublic_OwnerUpstreamCredentialRejectionKeepsTheChallenge(t *testing.T) {
	t.Parallel()

	ctx, ti, _ := newTestMCPServiceWithDevIDP(t)
	ti.service.manual = true
	endpoint := newRejectedCredentialEndpoint(t, ctx, ti, expiredChallenge)

	w, err := servePublicHTTP(t, context.Background(), ti, endpoint.slug, toolsCallBody(t), endpoint.ownerToken, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	require.Contains(t, w.Header().Get("WWW-Authenticate"), "resource_metadata", "a person reauthorizes through this server")
	require.Contains(t, endpoint.upstream.authorizations(), "Bearer "+rejectedOwnerToken)
}

// workloadToolsCallRejection is the message a workload reads when a tool call
// meets an upstream answering as answer.
func workloadToolsCallRejection(t *testing.T, answer rejection) string {
	t.Helper()

	ctx, ti, _ := newTestMCPServiceWithDevIDP(t)
	ti.service.manual = true
	endpoint := newRejectedCredentialEndpoint(t, ctx, ti, answer)

	w, err := servePublicHTTP(t, context.Background(), ti, endpoint.slug, toolsCallBody(t), endpoint.workloadToken, nil)
	message := requireOopsCode(t, err, oops.CodeFailedPrecondition)
	require.Empty(t, w.Header().Get("WWW-Authenticate"))
	require.NotContains(t, message, rejectedOwnerToken)
	require.NotContains(t, w.Body.String(), rejectedOwnerToken)

	return message
}

func TestServePublic_WorkloadUpstreamCredentialRejectionQuotesAPlainTextBody(t *testing.T) {
	t.Parallel()

	message := workloadToolsCallRejection(t, rejection{
		challenge:   `Bearer error="invalid_token"`,
		contentType: "text/plain; charset=utf-8",
		body:        "unauthorized: token authentication failed: credential expired\nrequest id 1234",
	})

	require.Contains(t, message, `(HTTP 401, invalid_token: unauthorized: token authentication failed: credential expired)`)
	require.NotContains(t, message, "request id", "only the first line is quoted")
}

func TestServePublic_WorkloadUpstreamCredentialRejectionQuotesAJSONBody(t *testing.T) {
	t.Parallel()

	message := workloadToolsCallRejection(t, rejection{
		challenge:   "",
		contentType: "application/json",
		body:        `{"error":"bad_credentials","message":"Bad credentials","documentation_url":"https://docs.example.com/rest"}`,
	})

	require.Contains(t, message, `(HTTP 401: Bad credentials)`)
	require.NotContains(t, message, "documentation_url")
}

func TestServePublic_WorkloadUpstreamCredentialRejectionPrefersTheChallengeReason(t *testing.T) {
	t.Parallel()

	message := workloadToolsCallRejection(t, rejection{
		challenge:   `Bearer error="invalid_token", error_description="credential expired"`,
		contentType: "application/json",
		body:        `{"message":"Bad credentials"}`,
	})

	require.Contains(t, message, `(HTTP 401, invalid_token: credential expired)`)
	require.NotContains(t, message, "Bad credentials")
}

func TestServePublic_WorkloadUpstreamCredentialRejectionDoesNotReadAnEventStream(t *testing.T) {
	t.Parallel()

	message := workloadToolsCallRejection(t, rejection{
		challenge:   `Bearer error="invalid_token"`,
		contentType: "text/event-stream",
		body:        "event: message\ndata: {\"jsonrpc\":\"2.0\"}\n\n",
	})

	require.Contains(t, message, `(HTTP 401, invalid_token)`)
	require.NotContains(t, message, "event:")
}

func TestServePublic_WorkloadUpstreamCredentialRejectionRedactsAnEchoedCredential(t *testing.T) {
	t.Parallel()

	message := workloadToolsCallRejection(t, rejection{
		challenge:   "",
		contentType: "text/plain",
		body:        "token " + rejectedOwnerToken + " has expired",
	})

	require.Contains(t, message, `(HTTP 401: token [redacted] has expired)`)
}
