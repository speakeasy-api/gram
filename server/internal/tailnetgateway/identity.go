package tailnetgateway

import (
	"mime"
	"net/http"
	"strings"
	"unicode"

	"tailscale.com/client/tailscale/apitype"

	"github.com/speakeasy-api/gram/server/internal/netingress"
)

// IdentityKind classifies the tailnet peer behind a request for metrics.
type IdentityKind string

const (
	// IdentityKindUser is a device signed in by a tailnet user.
	IdentityKindUser IdentityKind = "user"

	// IdentityKindTagged is a tagged device, which carries no user identity.
	IdentityKindTagged IdentityKind = "tagged"

	// IdentityKindUnknown means WhoIs returned no usable node or user.
	IdentityKindUnknown IdentityKind = "unknown"
)

// SetIdentityHeaders replaces every Tailscale-owned header on the request with
// the identity resolved by WhoIs, so the private listener sees exactly the
// headers the Tailscale operator proxy used to inject. Tagged devices and
// unknown peers get no identity headers, which the listener's identity_required
// setting then rejects.
func SetIdentityHeaders(headers http.Header, who *apitype.WhoIsResponse) IdentityKind {
	netingress.DeleteTailscaleIdentityHeaders(headers)
	netingress.StripUnsupportedTailscaleHeaders(headers)
	if who == nil || who.Node == nil || who.UserProfile == nil {
		return IdentityKindUnknown
	}
	if who.Node.IsTagged() {
		return IdentityKindTagged
	}
	login := headerValue(who.UserProfile.LoginName)
	name := headerValue(who.UserProfile.DisplayName)
	if name == "" {
		name = login
	}
	if login == "" {
		return IdentityKindUnknown
	}
	headers.Set(netingress.TailscaleUserLoginHeader, login)
	headers.Set(netingress.TailscaleUserNameHeader, name)
	if pic := strings.TrimSpace(who.UserProfile.ProfilePicURL); pic != "" && isASCIIPrintable(pic) {
		headers.Set(netingress.TailscaleUserProfilePicHeader, pic)
	}
	return IdentityKindUser
}

// headerValue trims the value and RFC 2047 encodes it when it is not plain
// printable ASCII, matching what the Tailscale serve proxy sends.
func headerValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\r\n\x00") {
		return ""
	}
	if isASCIIPrintable(value) {
		return value
	}
	return mime.BEncoding.Encode("utf-8", value)
}

func isASCIIPrintable(value string) bool {
	for _, r := range value {
		if r > unicode.MaxASCII || !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}
