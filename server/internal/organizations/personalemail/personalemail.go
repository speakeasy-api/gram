// Package personalemail recognizes addresses at personal and disposable
// mailbox providers, whose domain names a mail service rather than the
// owner's company.
package personalemail

import (
	_ "embed"
	"strings"
	"sync"
)

//go:embed domains.txt
var domainsFile string

var domains = sync.OnceValue(func() map[string]struct{} {
	lines := strings.Split(domainsFile, "\n")
	set := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		line = strings.ToLower(strings.TrimSpace(line))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		set[line] = struct{}{}
	}
	return set
})

// IsPersonalDomain reports whether domain is a personal or disposable mailbox
// provider. Only exact matches count: the lists hold registry-level entries
// such as or.ru, so matching parents would refuse every company beneath them.
func IsPersonalDomain(domain string) bool {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	_, ok := domains()[domain]
	return ok
}

// IsPersonal reports whether email is at a personal or disposable mailbox
// provider. An address without a domain is not personal.
func IsPersonal(email string) bool {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return false
	}
	return IsPersonalDomain(email[at+1:])
}
