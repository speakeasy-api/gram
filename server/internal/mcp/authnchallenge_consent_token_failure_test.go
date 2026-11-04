package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func TestServeConsentMCP_ConfiguredChainPreservesTokenFailure(t *testing.T) {
	t.Parallel()
	for _, self := range []bool{false, true} {
		for _, unavailable := range []bool{false, true} {
			name := "remote-session"
			if self {
				name = "client-credential"
			}
			if unavailable {
				name += "-unavailable"
			} else {
				name += "-misconfigured"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				ctx, fx, issuer := seedChainedConsentEndpoint(t, "consent-"+name)
				g := setConsentGovernor(t, fx, issuer, consentChainedUpstream)
				if self {
					markConsentClientSelf(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, fx.clientA)
					sourceErr := remotesessions.ErrTokenEndpointConfiguration
					if unavailable {
						sourceErr = &remotesessions.TokenEndpointError{Transport: true}
					}
					fx.ti.clientCredentials.use(&scriptedClientCredentials{err: sourceErr})
				} else {
					upsertExpiredRefreshableRemoteSession(t, ctx, fx.ti, fx.shared, fx.clientA, fx.subject)
					pointRemoteSessionTokenEndpoint(t, ctx, fx.ti, requireProjectAuthContext(t, ctx), fx.clientA, func(w http.ResponseWriter, _ *http.Request) {
						if unavailable {
							w.WriteHeader(http.StatusServiceUnavailable)
							return
						}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusBadRequest)
						_, _ = w.Write([]byte(`{"error":"unauthorized_client"}`))
					})
				}

				req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.endpoint.Slug+"/connect/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)).WithContext(ctx)
				req.Header.Set("Accept", "application/json, text/event-stream")
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Gram-Consent-State", fx.stateID)
				req.Header.Set("Gram-Consent-Csrf", "csrf-token")
				req.Header.Set("Gram-Consent-Inventory-Attempt", uuid.NewString())
				w := httptest.NewRecorder()
				err := fx.ti.service.ServeConsentMCP(w, req, fx.endpoint)
				var failure *oops.ShareableError
				require.ErrorAs(t, err, &failure)
				if unavailable {
					require.Equal(t, oops.CodeUnavailable, failure.Code)
					require.Equal(t, http.StatusServiceUnavailable, failure.HTTPStatus(ctx))
					require.Equal(t, "30", w.Header().Get("Retry-After"))
				} else {
					require.Equal(t, oops.CodeFailedPrecondition, failure.Code)
					require.Equal(t, oops.StatusCodes[oops.CodeFailedPrecondition], failure.HTTPStatus(ctx))
					require.Empty(t, w.Header().Get("Retry-After"))
					if self {
						require.Contains(t, failure.Error(), "Contact the MCP server administrator")
					}
				}
				g.mu.Lock()
				defer g.mu.Unlock()
				require.Empty(t, g.acquired, "token resolution failures must not acquire a chained credential")
				require.Empty(t, g.signInIDs, "AcquireConsent must not run")
			})
		}
	}
}
