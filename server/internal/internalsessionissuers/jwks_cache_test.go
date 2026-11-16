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
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// jwksReader serves the organization's JWKS from a fresh discovery mux and
// returns the HTTP status and the published kids.
func jwksReader(t *testing.T, ti *testInstance) func() (int, []string) {
	t.Helper()
	mux := goahttp.NewMuxer()
	internalsessionissuers.AttachDiscovery(mux, ti.conn, testenv.NewLogger(t))
	return func() (int, []string) {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/oauth/idp/org/"+ti.orgID+"/jwks", nil))
		if response.Code != http.StatusOK {
			return response.Code, nil
		}
		var set jose.JSONWebKeySet
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &set))
		out := make([]string, 0, len(set.Keys))
		for _, key := range set.Keys {
			out = append(out, key.KeyID)
		}
		return response.Code, out
	}
}

func TestDiscoveryPublishesACompromiseReplacementPromptly(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, true)
	admin := adminCtx(t, ctx)
	compromised := enable(t, ti, admin)
	read := jwksReader(t, ti)
	kids := func() []string {
		code, out := read()
		require.Equal(t, http.StatusOK, code)
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

func TestDiscoveryNoticesAKeyChangeThatKeepsUpdatedAt(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, true)
	admin := adminCtx(t, ctx)
	active := enable(t, ti, admin)
	_, err := ti.service.Rotate(admin, &gen.RotatePayload{ActiveKid: &active, Compromised: false, SessionToken: nil})
	require.NoError(t, err)
	read := jwksReader(t, ti)
	_, warm := read()
	require.Len(t, warm, 2, "the warm cache holds the active and pending keys")

	// Withdraw the active key without moving any row's updated_at, so a
	// count-and-latest-timestamp fingerprint would not change.
	withdrawn, err := testrepo.New(ti.conn).WithdrawJsonWebKeyKeepingUpdatedAtFixture(ctx, testrepo.WithdrawJsonWebKeyKeepingUpdatedAtFixtureParams{OrganizationID: ti.orgID, Kid: active})
	require.NoError(t, err)
	require.EqualValues(t, 1, withdrawn)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		_, kids := read()
		assert.NotContains(c, kids, active)
		assert.Len(c, kids, 1)
	}, 3*time.Second, 100*time.Millisecond)
}

func TestDiscoveryStopsServingADisabledOrganizationsKeys(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, true)
	enable(t, ti, adminCtx(t, ctx))
	read := jwksReader(t, ti)
	code, _ := read()
	require.Equal(t, http.StatusOK, code)

	require.NoError(t, testrepo.New(ti.conn).DisableDelegationOrganizationFixture(ctx, ti.orgID))
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		code, _ := read()
		assert.Equal(c, http.StatusNotFound, code)
	}, 3*time.Second, 100*time.Millisecond)
}
