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

	agents_repo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/signins"
	signinsrepo "github.com/speakeasy-api/gram/server/internal/signins/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestInboundRefresh_Refreshed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	toolset, _, client, refreshToken, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(time.Hour))

	refreshed := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, refreshed.err)
	require.Equal(t, http.StatusOK, refreshed.code, refreshed.body)

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultRefreshed)
	require.Equal(t, "refresh_token", row.GrantType)
	require.Equal(t, urn.NewUserSubject("refresh-replay-user").String(), row.SubjectURN)
	require.Equal(t, client.ID.String(), row.UserSessionClientID)
}

func TestInboundRefresh_WriteFailureLeavesNoVerdict(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	toolset, _, client, refreshToken, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(time.Hour))

	req := newRefreshHTTPRequest(ctx, toolset.McpSlug.String, client.ClientID, refreshToken)
	writeErr := errors.New("client disconnected during token response")
	w := &refreshFailingResponseWriter{ResponseRecorder: httptest.NewRecorder(), err: writeErr}

	require.ErrorIs(t, ti.service.HandleToken(w, req), writeErr)
	require.Equal(t, http.StatusOK, w.Code)
	rec.RequireNothing()

	// Rotation is committed despite the failed delivery; retry recovers its successor.
	replayed := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, replayed.err)
	require.Equal(t, http.StatusOK, replayed.code, replayed.body)
	rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultRefreshReplayed)
}

// requireRefusalRecordedOnDelivery asserts an undelivered refusal records nothing and a delivered retry records want.
func requireRefusalRecordedOnDelivery(t *testing.T, ctx context.Context, ti *testInstance, mcpSlug, clientID, refreshToken string, want mcpconnectionattempts.Code) {
	t.Helper()
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)

	writeErr := errors.New("client disconnected during error response")
	w := &refreshFailingResponseWriter{ResponseRecorder: httptest.NewRecorder(), err: writeErr}
	require.ErrorIs(t, ti.service.HandleToken(w, newRefreshHTTPRequest(ctx, mcpSlug, clientID, refreshToken)), writeErr)
	require.Equal(t, http.StatusBadRequest, w.Code)
	rec.RequireNothing()

	refused := performRefreshRequest(ctx, ti, mcpSlug, clientID, refreshToken)
	require.NoError(t, refused.err)
	require.Equal(t, http.StatusBadRequest, refused.code, refused.body)
	rec.RequireEnded(mcpconnectionattempts.StepInboundToken, want)
}

func TestInboundRefresh_ExpiredWriteFailureLeavesNoVerdict(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, _, client, refreshToken, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(-time.Hour))
	requireRefusalRecordedOnDelivery(t, ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken, mcpconnectionattempts.CodeRefreshTokenExpired)
}

func TestInboundRefresh_UnknownWriteFailureLeavesNoVerdict(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, _, client, _, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(time.Hour))
	requireRefusalRecordedOnDelivery(t, ctx, ti, toolset.McpSlug.String, client.ClientID, "refresh-"+uuid.NewString(), mcpconnectionattempts.CodeRefreshTokenUnknownOrAlreadyUsed)
}

func TestInboundRefresh_UpstreamWriteFailureLeavesNoVerdict(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, issuer, client, refreshToken, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(time.Hour))
	attachTestRemoteSessionClient(t, ctx, ti, requireProjectAuthContext(t, ctx), issuer.ID)
	requireRefusalRecordedOnDelivery(t, ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken, mcpconnectionattempts.CodeUpstreamReconnectRequired)
}

func TestInboundRefresh_ExpiredReplayWriteFailureLeavesNoVerdict(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, _, client, refreshToken, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(-time.Hour))
	refused := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, refused.err)
	require.Equal(t, http.StatusBadRequest, refused.code, refused.body)
	requireRefusalRecordedOnDelivery(t, ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken, mcpconnectionattempts.CodeRefreshTokenExpired)
}

func TestInboundRefresh_UnknownReplayWriteFailureLeavesNoVerdict(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, _, client, _, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(time.Hour))
	refreshToken := "refresh-" + uuid.NewString()
	refused := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, refused.err)
	require.Equal(t, http.StatusBadRequest, refused.code, refused.body)
	requireRefusalRecordedOnDelivery(t, ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken, mcpconnectionattempts.CodeRefreshTokenUnknownOrAlreadyUsed)
}

