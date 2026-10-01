package remotesessions

import (
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

// remoteLoginCallbackPath is the path of the redirect_uri Gram registers with
// every upstream provider. It is served on every platform host.
const remoteLoginCallbackPath = "/" + canonicalCallbackRouteBase + "/remote_login_callback"

// legacyProxyCallbackPath is the oauth_proxy_servers-era redirect_uri path.
const legacyProxyCallbackPath = "/oauth/callback"

// CallbackOrigins decides which origin a remote-session client's outbound OAuth
// URLs use: its redirect_uri and, for CIMD and private_key_jwt clients, its
// client metadata document and JWKS URLs. Providers store these URLs when the
// client is registered, so a client's origin never changes after it is created.
type CallbackOrigins struct {
	// Outbound is the pinned origin for clients with no recorded
	// callback_base_url. It must stay the origin those clients were registered
	// with, even when the server URL moves to another host.
	Outbound *url.URL

	// Registration is the origin recorded on newly created organization-owned
	// clients. Nil records none, so new clients use Outbound.
	Registration *url.URL
}

// DefaultCallbackOrigins pins every client to serverURL and records no origin
// on new clients.
func DefaultCallbackOrigins(serverURL *url.URL) CallbackOrigins {
	return CallbackOrigins{Outbound: serverURL, Registration: nil}
}

// ForClient returns the origin of a client whose recorded callback_base_url is
// stored. A NULL or unparseable value resolves to the pinned outbound origin.
func (o CallbackOrigins) ForClient(stored pgtype.Text) *url.URL {
	if stored.Valid && stored.String != "" {
		if parsed, err := url.Parse(stored.String); err == nil && parsed.Scheme != "" && parsed.Host != "" {
			return parsed
		}
	}
	return o.Outbound
}

// NewClientBaseURL is the callback_base_url to record on a client being created
// now. Only organization-owned clients adopt the registration origin: shared
// global clients stay on the pinned outbound origin.
func (o CallbackOrigins) NewClientBaseURL(organizationOwned bool) pgtype.Text {
	if !organizationOwned || o.Registration == nil {
		return pgtype.Text{String: "", Valid: false}
	}
	return conv.ToPGText(trimOrigin(o.Registration))
}

// ForNewClient returns the origin a client created now would use.
func (o CallbackOrigins) ForNewClient(organizationOwned bool) *url.URL {
	return o.ForClient(o.NewClientBaseURL(organizationOwned))
}

// RemoteLoginCallbackURL is the redirect_uri a client on origin registers.
func RemoteLoginCallbackURL(origin *url.URL) string {
	return trimOrigin(origin) + remoteLoginCallbackPath
}

// LegacyProxyCallbackURL is the oauth_proxy_servers-era redirect_uri on origin.
func LegacyProxyCallbackURL(origin *url.URL) string {
	return trimOrigin(origin) + legacyProxyCallbackPath
}

// ClientCallbackURL is the redirect_uri of a client whose recorded
// callback_base_url is stored.
func (o CallbackOrigins) ClientCallbackURL(stored pgtype.Text) string {
	return RemoteLoginCallbackURL(o.ForClient(stored))
}

// RemoteLoginCallbackOrigin is the origin whose remote-login callback a login
// with client lands on. A legacy client's /oauth/callback forwards to the
// pinned outbound origin.
func (m *ChallengeManager) RemoteLoginCallbackOrigin(client Client) *url.URL {
	if client.LegacyCallbackUrl {
		return m.origins.Outbound
	}
	return m.origins.ForClient(client.CallbackBaseURL)
}

func trimOrigin(origin *url.URL) string {
	return strings.TrimRight(origin.String(), "/")
}
