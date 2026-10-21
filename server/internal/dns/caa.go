package dns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

// LetsEncryptIssueDomain is the CAA issue value that authorizes cert-manager's
// gram-letsencrypt ClusterIssuer.
const LetsEncryptIssueDomain = "letsencrypt.org"

// ExpectedLetsEncryptCAA is the customer-facing record that authorizes
// Let's Encrypt when a domain already has a CAA RRset.
const ExpectedLetsEncryptCAA = `0 issue "letsencrypt.org"`

// CAA is one Certification Authority Authorization record (RFC 8659).
type CAA struct {
	// Flag is the record flag octet. The issuer-critical bit is 1.
	Flag uint8

	// Tag is the property name, typically issue, issuewild, or iodef.
	Tag string

	// Value is the property value. For issue tags this is a CA domain name,
	// optionally followed by a semicolon and parameters.
	Value string
}

// IssueRestriction is the closest CAA RRset that applies to a hostname.
// An empty Name means no CAA records were found walking to the TLD, so any
// CA may issue.
type IssueRestriction struct {
	// Name is the DNS name whose CAA RRset was used. Empty when none found.
	Name string

	// Records is the CAA RRset at Name.
	Records []CAA
}

// FindIssueRestriction walks domain and its parents until a CAA RRset is
// found or only the TLD remains. NXDOMAIN and empty answers continue the
// walk; any other lookup error is returned.
func FindIssueRestriction(ctx context.Context, resolver Resolver, domain string) (IssueRestriction, error) {
	var none IssueRestriction
	name := normalizeDNSName(domain)
	for name != "" && strings.Contains(name, ".") {
		records, err := resolver.LookupCAA(ctx, name)
		if err != nil {
			var dnsErr *net.DNSError
			if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
				name = parentDNSName(name)
				continue
			}
			return none, fmt.Errorf("lookup CAA %s: %w", name, err)
		}
		if len(records) > 0 {
			return IssueRestriction{Name: name, Records: records}, nil
		}
		name = parentDNSName(name)
	}
	return none, nil
}

// IssueAllows reports whether the closest CAA RRset authorizes issuer to
// issue a non-wildcard certificate. No issue tags (only iodef / issuewild /
// issuemail) do not restrict non-wildcard issuance. A critical unknown tag
// denies issuance.
func IssueAllows(records []CAA, issuer string) bool {
	issuer = strings.ToLower(strings.TrimSpace(issuer))
	sawIssue := false
	for _, rec := range records {
		tag := strings.ToLower(rec.Tag)
		if rec.Flag&1 != 0 && !knownCAATag(tag) {
			return false
		}
		if tag != "issue" {
			continue
		}
		sawIssue = true
		if issueValueAllows(rec.Value, issuer) {
			return true
		}
	}
	return !sawIssue
}

func knownCAATag(tag string) bool {
	switch tag {
	case "issue", "issuewild", "iodef", "issuemail":
		return true
	default:
		return false
	}
}

func issueValueAllows(value, issuer string) bool {
	value = strings.TrimSpace(value)
	if value == "" || value == ";" {
		return false
	}
	domain, _, _ := strings.Cut(value, ";")
	domain = strings.ToLower(strings.Trim(strings.TrimSpace(domain), `"'`))
	return domain == issuer
}

func parentDNSName(name string) string {
	name = normalizeDNSName(name)
	i := strings.Index(name, ".")
	if i < 0 {
		return ""
	}
	return name[i+1:]
}

func normalizeDNSName(name string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
}