func newRefreshHTTPRequest(ctx context.Context, mcpSlug, clientID, refreshToken string) *http.Request {
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}, "client_id": {clientID}}
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug+"/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("mcpSlug", mcpSlug)
	return req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, routeCtx))
}

type refreshFailingResponseWriter struct {
	*httptest.ResponseRecorder
	err error
}

func (w *refreshFailingResponseWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func TestInboundRefresh_Expired(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	toolset, _, client, refreshToken, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(-time.Hour))

	refused := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, refused.err)
	require.Equal(t, http.StatusBadRequest, refused.code, refused.body)

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeRefreshTokenExpired)
	require.Equal(t, string(mcpconnectionattempts.WhoPerson), row.WhoFixes)
	require.Equal(t, string(mcpconnectionattempts.OutcomeFailed), row.Outcome, "expiry is not a policy")
	require.Equal(t, "failed", row.StepResult)
}

// Another app's unrotated token burns the owner's session, so the person reconnects.
func TestInboundRefresh_OtherAppBurnsTheSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	toolset, issuer, _, refreshToken, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(time.Hour))
	other := seedOtherRefreshClient(t, ctx, ti, issuer.ID)

	refused := performRefreshRequest(ctx, ti, toolset.McpSlug.String, other.ClientID, refreshToken)
	require.NoError(t, refused.err)
	require.Equal(t, http.StatusBadRequest, refused.code, refused.body)

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeRefreshTokenUsedByOtherApp)
	require.Equal(t, string(mcpconnectionattempts.WhoPerson), row.WhoFixes)
	require.Equal(t, "refresh_token_client_mismatch", row.SubReason)
}

// Another app replaying a rotated token leaves the owner's session alone, so the app is wrong.
func TestInboundRefresh_OtherAppReplayBlamesTheApp(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	toolset, issuer, client, refreshToken, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(time.Hour))
	other := seedOtherRefreshClient(t, ctx, ti, issuer.ID)

	refreshed := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, refreshed.err)
	require.Equal(t, http.StatusOK, refreshed.code, refreshed.body)

	refused := performRefreshRequest(ctx, ti, toolset.McpSlug.String, other.ClientID, refreshToken)
	require.NoError(t, refused.err)
	require.Equal(t, http.StatusBadRequest, refused.code, refused.body)

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeRefreshTokenClientMismatch)
	require.Equal(t, string(mcpconnectionattempts.WhoApp), row.WhoFixes)
	require.Equal(t, "refresh_token_client_mismatch", row.SubReason)
}

// A stored tool selection that does not parse is our damage, but the person reconnects.
func TestInboundRefresh_MalformedToolSelection(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	toolset, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	refreshToken := "refresh-" + uuid.NewString()
	refreshHash := sha256.Sum256([]byte(refreshToken))
	_, err := usersessions_repo.New(ti.conn).CreateUserSession(ctx, usersessions_repo.CreateUserSessionParams{
		UserSessionIssuerID: issuer.ID,
		UserSessionClientID: uuid.NullUUID{UUID: client.ID, Valid: true},
		SubjectUrn:          urn.NewUserSubject("refresh-replay-user"),
		Jti:                 uuid.NewString(),
		RefreshTokenHash:    conv.ToPGText(base64.RawURLEncoding.EncodeToString(refreshHash[:])),
		RefreshExpiresAt:    pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		ExpiresAt:           pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true},
		ToolSelection:       []byte(`{"version":99}`),
	})
	require.NoError(t, err)

	refused := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, refused.err)
	require.Equal(t, http.StatusBadRequest, refused.code, refused.body)

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeStoredSessionInvalid)
	require.Equal(t, string(mcpconnectionattempts.WhoPerson), row.WhoFixes, "the person reconnects; sub_reason keeps our cause")
	require.Equal(t, "tool_selection_malformed", row.SubReason)
}

// An admin revocation is the admin's decision, so it is blocked, not a failure the person fixes.
func TestInboundRefresh_AdminRevokedSignInIsBlocked(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	toolset, refreshToken, client := seedRevokedSignInGrant(t, ctx, ti, signins.RevokeReasonAdmin)

	refused := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, refused.err)
	require.Equal(t, http.StatusBadRequest, refused.code, refused.body)

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeSignInRevokedByAdmin)
	require.Equal(t, string(mcpconnectionattempts.WhoAdmin), row.WhoFixes)
	require.Equal(t, string(mcpconnectionattempts.OutcomeBlocked), row.Outcome)
	require.Equal(t, "sign_in_revoked_by_admin", row.SubReason)
}

