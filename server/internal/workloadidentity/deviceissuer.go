package workloadidentity

import (
	"strings"
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

// DeviceIssuerOrganization returns the organization a Speakeasy device issuer
// URL names, and whether issuerURL is one on this server at all.
//
// It reads the URL only. The caller still resolves the issuer row in that
// organization, so an organization that registers another's device issuer URL
// as its own issuer gains nothing: the row looked up is always the named
// organization's.
func DeviceIssuerOrganization(serverURL, issuerURL string) (string, bool) {
	prefix := strings.TrimSuffix(serverURL, "/") + deviceIssuerPath
	organizationID, ok := strings.CutPrefix(issuerURL, prefix)
	if !ok || organizationID == "" || strings.ContainsAny(organizationID, "/?#") {
		return "", false
	}
	return organizationID, true
}
