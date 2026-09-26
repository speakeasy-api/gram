package mcpregistry

import (
	"context"
	"encoding/json"
	client "github.com/speakeasy-api/gram/server/gen/http/registry_discovery/client"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	srv "github.com/speakeasy-api/gram/server/gen/http/registry_discovery/server"
	gen "github.com/speakeasy-api/gram/server/gen/registry_discovery"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
)

// Exercise generated decoding and serialization through the actual Goa/chi mux,
// independently of credential fixtures. Authentication is tested separately.
func TestDiscoveryGeneratedTransportEncodedNames(t *testing.T) {
	t.Parallel()

	m := discoveryMux{goahttp.NewMuxer()}
	ep := &gen.Endpoints{
		DiscoverVersion: func(_ context.Context, p any) (any, error) {
			v, ok := p.(*gen.DiscoverVersionPayload)
			require.True(t, ok)
			require.Equal(t, "io.example/a+b/c", v.ServerName)
			require.Equal(t, "v/1+2", v.Version)
			require.NotNil(t, v.UpdatedSince)
			return json.RawMessage(`{"server":{"name":"io.example/a+b/c","unknown":9007199254740993},"_meta":{"extension":{"n":9007199254740993}}}`), nil
		},
		DiscoverServers: func(context.Context, any) (any, error) {
			return discoveryResult(DiscoveryPage{Records: []json.RawMessage{}}), nil
		},
		DiscoverVersions: func(context.Context, any) (any, error) {
			return discoveryResult(DiscoveryPage{Records: []json.RawMessage{}}), nil
		},
	}
	srv.Mount(m, srv.New(ep, m, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
	w := httptest.NewRecorder()
	m.ServeHTTP(w, httptest.NewRequest("GET", "/v0.1/servers/io.example%2Fa%2Bb%2Fc/versions/v%2F1%2B2?updated_since=", nil))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `9007199254740993`)
	require.Contains(t, w.Body.String(), `"_meta"`)
	require.NotContains(t, w.Body.String(), `data_json`)
}

func TestDiscoveryMountDisabledAndFailClosed(t *testing.T) {
	t.Parallel()

	ctx, s, _ := newTestService(t)
	m := goahttp.NewMuxer()
	require.NoError(t, s.AttachDiscovery(ctx, m, false, nil, nil))
	w := httptest.NewRecorder()
	m.ServeHTTP(w, httptest.NewRequest("GET", "/v0.1/servers", nil))
	require.Equal(t, 404, w.Code)
	require.Error(t, s.AttachDiscovery(ctx, m, true, nil, nil))
	w = httptest.NewRecorder()
	m.ServeHTTP(w, httptest.NewRequest("GET", "/v0.1/servers", nil))
	require.Equal(t, 404, w.Code)
}

func TestDiscoveryGeneratedClientPathSegments(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"io.example/slash", "percent%", "unicodeé", "literal%2F"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()

			m := discoveryMux{goahttp.NewMuxer()}
			called := 0
			ep := &gen.Endpoints{
				DiscoverVersions: func(_ context.Context, p any) (any, error) {
					v, ok := p.(*gen.DiscoverVersionsPayload)
					require.True(t, ok)
					require.Equal(t, value, v.ServerName)
					called++
					return discoveryResult(DiscoveryPage{Records: []json.RawMessage{}}), nil
				},
				DiscoverVersion: func(_ context.Context, p any) (any, error) {
					v, ok := p.(*gen.DiscoverVersionPayload)
					require.True(t, ok)
					require.Equal(t, value, v.ServerName)
					require.Equal(t, value, v.Version)
					called++
					return json.RawMessage(`{}`), nil
				},
			}
			srv.Mount(m, srv.New(ep, m, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
			c := client.NewClient("http", "example.test", http.DefaultClient, nil, nil, false)
			versions, err := c.BuildDiscoverVersionsRequest(context.Background(), &gen.DiscoverVersionsPayload{ServerName: value})
			require.NoError(t, err)
			version, err := c.BuildDiscoverVersionRequest(context.Background(), &gen.DiscoverVersionPayload{ServerName: value, Version: value})
			require.NoError(t, err)
			for i, req := range []*http.Request{versions, version} {
				expected := "/v0.1/servers/" + url.PathEscape(value) + "/versions"
				if i == 1 {
					expected += "/" + url.PathEscape(value)
				}
				require.Equal(t, expected, req.URL.EscapedPath())
				require.Equal(t, expected, req.URL.RequestURI())
				decoded, err := url.PathUnescape(expected)
				require.NoError(t, err)
				require.Equal(t, decoded, req.URL.Path)
				w := httptest.NewRecorder()
				m.ServeHTTP(w, httptest.NewRequest("GET", req.URL.String(), nil))
				require.Equal(t, 200, w.Code, w.Body.String())
			}
			require.Equal(t, 2, called)
		})
	}
}