// Signing out everywhere is the person's own choice, so they reconnect.
func TestInboundRefresh_SignedOutEverywhereIsAFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	toolset, refreshToken, client := seedRevokedSignInGrant(t, ctx, ti, signins.RevokeReasonSignOutEverywhere)

	refused := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, refused.err)
	require.Equal(t, http.StatusBadRequest, refused.code, refused.body)

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeSignInRevoked)
	require.Equal(t, string(mcpconnectionattempts.WhoPerson), row.WhoFixes)
	require.Equal(t, string(mcpconnectionattempts.OutcomeFailed), row.Outcome)
}

// A replay whose successor session has simply expired is expiry, not damaged state.
func TestInboundRefresh_ReplayOfAnExpiredSuccessorIsExpiry(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	fx, _, refreshToken, _ := seedAgentRefreshSession(t, ctx, ti)

	winner := performRefreshRequest(ctx, ti, fx.toolset.McpSlug.String, fx.client.ClientID, refreshToken)
	require.NoError(t, winner.err)
	require.Equal(t, http.StatusOK, winner.code, winner.body)
	var response tokenResponseFixture
	require.NoError(t, json.Unmarshal([]byte(winner.body), &response))
	claims, err := sessiontokens.NewSigner("test-jwt-secret").Validate(response.AccessToken, urn.NewToolset(fx.toolset.ID).String())
	require.NoError(t, err)
	successor, err := usersessions_repo.New(ti.conn).GetUserSessionByJTI(ctx, usersessions_repo.GetUserSessionByJTIParams{
		UserSessionIssuerID: fx.target.UserSessionIssuerID,
		Jti:                 claims.ID,
	})
	require.NoError(t, err)
	expired, err := testrepo.New(ti.conn).ExpireUserSessionFixture(ctx, testrepo.ExpireUserSessionFixtureParams{
		ID:                  successor.ID,
		OrganizationID:      fx.orgID,
		ProjectID:           fx.target.ProjectID,
		UserSessionIssuerID: fx.target.UserSessionIssuerID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, expired)

	replay := performRefreshRequest(ctx, ti, fx.toolset.McpSlug.String, fx.client.ClientID, refreshToken)
	require.NoError(t, replay.err)
	require.Equal(t, http.StatusBadRequest, replay.code, replay.body)
	require.Contains(t, replay.body, "refreshed session is no longer active")

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeRefreshTokenExpired)
	require.Equal(t, string(mcpconnectionattempts.WhoPerson), row.WhoFixes)
	require.Equal(t, "refresh_token_expired", row.SubReason)
}

// seedRevokedSignInGrant links a grant to a sign-in, then revokes the sign-in but not the grant, as a racing refresh sees it.
func seedRevokedSignInGrant(t *testing.T, ctx context.Context, ti *testInstance, reason signins.RevokeReason) (hostedToolset, string, usersessions_repo.UserSessionClient) {
	t.Helper()
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	signins.EnableForTests(ti.features, toolset.OrganizationID)
	userID := "revoked-user-" + uuid.NewString()
	signIn, err := signins.Record(ctx, ti.conn, signins.RecordParams{OrganizationID: toolset.OrganizationID, UserID: userID, WorkOSSessionID: "session_" + uuid.NewString(), AuthenticationMethod: signins.MethodSSO, AuthenticatedAt: time.Now()})
	require.NoError(t, err)
	tokens := exchangeLinkedCode(t, ctx, ti, toolset, client, userID, &signIn.ID.UUID)
	_, err = signinsrepo.New(ti.conn).RevokeUserSignIns(ctx, signinsrepo.RevokeUserSignInsParams{
		RevokeReason:    conv.ToPGText(string(reason)),
		OrganizationIds: []string{toolset.OrganizationID},
		UserID:          userID,
	})
	require.NoError(t, err)
	return toolset, tokens.RefreshToken, client
}

func seedOtherRefreshClient(t *testing.T, ctx context.Context, ti *testInstance, issuerID uuid.UUID) usersessions_repo.UserSessionClient {
	t.Helper()
	other, err := usersessions_repo.New(ti.conn).CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
		UserSessionIssuerID:     issuerID,
		ClientID:                "other-client-" + uuid.NewString(),
		ClientName:              "other client",
		RedirectUris:            []string{"http://localhost:3001/callback"},
		TokenEndpointAuthMethod: "none",
	})
	require.NoError(t, err)
	return other
}

