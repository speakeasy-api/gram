// Package emaildomain classifies the domain of an email address by the kind of
// mail provider behind it. The lists are curated and hard-coded; nothing is
// fetched at runtime.
package emaildomain

import "strings"

// Class is the kind of mail provider an email domain belongs to. The values
// are bounded so they can label metrics.
type Class string

const (
	// Unknown means there was no usable domain to classify.
	Unknown Class = "unknown"
	// FreeWebmail is a personal mailbox provider, such as gmail.com.
	FreeWebmail Class = "free_webmail"
	// Disposable is a throwaway or temporary mail provider.
	Disposable Class = "disposable"
	// Business is any other domain, typically the user's company.
	Business Class = "business"
)

// freeWebmailDomains are personal mailbox providers. Their domain names a mail
// service, not the user's company.
var freeWebmailDomains = map[string]struct{}{
	"126.com":        {},
	"163.com":        {},
	"aol.com":        {},
	"fastmail.com":   {},
	"gmail.com":      {},
	"gmx.com":        {},
	"gmx.de":         {},
	"gmx.net":        {},
	"googlemail.com": {},
	"hey.com":        {},
	"hotmail.com":    {},
	"icloud.com":     {},
	"live.com":       {},
	"mac.com":        {},
	"mail.com":       {},
	"mail.ru":        {},
	"me.com":         {},
	"msn.com":        {},
	"outlook.com":    {},
	"pm.me":          {},
	"proton.me":      {},
	"protonmail.com": {},
	"qq.com":         {},
	"tuta.io":        {},
	"tutanota.com":   {},
	"yahoo.com":      {},
	"yandex.com":     {},
	"yandex.ru":      {},
	"ymail.com":      {},
	"zoho.com":       {},
	"zohomail.com":   {},
}

// disposableDomains are well-known throwaway and temporary mail providers.
var disposableDomains = map[string]struct{}{
	"10minutemail.com":  {},
	"dispostable.com":   {},
	"emailondeck.com":   {},
	"getnada.com":       {},
	"guerrillamail.com": {},
	"luxusmail.org":     {},
	"maildrop.cc":       {},
	"mailinator.com":    {},
	"mohmal.com":        {},
	"sharklasers.com":   {},
	"temp-mail.org":     {},
	"tempmail.com":      {},
	"throwawaymail.com": {},
	"trashmail.com":     {},
	"yopmail.com":       {},
}

// Domain returns the normalized domain of email: lowercased and trimmed, the
// part after the last "@", without a trailing dot. It returns "" when email
// has no domain.
func Domain(email string) string {
	normalized := strings.ToLower(strings.TrimSpace(email))
	at := strings.LastIndex(normalized, "@")
	if at < 0 {
		return ""
	}
	return strings.TrimSuffix(normalized[at+1:], ".")
}

// Classify returns the class of email's domain. Domains match exactly, so a
// subdomain of a listed provider is Business.
func Classify(email string) Class {
	return ClassifyDomain(Domain(email))
}

// ClassifyDomain returns the class of an already normalized domain.
func ClassifyDomain(domain string) Class {
	switch {
	case domain == "":
		return Unknown
	case IsFreeWebmail(domain):
		return FreeWebmail
	case isDisposable(domain):
		return Disposable
	default:
		return Business
	}
}

// IsFreeWebmail reports whether domain, already normalized, is a personal
// mailbox provider.
func IsFreeWebmail(domain string) bool {
	_, ok := freeWebmailDomains[domain]
	return ok
}

func isDisposable(domain string) bool {
	_, ok := disposableDomains[domain]
	return ok
}
