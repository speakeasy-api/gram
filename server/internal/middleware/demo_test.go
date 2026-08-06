package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
)

func TestDemoOrgWriteGuard(t *testing.T) {
	t.Parallel()

	const demoOrgID = "org_gram_demo_workspace"

	orgBySession := map[string]string{
		"demo-token": demoOrgID,
		"real-token": "org_customer",
	}
	guard := middleware.DemoOrgWriteGuard(demoOrgID, func(_ context.Context, token string) (string, bool) {
		org, ok := orgBySession[token]
		return org, ok
	})

	handler := guard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name       string
		method     string
		path       string
		token      string
		wantStatus int
	}{
		{name: "demo session blocked on create", method: http.MethodPost, path: "/rpc/toolsets.createToolset", token: "demo-token", wantStatus: http.StatusForbidden},
		{name: "demo session blocked on delete method", method: http.MethodDelete, path: "/rpc/keys.revokeKey", token: "demo-token", wantStatus: http.StatusForbidden},
		{name: "demo session blocked on update", method: http.MethodPost, path: "/rpc/skills.updateSkill", token: "demo-token", wantStatus: http.StatusForbidden},
		{name: "demo session allowed POST read", method: http.MethodPost, path: "/rpc/telemetry.query", token: "demo-token", wantStatus: http.StatusOK},
		{name: "demo session allowed POST search", method: http.MethodPost, path: "/rpc/telemetry.searchLogs", token: "demo-token", wantStatus: http.StatusOK},
		{name: "demo session allowed GET", method: http.MethodGet, path: "/rpc/toolsets.listToolsets", token: "demo-token", wantStatus: http.StatusOK},
		{name: "demo session allowed auth exit", method: http.MethodPost, path: "/rpc/auth.switchScopes", token: "demo-token", wantStatus: http.StatusOK},
		{name: "demo session allowed auth logout", method: http.MethodPost, path: "/rpc/auth.logout", token: "demo-token", wantStatus: http.StatusOK},
		{name: "real org session unaffected", method: http.MethodPost, path: "/rpc/toolsets.createToolset", token: "real-token", wantStatus: http.StatusOK},
		{name: "unknown session unaffected", method: http.MethodPost, path: "/rpc/toolsets.createToolset", token: "missing", wantStatus: http.StatusOK},
		{name: "no session token unaffected", method: http.MethodPost, path: "/rpc/toolsets.createToolset", token: "", wantStatus: http.StatusOK},
		{name: "non-rpc path unaffected", method: http.MethodPost, path: "/mcp/foo", token: "demo-token", wantStatus: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(tt.method, tt.path, nil)
			if tt.token != "" {
				req = req.WithContext(contextvalues.SetSessionTokenInContext(req.Context(), tt.token))
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			require.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}
