// Package wire defines the forward-header contract between network-gateway
// and gram-server. The gateway sets these headers after validating a request
// against the org's overlay node; gram-server's netingress middleware trusts
// them only when the forward token matches, and strips them from every other
// request so header injection via the public path is inert.
package wire

import (
	"net/http"
	"strings"
)

// HeaderPrefix is shared by every gateway-set header. Both sides strip any
// inbound header carrying this prefix before setting their own.
const HeaderPrefix = "X-Gram-Netingress-"

const (
	// HeaderForwardToken carries the shared secret that authenticates the
	// gateway to gram-server. Compared in constant time server-side.
	HeaderForwardToken = "X-Gram-Netingress-Forward-Token"

	// HeaderIngressID is the network_ingresses row the request arrived through.
	HeaderIngressID = "X-Gram-Netingress-Ingress-Id"

	// HeaderProvider is the overlay provider kind, e.g. "tailscale".
	HeaderProvider = "X-Gram-Netingress-Provider"

	// HeaderUserLogin is the network-attested login of the caller, when the
	// provider can attribute one (tailscale WhoIs user login).
	HeaderUserLogin = "X-Gram-Netingress-User-Login"

	// HeaderUserName is the caller's human display name, when attributed.
	HeaderUserName = "X-Gram-Netingress-User-Name"

	// HeaderUserNode is the caller's device name on the overlay network.
	HeaderUserNode = "X-Gram-Netingress-User-Node"

	// HeaderUserCaps is a comma-separated list of provider capability grants
	// attached to the caller. Advisory in MVP; never authz-bearing without
	// validation against the ingress's org.
	HeaderUserCaps = "X-Gram-Netingress-User-Caps"
)

// Strip removes every gateway forward header, canonical or not, from h.
// Deletion uses the literal map key rather than http.Header.Del, which
// canonicalizes its argument and would leave a non-canonically spelled
// forgery in place.
func Strip(h http.Header) {
	for name := range h {
		if strings.HasPrefix(http.CanonicalHeaderKey(name), HeaderPrefix) {
			delete(h, name)
		}
	}
}
