package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/bindings"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/require"
)

func TestAssistantWorkloadClientCredentialsEndToEnd(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"client_secret_basic", "private_key_jwt"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
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
			tx := testenv.BeginTx(t, ctx, ti.conn)
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
			_, err = usersessionsrepo.New(ti.conn).GetKitUserSessionIssuerByID(ctx, usersessionsrepo.GetKitUserSessionIssuerByIDParams{ID: p.UserSessionIssuerID, ProjectID: p.ProjectID, OrganizationID: "other-org"})
			require.ErrorIs(t, err, pgx.ErrNoRows, "issuer project must belong to the requested organization")
			bootstrapConfig := mcp.AssistantRuntimeWorkloadClientConfig{UserSessionIssuerID: p.UserSessionIssuerID, TriggerID: trigger.ID, AuthMethod: method, JWKS: p.JWKS}
			// Reuse the already-held identity/lifecycle transaction. Opening a
			// second pool transaction here would block on these same row locks.
			provisionTx := testenv.BeginTx(t, ctx, ti.conn)
			_, err = identities.ResolveLocked(ctx, provisionTx, fx.orgID, fx.target.ProjectID, a.ID, trigger.ID)
			require.NoError(t, err)
			provisionCtx, cancelProvision := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancelProvision()
			client, err := mcp.NewAssistantRuntimeWorkloadProvisioner(ti.conn, ti.audit).ProvisionLocked(provisionCtx, provisionTx, runtime.ID, bootstrapConfig)
			require.NoError(t, err)
			_, err = usersessionsrepo.New(ti.conn).GetUserSessionClientByID(ctx, usersessionsrepo.GetUserSessionClientByIDParams{ID: client.RegistrationID, ProjectID: fx.target.ProjectID, OrganizationID: fx.orgID})
			require.ErrorIs(t, err, pgx.ErrNoRows, "provisioning must not commit its caller's transaction")
			require.NoError(t, provisionTx.Commit(ctx))
			require.True(t, strings.HasPrefix(client.ClientID, "assistant-runtime-"))
			fence := testenv.BeginTx(t, ctx, ti.conn)
			require.NoError(t, assistantrepo.New(fence).AcquireAssistantAdvisoryLock(ctx, a.ID.String()))
			blockedCtx, cancelBlocked := context.WithTimeout(ctx, 250*time.Millisecond)
			_, err = ti.service.ProvisionAssistantRuntimeWorkloadClient(blockedCtx, runtime.ID, bootstrapConfig)
			cancelBlocked()
			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.NoError(t, fence.Rollback(ctx))
			require.NoError(t, assistantrepo.New(ti.conn).SetAssistantRuntimeStateForTest(ctx, assistantrepo.SetAssistantRuntimeStateForTestParams{State: "expiring", UpdatedAt: conv.ToPGTimestamptz(time.Now()), RuntimeID: runtime.ID, ProjectID: fx.target.ProjectID}))
			_, err = ti.service.ProvisionAssistantRuntimeWorkloadClient(ctx, runtime.ID, bootstrapConfig)
			require.ErrorIs(t, err, pgx.ErrNoRows)
			require.NoError(t, assistantrepo.New(ti.conn).SetAssistantRuntimeStateForTest(ctx, assistantrepo.SetAssistantRuntimeStateForTestParams{State: "starting", UpdatedAt: conv.ToPGTimestamptz(time.Now()), RuntimeID: runtime.ID, ProjectID: fx.target.ProjectID}))
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
			token := workloadResponseString(t, body, "access_token")
			admittedCtx, _, _, err := ti.service.ApplyIssuerGate(t.Context(), httptest.NewRecorder(), token, ti.serverURL.String(), endpoint)
			require.NoError(t, err)
			binding, err := mcp.ResolveAssistantRuntimeWorkloadContext(admittedCtx, ti.conn)
			require.NoError(t, err)
			require.Equal(t, runtime.ID, binding.RuntimeID)
			require.Equal(t, client.RegistrationID, binding.ClientRegistrationID)
			for _, state := range []string{"stopped", "expiring", "starting", "active"} {
				require.NoError(t, assistantrepo.New(ti.conn).SetAssistantRuntimeStateForTest(ctx, assistantrepo.SetAssistantRuntimeStateForTestParams{State: state, UpdatedAt: conv.ToPGTimestamptz(time.Now()), RuntimeID: runtime.ID, ProjectID: fx.target.ProjectID}))
				_, err = mcp.ResolveAssistantRuntimeWorkloadContext(admittedCtx, ti.conn)
				if state == "starting" || state == "active" {
					require.NoError(t, err, state)
				} else {
					require.ErrorIs(t, err, pgx.ErrNoRows, state)
				}
			}
			_, err = triggerrepo.New(ti.conn).UpdateTriggerInstance(ctx, triggerrepo.UpdateTriggerInstanceParams{ID: trigger.ID, ProjectID: fx.target.ProjectID, TargetRef: conv.ToPGText(uuid.NewString())})
			require.NoError(t, err)
			_, err = mcp.ResolveAssistantRuntimeWorkloadContext(admittedCtx, ti.conn)
			require.Error(t, err, "retargeted root must revoke wake authority")
			_, err = triggerrepo.New(ti.conn).UpdateTriggerInstance(ctx, triggerrepo.UpdateTriggerInstanceParams{ID: trigger.ID, ProjectID: fx.target.ProjectID, TargetRef: conv.ToPGText(a.ID.String())})
			require.NoError(t, err)
			require.NoError(t, usersessionsrepo.New(ti.conn).ClearAssistantWorkloadClientMarkersForTest(ctx, usersessionsrepo.ClearAssistantWorkloadClientMarkersForTestParams{ClientID: client.RegistrationID, OrganizationID: conv.ToPGText(fx.orgID)}))
			_, _, _, err = ti.service.ApplyIssuerGate(ctx, httptest.NewRecorder(), token, ti.serverURL.String(), endpoint)
			require.Error(t, err, "cleared foreign keys must not erase assistant client classification")
			_, err = usersessionsrepo.New(ti.conn).BindAssistantWorkloadClient(ctx, usersessionsrepo.BindAssistantWorkloadClientParams{ClientID: client.RegistrationID, RuntimeID: runtime.ID, OrganizationID: conv.ToPGText(fx.orgID), ProjectID: uuid.NullUUID{UUID: fx.target.ProjectID, Valid: true}, AssistantID: a.ID, TriggerID: trigger.ID})
			require.NoError(t, err)
			claims := accessTokenClaims(t, w.Body.Bytes())
			require.True(t, strings.HasPrefix(workloadResponseString(t, claims, "sub"), "workload:"))
			session, err := usersessionsrepo.New(ti.conn).GetUserSessionByJTI(ctx, usersessionsrepo.GetUserSessionByJTIParams{UserSessionIssuerID: fx.target.UserSessionIssuerID, Jti: workloadResponseString(t, claims, "jti")})
			require.NoError(t, err)
			require.True(t, session.DelegatedGrantsVersion.Valid)
			require.Equal(t, int32(5), session.DelegatedGrantsVersion.Int32, "assistant MCP issuance must remain readable by pre-inference replicas")
			require.True(t, session.UserSessionClientID.Valid)
			require.Equal(t, client.RegistrationID, session.UserSessionClientID.UUID)
			require.False(t, session.AuthorizerUserID.Valid)
			require.Equal(t, "toolset:"+fx.toolset.ID.String(), session.ResourceIdentity.String)
			revoke := httptest.NewRecorder()
			require.NoError(t, ti.service.ServeRevoke(revoke, request(urls.Revoke, url.Values{"token": {token}, "token_type_hint": {"access_token"}}), endpoint))
			require.Equal(t, http.StatusOK, revoke.Code)
			_, _, _, err = ti.service.ApplyIssuerGate(t.Context(), httptest.NewRecorder(), token, ti.serverURL.String(), endpoint)
			require.Error(t, err)
			w = exchange("mcp:connect", resource)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			token = workloadResponseString(t, body, "access_token")
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
			token = workloadResponseString(t, body, "access_token")
			require.NoError(t, assistantrepo.New(ti.conn).StopAssistantRuntime(ctx, assistantrepo.StopAssistantRuntimeParams{ProjectID: fx.target.ProjectID, RuntimeID: runtime.ID, State: "stopped", StartingState: "starting", ActiveState: "active", ExpiringState: "expiring"}))
			_, _, _, err = ti.service.ApplyIssuerGate(t.Context(), httptest.NewRecorder(), token, ti.serverURL.String(), endpoint)
			require.Error(t, err)
			require.NotEqual(t, http.StatusOK, exchange("mcp:connect", resource).Code)
			_, err = ti.service.ProvisionAssistantRuntimeWorkloadClient(t.Context(), runtime.ID, bootstrapConfig)
			require.Error(t, err)
			cleanupTx := testenv.BeginTx(t, ctx, ti.conn)
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

func workloadResponseString(t *testing.T, values map[string]any, key string) string {
	t.Helper()
	value, ok := values[key].(string)
	require.True(t, ok, "response field %s must be a string", key)
	return value
}

func TestKitIssuerLookupRejectsUnscopedAndForeignIssuers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := newWorkloadConsentFixture(t, ctx, ti)
	otherOrg := "org-" + uuid.NewString()
	require.NoError(t, testrepo.New(ti.conn).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{ID: otherOrg, Name: "Other organization", Slug: "other-" + uuid.NewString(), GramAccountType: "enterprise", FreeTrialStartedAt: conv.ToPGTimestamptz(time.Now()), FreeTrialEndsAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour))}))
	for _, tc := range []struct {
		name         string
		organization pgtype.Text
		allowed      bool
	}{
		{name: "global", organization: pgtype.Text{}, allowed: false},
		{name: "same organization", organization: conv.ToPGText(fx.orgID), allowed: true},
		{name: "other organization", organization: conv.ToPGText(otherOrg), allowed: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			id, err := testrepo.New(ti.conn).InsertOrganizationTierUserSessionIssuerFixture(ctx, testrepo.InsertOrganizationTierUserSessionIssuerFixtureParams{OrganizationID: tc.organization, Slug: uuid.NewString(), AuthnChallengeMode: "chain", SessionDuration: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true}})
			require.NoError(t, err)
			issuer, err := usersessionsrepo.New(ti.conn).GetKitUserSessionIssuerByID(ctx, usersessionsrepo.GetKitUserSessionIssuerByIDParams{ID: id, ProjectID: fx.target.ProjectID, OrganizationID: fx.orgID})
			tx := testenv.BeginTx(t, ctx, ti.conn)
			_, attachmentErr := bindings.ValidateAndLock(ctx, tx, id, fx.target.ProjectID, fx.orgID)
			require.NoError(t, tx.Rollback(ctx))
			if tc.allowed {
				require.NoError(t, err)
				require.Equal(t, id, issuer.ID)
				require.NoError(t, attachmentErr)
			} else {
				require.ErrorIs(t, attachmentErr, bindings.ErrNotFound)
				require.ErrorIs(t, err, pgx.ErrNoRows)
			}
		})
	}
}
