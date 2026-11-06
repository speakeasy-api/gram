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
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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

	ctx, fx := seedStandaloneValidationFixture(t, "check-fault")
	rec := recordValidationAttempts(t, ctx, fx)
	projectID, orgID := consentTestTenant(t, ctx)
	// A second client on the card's own issuer breaks the one-client-per-issuer invariant token lookup asserts.
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

	status, page := postValidatePage(t, fx, fx.clientID)
	require.Equal(t, http.StatusInternalServerError, status)
	require.Contains(t, page, "Couldn't check the connection")
	require.Contains(t, page, "Checking the server's connection")
	require.Contains(t, page, "We're looking into it.")
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
