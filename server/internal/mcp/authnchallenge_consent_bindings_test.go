package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/agentmanagement"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestConsentAgentBindingActionsUseRealAttachmentService(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := newAgentConsentFixture(t, ctx, ti)
	seedUserMCPConnectGrant(t, ctx, ti.conn, fx.orgID, fx.userID, fx.target.MCPResourceID.String())
	seedPrincipalGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeUser, fx.userID), authz.ScopeProjectRead, fx.target.ProjectID.String())
	agent := createConsentAgent(t, ctx, ti, fx, "Reusable connection agent")
	grantID := seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)
	policy, policyErr := guardian.NewUnsafePolicy(ti.tracerProvider, []string{})
	require.NoError(t, policyErr)
	meterProvider := testenv.NewMeterProvider(t)
	refresher := remotesessions.NewRefreshService(ti.logger, meterProvider, ti.conn, ti.enc, policy, nil, ti.cacheAdapter)
	bindings := remotesessions.NewService(ti.logger, ti.tracerProvider, meterProvider, ti.conn, ti.sessionManager, ti.authzEngine, ti.enc, nil, policy, nil, ti.audit, ti.serverURL, remotesessions.NewIdentityCommitter(ti.logger, ti.conn, ti.enc, ti.audit, ti.serverURL, policy, nil, nil), refresher, nil)
	bindings.SetBindingAuthorizer(func(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
		_, _, err := agentmanagement.NewAuthorizer(ti.authzEngine).RequireAgentOwnerForUpdate(ctx, tx, id, agentmanagement.OwnedAgentAuthorize)
		if err != nil {
			return fmt.Errorf("authorize attachment owner: %w", err)
		}
		return nil
	})
	ti.service.SetConsentBindingService(bindings)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, fx.toolset.McpSlug.String, "mcp")
	require.NoError(t, err)
	clientID := createConsentRemoteClient(t, ctx, ti.conn, fx.target.ProjectID, fx.orgID, "consent-reuse", "", []uuid.UUID{fx.target.UserSessionIssuerID})
	cipher, err := ti.enc.Encrypt([]byte("upstream-placeholder"))
	require.NoError(t, err)
	createSession := func(subject urn.SessionSubject) uuid.UUID {
		session, err := repo.New(ti.conn).UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
			SubjectUrn: subject, UserSessionIssuerID: fx.target.UserSessionIssuerID,
			RemoteSessionClientID: clientID, AccessTokenEncrypted: cipher,
			AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), Scopes: []string{},
		})
		require.NoError(t, err)
		// Existing human OAuth sessions already store provider identity; the
		// agent consent surface must use that same canonical session view.
		_, err = repo.New(ti.conn).UpdateRemoteSessionIdentity(ctx, repo.UpdateRemoteSessionIdentityParams{
			ID: session.ID, SubjectUrn: subject, RemoteSessionClientID: clientID,
			ExpectedUpdatedAt:   session.UpdatedAt,
			UpstreamEmail:       pgtype.Text{String: "existing-account@example.com", Valid: true},
			UpstreamDisplayName: pgtype.Text{String: "Existing upstream account", Valid: true},
		})
		require.NoError(t, err)
		return session.ID
	}
	owned := createSession(urn.NewUserSubject(fx.userID))
	other := createSession(urn.NewUserSubject("another-subject"))
	token := ti.getSessionToken(ctx, t)
	call := func(action string, cookie bool, extra url.Values) (*httptest.ResponseRecorder, error) {
		form := url.Values{"state": {fx.stateID}, "csrf_token": {fx.csrf}, "agent_id": {agent.ID.String()}, "action": {action}}
		maps.Copy(form, extra)
		// A real consent request has no RPC-prepared authorization grants.
		req := httptest.NewRequest(http.MethodPost, "/mcp/"+endpoint.Slug+"/connect/remote-session", strings.NewReader(form.Encode())).WithContext(t.Context())
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if cookie {
			req.AddCookie(&http.Cookie{Name: constants.SessionCookie, Value: token})
		}
		w := httptest.NewRecorder()
		err := ti.service.ServeConsentAction(w, req, endpoint)
		if err != nil {
			return w, fmt.Errorf("serve consent action: %w", err)
		}
		return w, nil
	}
	for _, flag := range []feature.Flag{feature.FlagAgentManagement, feature.FlagAgentIdentityCredentials} {
		ti.features.SetFlag(flag, fx.orgID, false)
		_, err := call("agent_connections", false, nil)
		var denied *oops.ShareableError
		require.ErrorAs(t, err, &denied)
		require.Equal(t, oops.CodeUnauthorized, denied.Code)
		_, err = call("agent_connections", true, nil)
		require.ErrorAs(t, err, &denied)
		require.Equal(t, oops.CodeNotFound, denied.Code)
		ti.features.SetFlag(flag, fx.orgID, true)
	}
	_, err = call("agent_attach", false, url.Values{"remote_session_id": {owned.String()}})
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnauthorized, shareable.Code)
	_, err = call("agent_attach", true, url.Values{"csrf_token": {"wrong"}, "remote_session_id": {owned.String()}})
	require.Error(t, err)
	_, err = call("agent_attach", true, url.Values{"remote_session_id": {other.String()}})
	require.Error(t, err)
	w, err := call("agent_connections", true, nil)
	require.NoError(t, err)
	require.Contains(t, w.Body.String(), owned.String())
	require.NotContains(t, w.Body.String(), other.String())
	require.NotContains(t, w.Body.String(), "upstream-placeholder")
	require.Contains(t, w.Body.String(), "existing-account@example.com")
	require.Contains(t, w.Body.String(), "Existing upstream account")
	require.Empty(t, w.Header().Get("Location"), "discovery must not start OAuth")
	w, err = call("agent_attach", true, url.Values{"remote_session_id": {owned.String()}})
	require.NoError(t, err)
	var binding struct {
		ID              string `json:"ID"`
		RemoteSessionID string `json:"RemoteSessionID"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &binding))
	require.Equal(t, owned.String(), binding.RemoteSessionID)
	require.Empty(t, w.Header().Get("Location"), "attaching an existing account must not redirect to OAuth")
	_, err = ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).DeletePrincipalGrant(ctx, accessrepo.DeletePrincipalGrantParams{ID: grantID, OrganizationID: fx.orgID})
	require.NoError(t, err)
	_, err = call("agent_detach", true, url.Values{"binding_id": {binding.ID}})
	require.NoError(t, err)
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)
	// Restoring the explicit attachment completes consent without changing
	// the agent's fixed connect policy or borrowing the human subject.
	_, err = call("agent_attach", true, url.Values{"remote_session_id": {owned.String()}})
	require.NoError(t, err)
	w, err = serveAgentConsentPost(t, ctx, ti, fx, agent.ID)
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, w.Code)
	require.Contains(t, w.Header().Get("Location"), "code=agent-v1.")
}
