package mcp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/auth/deviceagenttoken"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
)

func TestOAuthMountedRoutesSetupErrors(t *testing.T) {
	t.Parallel()
	for _, surface := range []string{"public", "private", "authentication host"} {
		for _, outage := range []bool{false, true} {
			name := surface + "/missing"
			if outage {
				name = surface + "/outage"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				_, ti := newTestMCPService(t)
				router := goahttp.NewMuxer()
				var handler http.Handler = router
				host := ""
				switch surface {
				case "public":
					mcp.Attach(router, ti.service.Service, nil)
				case "private":
					mcp.AttachPrivate(router, ti.service.Service, nil)
				case "authentication host":
					handler = newAuthenticationHostHarness(t, ti).handler
					host = "auth.example.com"
				}
				if outage {
					ti.conn.Close()
				}
				bases := []string{"/mcp/missing"}
				if surface != "private" {
					bases = append(bases, "/oauth/usi/"+uuid.NewString(), "/mcp/gateway/missing")
				}
				for _, base := range bases {
					for _, suffix := range []string{"/token", "/revoke"} {
						request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, base+suffix, nil)
						if host != "" {
							request.Host = host
						}
						response := httptest.NewRecorder()
						handler.ServeHTTP(response, request)
						status := http.StatusNotFound
						code := oautherr.CodeInvalidRequest
						if outage {
							status = http.StatusInternalServerError
							code = oautherr.CodeServerError
						}
						require.Equal(t, status, response.Code, base+suffix+": "+response.Body.String())
						var body oautherr.RFC6749Error
						require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
						require.Equal(t, code, body.Code)
						require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
						require.Equal(t, "no-cache", response.Header().Get("Pragma"))
					}
				}
			})
		}
	}
}

func TestOAuthMountedSharedMissingGrantType(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fixture := seedSharedIssuer(t, ctx, ti)
	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service.Service, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequestWithContext(ctx, http.MethodPost, fixture.issuerURL+"/token", nil))
	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"error":"invalid_request"`)
}

func TestOAuthMountedDeviceAgentDatabaseFailure(t *testing.T) {
	t.Parallel()
	fixture := newDeviceAgentGrantFixture(t)
	ti := fixture.ti
	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service.Service, nil)
	ti.conn.Close()
	form := url.Values{oauthwire.ParamGrantType: {oauthwire.GrantTypeJWTBearer}, oauthwire.ParamAssertion: {fixture.assertion(t, fixture.tokenURL)}}
	request := httptest.NewRequest(http.MethodPost, deviceagenttoken.TokenPath(fixture.fx.orgID), strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"error":"temporarily_unavailable"`)
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
}
