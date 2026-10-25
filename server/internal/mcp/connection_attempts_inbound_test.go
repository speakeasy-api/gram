package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// inboundAttemptRow is one raw inbound_authorize or inbound_token row.
type inboundAttemptRow struct {
	Stage        string `ch:"stage"`
	Outcome      string `ch:"outcome"`
	FailureClass string `ch:"failure_class"`
	Reason       string `ch:"reason"`
	Subject      string `ch:"subject"`
	ClientKey    string `ch:"client_key"`
	GrantType    string `ch:"grant_type"`
	Attributes   string `ch:"attributes"`
}

// inboundAttempts reads a gateway's committed inbound rows, oldest first.
func inboundAttempts(t *testing.T, projectID, gatewayID uuid.UUID) []inboundAttemptRow {
	t.Helper()
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	var rows []inboundAttemptRow
	require.NoError(t, chConn.Select(t.Context(), &rows,
		`SELECT toString(attributes.gram.connection_attempt.stage) AS stage,
			        toString(attributes.gram.connection_attempt.outcome) AS outcome,
			        toString(attributes.gram.connection_attempt.failure_class) AS failure_class,
			        toString(attributes.gram.connection_attempt.reason) AS reason,
			        toString(attributes.gram.connection_attempt.subject) AS subject,
			        toString(attributes.gram.connection_attempt.client_key) AS client_key,
			        toString(attributes.gram.connection_attempt.grant_type) AS grant_type,
			        toJSONString(attributes) AS attributes
			 FROM telemetry_logs
			 WHERE gram_project_id = ? AND event_urn = ? AND meta_mcp_server_id = ?
			   AND startsWith(toString(attributes.gram.connection_attempt.stage), 'inbound_')
			 ORDER BY time_unix_nano`,
		projectID.String(), mcp.MCPConnectionAttemptEventURN, gatewayID.String()))
	return rows
}

// stageOutcomes reads the rollup's attempts per stage and outcome for one
// caller and client, the counts the health reader serves.
func stageOutcomes(t *testing.T, projectID, gatewayID uuid.UUID, subject, clientKey string) map[string]uint64 {
	t.Helper()
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	var rows []struct {
		Stage    string `ch:"stage"`
		Outcome  string `ch:"outcome"`
		Attempts uint64 `ch:"attempts"`
	}
	require.NoError(t, chConn.Select(t.Context(), &rows,
		`SELECT stage, outcome, sum(attempts) AS attempts
		 FROM mcp_connection_attempt_summaries
		 WHERE gram_project_id = toUUID(?) AND server_kind = 'meta' AND server_id = ?
		   AND subject = ? AND provider_id = '' AND client_key = ?
		 GROUP BY stage, outcome`,
		projectID.String(), gatewayID.String(), subject, clientKey))
	counts := map[string]uint64{}
	for _, row := range rows {
		counts[row.Stage+"/"+row.Outcome] = row.Attempts
	}
	return counts
}

// requireNoSecrets checks an inbound row's dimensions carry none of the
// request's raw values.
func requireNoSecrets(t *testing.T, rows []inboundAttemptRow, secrets ...string) {
	t.Helper()
	for _, row := range rows {
		for _, secret := range secrets {
			require.NotContains(t, row.Attributes, secret)
		}
	}
}

func listGatewayAs(t *testing.T, ctx context.Context, ti *testInstance, slug, token string, params map[string]any) directListing {
	t.Helper()
	w, err := servePublicHTTP(t, ctx, ti, slug, makeMetaRPCBody(t, "tools/list", params), token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var listing directListing
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listing), w.Body.String())
	return listing
}

