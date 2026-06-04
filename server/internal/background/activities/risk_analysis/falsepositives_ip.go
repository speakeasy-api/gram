package risk_analysis

import (
	"net/netip"
)

// nonPIIIPReason returns a short, human-readable reason why the given IP
// string should be treated as a non-PII false positive, or "" if the IP
// is unrecognised (i.e. should be treated as real PII).
//
// Covers:
//   - IANA-reserved address space (RFC1918 private, loopback, link-local,
//     multicast, CGNAT/RFC6598, documentation/RFC5737, 6to4 deprecated,
//     class E, "this network", limited broadcast, RFC2544 benchmarking).
//   - Well-known public DNS resolvers (Cloudflare 1.1.1.1, Google 8.8.8.8,
//     Quad9, OpenDNS, AdGuard, CleanBrowsing, Yandex, etc.) curated against
//     winutil, Technitium, RaspAP, Tunnelblick, and several maintained gists.
//   - Common placeholder IPs (1.2.3.4, 2.2.2.2, etc.) that appear in
//     documentation and example traffic.
//   - Shape heuristics: network address of a public /8 (X.0.0.0), and
//     sparse IPv6 with at most two non-zero bytes (e.g. 1::, dead::).
//
// Cloud / CDN / managed-hosting attribution falls through to a DB-IP
// ASN lookup (see falsepositives_ip_asn.go). Consumer ISP brands stay
// out of the infra regex so residential IPs still flow through as PII.
func nonPIIIPReason(s string) string {
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return ""
	}
	// Key the exact lookup off the parsed address's canonical form, not the
	// raw input, so equivalent spellings (uppercase hex, expanded zero
	// groups like 2606:4700:4700:0:0:0:0:1111, IPv4 in non-canonical form)
	// still match the catalog. nonPIIIPExact keys are canonical netip
	// strings; TestNonPIIIPExactKeysAreCanonical locks that invariant.
	if d := nonPIIIPExact[addr.String()]; d != "" {
		return d
	}
	for _, p := range nonPIIIPPrefixes {
		if p.prefix.Contains(addr) {
			return p.description
		}
	}
	if d := nonPIIIPHeuristic(addr); d != "" {
		return d
	}
	return infraASNDescription(addr)
}

// nonPIIIPHeuristic returns a description for IPs that fit common
// placeholder shapes not captured by the exact-IP or prefix sets:
//   - IPv4 ending in .0.0.0 — the network address of a public /8.
//   - IPv6 whose 16-byte representation has at most two non-zero bytes
//     (catches 1::, b::, dead::, ::cafe, etc.).
func nonPIIIPHeuristic(addr netip.Addr) string {
	if addr.Is4() {
		b := addr.As4()
		if b[1] == 0 && b[2] == 0 && b[3] == 0 {
			return "network address of /8"
		}
	}
	if addr.Is6() {
		b := addr.As16()
		nonzero := 0
		for _, x := range b {
			if x != 0 {
				nonzero++
			}
		}
		if nonzero <= 2 {
			return "sparse IPv6 likely placeholder"
		}
	}
	return ""
}

type prefixHit struct {
	prefix      netip.Prefix
	description string
}

// nonPIIIPPrefixes is the union of IANA-reserved address space that
// should never be treated as PII.
var nonPIIIPPrefixes = []prefixHit{
	{netip.MustParsePrefix("10.0.0.0/8"), "RFC1918 private space"},
	{netip.MustParsePrefix("172.16.0.0/12"), "RFC1918 private space"},
	{netip.MustParsePrefix("192.168.0.0/16"), "RFC1918 private space"},
	{netip.MustParsePrefix("127.0.0.0/8"), "loopback addresses"},
	{netip.MustParsePrefix("::1/128"), "loopback addresses"},
	{netip.MustParsePrefix("169.254.0.0/16"), "link-local addresses"},
	{netip.MustParsePrefix("fe80::/10"), "link-local addresses"},
	{netip.MustParsePrefix("224.0.0.0/4"), "multicast address space"},
	{netip.MustParsePrefix("ff00::/8"), "multicast address space"},
	{netip.MustParsePrefix("100.64.0.0/10"), "CGNAT shared space RFC6598"},
	{netip.MustParsePrefix("192.0.2.0/24"), "documentation range RFC5737"},
	{netip.MustParsePrefix("198.51.100.0/24"), "documentation range RFC5737"},
	{netip.MustParsePrefix("203.0.113.0/24"), "documentation range RFC5737"},
	{netip.MustParsePrefix("2001:db8::/32"), "documentation range RFC5737"},
	{netip.MustParsePrefix("192.88.99.0/24"), "6to4 deprecated anycast"},
	{netip.MustParsePrefix("240.0.0.0/4"), "class E reserved"},
	{netip.MustParsePrefix("0.0.0.0/8"), "this network RFC791"},
	{netip.MustParsePrefix("198.18.0.0/15"), "benchmarking range RFC2544"},
}