// A client that authenticated and sent a spent or unknown token gets a row; the limiter bounds it.

func TestInboundRefresh_UnknownTokenRecords(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	toolset, _, client, _, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(time.Hour))

	req := newRefreshHTTPRequest(ctx, toolset.McpSlug.String, client.ClientID, "refresh-"+uuid.NewString())
	req.Header.Set("User-Agent", "claude-code/2.1.0")
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleToken(w, req))
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	require.JSONEq(t, `{"error":"invalid_grant","error_description":"refresh_token is unknown or already used"}`, w.Body.String())

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeRefreshTokenUnknownOrAlreadyUsed)
	require.Empty(t, row.SubjectURN, "an unknown token names nobody")
	require.Equal(t, client.ID.String(), row.UserSessionClientID)
	require.Equal(t, mcpconnectionattempts.FamilyClaudeCode, row.ClientFamily)
	require.Equal(t, "2.1.0", row.ClientVersion)
	require.Equal(t, string(mcpconnectionattempts.WhoPerson), row.WhoFixes)
}

func TestInboundRefresh_ReplayIsNotAFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	toolset, _, client, refreshToken, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(time.Hour))

	for range 2 {
		refreshed := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
		require.NoError(t, refreshed.err)
		require.Equal(t, http.StatusOK, refreshed.code, refreshed.body)
	}
	rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultRefreshed)
	rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultRefreshReplayed)
}

func TestInboundRefresh_UpstreamReconnect(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	toolset, issuer, client, refreshToken, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(time.Hour))
	attachTestRemoteSessionClient(t, ctx, ti, authCtx, issuer.ID)

	refused := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, refused.err)
	require.Equal(t, http.StatusBadRequest, refused.code, refused.body)

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeUpstreamReconnectRequired)
	require.Equal(t, string(mcpconnectionattempts.WhoPerson), row.WhoFixes)
}

// A malformed request has no token to judge, and leaves without a no_verdict row.

func TestInboundRefresh_NoTokenLeavesNoRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	toolset, _, client, _, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(time.Hour))

	refused := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, "")
	require.NoError(t, refused.err)
	require.Equal(t, http.StatusBadRequest, refused.code, refused.body)
	rec.RequireNothing()
}

func TestInboundRefresh_SuspendedAgentIsNotAdmitted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	fx, agent, refreshToken, _ := seedAgentRefreshSession(t, ctx, ti)
	_, err := agents_repo.New(ti.conn).SuspendAgent(ctx, agents_repo.SuspendAgentParams{OrganizationID: fx.orgID, ID: agent.ID})
	require.NoError(t, err)

	refused := performRefreshRequest(ctx, ti, fx.toolset.McpSlug.String, fx.client.ClientID, refreshToken)
	require.NoError(t, refused.err)
	require.Equal(t, http.StatusBadRequest, refused.code, refused.body)
	require.Contains(t, refused.body, "agent authorization is no longer valid")

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeSubjectNotAdmitted)
	require.Equal(t, string(mcpconnectionattempts.WhoAdmin), row.WhoFixes)
}

func TestInboundRefresh_SuspendedAgentReplayIsNotAdmitted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	fx, agent, refreshToken, _ := seedAgentRefreshSession(t, ctx, ti)

	refreshed := performRefreshRequest(ctx, ti, fx.toolset.McpSlug.String, fx.client.ClientID, refreshToken)
	require.NoError(t, refreshed.err)
	require.Equal(t, http.StatusOK, refreshed.code, refreshed.body)

	_, err := agents_repo.New(ti.conn).SuspendAgent(ctx, agents_repo.SuspendAgentParams{OrganizationID: fx.orgID, ID: agent.ID})
	require.NoError(t, err)

	refused := performRefreshRequest(ctx, ti, fx.toolset.McpSlug.String, fx.client.ClientID, refreshToken)
	require.NoError(t, refused.err)
	require.Equal(t, http.StatusBadRequest, refused.code, refused.body)
	require.Contains(t, refused.body, "agent authorization is no longer valid")

	rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeSubjectNotAdmitted)
}

func TestInboundRefresh_ExpiredStaysExpiredAfterReplayGrace(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, issuer, client, refreshToken, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(-time.Hour))
	first := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, first.err)
	require.JSONEq(t, `{"error":"invalid_grant","error_description":"refresh_token has expired"}`, first.body)
	expireRefreshReplay(t, ctx, ti, issuer.ID, refreshToken)

	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	again := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, again.err)
	require.Equal(t, http.StatusBadRequest, again.code)
	require.JSONEq(t, `{"error":"invalid_grant","error_description":"refresh_token has expired"}`, again.body)

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeRefreshTokenExpired)
	require.Equal(t, urn.NewUserSubject("refresh-replay-user").String(), row.SubjectURN)
}

