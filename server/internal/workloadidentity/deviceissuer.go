package workloadidentity

import (
	"strings"

	"github.com/speakeasy-api/gram/server/internal/issuerurl"
)

// deviceIssuerPath is the path under the server URL that names an
// organization's Speakeasy device issuer. The organization ID is its final
// segment, which is what lets a token endpoint serving every organization
// read the tenant from an assertion's iss.
const deviceIssuerPath = "/workload/devices/"

// DeviceIssuerURL is the issuer string of an organization's Speakeasy device
// issuer: the workload issuer that vouches for devices which joined with a
// join token instead of presenting a platform identity.
func DeviceIssuerURL(serverURL, organizationID string) string {
	return strings.TrimSuffix(serverURL, "/") + deviceIssuerPath + organizationID
}

// IsUnderDeviceIssuerPrefix reports whether issuerURL falls anywhere under
// serverURL's device issuer prefix. The whole prefix is reserved: an operator
// issuer there would verify against a key set of its choosing instead of the
// keys devices registered.
//
// Both sides are compared in the canonical form issuer lookup uses, so a
// spelling such as an explicit default port or an uppercase host cannot claim
// the namespace.
func IsUnderDeviceIssuerPrefix(serverURL, issuerURL string) bool {
	prefix, ok := canonicalDeviceIssuerPrefix(serverURL)
	if !ok {
		return strings.HasPrefix(strings.TrimSpace(issuerURL), strings.TrimSuffix(serverURL, "/")+deviceIssuerPath)
	}
	canonical, err := issuerurl.Parse(issuerURL)
	if err != nil {
		return false
	}
	c := canonical.String()
	return c == prefix || strings.HasPrefix(c, prefix+"/")
}

// canonicalDeviceIssuerPrefix is serverURL's device issuer prefix in canonical
// form, without its trailing slash.
func canonicalDeviceIssuerPrefix(serverURL string) (string, bool) {
	canonical, err := issuerurl.Parse(strings.TrimSuffix(serverURL, "/") + strings.TrimSuffix(deviceIssuerPath, "/"))
	if err != nil {
		return "", false
	}
	return canonical.String(), true
}

// DeviceIssuerOrganization returns the organization a Speakeasy device issuer
// URL names, and whether issuerURL is one on this server at all.
//
// It reads the URL only. The caller still resolves the issuer row in that
// organization, so an organization that registers another's device issuer URL
// as its own issuer gains nothing: the row looked up is always the named
// organization's.
func DeviceIssuerOrganization(serverURL, issuerURL string) (string, bool) {
	prefix, ok := canonicalDeviceIssuerPrefix(serverURL)
	if !ok {
		return "", false
	}
	canonical, err := issuerurl.Parse(issuerURL)
	if err != nil {
		return "", false
	}
	organizationID, ok := strings.CutPrefix(canonical.String(), prefix+"/")
	if !ok || organizationID == "" || strings.ContainsAny(organizationID, "/?#") {
		return "", false
	}
	return organizationID, true
}
