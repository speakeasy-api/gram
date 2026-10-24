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
		require.Equal(t, map[string]any{"default": "metadata", "text": "body"}, sub.Content)
		for _, event := range sub.Events {
			require.NotContains(t, events, event, "must not deliver the same event twice")
			events[event] = sub.Mode
		}
		if sub.Mode == "intercept" {
			intercepts += len(sub.Events)
			require.Equal(t, failurePolicy, sub.FailurePolicy)
			require.Equal(t, 5000, sub.TimeoutMS)
		} else {
			require.Equal(t, "observe", sub.Mode)
			require.Empty(t, sub.FailurePolicy)
			require.Zero(t, sub.TimeoutMS)
		}
	}
	require.Equal(t, map[string]string{
		"tool.before":              "intercept",
		"tool.after":               "intercept",
		"tool.permission.request":  "intercept",
		"user.message.inbound":     "intercept",
		"user.message.outbound":    "intercept",
		"model.request.before":     "intercept",
		"model.response.after":     "intercept",
		"context.compact.before":   "intercept",
		"context.compact.after":    "intercept",
		"tool.permission.resolved": "observe",
		"tool.batch.after":         "observe",
		"model.error":              "observe",
		"turn.end":                 "observe",
		"session.start":            "observe",
		"session.end":              "observe",
	}, events)
	require.Equal(t, 9, intercepts)
}

func TestAHPConfigurationDynamicPosture(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ti.service.auth = &ahpTestAuthorizer{authCtx: auth}
	h, err := ti.service.ahpHandler()
	require.NoError(t, err)
	requireAHPConfiguration(t, h, "fail-closed")
	features := featuresrepo.New(ti.conn)
	_, err = features.EnableFeature(ctx, featuresrepo.EnableFeatureParams{OrganizationID: auth.ActiveOrganizationID, FeatureName: string(productfeatures.FeatureHooksFailOpen)})
	require.NoError(t, err)
	requireAHPConfiguration(t, h, "fail-open")
	// Fail-open belongs to the authenticated organization, not server state.
	other := *auth
	otherProject := uuid.New()
	other.ProjectID = &otherProject
	other.ActiveOrganizationID = "org_other"
	ti.service.auth = &ahpTestAuthorizer{authCtx: &other}
	requireAHPConfiguration(t, h, "fail-closed")
	ti.service.auth = &ahpTestAuthorizer{authCtx: auth}
	requireAHPConfiguration(t, h, "fail-open")
	_, err = features.DeleteFeature(ctx, featuresrepo.DeleteFeatureParams{OrganizationID: auth.ActiveOrganizationID, FeatureName: string(productfeatures.FeatureHooksFailOpen)})
	require.NoError(t, err)
	requireAHPConfiguration(t, h, "fail-closed")
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
	requireAHPConfiguration(t, h, "fail-closed")
	r := httptest.NewRequest(http.MethodPost, "/ahp", strings.NewReader(strings.Repeat("x", (1<<20)+1)))
	for key, value := range ahpAuthHeaders {
		r.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	r = httptest.NewRequest(http.MethodPost, "/ahp", strings.NewReader(`{"jsonrpc":"2.0","id":"discovery","method":"hooks/configuration","params":{"protocolVersion":"draft"}}`)).WithContext(canceled)
	r.Header.Set("Content-Type", "application/json")
	for key, value := range ahpAuthHeaders {
		r.Header.Set(key, value)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.NotContains(t, w.Body.String(), "subscriptions")
	ti.service.db = nil
	require.Equal(t, http.StatusServiceUnavailable, ahpConfigurationRequest(h, ahpAuthHeaders).Code)
}
