package k8s

import (
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
