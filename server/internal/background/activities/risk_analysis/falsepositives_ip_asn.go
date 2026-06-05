package risk_analysis

import (
	_ "embed"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"sync"

	"github.com/oschwald/maxminddb-golang"
)

//go:embed data/dbip-asn.mmdb
var dbipASNBytes []byte

// infraASNRE matches the AS organisation names of cloud, CDN, and
// managed-hosting providers as DB-IP labels them. Goal: drop org names
// that belong only to infrastructure, never to an end user's home
// connection.
//
// Conservatism rules (apply when extending):
//   - Bare "google" would over-match "Google Fiber Inc." (residential
//     ISP). Use "google llc" / "google cloud" / "youtube" instead, which
//     pin to the AS15169 / Google Cloud Platform / YouTube AS naming.
//   - "amazon", "microsoft", "azure", "oracle cloud", "alibaba",
//     "tencent" are kept bare because none of those companies operate a
//     consumer ISP, so the DB-IP org name is always infrastructure.
//   - Consumer-ISP brands (Verizon, Virgin Media, Comcast, AT&T, BT,
//     Charter, Telefonica, etc.) are intentionally excluded: their IPs
//     can identify an end user and should still flow through as PII.
var infraASNRE = regexp.MustCompile(`(?i)\b(amazon|aws|google llc|google cloud|googlebot|youtube|microsoft|azure|cloudflare|fastly|github|akamai|digitalocean|linode|hetzner|ovh|alibaba|tencent|baidu|vultr|the constant company|scaleway|leaseweb|hostinger|oracle cloud|equinix|cloudfront|netlify|vercel|datacamp|m247|choopa|phoenix nap|hurricane electric|psychz|colocrossing|sharktech|cdn|cdnetworks|edgecast|maxcdn|keycdn|jsdelivr|stackpath|cachefly|level 3|lumen)\b`)

var (
	dbipReaderOnce sync.Once
	dbipReader     *maxminddb.Reader
)

func loadDBIPReader() *maxminddb.Reader {
	dbipReaderOnce.Do(func() {
		r, err := maxminddb.FromBytes(dbipASNBytes)
		if err != nil {
			// The mmdb is embedded at compile time; a parse failure means
			// the checked-in file is corrupt. Leave dbipReader nil so
			// lookups become no-ops rather than crashing the server.
			return
		}
		dbipReader = r
	})
	return dbipReader
}

// asnRecord is the subset of the DB-IP ASN record we read.
type asnRecord struct {
	ASN int    `maxminddb:"autonomous_system_number"`
	Org string `maxminddb:"autonomous_system_organization"`
}

// infraASNDescription returns a short description if addr belongs to an AS
// organisation matching infraASNRE (cloud / CDN / managed hosting), or
// "" otherwise. Returns "" on parse / lookup failure so the caller falls
// through to "treat as PII".
func infraASNDescription(addr netip.Addr) string {
	r := loadDBIPReader()
	if r == nil {
		return ""
	}
	ip := net.IP(addr.AsSlice())
	if ip == nil {
		return ""
	}
	var rec asnRecord
	if err := r.Lookup(ip, &rec); err != nil || rec.ASN == 0 {
		return ""
	}
	if !infraASNRE.MatchString(rec.Org) {
		return ""
	}
	return fmt.Sprintf("AS%d %s (cloud, CDN, or managed hosting)", rec.ASN, rec.Org)
}
