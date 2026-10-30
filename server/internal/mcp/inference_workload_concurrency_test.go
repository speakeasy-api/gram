package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/stretchr/testify/require"
)

func TestWorkloadInference_ConcurrentAssertionExchange(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	issuer := f.sharedIssuer(t)
	enableWorkloadInference(t, f)
	assertion := f.assertion(t, issuer)
	form := url.Values{
		"grant_type": {workloadGrantJWTBearer}, "assertion": {assertion},
		"resource": {mcp.InferenceResource}, "project_id": {f.fx.target.ProjectID.String()},
	}.Encode()
	const attempts = 8
	type result struct {
		response *httptest.ResponseRecorder
		err      error
	}
	results := make([]result, attempts)
	start := make(chan struct{})
	var wg, ready sync.WaitGroup
	ready.Add(attempts)
	for i := range attempts {
		wg.Go(func() {
			ready.Done()
			<-start
			w := httptest.NewRecorder()
			issuerID := f.fx.target.UserSessionIssuerID.String()
			err := f.ti.service.HandleSharedToken(w, sharedRequest(t.Context(), http.MethodPost, "/oauth/usi/"+issuerID+"/token", issuerID, strings.NewReader(form)))
			results[i] = result{response: w, err: err}
		})
	}
	ready.Wait()
	close(start)
	wg.Wait()
	successes := 0
	var originalToken string
	for _, result := range results {
		require.NoError(t, result.err)
		response := decodeSharedTokenResponse(t, result.response)
		if result.response.Code == http.StatusOK {
			successes++
			require.NotEmpty(t, response.AccessToken)
			require.Empty(t, response.RefreshToken)
			originalToken = response.AccessToken
		} else {
			require.Equal(t, http.StatusBadRequest, result.response.Code, result.response.Body.String())
			require.Equal(t, "invalid_grant", response.Error)
			require.Empty(t, response.AccessToken)
		}
	}
	require.Equal(t, 1, successes, "one assertion may mint exactly one session under contention")
	require.Len(t, f.workloadSessions(t), 1)

	// Re-exchanging a fresh assertion is the renewal boundary; replay rejection
	// must neither poison fresh exchanges nor revoke the already issued token.
	renewedToken := requireWorkloadInferenceToken(t, f, issuer)
	require.NotEqual(t, originalToken, renewedToken)
	for _, token := range []string{originalToken, renewedToken} {
		_, handled, err := f.ti.service.AuthorizeWorkloadInference(t.Context(), token)
		require.True(t, handled)
		require.NoError(t, err)
	}
	require.Len(t, f.workloadSessions(t), 2)
}
