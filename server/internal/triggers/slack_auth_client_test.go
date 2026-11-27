package triggers_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	authclientsgen "github.com/speakeasy-api/gram/server/gen/auth_clients"
	gen "github.com/speakeasy-api/gram/server/gen/triggers"
	"github.com/speakeasy-api/gram/server/internal/authclients"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/triggers"
)

func signSlackWebhook(body, secret string) (timestamp, signature string) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "v0:%s:%s", ts, body)
	return ts, "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func TestSlackTriggerWithAuthClientIngressAndCoexistence(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := authztest.WithExactGrants(t, ctx,
		authz.NewGrant(authz.ScopeOrgRead, ti.organizationID),
		authz.NewGrant(authz.ScopeOrgAdmin, ti.organizationID),
		authz.NewGrant(authz.ScopeProjectRead, ti.projectID.String()),
		authz.NewGrant(authz.ScopeProjectWrite, ti.projectID.String()),
	)

	const authClientSecret = "slack-auth-client-signing-secret"
	client, err := ti.authClientsSvc.CreateAuthClient(adminCtx, &authclientsgen.CreateAuthClientPayload{
		ClientType: authz.AuthClientTypeSlackApp,
		Name:       "Assistant Slack",
		ProjectID:  new(ti.projectID.String()),
		Headers: []*authclientsgen.AuthClientHeaderInput{
			{Name: authclients.SlackHeaderBotToken, IsSecret: true, Value: new("xoxb-trigger-bot")},
			{Name: authclients.SlackHeaderSigningSecret, IsSecret: true, Value: new(authClientSecret)},
		},
	})
	require.NoError(t, err)

	// Setting both environment_id and auth_client_id is rejected.
	_, err = ti.service.CreateTriggerInstance(ctx, &gen.CreateTriggerInstancePayload{
		DefinitionSlug: "slack",
		Name:           "Both sources",
		EnvironmentID:  new(ti.environmentID.String()),
		AuthClientID:   new(client.ID),
		TargetKind:     "noop",
		TargetRef:      "noop",
		TargetDisplay:  "Noop",
		Config:         map[string]any{},
	})
	require.Error(t, err)
	shareable, ok := errors.AsType[*oops.ShareableError](err)
	require.True(t, ok)
	require.Equal(t, oops.CodeBadRequest, shareable.Code)

	// Non-slack triggers cannot use auth_client_id.
	_, err = ti.service.CreateTriggerInstance(ctx, &gen.CreateTriggerInstancePayload{
		DefinitionSlug: "github",
		Name:           "GitHub with slack_app",
		AuthClientID:   new(client.ID),
		TargetKind:     "noop",
		TargetRef:      "noop",
		TargetDisplay:  "Noop",
		Config:         map[string]any{},
	})
	require.Error(t, err)

	// Non-slack_app auth clients cannot be bound to a Slack trigger.
	staticClient, err := ti.authClientsSvc.CreateAuthClient(adminCtx, &authclientsgen.CreateAuthClientPayload{
		ClientType: authz.AuthClientTypeStaticCredential,
		Name:       "Static key",
		ProjectID:  new(ti.projectID.String()),
		Headers:    []*authclientsgen.AuthClientHeaderInput{{Name: "X-Api-Key", IsSecret: true, Value: new("k")}},
	})
	require.NoError(t, err)
	_, err = ti.service.CreateTriggerInstance(ctx, &gen.CreateTriggerInstancePayload{
		DefinitionSlug: "slack",
		Name:           "Wrong client type",
		AuthClientID:   new(staticClient.ID),
		TargetKind:     "noop",
		TargetRef:      "noop",
		TargetDisplay:  "Noop",
		Config:         map[string]any{},
	})
	require.Error(t, err)

	// Create an auth-client-backed Slack trigger (no environment_id) AND an
	// environment-backed Slack trigger in the same project.
	authTrigger, err := ti.service.CreateTriggerInstance(ctx, &gen.CreateTriggerInstancePayload{
		DefinitionSlug: "slack",
		Name:           "Auth-client Slack",
		AuthClientID:   new(client.ID),
		TargetKind:     "noop",
		TargetRef:      "noop",
		TargetDisplay:  "Noop",
		Config:         map[string]any{"event_types": []any{"app_mention"}},
	})
	require.NoError(t, err)
	require.Nil(t, authTrigger.EnvironmentID)
	require.NotNil(t, authTrigger.AuthClientID)
	require.Equal(t, client.ID, *authTrigger.AuthClientID)

	envPayload := newCreatePayload(ti.environmentID, "Environment Slack")
	envPayload.Config = map[string]any{"event_types": []any{"app_mention"}}
	envTrigger, err := ti.service.CreateTriggerInstance(ctx, envPayload)
	require.NoError(t, err)
	require.NotNil(t, envTrigger.EnvironmentID)
	require.Nil(t, envTrigger.AuthClientID)

	mux := goahttp.NewMuxer()
	triggers.Attach(mux, ti.service)
	deliver := func(triggerID, body, secret string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/rpc/triggers/"+triggerID+"/webhook", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if secret != "" {
			ts, sig := signSlackWebhook(body, secret)
			req.Header.Set("X-Slack-Request-Timestamp", ts)
			req.Header.Set("X-Slack-Signature", sig)
		}
		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, req)
		return resp
	}

	// Event filtered out cleanly (reaction_added not in event_types) so dispatch
	// doesn't need Temporal while still exercising full HMAC auth + ingest.
	eventBody := `{"type":"event_callback","event_id":"Ev-1","event":{"type":"reaction_added","user":"U1","reaction":"eyes","item":{"type":"message","channel":"C1","ts":"1.0"}}}`

	// Auth-client trigger verifies strictly against the slack_app signing secret.
	require.Equal(t, http.StatusUnauthorized, deliver(authTrigger.ID, eventBody, "test-secret").Code, "must not accept environment signing secret")
	require.Equal(t, http.StatusOK, deliver(authTrigger.ID, eventBody, authClientSecret).Code)

	// Environment-backed trigger verifies against its environment secret.
	require.Equal(t, http.StatusOK, deliver(envTrigger.ID, eventBody, "test-secret").Code)
	require.Equal(t, http.StatusUnauthorized, deliver(envTrigger.ID, eventBody, authClientSecret).Code)

	// Disabling the slack_app client makes ingress fail closed (401) and prevents binding new triggers to it.
	_, err = ti.authClientsSvc.UpdateAuthClient(adminCtx, &authclientsgen.UpdateAuthClientPayload{
		ID:      client.ID,
		Enabled: new(false),
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, deliver(authTrigger.ID, eventBody, authClientSecret).Code)
	_, err = ti.service.CreateTriggerInstance(ctx, &gen.CreateTriggerInstancePayload{
		DefinitionSlug: "slack",
		Name:           "Disabled client trigger",
		AuthClientID:   new(client.ID),
		TargetKind:     "noop",
		TargetRef:      "noop",
		TargetDisplay:  "Noop",
		Config:         map[string]any{},
	})
	require.Error(t, err)
	shareable, ok = errors.AsType[*oops.ShareableError](err)
	require.True(t, ok)
	require.Equal(t, oops.CodeBadRequest, shareable.Code)

	// Re-enabling restores ingress.
	_, err = ti.authClientsSvc.UpdateAuthClient(adminCtx, &authclientsgen.UpdateAuthClientPayload{
		ID:      client.ID,
		Enabled: new(true),
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, deliver(authTrigger.ID, eventBody, authClientSecret).Code)

	// Updating a trigger with both environment_id and auth_client_id is rejected.
	_, err = ti.service.UpdateTriggerInstance(ctx, &gen.UpdateTriggerInstancePayload{
		ID:            envTrigger.ID,
		EnvironmentID: new(ti.environmentID.String()),
		AuthClientID:  new(client.ID),
	})
	require.Error(t, err)
	shareable, ok = errors.AsType[*oops.ShareableError](err)
	require.True(t, ok)
	require.Equal(t, oops.CodeBadRequest, shareable.Code)

	// Switching an environment-backed trigger to auth_client_id clears environment_id,
	// and switching back to environment_id clears auth_client_id, updating webhook verification.
	switched, err := ti.service.UpdateTriggerInstance(ctx, &gen.UpdateTriggerInstancePayload{
		ID:           envTrigger.ID,
		AuthClientID: new(client.ID),
	})
	require.NoError(t, err)
	require.Nil(t, switched.EnvironmentID)
	require.NotNil(t, switched.AuthClientID)
	require.Equal(t, client.ID, *switched.AuthClientID)
	require.Equal(t, http.StatusOK, deliver(envTrigger.ID, eventBody, authClientSecret).Code)
	require.Equal(t, http.StatusUnauthorized, deliver(envTrigger.ID, eventBody, "test-secret").Code)

	switchedBack, err := ti.service.UpdateTriggerInstance(ctx, &gen.UpdateTriggerInstancePayload{
		ID:            envTrigger.ID,
		EnvironmentID: new(ti.environmentID.String()),
	})
	require.NoError(t, err)
	require.NotNil(t, switchedBack.EnvironmentID)
	require.Nil(t, switchedBack.AuthClientID)
	require.Equal(t, http.StatusOK, deliver(envTrigger.ID, eventBody, "test-secret").Code)
	require.Equal(t, http.StatusUnauthorized, deliver(envTrigger.ID, eventBody, authClientSecret).Code)

	// Hard-deleting the auth client triggers ON DELETE SET NULL (auth_client_id),
	// leaving environment_id NULL so ingress fails closed and never falls back.
	require.NoError(t, testrepo.New(ti.conn).HardDeleteAuthClientFixture(ctx, testrepo.HardDeleteAuthClientFixtureParams{
		ID:             uuid.MustParse(client.ID),
		OrganizationID: ti.organizationID,
	}))
	afterDelete, err := ti.service.GetTriggerInstance(ctx, &gen.GetTriggerInstancePayload{ID: authTrigger.ID})
	require.NoError(t, err)
	require.Nil(t, afterDelete.AuthClientID)
	require.Nil(t, afterDelete.EnvironmentID)
	require.Equal(t, http.StatusUnauthorized, deliver(authTrigger.ID, eventBody, authClientSecret).Code)
	require.Equal(t, http.StatusUnauthorized, deliver(authTrigger.ID, eventBody, "test-secret").Code)
}
