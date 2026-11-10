package mcpauthz

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/tunnel/gateway"
	"github.com/speakeasy-api/gram/tunnel/jwks"
	"github.com/speakeasy-api/gram/tunnel/route"
	"github.com/stretchr/testify/require"
)

func servedKeys(t *testing.T, publicPEM string) jose.JSONWebKeySet {
	t.Helper()
	// The gateway receives only the public bundle, independently of the issuer.
	gw, err := gateway.New(gateway.Config{AdvertiseAddr: "", MaxStreamsPerTunnel: 0, MaxSessions: 0,
		ForwardToken: "test-forward-token", AuthzPublicKeys: publicPEM},
		gateway.NewStaticKeyStore(map[string]string{}), route.NewRouteTable(), testenv.NewLogger(t))
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		gw.Drain(ctx)
	})
	w := httptest.NewRecorder()
	gw.PublicHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, jwks.Path, nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "public, max-age=300, must-revalidate", w.Header().Get("Cache-Control"))
	var keys jose.JSONWebKeySet
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &keys))
	var raw struct {
		Keys []map[string]any `json:"keys"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	for _, key := range raw.Keys {
		for _, secret := range []string{"d", "p", "q", "dp", "dq", "qi", "oth"} {
			require.NotContains(t, key, secret)
		}
	}
	return keys
}
