package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// recordValidationAttempts gives the fixture's challenge an attempt and records its rows.
func recordValidationAttempts(t *testing.T, ctx context.Context, fx validationFixture) *mcpconnectionattemptstest.Recording {
	t.Helper()
	state, err := fx.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err)
	state.FlowID = uuid.NewString()
	require.NoError(t, fx.ti.authnChallengeCache.Store(ctx, state))
	return recordConnections(t, fx.ti)
}

// postValidatePage posts Verify through the browser error handler and returns the page, whitespace collapsed.
func postValidatePage(t *testing.T, fx validationFixture, clientID uuid.UUID) (int, string) {
	t.Helper()
	form := url.Values{"state": {fx.stateID}, "csrf_token": {"csrf-token"}, "action": {"validate"}, "client_id": {clientID.String()}}
	if fx.server != "" {
		form.Set("server", fx.server)
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.endpoint.Slug+"/connect/remote-session", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	oops.BrowserErrHandle(testenv.NewLogger(t), func(w http.ResponseWriter, r *http.Request) error {
		return fx.ti.service.ServeConsentAction(w, r, fx.endpoint)
	}).ServeHTTP(w, req)
	return w.Code, strings.ReplaceAll(strings.Join(strings.Fields(w.Body.String()), " "), "&#39;", "'")
}

// checkRows is what the checks recorded, without the consent page's own rows.
func checkRows(rec *mcpconnectionattemptstest.Recording) []mcpconnectionattempts.Row {
	var rows []mcpconnectionattempts.Row
	for _, row := range rec.Rows() {
		if row.Step == mcpconnectionattempts.StepProviderSignIn.String() {
			rows = append(rows, row)
		}
	}
	return rows
}

// A token lookup that breaks on our side records internal_error, and the page says so without suggesting a retry.
func TestServeConsentAction_ValidateTokenLookupFaultRecordsAndSaysNoRetry(t *testing.T) {
	t.Parallel()

	ctx, fx := seedMetaValidationFixture(t, "check-fault")
	rec := recordValidationAttempts(t, ctx, fx)
	projectID, orgID := consentTestTenant(t, ctx)
	// The member is associated with the card's client and a second client of
	// the same issuer, which breaks the one-client-per-issuer invariant its
	// credential lookup asserts.
	q := remotesessions_repo.New(fx.ti.conn)
	twin, err := q.CreateRemoteSessionClient(ctx, remotesessions_repo.CreateRemoteSessionClientParams{
		ProjectID:             conv.ToNullUUID(projectID),
		OrganizationID:        conv.ToPGTextEmpty(orgID),
		RemoteSessionIssuerID: clientRemoteIssuerID(t, ctx, fx.ti.conn, projectID, orgID, fx.clientID),
		ClientID:              "check-fault-twin",
		ClientIDIssuedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true, InfinityModifier: pgtype.Finite},
		CallbackBaseUrl:       conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessions_repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: twin.ID,
		UserSessionIssuerID:   fx.endpoint.UserSessionIssuerID,
	}))
	member, err := mcpserversrepo.New(fx.ti.conn).GetMCPServerBySlug(ctx, mcpserversrepo.GetMCPServerBySlugParams{Slug: conv.ToPGText(fx.server), ProjectID: projectID})
	require.NoError(t, err)
	for _, clientID := range []uuid.UUID{fx.clientID, twin.ID} {
		_, err := upstreamclientsrepo.New(fx.ti.conn).AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{OrganizationID: orgID, ProjectID: projectID, McpServerID: member.ID, RemoteSessionClientID: clientID})
		require.NoError(t, err)
	}

	status, page := postValidatePage(t, fx, fx.clientID)
	require.Equal(t, http.StatusInternalServerError, status)
	require.Contains(t, page, "Couldn't check the connection")
	require.Contains(t, page, "Checking the server's connection")
	require.Contains(t, page, "Share the technical details below with Speakeasy support.")
	require.NotContains(t, page, "again shortly")
	require.NotContains(t, page, "resolve upstream tokens")
	require.Contains(t, page, "Error ID")

	require.Len(t, checkRows(rec), 1)
	row := rec.RequireStep(mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeInternalError)
	require.Equal(t, string(mcpconnectionattempts.WhoSpeakeasy), row.WhoFixes)
	require.Equal(t, fx.clientID.String(), row.RemoteSessionClientID)
	require.Empty(t, fx.member.drain(), "no probe reached the member")
}