// The diagnosis this stage exists for, driven through the real consent,
// token, and listing handlers: a token is written for the person's registered
// client, a cursor from an earlier listing still fails, signing in again
// writes another token and the old cursor still fails, and a listing without
// a cursor completes. Every fact names the same person and client.
func TestGatewayInboundTokensBesideStaleListings(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	writes := watchConnectionAttempts(t, ti)
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "inbound-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	seedHostedMetaMember(t, ctx, ti, gateway.ID, "Source", 1, mcpservers.VisibilityPublic, "alpha")
	caller := authztest.WithExactGrants(t, t.Context(), authz.NewGrant(authz.ScopeMCPConnect, gateway.ID.String()))
	staleCursor := map[string]any{"cursor": "eyJvZmZzZXQiOjEwMH0"}

	first := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeDirect, nil)
	require.Equal(t, 1, writes.written(t), "the token request is one attempt")
	stale := listGatewayAs(t, caller, ti, slug, first.token, staleCursor)
	require.NotNil(t, stale.Error)
	require.Contains(t, stale.Error.Message, "restart tools/list")
	require.Equal(t, 1, writes.written(t))

	second := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeDirect, nil)
	require.Equal(t, 1, writes.written(t))
	stale = listGatewayAs(t, caller, ti, slug, second.token, staleCursor)
	require.NotNil(t, stale.Error, "a new token does not make an old cursor valid")
	require.Contains(t, stale.Error.Message, "restart tools/list")
	require.Equal(t, 1, writes.written(t))

	fresh := listGatewayAs(t, caller, ti, slug, second.token, map[string]any{})
	require.Nil(t, fresh.Error)
	require.NotNil(t, fresh.Result)
	require.Nil(t, fresh.Result.NextCursor)
	require.Equal(t, 2, writes.written(t), "a complete listing and its member")

	person := urn.NewUserSubject(mockidp.MockUserID).String()
	tokens := inboundAttempts(t, *auth.ProjectID, gateway.ID)
	require.Len(t, tokens, 2)
	for _, row := range tokens {
		require.Equal(t, []string{"inbound_token", "code_issued", "", "", "authorization_code", person, client.ID.String()},
			[]string{row.Stage, row.Outcome, row.FailureClass, row.Reason, row.GrantType, row.Subject, row.ClientKey})
	}
	requireNoSecrets(t, tokens, client.ClientID, first.token, second.token, gatewayConsentVerifier)

	listings := map[string][]string{}
	for _, row := range connectionAttempts(t, *auth.ProjectID, gateway.ID) {
		require.Equal(t, []string{person, client.ID.String()}, []string{row.Subject, row.ClientKey}, "every listing fact names the same person and client")
		listings[row.Stage] = append(listings[row.Stage], row.Outcome+"/"+row.Reason)
	}
	require.Equal(t, []string{"failed/stale_cursor", "failed/stale_cursor", "complete/"}, listings["discovery_listing"])
	require.Equal(t, []string{"listed/"}, listings["discovery_member"])

	require.Equal(t, map[string]uint64{
		"inbound_token/code_issued":  2,
		"discovery_member/listed":    1,
		"discovery_listing/failed":   2,
		"discovery_listing/complete": 1,
	}, stageOutcomes(t, *auth.ProjectID, gateway.ID, person, client.ID.String()))
}

// A refresh is answered by whichever gateway on the issuer serves it, and is
// recorded there alone; the session keeps the gateway it was issued for. A
// replayed refresh is its own outcome, and neither is a sign-in.
func TestGatewayInboundRefreshIsRecordedWhereItIsServed(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	writes := watchConnectionAttempts(t, ti)
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slugA, slugB := "inbound-a-"+uuid.NewString(), "inbound-b-"+uuid.NewString()
	gatewayA := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slugA, issuer.ID)
	gatewayB := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slugB, issuer.ID)

	code := storeGatewayCode(t, ctx, ti, issuer.ID, client)
	issued := redeemTokens(t, postForm(t, ti, slugA, "token", codeGrantForm(client, code, gatewayConsentVerifier)))
	require.Equal(t, 1, writes.written(t))
	refreshed := redeemTokens(t, postRefreshGrant(t, ti, slugB, client.ClientID, issued.RefreshToken, ""))
	require.Equal(t, 1, writes.written(t))
	redeemTokens(t, postRefreshGrant(t, ti, slugB, client.ClientID, issued.RefreshToken, ""))
	require.Equal(t, 1, writes.written(t))

	person := urn.NewUserSubject(mockidp.MockUserID).String()
	onA := inboundAttempts(t, *auth.ProjectID, gatewayA.ID)
	require.Len(t, onA, 1, "the issuing gateway records only its own request")
	require.Equal(t, []string{"code_issued", "authorization_code", person, client.ID.String()}, []string{onA[0].Outcome, onA[0].GrantType, onA[0].Subject, onA[0].ClientKey})
	onB := inboundAttempts(t, *auth.ProjectID, gatewayB.ID)
	require.Len(t, onB, 2)
	for i, outcome := range []string{"refreshed", "refresh_replayed"} {
		require.Equal(t, []string{outcome, "refresh_token", person, client.ID.String()}, []string{onB[i].Outcome, onB[i].GrantType, onB[i].Subject, onB[i].ClientKey})
	}
	requireNoSecrets(t, append(onA, onB...), client.ClientID, issued.AccessToken, issued.RefreshToken, refreshed.RefreshToken, code)

	claims, err := usersessions.NewSigner("test-jwt-secret").Validate(refreshed.AccessToken, urn.NewUserSessionIssuer(issuer.ID).String())
	require.NoError(t, err)
	session, err := usersessions_repo.New(ti.conn).GetUserSessionByJTI(ctx, usersessions_repo.GetUserSessionByJTIParams{UserSessionIssuerID: issuer.ID, Jti: claims.ID})
	require.NoError(t, err)
	require.Equal(t, "meta_mcp_server:"+gatewayA.ID.String(), session.ResourceIdentity.String, "recording never moves the session")
}

