package k8s

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

func SanitizeDomainForK8sName(domain string) (string, error) {
	name := strings.ReplaceAll(domain, ".", "-")
	reg := regexp.MustCompile("[^a-zA-Z0-9-]+")
	name = reg.ReplaceAllString(name, "")
	name = strings.Trim(name, "-")
	if len(name) > 63 {
		name = name[:63]
	}
	if name == "" {
		return "", fmt.Errorf("invalid domain name")
	}
	return strings.ToLower(name), nil
}

// TLSSecretNameForDomain derives the TLS Secret name for a custom domain.
// Apply and the DeleteDomain identity checkpoint share it so every tombstone
// carries a cleanable identity even when Apply never persisted one.
func TLSSecretNameForDomain(domain string) string {
	return strings.ToLower(strings.ReplaceAll(domain, ".", "-")) + "-tls"
}

// RootIngressName derives the secondary root-ingress name from the primary
// custom-domain resource name. The hash preserves determinism when the primary
// name is too long to append the role suffix directly.
func RootIngressName(primaryResourceName string) (string, error) {
	return secondaryIngressName(primaryResourceName, "root")
}

// WellKnownRootIngressName derives the secondary ingress name that rewrites
// root OAuth discovery paths to the selected MCP endpoint.
func WellKnownRootIngressName(primaryResourceName string) (string, error) {
	return secondaryIngressName(primaryResourceName, "wellknown-root")
}

func secondaryIngressName(primaryResourceName, role string) (string, error) {
	if primaryResourceName == "" {
		return "", fmt.Errorf("primary resource name is empty")
	}

	suffix := "-" + role
	if len(primaryResourceName)+len(suffix) <= 63 {
		return primaryResourceName + suffix, nil
	}

	digest := sha256.Sum256([]byte(primaryResourceName))
	hash := hex.EncodeToString(digest[:4])
	hashedSuffix := suffix + "-"
	prefixLength := 63 - len(hashedSuffix) - len(hash)
	prefix := strings.Trim(primaryResourceName[:prefixLength], "-")
	if prefix == "" {
		return "", fmt.Errorf("invalid primary resource name")
	}
	return prefix + hashedSuffix + hash, nil
}

// RootIngressNameForDomain is shared by reconciliation and health checks so
// their expected Kubernetes resource identities cannot drift.
func RootIngressNameForDomain(domain string) (string, error) {
	primaryName, err := SanitizeDomainForK8sName(domain)
	if err != nil {
		return "", err
	}
	return RootIngressName(primaryName)
}

// WellKnownRootIngressNameForDomain is shared by reconciliation and health
// checks so their expected Kubernetes resource identities cannot drift.
func WellKnownRootIngressNameForDomain(domain string) (string, error) {
	primaryName, err := SanitizeDomainForK8sName(domain)
	if err != nil {
		return "", err
	}
	return WellKnownRootIngressName(primaryName)
}