// A grant no member is handed records credential_misconfigured, and the page sends the person to an admin.
func TestServeConsentAction_ValidateUnroutableRecordsAndAsksAdmin(t *testing.T) {
	t.Parallel()

	ctx, fx := seedMetaValidationFixture(t, "check-unroutable")
	rec := recordValidationAttempts(t, ctx, fx)
	projectID, orgID := consentTestTenant(t, ctx)
	elsewhere := createConsentRemoteClient(t, ctx, fx.ti.conn, projectID, orgID, "check-unroutable-elsewhere", "", []uuid.UUID{fx.endpoint.UserSessionIssuerID})
	elsewhereIssuer := clientRemoteIssuerID(t, ctx, fx.ti.conn, projectID, orgID, elsewhere)
	createMetaMember(t, ctx, fx.ti.conn, projectID, fx.endpoint.MetaMcpServerID.UUID, "check-unroutable-claimant", "https://claimed.example.com/mcp", conv.ToNullUUID(elsewhereIssuer), 1)
	insertQualifiedRemoteSessionToken(t, ctx, fx.ti, fx.endpoint.UserSessionIssuerID, elsewhere, fx.subject, "token-elsewhere", "https://elsewhere.example.com/mcp")

	status, page := postValidatePage(t, fx, elsewhere)
	require.Equal(t, http.StatusBadRequest, status)
	require.Contains(t, page, "Checking the server's connection")
	require.Contains(t, page, "Ask your IT administrators")
	require.NotContains(t, page, "again shortly")

	require.Len(t, checkRows(rec), 1)
	row := rec.RequireStep(mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeCredentialMisconfigured)
	require.Equal(t, string(mcpconnectionattempts.WhoAdmin), row.WhoFixes)
}

// An expired grant shows on its card and records invalid_remote_session for the person to fix.
func TestServeConsentAction_ValidateExpiredGrantRecordsReconnect(t *testing.T) {
	t.Parallel()

	ctx, fx := seedMetaValidationFixture(t, "check-expired")
	rec := recordValidationAttempts(t, ctx, fx)
	projectID, _ := consentTestTenant(t, ctx)
	sess := storedSession(t, ctx, fx)
	require.NoError(t, remotesessions_repo.New(fx.ti.conn).SetRemoteSessionAccessExpiresAt(ctx, remotesessions_repo.SetRemoteSessionAccessExpiresAtParams{
		AccessExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true, InfinityModifier: pgtype.Finite},
		ID:              sess.ID,
		ProjectID:       conv.ToNullUUID(projectID),
	}))

	requireVerifyNotice(t, fx, fx.clientID, verifyExpiredCard)
	require.Len(t, checkRows(rec), 1)
	row := rec.RequireStep(mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeInvalidRemoteSession)
	require.Equal(t, string(mcpconnectionattempts.WhoPerson), row.WhoFixes)
}

// A Verify whose refresh fails writes the refresh's row on the attempt, and no second row for the check:
// an outage stops the check, and a refused grant collapses to a card with no token.
func TestServeConsentAction_ValidateRefreshFailureWritesOneRow(t *testing.T) {
	t.Parallel()

	for name, answer := range map[string]http.HandlerFunc{
		"outage": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		},
		"refused grant": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			requireOneRefreshRow(t, answer)
		})
	}
}