// A refusal before the grant names its caller has no subject; one after the
// refresh token is bound to its client keeps the caller it names.
func TestGatewayInboundRefusalsNameOnlyValidatedCallers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	writes := watchConnectionAttempts(t, ti)
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "inbound-refusal-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)

	code := storeGatewayCode(t, ctx, ti, issuer.ID, client)
	w := postForm(t, ti, slug, "token", codeGrantForm(client, code, "not-the-verifier"))
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, 1, writes.written(t))

	refreshToken := "expired-refresh-" + uuid.NewString()
	sum := sha256.Sum256([]byte(refreshToken))
	past := pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true}
	_, err := usersessions_repo.New(ti.conn).CreateUserSession(ctx, usersessions_repo.CreateUserSessionParams{
		UserSessionClientID: uuid.NullUUID{UUID: client.ID, Valid: true}, SubjectUrn: urn.NewUserSubject(mockidp.MockUserID),
		AuthorizerUserID: pgtype.Text{}, DelegatedGrants: nil, DelegatedGrantsVersion: pgtype.Int4{}, Jti: uuid.NewString(),
		RefreshTokenHash: pgtype.Text{String: base64.RawURLEncoding.EncodeToString(sum[:]), Valid: true}, RefreshExpiresAt: past, ExpiresAt: past,
		ToolSelection: nil, Resource: pgtype.Text{}, ResourceIdentity: pgtype.Text{}, CredentialID: uuid.NullUUID{},
		UserSessionIssuerID: issuer.ID, SignInID: uuid.NullUUID{}, SignInUserID: pgtype.Text{},
	})
	require.NoError(t, err)
	w = postRefreshGrant(t, ti, slug, client.ClientID, refreshToken, "")
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, 1, writes.written(t))

	rows := inboundAttempts(t, *auth.ProjectID, gateway.ID)
	require.Len(t, rows, 2)
	require.Equal(t, []string{"failed", "configuration", "pkce_mismatch", "", client.ID.String()}, []string{rows[0].Outcome, rows[0].FailureClass, rows[0].Reason, rows[0].Subject, rows[0].ClientKey}, "the verifier never matched, so no caller is named")
	require.Equal(t, []string{"failed", "configuration", "refresh_token_expired", urn.NewUserSubject(mockidp.MockUserID).String(), "refresh_token"}, []string{rows[1].Outcome, rows[1].FailureClass, rows[1].Reason, rows[1].Subject, rows[1].GrantType}, "the refresh token was bound to this client, so its caller is named")
}

