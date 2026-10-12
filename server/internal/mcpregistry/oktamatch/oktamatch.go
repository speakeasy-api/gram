// Package oktamatch proposes Okta Integration Network application names for
// a catalog entry from names observed across synced tenants. It is a
// heuristic for staff to confirm, never a source of truth: Okta exposes no
// link from an application to an MCP server.
package oktamatch

import (
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// Observed is one application name seen in tenant snapshots, aggregated.
type Observed struct {
	// Name is the Okta application name, the OIN key.
	Name string
	// Labels are distinct admin-facing labels seen for the name.
	Labels []string
	// SignOnModes are the distinct sign-on modes seen for the name.
	SignOnModes []string
	// Organizations is how many organizations have the application.
	Organizations int
}

// Entry is the slice of a catalog record the heuristic reads.
type Entry struct {
	Name  string
	Title string
	// Hosts are the remote and website hosts.
	Hosts []string
}

// Reasons, strongest first.
const (
	ReasonDomain = "domain"
	ReasonTitle  = "title"
	ReasonLabel  = "label"
)

// Candidate is a proposed name with why it matched.
type Candidate struct {
	Observed
	Reason string
}

var integratorPrefix = regexp.MustCompile(`^integrator-\d+_`)

var instanceSuffix = regexp.MustCompile(`_\d+$`)

var nonWord = regexp.MustCompile(`[^a-z0-9]+`)

// genericNames are what Okta assigns to admin-created apps; they say nothing
// about the vendor, and their labels are chosen by the tenant.
var genericNames = regexp.MustCompile(`^(oidc_client|bookmark|template_.*|saml_2_0|wsfed|auto_login|browser_plugin|secure_password_store)$`)

// Integrator reports whether the name comes from an integrator listing rather
// than Okta's public catalog. Any integrator account can publish under a
// vendor-like key, so staff should confirm these with more care.
func Integrator(name string) bool {
	return integratorPrefix.MatchString(strings.ToLower(name))
}

// Key reduces an Okta application name to its vendor token: the integrator
// prefix and instance suffix are dropped and separators removed, so
// "integrator-4080826_linear_1" and "linear" both read "linear".
func Key(name string) string {
	name = strings.ToLower(name)
	name = integratorPrefix.ReplaceAllString(name, "")
	name = instanceSuffix.ReplaceAllString(name, "")
	return nonWord.ReplaceAllString(name, "")
}

// EntryFromRecord reads the entry facts from a stored catalog record.
func EntryFromRecord(data json.RawMessage) Entry {
	var root struct {
		Server struct {
			Name       string `json:"name"`
			Title      string `json:"title"`
			WebsiteURL string `json:"websiteUrl"`
			Remotes    []struct {
				URL string `json:"url"`
			} `json:"remotes"`
		} `json:"server"`
	}
	_ = json.Unmarshal(data, &root)
	hosts := make([]string, 0, len(root.Server.Remotes)+1)
	if root.Server.WebsiteURL != "" {
		hosts = append(hosts, root.Server.WebsiteURL)
	}
	for _, r := range root.Server.Remotes {
		hosts = append(hosts, r.URL)
	}
	return Entry{Name: root.Server.Name, Title: root.Server.Title, Hosts: hosts}
}

// Match returns the observed names that plausibly belong to the entry,
// strongest reason first, then by how many organizations have the app.
func Match(entry Entry, observed []Observed) []Candidate {
	domains := domainTokens(entry)
	title := nonWord.ReplaceAllString(strings.ToLower(entry.Title), "")
	candidates := make([]Candidate, 0)
	for _, o := range observed {
		key := Key(o.Name)
		if key == "" || genericNames.MatchString(strings.ToLower(o.Name)) {
			continue
		}
		switch {
		case domains[key]:
			candidates = append(candidates, Candidate{Observed: o, Reason: ReasonDomain})
		case title != "" && key == title:
			candidates = append(candidates, Candidate{Observed: o, Reason: ReasonTitle})
		case title != "" && labelMatches(o.Labels, entry.Title):
			candidates = append(candidates, Candidate{Observed: o, Reason: ReasonLabel})
		}
	}
	rank := map[string]int{ReasonDomain: 0, ReasonTitle: 1, ReasonLabel: 2}
	sort.SliceStable(candidates, func(i, j int) bool {
		if rank[candidates[i].Reason] != rank[candidates[j].Reason] {
			return rank[candidates[i].Reason] < rank[candidates[j].Reason]
		}
		if candidates[i].Organizations != candidates[j].Organizations {
			return candidates[i].Organizations > candidates[j].Organizations
		}
		return candidates[i].Name < candidates[j].Name
	})
	return candidates
}

// domainTokens collects the registrable label of every host, plus the same
// label from the entry name's reverse-DNS prefix ("com.notion/mcp" → notion).
func domainTokens(entry Entry) map[string]bool {
	tokens := map[string]bool{}
	for _, raw := range entry.Hosts {
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" {
			continue
		}
		if label := registrableLabel(u.Hostname()); label != "" {
			tokens[label] = true
		}
	}
	if prefix, _, ok := strings.Cut(entry.Name, "/"); ok {
		parts := strings.Split(strings.ToLower(prefix), ".")
		if len(parts) >= 2 {
			tokens[nonWord.ReplaceAllString(parts[len(parts)-1], "")] = true
		}
	}
	delete(tokens, "")
	return tokens
}

// secondLevelSuffixes are the common two-label public suffixes; anything
// else is treated as a single-label suffix, so "mcp.foo.io" reads foo.
var secondLevelSuffixes = map[string]bool{
	"co.uk": true, "org.uk": true, "ac.uk": true, "gov.uk": true, "me.uk": true,
	"co.jp": true, "ne.jp": true, "or.jp": true, "ac.jp": true,
	"com.au": true, "net.au": true, "org.au": true, "edu.au": true,
	"com.br": true, "com.mx": true, "com.ar": true, "com.co": true,
	"co.nz": true, "co.in": true, "co.za": true, "co.kr": true, "co.il": true,
	"com.sg": true, "com.cn": true, "com.tw": true, "com.hk": true, "com.my": true, "com.tr": true,
}

// registrableLabel returns the label left of the public suffix
// ("mcp.notion.com" → notion, "mcp.atlassian.co.uk" → atlassian,
// "mcp.foo.io" → foo).
func registrableLabel(host string) string {
	parts := strings.Split(strings.ToLower(host), ".")
	n := len(parts)
	if n < 2 {
		return ""
	}
	if n >= 3 && secondLevelSuffixes[parts[n-2]+"."+parts[n-1]] {
		return nonWord.ReplaceAllString(parts[n-3], "")
	}
	return nonWord.ReplaceAllString(parts[n-2], "")
}

// labelMatches reports whether any tenant label equals the entry title, word
// for word after normalization, so "Linear - XAA" does not match "Linear"
// but "Notion" matches "Notion".
func labelMatches(labels []string, title string) bool {
	want := nonWord.ReplaceAllString(strings.ToLower(title), "")
	for _, l := range labels {
		if nonWord.ReplaceAllString(strings.ToLower(l), "") == want {
			return true
		}
	}
	return false
}
