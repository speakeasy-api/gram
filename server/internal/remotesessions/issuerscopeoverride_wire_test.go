// scope_override on the wire: the generated client sends [] and null apart,
// and the server decodes them into clear and keep.

package remotesessions_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	orgissuersclient "github.com/speakeasy-api/gram/server/gen/http/organization_remote_session_issuers/client"
	orgissuersserver "github.com/speakeasy-api/gram/server/gen/http/organization_remote_session_issuers/server"
	issuersclient "github.com/speakeasy-api/gram/server/gen/http/remote_session_issuers/client"
	issuersserver "github.com/speakeasy-api/gram/server/gen/http/remote_session_issuers/server"
	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	gen "github.com/speakeasy-api/gram/server/gen/remote_session_issuers"
	"github.com/speakeasy-api/gram/server/gen/types"
)

// encodeUpdate runs the generated client encoder and returns the request
// body bytes it put on the wire.
func encodeUpdate(t *testing.T, path string, encode func(*http.Request, any) error, payload any) []byte {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, nil)
	require.NoError(t, encode(req, payload))
	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	return body
}

// decodeUpdate feeds body through the generated server decoder.
func decodeUpdate[P any](t *testing.T, path string, decode func(goahttp.Muxer, func(*http.Request) goahttp.Decoder) func(*http.Request) (P, error), body []byte) P {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	payload, err := decode(goahttp.NewMuxer(), goahttp.RequestDecoder)(req)
	require.NoError(t, err)
	return payload
}

// scopeOverrideWireTier is one tenancy tier's round trip: client encoder →
// bytes → server decoder → handler, returning the wire bytes, the decoded
// override, and the handler result.
type scopeOverrideWireTier struct {
	name   string
	create func(ctx context.Context, ti *testInstance, slug string, override []string) (*types.RemoteSessionIssuer, error)
	update func(t *testing.T, ctx context.Context, ti *testInstance, id string, override []string) (wire []byte, decoded []string, result *types.RemoteSessionIssuer)
}

func scopeOverrideWireTiers() []scopeOverrideWireTier {
	return []scopeOverrideWireTier{
		{
			name: "project",
			create: func(ctx context.Context, ti *testInstance, slug string, override []string) (*types.RemoteSessionIssuer, error) {
				payload := newIssuerPayload(slug)
				payload.ScopeOverride = override
				return ti.service.CreateRemoteSessionIssuer(ctx, payload)
			},
			update: func(t *testing.T, ctx context.Context, ti *testInstance, id string, override []string) ([]byte, []string, *types.RemoteSessionIssuer) {
				t.Helper()
				path := issuersclient.UpdateRemoteSessionIssuerRemoteSessionIssuersPath()
				wire := encodeUpdate(t, path, issuersclient.EncodeUpdateRemoteSessionIssuerRequest(goahttp.RequestEncoder), &gen.UpdateRemoteSessionIssuerPayload{ID: id, ScopeOverride: override})
				payload := decodeUpdate(t, path, issuersserver.DecodeUpdateRemoteSessionIssuerRequest, wire)
				result, err := ti.service.UpdateRemoteSessionIssuer(ctx, payload)
				require.NoError(t, err)
				return wire, payload.ScopeOverride, result
			},
		},
		{
			name: "organization",
			create: func(ctx context.Context, ti *testInstance, slug string, override []string) (*types.RemoteSessionIssuer, error) {
				payload := newCreateIssuerPayload(slug, nil)
				payload.ScopeOverride = override
				return ti.service.CreateIssuer(ctx, payload)
			},
			update: func(t *testing.T, ctx context.Context, ti *testInstance, id string, override []string) ([]byte, []string, *types.RemoteSessionIssuer) {
				t.Helper()
				path := orgissuersclient.UpdateIssuerOrganizationRemoteSessionIssuersPath()
				wire := encodeUpdate(t, path, orgissuersclient.EncodeUpdateIssuerRequest(goahttp.RequestEncoder), &orgissuersgen.UpdateIssuerPayload{ID: id, ScopeOverride: override})
				payload := decodeUpdate(t, path, orgissuersserver.DecodeUpdateIssuerRequest, wire)
				result, err := ti.service.UpdateIssuer(ctx, payload)
				require.NoError(t, err)
				return wire, payload.ScopeOverride, result
			},
		},
	}
}

func TestIssuerScopeOverride_Wire_EmptySliceIsSentAndClears(t *testing.T) {
	t.Parallel()

	for _, tier := range scopeOverrideWireTiers() {
		t.Run(tier.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)

			created, err := tier.create(ctx, ti, "scope-override-wire-clear-"+tier.name, []string{"custom:one"})
			require.NoError(t, err)

			wire, decoded, cleared := tier.update(t, ctx, ti, created.ID, []string{})
			require.Contains(t, string(wire), `"scope_override":[]`, "an explicit empty slice reaches the wire")
			require.NotNil(t, decoded)
			require.Empty(t, decoded)
			require.Nil(t, cleared.ScopeOverride)
			require.Nil(t, storedScopeOverride(t, ctx, ti, created.ID))
		})
	}
}

func TestIssuerScopeOverride_Wire_NilIsNullAndKeeps(t *testing.T) {
	t.Parallel()

	for _, tier := range scopeOverrideWireTiers() {
		t.Run(tier.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)

			created, err := tier.create(ctx, ti, "scope-override-wire-keep-"+tier.name, []string{"custom:one"})
			require.NoError(t, err)

			wire, decoded, kept := tier.update(t, ctx, ti, created.ID, nil)
			require.Contains(t, string(wire), `"scope_override":null`, "nil is null, never []")
			require.Nil(t, decoded)
			require.Equal(t, []string{"custom:one"}, kept.ScopeOverride)
			require.Equal(t, []string{"custom:one"}, storedScopeOverride(t, ctx, ti, created.ID))
		})
	}
}
