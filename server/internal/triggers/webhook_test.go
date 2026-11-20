package triggers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/triggers"
	"github.com/speakeasy-api/gram/server/internal/triggers"
)

func TestWebhookAcknowledgesDeletedSlackTrigger(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created, err := ti.service.CreateTriggerInstance(ctx, newCreatePayload(ti.environmentID, "deleted-slack-webhook"))
	require.NoError(t, err)

	mux := goahttp.NewMuxer()
	triggers.Attach(mux, ti.service)
	deliver := func() *httptest.ResponseRecorder {
		body := strings.NewReader(`{"type":"event_callback","event_id":"Ev-test","event":{"type":"app_mention","channel":"C1","user":"U1","ts":"1.0"}}`)
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/rpc/triggers/"+created.ID+"/webhook", body)
		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, req)
		return resp
	}

	// A live trigger still requires a Slack signature.
	require.Equal(t, http.StatusUnauthorized, deliver().Code)

	err = ti.service.DeleteTriggerInstance(ctx, &gen.DeleteTriggerInstancePayload{
		ID:               created.ID,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	resp := deliver()
	require.Equal(t, http.StatusOK, resp.Code)
	require.JSONEq(t, `{"ok":true}`, resp.Body.String())
}
