package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHandleCallback_ServesSpeakeasyBrandedPage(t *testing.T) {
	t.Parallel()

	l := &Listener{
		server:   nil,
		listener: nil,
		result:   make(chan CallbackResult, 1),
		errChan:  make(chan error, 1),
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/callback?api_key=key&project=proj", nil)
	l.handleCallback(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, CallbackResult{APIKey: "key", Project: "proj"}, <-l.result)

	body := rec.Body.String()
	require.Contains(t, body, `aria-label="Speakeasy"`)
	require.NotContains(t, body, `viewBox="0 0 37 45"`, "the success page still shows the old logo")
}
