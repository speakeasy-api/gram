package chat_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

func TestHandleModels(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := authztest.WithAdminGrants(testenv.InitAuthContext(t, t.Context(), ti.conn, ti.sessions))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	mux := goahttp.NewMuxer()
	chat.Attach(mux, ti.service)

	t.Run("authenticated sorted exact catalog", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/chat/models", nil)
		req.Header.Set(constants.SessionHeader, *authCtx.SessionID)
		req.Header.Set(constants.ProjectHeader, *authCtx.ProjectSlug)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		var got struct {
			Object string `json:"object"`
			Data   []struct {
				ID      string `json:"id"`
				Object  string `json:"object"`
				OwnedBy string `json:"owned_by"`
			} `json:"data"`
		}
		decoder := json.NewDecoder(rec.Body)
		decoder.DisallowUnknownFields()
		require.NoError(t, decoder.Decode(&got))
		require.Equal(t, "list", got.Object)
		ids := make([]string, 0, len(got.Data))
		for _, model := range got.Data {
			require.Equal(t, "model", model.Object)
			provider, _, _ := strings.Cut(model.ID, "/")
			require.Equal(t, provider, model.OwnedBy)
			ids = append(ids, model.ID)
		}
		require.Equal(t, openrouter.AllowedModels(), ids)
	})

	for _, token := range []string{"", "invalid-session"} {
		t.Run("refuses session "+token, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/chat/models", nil)
			req.Header.Set(constants.SessionHeader, token)
			rec := httptest.NewRecorder()
			oops.ErrHandle(testenv.NewLogger(t), ti.service.HandleModels).ServeHTTP(rec, req)
			require.Equal(t, http.StatusUnauthorized, rec.Code)
			require.NotContains(t, rec.Body.String(), `"data"`)
		})
	}

	t.Run("refuses unknown project", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/chat/models", nil)
		req.Header.Set(constants.SessionHeader, *authCtx.SessionID)
		req.Header.Set(constants.ProjectHeader, "unknown-project")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
		require.NotContains(t, rec.Body.String(), `"data"`)
	})
}