// pastSessionDeadlines moves the issuer's session timestamps two hours back, so
// a row retired before its deadline is read after that deadline has passed.
func pastSessionDeadlines(t *testing.T, ctx context.Context, ti *testInstance, issuerID uuid.UUID) {
	t.Helper()
	_, err := ti.conn.Exec(ctx, "UPDATE user_sessions SET deleted_at = deleted_at - interval '2 hours', refresh_expires_at = refresh_expires_at - interval '2 hours' WHERE user_session_issuer_id = $1", issuerID) //nolint:glint // notestingrawsql: deliberate clock shift to read a retired row after its deadline; not expressible as an SQLc query
	require.NoError(t, err)
}

func TestInboundRefresh_RotatedTokenAfterDeadlineIsUnknown(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, issuer, client, refreshToken, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(time.Hour))
	first := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, first.err)
	require.Equal(t, http.StatusOK, first.code, first.body)
	expireRefreshReplay(t, ctx, ti, issuer.ID, refreshToken)
	pastSessionDeadlines(t, ctx, ti, issuer.ID)

	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	reused := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, reused.err)
	require.JSONEq(t, `{"error":"invalid_grant","error_description":"refresh_token is unknown or already used"}`, reused.body)
	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeRefreshTokenUnknownOrAlreadyUsed)
	require.Equal(t, urn.NewUserSubject("refresh-replay-user").String(), row.SubjectURN, "the row names the spent token's owner")
}

func TestInboundRefresh_RevokedTokenAfterDeadlineIsUnknown(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, issuer, client, refreshToken, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(time.Hour))
	revoked := postForm(t, ti, toolset.McpSlug.String, "revoke", url.Values{
		"token":     {refreshToken},
		"client_id": {client.ClientID},
	})
	require.Equal(t, http.StatusOK, revoked.Code, revoked.Body.String())
	pastSessionDeadlines(t, ctx, ti, issuer.ID)

	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	reused := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, reused.err)
	require.JSONEq(t, `{"error":"invalid_grant","error_description":"refresh_token is unknown or already used"}`, reused.body)
	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeRefreshTokenUnknownOrAlreadyUsed)
	require.Equal(t, urn.NewUserSubject("refresh-replay-user").String(), row.SubjectURN, "the row names the spent token's owner")
}

func TestInboundRefresh_ExpiredTokenFromAnotherClientIsUnknown(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, issuer, client, refreshToken, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(-time.Hour))
	first := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, first.err)
	require.Equal(t, http.StatusBadRequest, first.code, first.body)
	expireRefreshReplay(t, ctx, ti, issuer.ID, refreshToken)

	other, err := usersessions_repo.New(ti.conn).CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
		UserSessionIssuerID:     issuer.ID,
		ClientID:                "test-client-" + uuid.New().String()[:8],
		ClientName:              "other test client",
		RedirectUris:            []string{"http://localhost:3000/callback"},
		TokenEndpointAuthMethod: "none",
	})
	require.NoError(t, err)

	refused := performRefreshRequest(ctx, ti, toolset.McpSlug.String, other.ClientID, refreshToken)
	require.NoError(t, refused.err)
	require.JSONEq(t, `{"error":"invalid_grant","error_description":"refresh_token is unknown or already used"}`, refused.body)
}

// End-to-end scoping check: the other issuer's client also fails the client
// predicate, so this cannot isolate the issuer predicate on its own.
func TestInboundRefresh_ExpiredTokenIsScopedToItsIssuerEndToEnd(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, _, client, refreshToken, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(-time.Hour))
	first := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, first.err)
	require.Equal(t, http.StatusBadRequest, first.code, first.body)

	otherToolset, _, otherClient := seedPrivateToolsetWithIssuer(t, ctx, ti)
	refused := performRefreshRequest(ctx, ti, otherToolset.McpSlug.String, otherClient.ClientID, refreshToken)
	require.NoError(t, refused.err)
	require.JSONEq(t, `{"error":"invalid_grant","error_description":"refresh_token is unknown or already used"}`, refused.body)
}

