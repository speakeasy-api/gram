package chat_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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

type catalogCompletionClient struct {
	openrouter.CompletionClient
	response map[string]json.RawMessage
	err      error
}

func (c *catalogCompletionClient) ListModels(context.Context) (map[string]json.RawMessage, error) {
	return c.response, c.err
}

func TestHandleModels(t *testing.T) {
	t.Parallel()
	upstream := map[string]json.RawMessage{"data": json.RawMessage(`[{"id":"openai/gpt-5.4","context_length":100000,"future_field":{"enabled":true}}]`)}
	ti := newTestChatServiceWithCompletion(t, chat.NewAgenticChatClient(&catalogCompletionClient{response: upstream, err: nil}))
	ctx := authztest.WithAdminGrants(testenv.InitAuthContext(t, t.Context(), ti.conn, ti.sessions))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	mux := goahttp.NewMuxer()
	chat.Attach(mux, ti.service)

	t.Run("authenticated upstream metadata", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/chat/models", nil)
		req.Header.Set(constants.SessionHeader, *authCtx.SessionID)
		req.Header.Set(constants.ProjectHeader, *authCtx.ProjectSlug)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		want, err := json.Marshal(upstream)
		require.NoError(t, err)
		require.JSONEq(t, string(want), rec.Body.String())
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

func TestHandleModelsUpstreamFailure(t *testing.T) {
	t.Parallel()
	ti := newTestChatServiceWithCompletion(t, chat.NewAgenticChatClient(&catalogCompletionClient{response: nil, err: errors.New("upstream unavailable")}))
	ctx := authztest.WithAdminGrants(testenv.InitAuthContext(t, t.Context(), ti.conn, ti.sessions))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/chat/models", nil)
	req.Header.Set(constants.SessionHeader, *authCtx.SessionID)
	req.Header.Set(constants.ProjectHeader, *authCtx.ProjectSlug)
	rec := httptest.NewRecorder()
	oops.ErrHandle(testenv.NewLogger(t), ti.service.HandleModels).ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())
	require.NotContains(t, rec.Body.String(), `"data"`)
}

func TestAgenticClientListModels(t *testing.T) {
	t.Parallel()
	response := map[string]json.RawMessage{"data": json.RawMessage(`[{"id":"supported/model","future_field":9007199254740993}]`)}
	client := chat.NewAgenticChatClient(&catalogCompletionClient{response: response, err: nil})
	got, err := client.ListModels(t.Context())
	require.NoError(t, err)
	require.Equal(t, response, got, "wrapper must preserve upstream policy and metadata unchanged")
	upstreamError := errors.New("catalog unavailable")
	client = chat.NewAgenticChatClient(&catalogCompletionClient{response: nil, err: upstreamError})
	got, err = client.ListModels(t.Context())
	require.ErrorIs(t, err, upstreamError)
	require.Nil(t, got)
	client = chat.NewAgenticChatClient(nil)
	got, err = client.ListModels(t.Context())
	require.ErrorContains(t, err, "does not support model discovery")
	require.Nil(t, got, "missing capability must not synthesize a fallback catalog")
}
