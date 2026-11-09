package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/stretchr/testify/require"
)

func TestCatalogResetSafetyBoundary(t *testing.T) {
	t.Parallel()
	_, s, _ := newTestAdminService(t)
	session := catalogSession(t, s, "sub-admin")
	calls := 0
	source := catalogSourceFunc(func(context.Context) (mcpregistry.CatalogBundle, error) {
		calls++
		return mcpregistry.CatalogBundle{}, nil
	})
	store := &mcpregistry.CatalogAssetStore{}
	for _, environment := range []string{"prod", "production", "", "preview", "staging"} {
		body := &catalogUnreadBody{}
		response := catalogRequest(t, s.CatalogResetHandler(environment, "production-test", "local https://admin.example.test", source, store), session, body)
		require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
		require.False(t, body.read)
	}
	handler := s.CatalogResetHandler("local", "production-test", "local https://admin.example.test", source, store)
	for _, credential := range []string{"", "not-a-session", catalogSession(t, s, "wrong-subject")} {
		response := catalogRequest(t, handler, credential, bytes.NewBufferString(`{"action":"preview"}`))
		require.Equal(t, http.StatusUnauthorized, response.Code, response.Body.String())
	}
	for _, body := range []string{
		`{"action":"preview","confirmation":null}`, `{"action":"status","planId":null}`, `{"action":"preview","confirmation":""}`, `{ "action":"status", "planId":""}`, `{ "action":"preview", "destination":"other"}`, `{}`, `null`, `[]`, `{"action":"reset"}`, `{"action":"apply"}`,
		`{"action":"apply","planId":"bad"}`, `{"action":"apply","planId":"00000000-0000-0000-0000-000000000000"}`,
		`{"action":"preview","planId":"018f0493-938a-7010-97c2-6fa1aaa88d05"}`,
		`{"action":"status","planId":"018f0493-938a-7010-97c2-6fa1aaa88d05"}`,
		`{"action":"preview","owner":"sub-admin"}`, `{"action":"preview","sourceIdentity":"production"}`,
		`{"action":"preview","source_url":"https://example.invalid"}`, `{"action":"preview","environment":"local"}`,
		`{"action":"preview"} {}`, `{"action":"` + strings.Repeat("x", 4096) + `"}`,
	} {
		response := catalogRequest(t, handler, session, bytes.NewBufferString(body))
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	}
	for _, origin := range []string{"https://untrusted.example.test", ""} {
		r := httptest.NewRequest(http.MethodPost, "https://admin.example.test"+CatalogResetPath, bytes.NewBufferString(`{"action":"preview"}`))
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: session})
		response := httptest.NewRecorder()
		SessionMiddleware(handler).ServeHTTP(response, r)
		require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
	}
	for _, contentType := range []string{"", "text/plain", "application/json; malformed"} {
		r := httptest.NewRequest(http.MethodPost, "https://admin.example.test"+CatalogResetPath, bytes.NewBufferString(`{"action":"preview"}`))
		r.Header.Set("Origin", "https://admin.example.test")
		r.Header.Set("Content-Type", contentType)
		r.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: session})
		response := httptest.NewRecorder()
		SessionMiddleware(handler).ServeHTTP(response, r)
		require.Equal(t, http.StatusBadRequest, response.Code)
	}
	bearer := httptest.NewRequest(http.MethodPost, "https://admin.example.test"+CatalogResetPath, bytes.NewBufferString(`{"action":"preview"}`))
	bearer.Header.Set("Origin", "https://admin.example.test")
	bearer.Header.Set("Authorization", "Bearer synthetic-export-token")
	bearerResponse := httptest.NewRecorder()
	SessionMiddleware(handler).ServeHTTP(bearerResponse, bearer)
	require.Equal(t, http.StatusUnauthorized, bearerResponse.Code)
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		r := httptest.NewRequest(method, "https://admin.example.test"+CatalogResetPath, nil)
		r.Header.Set("Origin", "https://admin.example.test")
		r.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: session})
		response := httptest.NewRecorder()
		SessionMiddleware(handler).ServeHTTP(response, r)
		require.Equal(t, http.StatusBadRequest, response.Code)
	}
	for _, disabled := range []http.Handler{
		s.CatalogResetHandler("local", "production-test", "local https://admin.example.test", nil, store),
		s.CatalogResetHandler("local", "production-test", "local https://admin.example.test", source, nil),
		s.CatalogResetHandler("local", "", "local https://admin.example.test", source, store),
		s.CatalogResetHandler("local", "production-test", "", source, store),
	} {
		response := catalogRequest(t, disabled, session, bytes.NewBufferString(`{"action":"preview"}`))
		require.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
	}
	require.Zero(t, calls, "rejected requests must never fetch the source")
}

