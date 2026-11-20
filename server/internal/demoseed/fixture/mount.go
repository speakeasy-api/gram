package fixture

import (
	"net/http"
	"net/url"
	"strings"

	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/demoseed/fixture/fixtureauth"
)

// PathPrefix is where the Speakeasy server serves the fixture.
const PathPrefix = fixtureauth.PathPrefix

// BaseURL is the fixture's external URL on a Speakeasy server at serverURL.
func BaseURL(serverURL *url.URL) string { return fixtureauth.BaseURL(serverURL) }

// wellKnownKinds are the metadata documents RFC 8414 and RFC 9728 place at
// the host root, with the issuer's or resource's path after them.
var wellKnownKinds = []string{"oauth-authorization-server", "openid-configuration", "oauth-protected-resource"}

// Attach serves the fixture under PathPrefix on mux, and its metadata at the
// root locations RFC 8414 and RFC 9728 derive from its issuers and resources
// (/.well-known/<kind>/rpc/demoseed/...). Discovery also finds the metadata
// under PathPrefix, where the deployed ingress already sends requests.
func Attach(mux goahttp.Muxer, s *Server) {
	h := s.Handler()
	prefixed := http.StripPrefix(PathPrefix, h)
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete, http.MethodOptions} {
		mux.Handle(method, PathPrefix+"/{*path}", prefixed.ServeHTTP)
	}
	for _, kind := range wellKnownKinds {
		root := "/.well-known/" + kind + PathPrefix
		rewrite := func(w http.ResponseWriter, r *http.Request) {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/.well-known/" + kind + strings.TrimPrefix(r.URL.Path, root)
			r2.URL.RawPath = ""
			h.ServeHTTP(w, r2)
		}
		mux.Handle(http.MethodGet, root, rewrite)
		mux.Handle(http.MethodGet, root+"/{*path}", rewrite)
	}
}