// A client an administrator blocked is policy, not a failure, and is named
// once it is resolved and its redirect validated; an unknown client is the
// request's configuration and names none. Authorize never names a caller:
// nobody has signed in yet.
func TestGatewayInboundAuthorizeOutcomes(t *testing.T) {
	t.Parallel()
	idpURL, err := url.Parse("https://idp.example.com/authorize?state=challenge123")
	require.NoError(t, err)
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{buildAuthURLResult: idpURL})
	writes := watchConnectionAttempts(t, ti)
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "inbound-authorize-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleAuthorize(w, authorizeRequest(t, slug, client.ClientID, client.RedirectUris[0])))
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())
	require.Equal(t, 1, writes.written(t))

	w = httptest.NewRecorder()
	require.NoError(t, ti.service.HandleAuthorize(w, authorizeRequest(t, slug, "no-such-client", client.RedirectUris[0])))
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, 1, writes.written(t))

	blocked := seedCIMDClient(t, ctx, ti, issuer.ID, claudeCodeCIMDClientID)
	blockTarget(t, ctx, ti, auth.ActiveOrganizationID, "claude-code")
	w = httptest.NewRecorder()
	require.NoError(t, ti.service.HandleAuthorize(w, authorizeRequest(t, slug, claudeCodeCIMDClientID, cimdLoopbackRedirect)))
	require.Equal(t, http.StatusBadRequest, w.Code, "the response is unchanged by recording")
	require.Equal(t, 1, writes.written(t))

	rows := inboundAttempts(t, *auth.ProjectID, gateway.ID)
	require.Len(t, rows, 3)
	got := make([][]string, 0, len(rows))
	for _, row := range rows {
		require.Equal(t, "inbound_authorize", row.Stage)
		require.Empty(t, row.Subject)
		require.Empty(t, row.GrantType)
		got = append(got, []string{row.Outcome, row.FailureClass, row.Reason, row.ClientKey})
	}
	require.Equal(t, [][]string{
		{"accepted", "", "", client.ID.String()},
		{"failed", "configuration", "unknown_client_id", "unverified_client"},
		{"policy_blocked", "configuration", "ai_tool_blocked", blocked.ID.String()},
	}, got)
	requireNoSecrets(t, rows, client.ClientID, claudeCodeCIMDClientID, cimdLoopbackRedirect)
}

// Only a configured gateway's requests are observed: a token exchange on an
// ordinary MCP server writes no attempt.
func TestInboundAttemptsOnlyForConfiguredGateways(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	auth, _ := contextvalues.GetAuthContext(ctx)
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	exchangeLinkedCode(t, ctx, ti, toolset, client, mockidp.MockUserID, nil)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	var count uint64
	require.NoError(t, chConn.QueryRow(t.Context(),
		`SELECT count() FROM telemetry_logs WHERE gram_project_id = ? AND event_urn = ?`,
		auth.ProjectID.String(), mcp.MCPConnectionAttemptEventURN).Scan(&count))
	require.Zero(t, count)
}

type gatewayTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func redeemTokens(t *testing.T, w *httptest.ResponseRecorder) gatewayTokens {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var tokens gatewayTokens
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &tokens))
	return tokens
}

// storeGatewayCode stores a plain authorization code for the mock person on
// the issuer, as consent would, redeemable with gatewayConsentVerifier.
func storeGatewayCode(t *testing.T, ctx context.Context, ti *testInstance, issuerID uuid.UUID, client usersessions_repo.UserSessionClient) string {
	t.Helper()
	challenge := sha256.Sum256([]byte(gatewayConsentVerifier))
	code := uuid.NewString()
	grants := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	require.NoError(t, grants.Store(ctx, mcp.UserSessionGrant{
		Code: code, UserSessionIssuerID: issuerID, UserSessionClientID: client.ID,
		ClientID: client.ClientID, RedirectURI: client.RedirectUris[0],
		CodeChallenge: base64.RawURLEncoding.EncodeToString(challenge[:]), CodeChallengeMethod: "S256",
		Subject: urn.NewUserSubject(mockidp.MockUserID), CreatedAt: time.Now(),
	}))
	return code
}

