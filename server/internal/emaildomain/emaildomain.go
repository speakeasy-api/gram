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

// freeWebmailDomains are personal mailbox providers, including the regional and
// country-code domains a provider offers for new mailboxes. Their domain names
// a mail service, not the user's company. Each domain is listed explicitly
// rather than matched by provider name, because a provider's name under
// another suffix is often an unrelated company or a parked domain that hosts
// no mailboxes.
var freeWebmailDomains = map[string]struct{}{
	// Google.
	"gmail.com":      {},
	"googlemail.com": {},

	// Microsoft. Hotmail, Live and Outlook each have regional domains that Microsoft
	// offered as new personal addresses.
	"hotmail.com":     {},
	"hotmail.be":      {},
	"hotmail.ca":      {},
	"hotmail.co.jp":   {},
	"hotmail.co.uk":   {},
	"hotmail.com.ar":  {},
	"hotmail.com.au":  {},
	"hotmail.com.br":  {},
	"hotmail.de":      {},
	"hotmail.es":      {},
	"hotmail.fr":      {},
	"hotmail.it":      {},
	"hotmail.nl":      {},
	"live.com":        {},
	"live.be":         {},
	"live.ca":         {},
	"live.co.uk":      {},
	"live.com.au":     {},
	"live.com.mx":     {},
	"live.de":         {},
	"live.fr":         {},
	"live.it":         {},
	"live.nl":         {},
	"msn.com":         {},
	"outlook.com":     {},
	"outlook.be":      {},
	"outlook.com.au":  {},
	"outlook.com.br":  {},
	"outlook.de":      {},
	"outlook.es":      {},
	"outlook.fr":      {},
	"outlook.ie":      {},
	"outlook.in":      {},
	"outlook.it":      {},
	"outlook.jp":      {},
	"windowslive.com": {},

	// Yahoo and AOL. yahoo.co.jp is run separately in Japan but is also personal mail.
	"yahoo.com":      {},
	"yahoo.ca":       {},
	"yahoo.co.id":    {},
	"yahoo.co.in":    {},
	"yahoo.co.jp":    {},
	"yahoo.co.uk":    {},
	"yahoo.com.ar":   {},
	"yahoo.com.au":   {},
	"yahoo.com.br":   {},
	"yahoo.com.hk":   {},
	"yahoo.com.mx":   {},
	"yahoo.com.ph":   {},
	"yahoo.com.sg":   {},
	"yahoo.com.tw":   {},
	"yahoo.de":       {},
	"yahoo.es":       {},
	"yahoo.fr":       {},
	"yahoo.ie":       {},
	"yahoo.in":       {},
	"yahoo.it":       {},
	"ymail.com":      {},
	"rocketmail.com": {},
	"aol.com":        {},
	"aim.com":        {},

	// Apple.
	"icloud.com": {},
	"me.com":     {},
	"mac.com":    {},

	// Privacy-focused and independent providers.
	"fastmail.com":   {},
	"fastmail.fm":    {},
	"hey.com":        {},
	"mailbox.org":    {},
	"pm.me":          {},
	"posteo.de":      {},
	"proton.me":      {},
	"protonmail.ch":  {},
	"protonmail.com": {},
	"tuta.com":       {},
	"tuta.io":        {},
	"tutamail.com":   {},
	"tutanota.com":   {},
	"tutanota.de":    {},

	// Zoho, including its India and EU data center domains.
	"zoho.com":     {},
	"zohomail.com": {},
	"zohomail.eu":  {},
	"zohomail.in":  {},

	// United Internet (GMX, WEB.DE, mail.com) and other German providers.
	"gmx.at":      {},
	"gmx.ch":      {},
	"gmx.co.uk":   {},
	"gmx.com":     {},
	"gmx.de":      {},
	"gmx.fr":      {},
	"gmx.net":     {},
	"gmx.us":      {},
	"mail.com":    {},
	"mail.de":     {},
	"web.de":      {},
	"freenet.de":  {},
	"t-online.de": {},

	// France.
	"free.fr":     {},
	"laposte.net": {},
	"orange.fr":   {},
	"sfr.fr":      {},
	"wanadoo.fr":  {},

	// Italy.
	"alice.it":    {},
	"libero.it":   {},
	"tiscali.it":  {},
	"virgilio.it": {},

	// Switzerland.
	"bluewin.ch": {},

	// Poland and Czechia.
	"interia.pl": {},
	"o2.pl":      {},
	"onet.pl":    {},
	"wp.pl":      {},
	"seznam.cz":  {},

	// Russia, Ukraine and neighbors. Mail.ru also offers bk.ru, inbox.ru and list.ru.
	"bk.ru":         {},
	"inbox.ru":      {},
	"list.ru":       {},
	"mail.ru":       {},
	"rambler.ru":    {},
	"ukr.net":       {},
	"ya.ru":         {},
	"yandex.by":     {},
	"yandex.com":    {},
	"yandex.com.tr": {},
	"yandex.kz":     {},
	"yandex.ru":     {},
	"yandex.ua":     {},

	// China.
	"126.com":     {},
	"139.com":     {},
	"163.com":     {},
	"aliyun.com":  {},
	"foxmail.com": {},
	"qq.com":      {},
	"sina.com":    {},
	"sohu.com":    {},
	"yeah.net":    {},

	// South Korea.
	"daum.net":    {},
	"hanmail.net": {},
	"naver.com":   {},

	// Japanese mobile carriers.
	"docomo.ne.jp": {},
	"ezweb.ne.jp":  {},

	// India.
	"rediffmail.com": {},

	// Brazil.
	"bol.com.br": {},
	"uol.com.br": {},

	// Consumer internet providers in the US, UK and Australia. Their staff use
	// separate corporate domains.
	"att.net":        {},
	"comcast.net":    {},
	"sbcglobal.net":  {},
	"verizon.net":    {},
	"btinternet.com": {},
	"bigpond.com":    {},
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