func TestCatalogResetHTTPExactPlanAndOutage(t *testing.T) {
	t.Parallel()
	ctx, destination, _ := newTestAdminService(t)
	var audit bytes.Buffer
	destination.logger = slog.New(slog.NewJSONHandler(&audit, nil))
	_, source, _ := newTestAdminService(t)
	_, err := source.registry.Create(ctx, json.RawMessage(`{"server":{"name":"example.test/reset","description":"Canonical","version":"1"}}`))
	require.NoError(t, err)
	_, err = destination.registry.Create(ctx, json.RawMessage(`{"server":{"name":"example.test/local-only","description":"Local testing","version":"1"}}`))
	require.NoError(t, err)
	calls := 0
	outage := false
	fetcher := catalogSourceFunc(func(ctx context.Context) (mcpregistry.CatalogBundle, error) {
		calls++
		if outage {
			return mcpregistry.CatalogBundle{}, errors.New("secret-source-token")
		}
		return source.registry.ExportCatalog(ctx, nil)
	})
	store := &mcpregistry.CatalogAssetStore{}
	session := catalogSession(t, destination, "sub-admin")
	handler := destination.CatalogResetHandler("local", "production-test", "local https://admin.example.test", fetcher, store)
	send := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		return catalogRequest(t, handler, session, bytes.NewBufferString(body))
	}
	preview := send(`{"action":"preview"}`)
	require.Equal(t, http.StatusOK, preview.Code, preview.Body.String())
	require.Equal(t, "no-store", preview.Header().Get("Cache-Control"))
	var plan mcpregistry.CatalogRefreshPlan
	require.NoError(t, json.Unmarshal(preview.Body.Bytes(), &plan))
	require.Equal(t, "reset", plan.Operation)
	require.Equal(t, "local https://admin.example.test", plan.Destination)
	require.NotEmpty(t, plan.Confirmation)
	require.NotEmpty(t, plan.ScopeWarning)
	apply := func(confirmation string) string {
		return fmt.Sprintf(`{"action":"apply","planId":%q,"confirmation":%q}`, plan.ID.String(), confirmation)
	}
	before := calls
	for _, confirmation := range []string{"wrong confirmation", plan.Confirmation + " "} {
		wrong := send(apply(confirmation))
		require.Equal(t, http.StatusConflict, wrong.Code, wrong.Body.String())
		require.Equal(t, before, calls, "confirmation must be validated before source fetch")
	}
	changedDestination := destination.CatalogResetHandler("local", "production-test", "local https://another.example.test", fetcher, store)
	wrongDestination := catalogRequest(t, changedDestination, session, bytes.NewBufferString(apply(plan.Confirmation)))
	require.Equal(t, http.StatusConflict, wrongDestination.Code, wrongDestination.Body.String())
	require.Equal(t, before, calls)
	unknown := send(`{"action":"apply","planId":"018f0493-938a-7010-97c2-6fa1aaa88d05","confirmation":"unknown"}`)
	require.Equal(t, http.StatusNotFound, unknown.Code, unknown.Body.String())
	changedSource := destination.CatalogResetHandler("local", "another-source", "local https://admin.example.test", fetcher, store)
	invalid := catalogRequest(t, changedSource, session, bytes.NewBufferString(apply(plan.Confirmation)))
	require.Equal(t, http.StatusNotFound, invalid.Code, invalid.Body.String())
	outage = true
	unavailable := send(apply(plan.Confirmation))
	require.GreaterOrEqual(t, unavailable.Code, 400, unavailable.Body.String())
	require.NotContains(t, unavailable.Body.String(), "secret-source-token")
	_, err = destination.registry.GetByName(ctx, "example.test/local-only")
	require.NoError(t, err, "source outage must preserve local catalog")
	previewOutage := send(`{"action":"preview"}`)
	require.GreaterOrEqual(t, previewOutage.Code, 400)
	require.NotContains(t, previewOutage.Body.String(), "secret-source-token")
	outage = false
	// A fresh preview is required after any failed attempt.
	preview = send(`{"action":"preview"}`)
	require.Equal(t, http.StatusOK, preview.Code, preview.Body.String())
	require.NoError(t, json.Unmarshal(preview.Body.Bytes(), &plan))
	applied := send(apply(plan.Confirmation))
	require.Equal(t, http.StatusOK, applied.Code, applied.Body.String())
	var receipt mcpregistry.CatalogRefreshReceipt
	require.NoError(t, json.Unmarshal(applied.Body.Bytes(), &receipt))
	require.Equal(t, "reset", receipt.Operation)
	require.Equal(t, plan.Destination, receipt.Destination)
	retained, err := destination.registry.GetByName(ctx, "example.test/local-only")
	require.NoError(t, err)
	require.False(t, retained.Published, "destination-only records are retained but unpublished")
	_, err = destination.registry.GetByName(ctx, "example.test/reset")
	require.NoError(t, err)
	beforeReplay := calls
	outage = true
	replay := send(apply(plan.Confirmation))
	require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
	require.JSONEq(t, applied.Body.String(), replay.Body.String())
	require.Equal(t, beforeReplay, calls)
	status := send(`{"action":"status"}`)
	require.Equal(t, http.StatusOK, status.Code, status.Body.String())
	require.NotContains(t, status.Body.String(), "secret-source-token")
	for _, action := range []string{"catalog.reset.preview", "catalog.reset.apply", "catalog.reset.status"} {
		require.Contains(t, audit.String(), action)
	}
	require.NotContains(t, audit.String(), "secret-source-token")
	require.NotContains(t, audit.String(), plan.Confirmation)
}
