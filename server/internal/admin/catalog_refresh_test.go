package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	registryrepo "github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
	"github.com/stretchr/testify/require"
)

func TestCatalogSyncRetainsVerifiedStaffOwners(t *testing.T) {
	t.Parallel()
	ctx, first, db := newTestAdminService(t)
	_, source, _ := newTestAdminService(t)
	_, err := source.registry.Create(ctx, json.RawMessage(`{"server":{"name":"example.test/owned-import","description":"Owner fixture","version":"1"}}`))
	require.NoError(t, err)
	fetcher := catalogSourceFunc(func(ctx context.Context) (mcpregistry.CatalogBundle, error) {
		return source.registry.ExportCatalog(ctx, nil)
	})
	_, second, _ := newTestAdminServiceOnDB(t, db)
	second.oidc = newTestOIDCClient(t, userinfoOK("sub-other", "other@example.com"))
	second.verifier.oidc = second.oidc
	for _, operator := range []struct {
		service *Service
		subject string
	}{{first, "sub-admin"}, {second, "sub-other"}} {
		session := catalogSession(t, operator.service, operator.subject)
		handler := operator.service.CatalogSyncHandler("local", fetcher, &mcpregistry.CatalogAssetStore{}, "production-test")
		response := catalogRequest(t, handler, session, bytes.NewBufferString(`{}`))
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	}
	plans, err := registryrepo.New(db).RefreshListPlanOwnersFixture(ctx, "production-test")
	require.NoError(t, err)
	require.Len(t, plans, 2)
	require.Equal(t, "sub-admin", plans[0].Owner)
	require.Equal(t, "sub-other", plans[1].Owner)
	for _, plan := range plans {
		require.True(t, plan.Committed, "the committed initial-import receipt must retain the verified staff owner")
	}
}

func TestCatalogRefreshHTTPExactPlanAndReplay(t *testing.T) {
	t.Parallel()
	ctx, destination, db := newTestAdminService(t)
	_, source, _ := newTestAdminService(t)
	original := json.RawMessage(`{"server":{"name":"example.test/refresh","description":"Original","version":"1"}}`)
	upstream, err := source.registry.Create(ctx, original)
	require.NoError(t, err)
	calls := 0
	fetcher := catalogSourceFunc(func(ctx context.Context) (mcpregistry.CatalogBundle, error) {
		calls++
		return source.registry.ExportCatalog(ctx, nil)
	})
	store := &mcpregistry.CatalogAssetStore{}
	session := catalogSession(t, destination, "sub-admin")
	// The configured initial-import path records its baseline atomically.
	imported := catalogRequest(t, destination.CatalogSyncHandler("local", fetcher, store, "production-test"), session, bytes.NewBufferString(`{}`))
	require.Equal(t, http.StatusOK, imported.Code, imported.Body.String())
	handler := destination.CatalogRefreshHandler("local", "production-test", fetcher, store)
	send := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		return catalogRequest(t, handler, session, bytes.NewBufferString(body))
	}
	preview := func() mcpregistry.CatalogRefreshPlan {
		t.Helper()
		response := send(`{"action":"preview"}`)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		var plan mcpregistry.CatalogRefreshPlan
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &plan))
		return plan
	}
	updated := json.RawMessage(`{"server":{"name":"example.test/refresh","description":"Upstream update","version":"2"}}`)
	upstream, err = source.registry.Save(ctx, upstream.ID, mcpregistry.Token(upstream), updated)
	require.NoError(t, err)
	plan := preview()
	require.Contains(t, plan.Changes, mcpregistry.CatalogRefreshChange{Name: "example.test/refresh", Action: "update", Reason: ""})
	apply := fmt.Sprintf(`{"action":"apply","planId":%q}`, plan.ID.String())
	// A different live, verified staff member cannot confirm another owner's
	// plan. Staff identity comes from verification, not the request payload.
	_, other, _ := newTestAdminServiceOnDB(t, db)
	other.oidc = newTestOIDCClient(t, userinfoOK("sub-other", "other@example.com"))
	other.verifier.oidc = other.oidc
	otherSession := catalogSession(t, other, "sub-other")
	beforeOther := calls
	unauthorized := catalogRequest(t, other.CatalogRefreshHandler("local", "production-test", fetcher, store), otherSession, bytes.NewBufferString(apply))
	require.Equal(t, http.StatusNotFound, unauthorized.Code, unauthorized.Body.String())
	require.Equal(t, beforeOther, calls)
	response := send(apply)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var receipt mcpregistry.CatalogRefreshReceipt
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &receipt))
	require.Equal(t, "completed", receipt.State)
	current, err := destination.registry.GetByName(ctx, "example.test/refresh")
	require.NoError(t, err)
	require.Contains(t, string(current.Data), "Upstream update")
	// A receipt is authoritative before any new source fetch: replay remains
	// successful after the source changes, with no new writes or requests.
	_, err = source.registry.Save(ctx, upstream.ID, mcpregistry.Token(upstream), original)
	require.NoError(t, err)
	beforeReplay := calls
	replayed := send(apply)
	require.Equal(t, http.StatusOK, replayed.Code, replayed.Body.String())
	require.JSONEq(t, response.Body.String(), replayed.Body.String())
	require.Equal(t, beforeReplay, calls)
	// A destination edit after preview invalidates exact confirmation.
	stale := preview()
	_, err = destination.registry.Save(ctx, current.ID, mcpregistry.Token(current), json.RawMessage(`{"server":{"name":"example.test/refresh","description":"Local testing","version":"3"}}`))
	require.NoError(t, err)
	response = send(fmt.Sprintf(`{"action":"apply","planId":%q}`, stale.ID.String()))
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	current, err = destination.registry.GetByName(ctx, "example.test/refresh")
	require.NoError(t, err)
	require.Contains(t, string(current.Data), "Local testing")
	status := send(`{"action":"status"}`)
	require.Equal(t, http.StatusOK, status.Code, status.Body.String())
	require.NotContains(t, status.Body.String(), "access-token")
	var beforeConflicts mcpregistry.CatalogRefreshStatus
	require.NoError(t, json.Unmarshal(status.Body.Bytes(), &beforeConflicts))
	require.NotNil(t, beforeConflicts.LastFullSuccessAt)
	// Confirmation applies the exact safe subset, without pretending that an
	// unresolved local/upstream conflict was reconciled successfully.
	_, err = source.registry.Create(ctx, json.RawMessage(`{"server":{"name":"example.test/addition","description":"New upstream entry","version":"1"}}`))
	require.NoError(t, err)
	mixed := preview()
	actions := map[string]string{}
	for _, change := range mixed.Changes {
		actions[change.Name] = change.Action
	}
	require.Equal(t, "conflict", actions["example.test/refresh"])
	require.Equal(t, "create", actions["example.test/addition"])
	response = send(fmt.Sprintf(`{"action":"apply","planId":%q}`, mixed.ID.String()))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &receipt))
	require.Equal(t, "completed_with_conflicts", receipt.State)
	_, err = destination.registry.GetByName(ctx, "example.test/addition")
	require.NoError(t, err)
	current, err = destination.registry.GetByName(ctx, "example.test/refresh")
	require.NoError(t, err)
	require.Contains(t, string(current.Data), "Local testing")
	status = send(`{"action":"status"}`)
	require.Equal(t, http.StatusOK, status.Code, status.Body.String())
	var afterConflicts mcpregistry.CatalogRefreshStatus
	require.NoError(t, json.Unmarshal(status.Body.Bytes(), &afterConflicts))
	require.Equal(t, beforeConflicts.LastFullSuccessAt, afterConflicts.LastFullSuccessAt)
}

