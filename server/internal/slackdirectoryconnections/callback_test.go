package slackdirectoryconnections_test

import (
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCallbackBrowserRedirects(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	mux := goahttp.NewMuxer()
	slackdirectoryconnections.Attach(mux, f.service)
	req := httptest.NewRequest(http.MethodGet, slackdirectoryconnections.CallbackPath, nil)
	out := httptest.NewRecorder()
	mux.ServeHTTP(out, req)
	require.Equal(t, http.StatusSeeOther, out.Code)
	require.Equal(t, "https://dashboard.example/login", out.Header().Get("Location"))
	require.Equal(t, "no-store", out.Header().Get("Cache-Control"))

	authenticated := contextvalues.SetSessionTokenInContext(ctx, *f.auth.SessionID)
	req = httptest.NewRequest(http.MethodGet, slackdirectoryconnections.CallbackPath+"?state=invalid&code=unused", nil).WithContext(authenticated)
	out = httptest.NewRecorder()
	mux.ServeHTTP(out, req)
	require.Equal(t, http.StatusSeeOther, out.Code)
	require.Contains(t, out.Header().Get("Location"), "slack_result=invalid_state")
	require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
}
