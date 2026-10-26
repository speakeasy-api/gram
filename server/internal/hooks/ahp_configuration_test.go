package hooks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featuresrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
)

func ahpConfigurationRequest(h http.Handler, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/ahp", strings.NewReader(`{"jsonrpc":"2.0","id":"discovery","method":"hooks/configuration","params":{"protocolVersion":"draft"}}`))
	r.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		r.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func requireAHPConfiguration(t *testing.T, h http.Handler, failurePolicy string) {
	t.Helper()
	w := ahpConfigurationRequest(h, ahpAuthHeaders)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var response struct {
		ID     string          `json:"id"`
		Error  json.RawMessage `json:"error"`
		Result struct {
			Configuration struct {
				Subscriptions []struct {
					Upload        map[string]any `json:"upload"`
					Events        []string       `json:"events"`
					Mode          string         `json:"mode"`
					FailurePolicy string         `json:"failurePolicy"`
					TimeoutMS     int            `json:"timeoutMs"`
					Content       map[string]any `json:"content"`
				} `json:"subscriptions"`
			} `json:"configuration"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Empty(t, response.Error)
	require.Equal(t, "discovery", response.ID)
	events := map[string]string{}
	intercepts := 0
	for _, sub := range response.Result.Configuration.Subscriptions {
		require.Equal(t, map[string]any{"default": "body"}, sub.Content)
		require.Equal(t, map[string]any{"endpoint": "https://localhost:8080/ahp/uploads", "maxBytes": float64(ahpMaxUploadBytes), "timeoutMs": float64(10000)}, sub.Upload)
		for _, event := range sub.Events {
			require.NotContains(t, events, event, "must not deliver the same event twice")
			events[event] = sub.Mode
		}
		if sub.Mode == "intercept" {
			intercepts += len(sub.Events)
			require.Equal(t, failurePolicy, sub.FailurePolicy)
			require.Equal(t, 10000, sub.TimeoutMS)
		} else {
			require.Equal(t, "observe", sub.Mode)
			require.Empty(t, sub.FailurePolicy)
			require.Zero(t, sub.TimeoutMS)
		}
	}
	require.Equal(t, map[string]string{
		"user.message.inbound": "intercept",
		"tool.before":          "intercept",
		"session.start":        "observe",
		"session.end":          "observe",
		"turn.end":             "observe",
		"model.response.after": "observe",
		"tool.after":           "observe",
	}, events)
	require.Equal(t, 2, intercepts)
}

func TestAHPConfigurationDynamicPosture(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ti.service.auth = &ahpTestAuthorizer{authCtx: auth}
	h, err := ti.service.ahpHandler()
	require.NoError(t, err)
	// An organization that never configured the setting fails open.
	requireAHPConfiguration(t, h, "fail-open")
	features := featuresrepo.New(ti.conn)
	flag := featuresrepo.EnableFeatureParams{OrganizationID: auth.ActiveOrganizationID, FeatureName: string(productfeatures.FeatureHooksFailOpen)}
	_, err = features.EnableFeature(ctx, flag)
	require.NoError(t, err)
	requireAHPConfiguration(t, h, "fail-open")
	_, err = features.DeleteFeature(ctx, featuresrepo.DeleteFeatureParams(flag))
	require.NoError(t, err)
	requireAHPConfiguration(t, h, "fail-closed")
	// The posture belongs to the authenticated organization, not server state.
	other := *auth
	otherProject := uuid.New()
	other.ProjectID = &otherProject
	other.ActiveOrganizationID = "org_other"
	ti.service.auth = &ahpTestAuthorizer{authCtx: &other}
	requireAHPConfiguration(t, h, "fail-open")
	ti.service.auth = &ahpTestAuthorizer{authCtx: auth}
	requireAHPConfiguration(t, h, "fail-closed")
	_, err = features.EnableFeature(ctx, flag)
	require.NoError(t, err)
	requireAHPConfiguration(t, h, "fail-open")
}

func TestAHPConfigurationAuthentication(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ti.service.auth = &ahpTestAuthorizer{authCtx: auth}
	h, err := ti.service.ahpHandler()
	require.NoError(t, err)
	for _, headers := range []map[string]string{
		nil,
		{"Gram-Project": "chosen"},
		{"Authorization": "Bearer example"},
		{"Authorization": "Bearer invalid", "Gram-Project": "chosen"},
		{"Gram-Key": "example", "Gram-Project": "other"},
	} {
		require.Equal(t, http.StatusUnauthorized, ahpConfigurationRequest(h, headers).Code)
	}
	require.Equal(t, http.StatusOK, ahpConfigurationRequest(h, map[string]string{"Gram-Key": "example", "Gram-Project": "chosen"}).Code)
	requireAHPConfiguration(t, h, "fail-open")
	r := httptest.NewRequest(http.MethodPost, "/ahp", strings.NewReader(strings.Repeat("x", (1<<20)+1)))
	r.Header.Set("Content-Type", "application/json")
	for key, value := range ahpAuthHeaders {
		r.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	configuration, err := ti.service.ahpResolveConfiguration(canceled)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, configuration)
	r = httptest.NewRequest(http.MethodPost, "/ahp", strings.NewReader(`{"jsonrpc":"2.0","id":"discovery","method":"hooks/configuration","params":{"protocolVersion":"draft"}}`)).WithContext(canceled)
	r.Header.Set("Content-Type", "application/json")
	for key, value := range ahpAuthHeaders {
		r.Header.Set(key, value)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	requireAHPConfigurationUnavailable(t, w)
	ti.service.db = nil
	requireAHPConfigurationUnavailable(t, ahpConfigurationRequest(h, ahpAuthHeaders))
}

func requireAHPConfigurationUnavailable(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code)
	require.JSONEq(t, `{"jsonrpc":"2.0","id":"discovery","error":{"code":-32004,"message":"Backend internal error"}}`, w.Body.String())
}