// An enterprise assertion exchanged at a gateway stored with the trusted
// issuer is recorded as its own grant, for the caller the verified assertion
// names; it is never counted as a code sign-in.
func TestGatewayInboundAssertionExchange(t *testing.T) {
	t.Parallel()
	ctx, ti, assertionSigner, jwksURL := newIDJAGTestService(t)
	writes := watchConnectionAttempts(t, ti)
	auth, _ := contextvalues.GetAuthContext(ctx)
	const upstreamIssuer = "https://identity.example.test"
	issuer := createTrustedIDJAGIssuer(t, ctx, ti, auth.ActiveOrganizationID, upstreamIssuer, jwksURL)
	slug := "inbound-idjag-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	member := seedHostedMetaMember(t, ctx, ti, gateway.ID, "private ID-JAG member", 1, mcpservers.VisibilityPrivate, "member_tool")
	client := createIDJAGClient(t, ctx, ti, issuer.ID)
	seedIDJAGDirectoryUser(t, ctx, ti, auth.ActiveOrganizationID)
	seedPrincipalMCPConnectGrant(t, ctx, ti, auth.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, mockidp.MockUserID), member.toolsetID)

	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "mcp")
	require.NoError(t, err)
	require.Equal(t, issuer.ID, endpoint.UserSessionIssuerID, "the gateway is stored with the trusted issuer")
	resource, err := endpoint.RootURL(ti.serverURL.String())
	require.NoError(t, err)
	assertion := assertionSigner.sign(t, upstreamIssuer, resource, resource, client.ClientID, mockidp.MockUserEmail, uuid.NewString())
	w := postIDJAGToken(t, ctx, ti, slug, client.ClientID, assertion)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, 1, writes.written(t))

	rows := inboundAttempts(t, *auth.ProjectID, gateway.ID)
	require.Len(t, rows, 1)
	require.Equal(t, []string{"inbound_token", "assertion_issued", oauthwire.GrantTypeJWTBearer, urn.NewUserSubject(mockidp.MockUserID).String(), client.ID.String()},
		[]string{rows[0].Stage, rows[0].Outcome, rows[0].GrantType, rows[0].Subject, rows[0].ClientKey})
	requireNoSecrets(t, rows, assertion, client.ClientID)
}

// failingWriter accepts headers but fails every body write, as a client that
// hung up mid-response does.
type failingWriter struct{ *httptest.ResponseRecorder }

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("client went away") }

// A token the handler could not write is not issued: it is recorded as a
// failed write for the caller it was minted for.
func TestGatewayInboundUnwrittenTokenIsNotIssued(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	writes := watchConnectionAttempts(t, ti)
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "inbound-unwritten-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)

	code := storeGatewayCode(t, ctx, ti, issuer.ID, client)
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/token", strings.NewReader(codeGrantForm(client, code, gatewayConsentVerifier).Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("mcpSlug", slug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, routeCtx))
	require.Error(t, ti.service.HandleToken(failingWriter{httptest.NewRecorder()}, req))
	require.Equal(t, 1, writes.written(t))

	rows := inboundAttempts(t, *auth.ProjectID, gateway.ID)
	require.Len(t, rows, 1)
	require.Equal(t, []string{"failed", "internal", "response_write_failed", "authorization_code", urn.NewUserSubject(mockidp.MockUserID).String()},
		[]string{rows[0].Outcome, rows[0].FailureClass, rows[0].Reason, rows[0].GrantType, rows[0].Subject})
}

// An issuer whose administrator turned client metadata documents off refuses
// a metadata client at authorize: blocked by policy, before the client is
// verified.
func TestGatewayInboundClientMetadataAdmissionIsPolicy(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	writes := watchConnectionAttempts(t, ti)
	auth, _ := contextvalues.GetAuthContext(ctx)
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	setIssuerAdmissionMode(t, ctx, ti, toolset, admission.ModeDisabled)
	slug := "inbound-cimd-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)

	const metadataClient = "https://client.example.test/oauth/metadata.json"
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleAuthorize(w, authorizeRequest(t, slug, metadataClient, cimdLoopbackRedirect)))
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Equal(t, 1, writes.written(t))

	rows := inboundAttempts(t, *auth.ProjectID, gateway.ID)
	require.Len(t, rows, 1)
	require.Equal(t, []string{"policy_blocked", "configuration", "cimd_denied_disabled", "unverified_client", ""},
		[]string{rows[0].Outcome, rows[0].FailureClass, rows[0].Reason, rows[0].ClientKey, rows[0].Subject})
	requireNoSecrets(t, rows, metadataClient, "client.example.test")
}
