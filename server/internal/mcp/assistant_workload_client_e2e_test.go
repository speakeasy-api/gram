package mcp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/require"
)

func TestAssistantWorkloadClientCredentialsEndToEnd(t *testing.T) {
	for _, method := range []string{"client_secret_basic", "private_key_jwt"} {
		t.Run(method, func(t *testing.T) {
			ctx, ti := newTestMCPService(t)
			fx := newWorkloadConsentFixture(t, ctx, ti)
			a, err := assistantrepo.New(ti.conn).CreateAssistant(ctx, assistantrepo.CreateAssistantParams{
				ProjectID: fx.target.ProjectID, OrganizationID: fx.orgID, CreatedByUserID: conv.ToPGText(fx.userID),
				Name: "OAuth workload test", Model: "test/model", Instructions: "test", WarmTtlSeconds: 300, MaxConcurrency: 1, Status: "active",
			})
			require.NoError(t, err)
			trigger, err := triggerrepo.New(ti.conn).CreateTriggerInstance(ctx, triggerrepo.CreateTriggerInstanceParams{
				OrganizationID: fx.orgID, ProjectID: fx.target.ProjectID, DefinitionSlug: "manual", Name: "OAuth root", TargetKind: "assistant", TargetRef: a.ID.String(), TargetDisplay: "OAuth workload test", ConfigJson: []byte(`{}`), Status: "active",
			})
			require.NoError(t, err)
			identities := assistantidentity.New(ti.serverURL.String(), ti.audit)
			tx, err := ti.conn.Begin(ctx)
			require.NoError(t, err)
			require.NoError(t, identities.Provision(ctx, tx, assistantidentity.ProvisionParams{OrganizationID: fx.orgID, ProjectID: fx.target.ProjectID, AssistantID: a.ID, ActorUserID: fx.userID}))
			require.NoError(t, tx.Commit(ctx))
			seedPrincipalGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeUser, fx.userID), authz.ScopeAssistantWrite, a.ID.String())
			require.NoError(t, assistantrepo.New(ti.conn).SetAssistantDesiredRuntimeVersionForTest(ctx, assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{RuntimeVersion: 3, AssistantID: a.ID, ProjectID: fx.target.ProjectID}))
			require.NoError(t, assistantrepo.New(ti.conn).ReserveAssistantRuntimeV2(ctx, assistantrepo.ReserveAssistantRuntimeV2Params{AssistantThreadID: uuid.Nil, AssistantID: a.ID, ProjectID: fx.target.ProjectID, Backend: "local", State: "starting"}))
			runtime, err := assistantrepo.New(ti.conn).GetAssistantRuntimeV2(ctx, assistantrepo.GetAssistantRuntimeV2Params{AssistantID: a.ID, ProjectID: fx.target.ProjectID})
			require.NoError(t, err)
			signer := newAssertionSigner(t)
			p := mcp.ProvisionAssistantWorkloadClientParams{RuntimeID: runtime.ID, OrganizationID: fx.orgID, ProjectID: fx.target.ProjectID, AssistantID: a.ID, TriggerID: trigger.ID, UserSessionIssuerID: fx.target.UserSessionIssuerID, AuthMethod: method}
			if method == "private_key_jwt" {
				p.JWKS = signer.jwks
			}
			_, err = ti.service.ProvisionAssistantWorkloadClient(t.Context(), p)
			require.Error(t, err, "request-facing provisioning must not accept a worker without authority")
			bootstrapConfig := mcp.AssistantRuntimeWorkloadClientConfig{UserSessionIssuerID: p.UserSessionIssuerID, TriggerID: trigger.ID, AuthMethod: method, JWKS: p.JWKS}
			client, err := ti.service.ProvisionAssistantRuntimeWorkloadClient(t.Context(), runtime.ID, bootstrapConfig)
			require.NoError(t, err)
			endpoint := workloadSessionEndpoint(fx)
			resource, err := endpoint.RootURL(ti.serverURL.String())
			require.NoError(t, err)
			urls, err := endpoint.AuthorizationServerURLs(ti.serverURL.String())
			require.NoError(t, err)
			request := func(path string, form url.Values) *http.Request {
				form.Set("client_id", client.ClientID)
				if method == "private_key_jwt" {
					form.Set("client_assertion_type", "urn:ietf:params:oauth:client-assertion-type:jwt-bearer")
					form.Set("client_assertion", signer.assertion(t, client.ClientID, urls.Issuer))
				}
				r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				if method == "client_secret_basic" {
					r.SetBasicAuth(client.ClientID, client.Secret)
				}
				return r
			}
			exchange := func(scope, res string) *httptest.ResponseRecorder {
				w := httptest.NewRecorder()
				r := request(urls.Token, url.Values{"grant_type": {"client_credentials"}, "scope": {scope}, "resource": {res}})
				require.NoError(t, ti.service.ServeToken(w, r, endpoint))
				return w
			}
			require.Equal(t, http.StatusBadRequest, exchange("mcp:connect tools:read", resource).Code)
			require.Equal(t, http.StatusBadRequest, exchange("mcp:connect", resource+"/other").Code)
			w := exchange("mcp:connect", resource)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			var body map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			require.Equal(t, "mcp:connect", body["scope"])
			require.NotContains(t, body, "refresh_token")
			token := body["access_token"].(string)
			admittedCtx, _, _, err := ti.service.ApplyIssuerGate(t.Context(), httptest.NewRecorder(), token, ti.serverURL.String(), endpoint)
			require.NoError(t, err)
			binding, err := mcp.ResolveAssistantRuntimeWorkloadContext(admittedCtx, ti.conn)
			require.NoError(t, err)
			require.Equal(t, runtime.ID, binding.RuntimeID)
			require.Equal(t, client.RegistrationID, binding.ClientRegistrationID)
			claims := accessTokenClaims(t, w.Body.Bytes())
			require.True(t, strings.HasPrefix(claims["sub"].(string), "workload:"))
			session, err := usersessionsrepo.New(ti.conn).GetUserSessionByJTI(ctx, usersessionsrepo.GetUserSessionByJTIParams{UserSessionIssuerID: fx.target.UserSessionIssuerID, Jti: claims["jti"].(string)})
			require.NoError(t, err)
			require.True(t, session.UserSessionClientID.Valid)
			require.Equal(t, client.RegistrationID, session.UserSessionClientID.UUID)
			require.False(t, session.AuthorizerUserID.Valid)
			revoke := httptest.NewRecorder()
			require.NoError(t, ti.service.ServeRevoke(revoke, request(urls.Revoke, url.Values{"token": {token}, "token_type_hint": {"access_token"}}), endpoint))
			require.Equal(t, http.StatusOK, revoke.Code)
			_, _, _, err = ti.service.ApplyIssuerGate(t.Context(), httptest.NewRecorder(), token, ti.serverURL.String(), endpoint)
			require.Error(t, err)
			w = exchange("mcp:connect", resource)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			token = body["access_token"].(string)
			_, err = usersessionsrepo.New(ti.conn).RevokeUserSessionClient(ctx, usersessionsrepo.RevokeUserSessionClientParams{ID: client.RegistrationID, ProjectID: fx.target.ProjectID, OrganizationID: fx.orgID})
			require.NoError(t, err)
			_, _, _, err = ti.service.ApplyIssuerGate(t.Context(), httptest.NewRecorder(), token, ti.serverURL.String(), endpoint)
			require.Error(t, err)
			require.NotEqual(t, http.StatusOK, exchange("mcp:connect", resource).Code)
			// Retirement invalidates already issued tokens even before client cleanup.
			client, err = ti.service.ProvisionAssistantRuntimeWorkloadClient(t.Context(), runtime.ID, bootstrapConfig)
			require.NoError(t, err)
			w = exchange("mcp:connect", resource)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			token = body["access_token"].(string)
			require.NoError(t, assistantrepo.New(ti.conn).StopAssistantRuntime(ctx, assistantrepo.StopAssistantRuntimeParams{ProjectID: fx.target.ProjectID, RuntimeID: runtime.ID, State: "stopped", StartingState: "starting", ActiveState: "active", ExpiringState: "expiring"}))
			_, _, _, err = ti.service.ApplyIssuerGate(t.Context(), httptest.NewRecorder(), token, ti.serverURL.String(), endpoint)
			require.Error(t, err)
			require.NotEqual(t, http.StatusOK, exchange("mcp:connect", resource).Code)
			_, err = ti.service.ProvisionAssistantRuntimeWorkloadClient(t.Context(), runtime.ID, bootstrapConfig)
			require.Error(t, err)
			cleanupTx, err := ti.conn.Begin(ctx)
			require.NoError(t, err)
			revoked, err := mcp.RevokeAssistantRuntimeWorkloadClients(ctx, cleanupTx, fx.orgID, fx.target.ProjectID, runtime.ID)
			require.NoError(t, err)
			require.Contains(t, revoked, client.RegistrationID)
			require.NoError(t, cleanupTx.Commit(ctx))
			// DCR/public clients have no binding and can never receive this grant.
			unbound := httptest.NewRecorder()
			unboundForm := url.Values{"client_id": {fx.client.ClientID}, "grant_type": {"client_credentials"}, "scope": {"mcp:connect"}, "resource": {resource}}
			unboundReq := httptest.NewRequest(http.MethodPost, urls.Token, strings.NewReader(unboundForm.Encode()))
			unboundReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			require.NoError(t, ti.service.ServeToken(unbound, unboundReq, endpoint))
			require.NotEqual(t, http.StatusOK, unbound.Code)
		})
	}
}