// nonPIIIPExact lists individual addresses that should be ignored: the
// well-known public DNS resolvers plus a small set of widely-used
// placeholder IPs (1.2.3.4 et al.). Keys are canonical netip strings.
var nonPIIIPExact = map[string]string{
	// Limited broadcast
	"255.255.255.255": "limited broadcast",

	// Cloudflare 1.1.1.1 family
	"1.0.0.1":              "Cloudflare 1.1.1.1 resolver",
	"1.0.0.2":              "Cloudflare 1.1.1.1 resolver",
	"1.0.0.3":              "Cloudflare 1.1.1.1 resolver",
	"1.1.1.1":              "Cloudflare 1.1.1.1 resolver",
	"1.1.1.2":              "Cloudflare 1.1.1.1 resolver",
	"1.1.1.3":              "Cloudflare 1.1.1.1 resolver",
	"2606:4700:4700::64":   "Cloudflare 1.1.1.1 resolver",
	"2606:4700:4700::1001": "Cloudflare 1.1.1.1 resolver",
	"2606:4700:4700::1002": "Cloudflare 1.1.1.1 resolver",
	"2606:4700:4700::1003": "Cloudflare 1.1.1.1 resolver",
	"2606:4700:4700::1111": "Cloudflare 1.1.1.1 resolver",
	"2606:4700:4700::1112": "Cloudflare 1.1.1.1 resolver",
	"2606:4700:4700::1113": "Cloudflare 1.1.1.1 resolver",
	"2606:4700:4700::6400": "Cloudflare 1.1.1.1 resolver",

	// Google Public DNS
	"8.8.4.4":              "Google public DNS",
	"8.8.8.8":              "Google public DNS",
	"2001:4860:4860::64":   "Google public DNS",
	"2001:4860:4860::6464": "Google public DNS",
	"2001:4860:4860::8844": "Google public DNS",
	"2001:4860:4860::8888": "Google public DNS",

	// OpenDNS / Cisco
	"208.67.220.123":  "OpenDNS Cisco resolver",
	"208.67.220.220":  "OpenDNS Cisco resolver",
	"208.67.220.222":  "OpenDNS Cisco resolver",
	"208.67.222.123":  "OpenDNS Cisco resolver",
	"208.67.222.220":  "OpenDNS Cisco resolver",
	"208.67.222.222":  "OpenDNS Cisco resolver",
	"2620:0:ccc::2":   "OpenDNS Cisco resolver",
	"2620:0:ccd::2":   "OpenDNS Cisco resolver",
	"2620:119:35::35": "OpenDNS Cisco resolver",
	"2620:119:53::53": "OpenDNS Cisco resolver",

	// Quad9
	"9.9.9.9":         "Quad9 secure DNS",
	"9.9.9.10":        "Quad9 secure DNS",
	"9.9.9.11":        "Quad9 secure DNS",
	"149.112.112.9":   "Quad9 secure DNS",
	"149.112.112.10":  "Quad9 secure DNS",
	"149.112.112.11":  "Quad9 secure DNS",
	"149.112.112.112": "Quad9 secure DNS",
	"2620:fe::9":      "Quad9 secure DNS",
	"2620:fe::10":     "Quad9 secure DNS",
	"2620:fe::11":     "Quad9 secure DNS",
	"2620:fe::fe":     "Quad9 secure DNS",
	"2620:fe::fe:9":   "Quad9 secure DNS",
	"2620:fe::fe:10":  "Quad9 secure DNS",
	"2620:fe::fe:11":  "Quad9 secure DNS",

	// AdGuard
	"94.140.14.14":       "AdGuard filtering DNS",
	"94.140.14.15":       "AdGuard filtering DNS",
	"94.140.14.140":      "AdGuard filtering DNS",
	"94.140.14.141":      "AdGuard filtering DNS",
	"94.140.15.15":       "AdGuard filtering DNS",
	"94.140.15.16":       "AdGuard filtering DNS",
	"2a10:50c0::1:ff":    "AdGuard filtering DNS",
	"2a10:50c0::2:ff":    "AdGuard filtering DNS",
	"2a10:50c0::ad1:ff":  "AdGuard filtering DNS",
	"2a10:50c0::ad2:ff":  "AdGuard filtering DNS",
	"2a10:50c0::bad1:ff": "AdGuard filtering DNS",
	"2a10:50c0::bad2:ff": "AdGuard filtering DNS",

	// CleanBrowsing
	"185.228.168.9":   "CleanBrowsing filtering DNS",
	"185.228.168.10":  "CleanBrowsing filtering DNS",
	"185.228.168.168": "CleanBrowsing filtering DNS",
	"185.228.169.9":   "CleanBrowsing filtering DNS",
	"185.228.169.11":  "CleanBrowsing filtering DNS",
	"185.228.169.168": "CleanBrowsing filtering DNS",

	// Control D
	"76.76.2.0":  "Control D customisable DNS",
	"76.76.10.0": "Control D customisable DNS",

	// Yandex
	"77.88.8.1":              "Yandex public DNS",
	"77.88.8.2":              "Yandex public DNS",
	"77.88.8.3":              "Yandex public DNS",
	"77.88.8.7":              "Yandex public DNS",
	"77.88.8.8":              "Yandex public DNS",
	"2a02:6b8::feed:ff":      "Yandex public DNS",
	"2a02:6b8::feed:a11":     "Yandex public DNS",
	"2a02:6b8::feed:bad":     "Yandex public DNS",
	"2a02:6b8:0:1::feed:ff":  "Yandex public DNS",
	"2a02:6b8:0:1::feed:a11": "Yandex public DNS",
	"2a02:6b8:0:1::feed:bad": "Yandex public DNS",

	// Vercara / UltraDNS (includes inherited Norton ConnectSafe IPs)
	"64.6.64.6":     "Vercara UltraDNS resolver",
	"64.6.65.6":     "Vercara UltraDNS resolver",
	"156.154.70.1":  "Vercara UltraDNS resolver",
	"156.154.70.2":  "Vercara UltraDNS resolver",
	"156.154.70.3":  "Vercara UltraDNS resolver",
	"156.154.70.4":  "Vercara UltraDNS resolver",
	"156.154.70.5":  "Vercara UltraDNS resolver",
	"156.154.71.1":  "Vercara UltraDNS resolver",
	"156.154.71.2":  "Vercara UltraDNS resolver",
	"156.154.71.3":  "Vercara UltraDNS resolver",
	"156.154.71.4":  "Vercara UltraDNS resolver",
	"156.154.71.5":  "Vercara UltraDNS resolver",
	"199.85.126.10": "Vercara UltraDNS resolver",
	"199.85.127.10": "Vercara UltraDNS resolver",

	// Lumen / Level3 carrier DNS
	"4.2.2.1":         "Lumen Level3 carrier DNS",
	"4.2.2.2":         "Lumen Level3 carrier DNS",
	"4.2.2.3":         "Lumen Level3 carrier DNS",
	"4.2.2.4":         "Lumen Level3 carrier DNS",
	"4.2.2.5":         "Lumen Level3 carrier DNS",
	"4.2.2.6":         "Lumen Level3 carrier DNS",
	"205.171.3.66":    "Lumen Level3 carrier DNS",
	"205.171.202.166": "Lumen Level3 carrier DNS",
	"209.244.0.3":     "Lumen Level3 carrier DNS",
	"209.244.0.4":     "Lumen Level3 carrier DNS",

	// Oracle Dyn
	"216.146.35.35": "Oracle Dyn public DNS",
	"216.146.36.36": "Oracle Dyn public DNS",

	// CIRA Canadian Shield
	"149.112.121.10": "CIRA Canadian Shield",
	"149.112.122.10": "CIRA Canadian Shield",

	// DNSforFamily (Hetzner-hosted)
	"78.47.64.161":   "DNSforFamily on Hetzner",
	"94.130.180.225": "DNSforFamily on Hetzner",

	// FlashStart
	"185.236.104.104": "FlashStart filtering DNS",
	"185.236.105.105": "FlashStart filtering DNS",

	// Misc niche resolvers
	"185.222.222.222":  "DNS.SB privacy resolver",
	"2001:558:feed::1": "Comcast Xfinity ISP DNS",
	"2001:558:feed::2": "Comcast Xfinity ISP DNS",
	"223.5.5.5":        "Alibaba AliDNS public",
	"223.6.6.6":        "Alibaba AliDNS public",
	"119.29.29.29":     "Tencent DNSPod public",
	"182.254.116.116":  "Tencent DNSPod public",
	"180.76.76.76":     "Baidu public DNS",

	// Common placeholder / literal-example IPs (no real PII content)
	"1.2.3.4":   "common placeholder address",
	"1.2.1.1":   "common placeholder address",
	"1.3.86.78": "common placeholder address",
	"2.1.2.3":   "common placeholder address",
	"2.2.2.2":   "common placeholder address",
	"2.2.7.3":   "common placeholder address",
	"25.8.3.66": "common placeholder address",
	"9.3.15.0":  "common placeholder address",
	"133.0.0.0": "common placeholder address",
	"45.1.37.1": "common placeholder address",
}
