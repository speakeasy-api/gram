package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/stretchr/testify/require"
)

func TestWorkloadInference_RejectsClientAuthentication(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"basic", "client_id", "client_secret", "client_assertion", "client_assertion_type"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			f := newWorkloadGrantFixture(t)
			issuer := f.sharedIssuer(t)
			enableWorkloadInference(t, f)
			assertion := f.assertion(t, issuer)
			form := url.Values{"grant_type": {workloadGrantJWTBearer}, "assertion": {assertion}, "resource": {mcp.InferenceResource}, "project_id": {f.fx.target.ProjectID.String()}}
			if method != "basic" {
				form.Set(method, "")
			}
			issuerID := f.fx.target.UserSessionIssuerID.String()
			request := sharedRequest(t.Context(), http.MethodPost, "/oauth/usi/"+issuerID+"/token", issuerID, strings.NewReader(form.Encode()))
			if method == "basic" {
				request.SetBasicAuth("client", "secret")
			}
			response := httptest.NewRecorder()
			require.NoError(t, f.ti.service.HandleSharedToken(response, request))
			require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
			rejected := decodeSharedTokenResponse(t, response)
			require.Equal(t, "invalid_target", rejected.Error)
			require.Empty(t, rejected.AccessToken)
			require.Empty(t, f.workloadSessions(t))
			// Rejection must not consume the assertion or silently treat credentials as absent.
			accepted := exchangeWorkloadInference(t, f, assertion)
			require.Equal(t, http.StatusOK, accepted.Code, accepted.Body.String())
		})
	}
}
