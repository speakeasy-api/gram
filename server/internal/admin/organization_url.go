package admin

import (
	"net"
	"net/url"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgprovision"
)

const maxOrganizationURLBytes = 4000

func organizationHostname(raw string) (string, error) {
	invalid := func(message string) (string, error) {
		return "", oops.E(oops.CodeInvalid, nil, "%s", message)
	}
	if len(raw) > maxOrganizationURLBytes || !utf8.ValidString(raw) {
		return invalid("company URL must be valid text of at most 4000 bytes")
	}
	input := strings.TrimSpace(raw)
	if input == "" || strings.ContainsAny(input, "\\\t\r\n") {
		return invalid("enter a company HTTP(S) URL or hostname")
	}
	if lower := strings.ToLower(input); !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		input = "https://" + input
	}
	parsed, err := url.Parse(input)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return invalid("enter a company HTTP(S) URL or hostname")
	}
	if parsed.User != nil || strings.ContainsAny(parsed.Host, ":%[]") {
		return invalid("company URL must not contain credentials, ports, or IP addresses")
	}
	for _, r := range parsed.Host {
		if r > 127 {
			return invalid("use an ASCII hostname; Unicode domains must use valid punycode")
		}
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Host), ".")
	if net.ParseIP(host) != nil || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return invalid("use a public company hostname, not an IP address or localhost")
	}
	for label := range strings.SplitSeq(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return invalid("company hostname contains an invalid DNS label")
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return invalid("company hostname contains an invalid DNS label")
			}
		}
	}
	// Numeric final labels are interpreted as IPv4 by browsers, including hex and octal forms.
	last := host[strings.LastIndex(host, ".")+1:]
	if strings.Trim(last, "0123456789") == "" || strings.HasPrefix(last, "0x") {
		return invalid("use a company hostname, not a numeric address")
	}
	ascii, err := idna.Lookup.ToASCII(host)
	if err != nil || ascii != host {
		return invalid("company hostname contains invalid punycode or DNS labels")
	}
	if _, err := publicsuffix.EffectiveTLDPlusOne(host); err != nil {
		return invalid("company hostname must include a domain below the public suffix")
	}
	return orgprovision.ValidateName(host)
}