func TestCatalogRefreshSafetyBoundary(t *testing.T) {
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
		response := catalogRequest(t, s.CatalogRefreshHandler(environment, "production-test", source, store), session, body)
		require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
		require.False(t, body.read)
	}
	handler := s.CatalogRefreshHandler("local", "production-test", source, store)
	for _, credential := range []string{"", "not-a-session", catalogSession(t, s, "wrong-subject")} {
		response := catalogRequest(t, handler, credential, bytes.NewBufferString(`{"action":"preview"}`))
		require.Equal(t, http.StatusUnauthorized, response.Code, response.Body.String())
	}
	for _, body := range []string{
		`{}`, `null`, `[]`, `{"action":"reset"}`, `{"action":"apply"}`,
		`{"action":"apply","planId":"bad"}`, `{"action":"apply","planId":"00000000-0000-0000-0000-000000000000"}`,
		`{"action":"preview","planId":"018f0493-938a-7010-97c2-6fa1aaa88d05"}`,
		`{"action":"status","planId":"018f0493-938a-7010-97c2-6fa1aaa88d05"}`,
		`{"action":"preview","owner":"sub-admin"}`, `{"action":"preview","sourceIdentity":"production"}`,
		`{"action":"preview","source_url":"https://example.invalid"}`, `{"action":"preview","environment":"local"}`,
		`{"action":"preview"} {}`, `{"action":"` + strings.Repeat("x", 1024) + `"}`,
	} {
		response := catalogRequest(t, handler, session, bytes.NewBufferString(body))
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	}
	for _, origin := range []string{"https://untrusted.example.test", ""} {
		r := httptest.NewRequest(http.MethodPost, "https://admin.example.test"+CatalogRefreshPath, bytes.NewBufferString(`{"action":"preview"}`))
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: session})
		response := httptest.NewRecorder()
		SessionMiddleware(handler).ServeHTTP(response, r)
		require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
	}
	for _, contentType := range []string{"", "text/plain", "application/json; malformed"} {
		r := httptest.NewRequest(http.MethodPost, "https://admin.example.test"+CatalogRefreshPath, bytes.NewBufferString(`{"action":"preview"}`))
		r.Header.Set("Origin", "https://admin.example.test")
		r.Header.Set("Content-Type", contentType)
		r.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: session})
		response := httptest.NewRecorder()
		SessionMiddleware(handler).ServeHTTP(response, r)
		require.Equal(t, http.StatusBadRequest, response.Code)
	}
	bearer := httptest.NewRequest(http.MethodPost, "https://admin.example.test"+CatalogRefreshPath, bytes.NewBufferString(`{"action":"preview"}`))
	bearer.Header.Set("Origin", "https://admin.example.test")
	bearer.Header.Set("Authorization", "Bearer synthetic-export-token")
	bearerResponse := httptest.NewRecorder()
	SessionMiddleware(handler).ServeHTTP(bearerResponse, bearer)
	require.Equal(t, http.StatusUnauthorized, bearerResponse.Code)
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		r := httptest.NewRequest(method, "https://admin.example.test"+CatalogRefreshPath, nil)
		r.Header.Set("Origin", "https://admin.example.test")
		r.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: session})
		response := httptest.NewRecorder()
		SessionMiddleware(handler).ServeHTTP(response, r)
		require.Equal(t, http.StatusBadRequest, response.Code)
	}
	for _, disabled := range []http.Handler{
		s.CatalogRefreshHandler("local", "production-test", nil, store),
		s.CatalogRefreshHandler("local", "production-test", source, nil),
		s.CatalogRefreshHandler("local", "", source, store),
	} {
		response := catalogRequest(t, disabled, session, bytes.NewBufferString(`{"action":"preview"}`))
		require.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
	}
	require.Zero(t, calls, "rejected requests must never fetch the source")
}
