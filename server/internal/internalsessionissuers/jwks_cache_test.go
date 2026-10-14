package internalsessionissuers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/internal_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/internalsessionissuers"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestDiscoveryPublishesACompromiseReplacementPromptly(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, true)
	admin := adminCtx(t, ctx)
	compromised := enable(t, ti, admin)
	mux := goahttp.NewMuxer()
	internalsessionissuers.AttachDiscovery(mux, ti.conn, testenv.NewLogger(t))
	kids := func() []string {
		t.Helper()
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/oauth/idp/org/"+ti.orgID+"/jwks", nil))
		require.Equal(t, http.StatusOK, response.Code)
		var set jose.JSONWebKeySet
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &set))
		out := make([]string, 0, len(set.Keys))
		for _, key := range set.Keys {
			out = append(out, key.KeyID)
		}
		return out
	}
	require.Equal(t, []string{compromised}, kids(), "the warm cache holds the compromised key")

	view, err := ti.service.Rotate(admin, &gen.RotatePayload{ActiveKid: &compromised, Compromised: true, SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, view.Keys, 1)
	replacement := view.Keys[0].Kid

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Equal(c, []string{replacement}, kids())
	}, 3*time.Second, 100*time.Millisecond, "the key set reflects the replacement within the revalidation interval, well before the document TTL")
}
