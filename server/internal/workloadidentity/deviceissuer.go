package workloadidentity

import (
	"strings"

	"github.com/speakeasy-api/gram/server/internal/issuerurl"
)

// deviceIssuerPath names an organization's device issuer under the server URL,
// with the organization ID as its final segment.
const deviceIssuerPath = "/workload/devices/"

// DeviceIssuerURL is the issuer string of an organization's Speakeasy device
// issuer: the workload issuer that vouches for devices which joined with a
// join token instead of presenting a platform identity.
func DeviceIssuerURL(serverURL, organizationID string) string {
	return strings.TrimRight(serverURL, "/") + deviceIssuerPath + organizationID
}

// IsUnderDeviceIssuerPrefix reports whether issuerURL is under the reserved
// device issuer prefix, compared in canonical form so no alternate spelling
// (default port, uppercase host) can claim it.
func IsUnderDeviceIssuerPrefix(serverURL, issuerURL string) bool {
	prefix, ok := canonicalDeviceIssuerPrefix(serverURL)
	if !ok {
		return strings.HasPrefix(strings.TrimSpace(issuerURL), strings.TrimRight(serverURL, "/")+deviceIssuerPath)
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
	canonical, err := issuerurl.Parse(strings.TrimRight(serverURL, "/") + strings.TrimSuffix(deviceIssuerPath, "/"))
	if err != nil {
		return "", false
	}
	return canonical.String(), true
}

// DeviceIssuerOrganization returns the organization a device issuer URL on this
// server names. Callers resolve the issuer row within it, so registering another
// organization's URL gains nothing.
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