func requireOneRefreshRow(t *testing.T, answer http.HandlerFunc) {
	t.Helper()

	rec := mcpconnectionattemptstest.New(t)
	reader, provider := newValidationMeterProvider()
	ctx, ti := buildTestMCPService(t, testenv.NewLogger(t), provider, &mockIdentityResolver{hasAccessOK: true}, mcp.TunnelPublicConfig{}, nil, nil, false, mcp.MetaRuntimeConfig{MemberCallTimeout: 0, ValidationTimeout: validationProbeTimeout, AutoVerifyWait: 0, RecheckInterval: recheckTestInterval}, testenv.NewTracerProvider(t), nil, nil, "", false, rec.Recorder)
	ti.service.SetConnectionRecorder(rec.Recorder)
	fx := seedMetaValidationFixtureOn(t, ctx, ti, reader, "check-refresh")
	state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err)
	state.FlowID = uuid.NewString()
	require.NoError(t, ti.authnChallengeCache.Store(ctx, state))

	projectID, orgID := consentTestTenant(t, ctx)
	upstream := httptest.NewServer(answer)
	t.Cleanup(upstream.Close)
	rows, err := testrepo.New(ti.conn).ForceRemoteSessionIssuerTokenEndpointFixture(ctx, testrepo.ForceRemoteSessionIssuerTokenEndpointFixtureParams{
		TokenEndpoint: conv.ToPGText(upstream.URL), RemoteSessionClientID: fx.clientID, ProjectID: projectID, OrganizationID: orgID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
	sess := storedSession(t, ctx, fx)
	refresh, err := ti.enc.Encrypt([]byte("live-refresh"))
	require.NoError(t, err)
	access, err := ti.enc.Encrypt([]byte("expired"))
	require.NoError(t, err)
	_, err = remotesessions_repo.New(ti.conn).UpsertRemoteSession(ctx, remotesessions_repo.UpsertRemoteSessionParams{
		SubjectUrn: sess.SubjectUrn, UserSessionIssuerID: sess.UserSessionIssuerID, RemoteSessionClientID: sess.RemoteSessionClientID,
		AccessTokenEncrypted: access, AccessExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true, InfinityModifier: pgtype.Finite},
		RefreshTokenEncrypted: conv.ToPGText(refresh), Scopes: []string{}, Resource: sess.Resource,
	})
	require.NoError(t, err)

	w, err := postValidate(t, fx, fx.clientID)
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, w.Code)
	var refreshes int
	for _, row := range rec.Rows() {
		if row.Step == mcpconnectionattempts.StepProviderRefresh.String() {
			refreshes++
		}
	}
	require.Equal(t, 1, refreshes, "the refresh wrote its row: %+v", rec.Rows())
	require.Empty(t, checkRows(rec), "the check adds no second row")
}

// A first-party sign-in service's error is classified by its code: the page and the row agree, and neither names an MCP client.
func TestHandleIDPCallback_FirstPartyProviderErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		idpErr string
		status int
		says   string
		what   mcpconnectionattempts.Code
	}{
		{"server_error", http.StatusBadRequest, "Your sign-in service couldn't sign you in. This isn't something you did.", mcpconnectionattempts.CodeSignInProviderError},
		{"temporarily_unavailable", http.StatusServiceUnavailable, "Your sign-in service isn't available right now.", mcpconnectionattempts.CodeIDPTransientFailure},
		{"invalid_request", http.StatusInternalServerError, "This is a problem on our side", mcpconnectionattempts.CodeInternalError},
		{"request_not_supported", http.StatusInternalServerError, "Share the technical details below with Speakeasy support.", mcpconnectionattempts.CodeInternalError},
		{"login_required", http.StatusBadRequest, "Your sign-in service needs you to sign in again", mcpconnectionattempts.CodeReauthenticationRequired},
		{"made_up_by_caller", http.StatusBadRequest, "sign-in provider returned an error: unregistered", mcpconnectionattempts.CodeSignInProviderError},
	} {
		t.Run(tc.idpErr, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPServiceWithIdentityResolver(t, memberMock())
			rec := recordConnections(t, ti)
			challengeID := seedIDPCallbackChallenge(t, ctx, ti)
			state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+challengeID)
			require.NoError(t, err)
			state.FlowID = uuid.NewString()
			state.FirstParty = true
			require.NoError(t, ti.authnChallengeCache.Store(ctx, state))

			req := httptest.NewRequest(http.MethodGet, "/mcp/idp_callback?"+url.Values{"state": {challengeID}, "error": {tc.idpErr}}.Encode(), nil).WithContext(ctx)
			w := httptest.NewRecorder()
			oops.BrowserErrHandle(testenv.NewLogger(t), ti.service.HandleIDPCallback).ServeHTTP(w, req)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			page := strings.ReplaceAll(strings.Join(strings.Fields(w.Body.String()), " "), "&#39;", "'")
			require.Contains(t, page, tc.says)
			require.NotContains(t, strings.ToLower(page), "your mcp client")
			row := rec.RequireEnded(mcpconnectionattempts.StepInboundSignIn, tc.what)
			require.Equal(t, oautherr.RegisteredCodeLabel(tc.idpErr), row.SubReason)
		})
	}
}
