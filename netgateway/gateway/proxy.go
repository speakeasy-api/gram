package gateway

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/netgateway/wire"
)

// identityTimeout bounds the per-request identity lookup so a slow provider
// control path cannot stall the serving path.
const identityTimeout = 5 * time.Second

// NewProxyHandler serves one ingress's traffic: it attributes the caller via
// the node, enforces identity_required at the edge, and reverse-proxies into
// gram-server carrying the forward-token trust headers. Host is preserved so
// BaseURLForRequest-derived URLs (OAuth issuer, metadata) come out as the
// private hostname.
func NewProxyHandler(cfg IngressConfig, node Node, upstream *url.URL, forwardToken string, logger *slog.Logger) http.Handler {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(upstream)
			r.SetXForwarded()
			// The MCP surface resolves endpoints and issuer URLs from Host.
			r.Out.Host = r.In.Host
		},
		ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Inbound copies of the trust headers are forgeries by definition:
		// only this handler may set them.
		wire.Strip(r.Header)

		idCtx, cancel := context.WithTimeout(r.Context(), identityTimeout)
		identity, err := node.Identity(idCtx, r.RemoteAddr)
		cancel()
		if err != nil {
			logger.WarnContext(r.Context(), "network ingress identity lookup failed",
				slog.String("ingress_id", cfg.ID.String()), slog.Any("error", err))
			identity = nil
		}
		if cfg.IdentityRequired && identity == nil {
			http.Error(w, "network identity required", http.StatusForbidden)
			return
		}

		r.Header.Set(wire.HeaderForwardToken, forwardToken)
		r.Header.Set(wire.HeaderIngressID, cfg.ID.String())
		r.Header.Set(wire.HeaderProvider, cfg.Provider)
		if identity != nil {
			r.Header.Set(wire.HeaderUserLogin, identity.Login)
			if identity.DisplayName != "" {
				r.Header.Set(wire.HeaderUserName, identity.DisplayName)
			}
			if identity.Device != "" {
				r.Header.Set(wire.HeaderUserNode, identity.Device)
			}
			if len(identity.Caps) > 0 {
				r.Header.Set(wire.HeaderUserCaps, strings.Join(identity.Caps, ","))
			}
		}

		proxy.ServeHTTP(w, r)
	})
}