func TestInboundRefresh_ExpiredReplayInsideGraceKeepsSubject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, _, client, refreshToken, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(-time.Hour))
	first := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, first.err)
	require.Equal(t, http.StatusBadRequest, first.code, first.body)

	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	replayed := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, replayed.err)
	require.JSONEq(t, `{"error":"invalid_grant","error_description":"refresh_token has expired"}`, replayed.body)

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeRefreshTokenExpired)
	require.Equal(t, urn.NewUserSubject("refresh-replay-user").String(), row.SubjectURN)
}

func TestInboundRefresh_ExpiredReplayInsideGraceIsUnknownToAnotherClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, issuer, client, refreshToken, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(-time.Hour))
	first := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, first.err)
	require.Equal(t, http.StatusBadRequest, first.code, first.body)

	other, err := usersessions_repo.New(ti.conn).CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
		UserSessionIssuerID:     issuer.ID,
		ClientID:                "test-client-" + uuid.New().String()[:8],
		ClientName:              "other test client",
		RedirectUris:            []string{"http://localhost:3000/callback"},
		TokenEndpointAuthMethod: "none",
	})
	require.NoError(t, err)

	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	refused := performRefreshRequest(ctx, ti, toolset.McpSlug.String, other.ClientID, refreshToken)
	require.NoError(t, refused.err)
	require.JSONEq(t, `{"error":"invalid_grant","error_description":"refresh_token is unknown or already used"}`, refused.body)

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeRefreshTokenUnknownOrAlreadyUsed)
	require.Empty(t, row.SubjectURN)
	require.Equal(t, other.ID.String(), row.UserSessionClientID)
}

// A replayed refusal names the owner from the cache, with no query.
func TestInboundRefresh_UsedTokenReplayNamesTheOwner(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, issuer, client, refreshToken, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(time.Hour))
	first := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, first.err)
	require.Equal(t, http.StatusOK, first.code, first.body)
	expireRefreshReplay(t, ctx, ti, issuer.ID, refreshToken)

	refused := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, refused.err)
	require.JSONEq(t, `{"error":"invalid_grant","error_description":"refresh_token is unknown or already used"}`, refused.body)

	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	replayed := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, replayed.err)
	require.Equal(t, refused.body, replayed.body)
	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeRefreshTokenUnknownOrAlreadyUsed)
	require.Equal(t, urn.NewUserSubject("refresh-replay-user").String(), row.SubjectURN)
}

func TestInboundRefresh_UsedTokenFromAnotherClientNamesNobody(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, issuer, client, refreshToken, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(time.Hour))
	first := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, first.err)
	require.Equal(t, http.StatusOK, first.code, first.body)
	expireRefreshReplay(t, ctx, ti, issuer.ID, refreshToken)
	other := seedOtherRefreshClient(t, ctx, ti, issuer.ID)

	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	refused := performRefreshRequest(ctx, ti, toolset.McpSlug.String, other.ClientID, refreshToken)
	require.NoError(t, refused.err)
	require.JSONEq(t, `{"error":"invalid_grant","error_description":"refresh_token is unknown or already used"}`, refused.body)
	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeRefreshTokenUnknownOrAlreadyUsed)
	require.Empty(t, row.SubjectURN, "a token another client presented is not attributed to its owner")
	require.Equal(t, other.ID.String(), row.UserSessionClientID)
}

// A refusal cached for the owner's app names nobody when another app replays the token.
func TestInboundRefresh_CachedUsedTokenRefusalNamesNobodyForAnotherClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, issuer, client, refreshToken, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(time.Hour))
	first := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, first.err)
	require.Equal(t, http.StatusOK, first.code, first.body)
	expireRefreshReplay(t, ctx, ti, issuer.ID, refreshToken)

	refused := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, refused.err)
	require.JSONEq(t, `{"error":"invalid_grant","error_description":"refresh_token is unknown or already used"}`, refused.body)
	other := seedOtherRefreshClient(t, ctx, ti, issuer.ID)

	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	replayed := performRefreshRequest(ctx, ti, toolset.McpSlug.String, other.ClientID, refreshToken)
	require.NoError(t, replayed.err)
	require.JSONEq(t, `{"error":"invalid_grant","error_description":"refresh_token is unknown or already used"}`, replayed.body)
	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeRefreshTokenUnknownOrAlreadyUsed)
	require.Empty(t, row.SubjectURN, "a cached refusal names its owner only to the app that drew it")
	require.Equal(t, other.ID.String(), row.UserSessionClientID)
}
