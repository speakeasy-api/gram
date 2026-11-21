// Package problems groups the people who cannot connect right now into suspected problems.
//
// The rule, over the open episodes from mcpconnectionattempts.ReadFailingNow:
//  1. Each episode failed one way: its part and who fixes its latest reason.
//  2. Per way, these are candidates when MinPeople distinct people are in them:
//     - a server: its mcp_servers row however it was reached, else its kind and id;
//     - a service: the configured upstream (the issuer for a service sign-in when the
//     server has one, else the URL host), on MinServersPerService or more servers;
//     - an app version: a named client family and version, when another named family
//     was served on one of its servers after its earliest episode opened.
//  3. The candidate with the most people is picked, ties by most episodes, then app
//     version, server, service, then key. Its episodes leave the pool and the rest are
//     regrouped until no candidate qualifies.
//  4. Episodes left that opened OnePersonFor or more before now are one problem per
//     person and way, unless that person and way are already in a group problem.
package problems

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
)

const (
	// MinPeople is how many distinct people make a server, service or app version a problem.
	MinPeople = 3

	// MinServersPerService is how many servers must share an upstream for it to be a service problem.
	MinServersPerService = 2

	// OnePersonFor is how long one person must fail the same way to be a problem alone.
	OnePersonFor = 24 * time.Hour

	// TriedWithin is how recently a person must have tried to count.
	TriedWithin = 24 * time.Hour
)

// Rule is the rule a problem was listed by. The values are a contract with the dashboard.
type Rule string

const (
	// RuleSameServer is people failing the same way on one server.
	RuleSameServer Rule = "same_server"

	// RuleSameService is people failing the same way on servers sharing one upstream.
	RuleSameService Rule = "same_service"

	// RuleSameAppVersion is people failing the same way on one app version.
	RuleSameAppVersion Rule = "same_app_version"

	// RuleOnePerson is one person failing the same way for OnePersonFor.
	RuleOnePerson Rule = "one_person"
)

// ScopeKind is what a problem is about. The values are a contract with the dashboard.
type ScopeKind string

const (
	// ScopeServer is one server. Its key is the mcp_servers id, else kind:id.
	ScopeServer ScopeKind = "server"

	// ScopeService is a configured upstream. Its key is the issuer or the URL host.
	ScopeService ScopeKind = "service"

	// ScopeAppVersion is one client family and version. Its key is family@version.
	ScopeAppVersion ScopeKind = "app_version"

	// ScopePerson is one person. Its key is the subject URN.
	ScopePerson ScopeKind = "person"
)

// Scope is what a problem is about and the key that identifies it.
type Scope struct {
	// Kind is the kind of scope.
	Kind ScopeKind

	// Key identifies it within its kind.
	Key string
}

// App is a client family and version.
type App struct {
	// Family is the client family.
	Family string

	// Version is the client version.
	Version string
}

// Upstream is the configured upstream of an mcp_servers row.
type Upstream struct {
	// URL is the remote MCP server URL.
	URL string

	// Issuer is the issuer the server signs in to for the person, empty for none.
	Issuer string
}

// Problem is a suspected problem: people failing the same way that a rule groups.
type Problem struct {
	// ID is derived from the organization, scope and way.
	ID string

	// OrganizationID is the organization.
	OrganizationID string

	// Rule is the rule it was listed by.
	Rule Rule

	// Scope is what it is about.
	Scope Scope

	// Part is the part of the connection that is broken.
	Part mcpconnectionattempts.Part

	// WhoFixes is who has to fix the latest reason of its episodes.
	WhoFixes mcpconnectionattempts.WhoFixes

	// Since is the earliest OpenedAt of its episodes.
	Since time.Time

	// LastFailedAt is the latest failure of its episodes.
	LastFailedAt time.Time

	// People counts distinct subjects.
	People int

	// Episodes are the open episodes it groups.
	Episodes []mcpconnectionattempts.FailingPerson

	// SharedApp is the named family and version every episode has, zero otherwise.
	SharedApp App

	// OtherAppsServed is whether another named family was served on its servers after Since.
	OtherAppsServed bool
}

// way is how an episode failed: two episodes failed the same way when both are equal.
type way struct {
	part mcpconnectionattempts.Part
	who  mcpconnectionattempts.WhoFixes
}

func wayOf(e mcpconnectionattempts.FailingPerson) way {
	// Reasons missing from the catalog all share WhoNobody, so they are one way per part.
	meaning, _ := mcpconnectionattempts.MeaningOf(e.Reason)
	return way{part: e.Part, who: meaning.WhoFixes}
}

type servedKey struct{ kind, id, member string }

// serverOf is the episode's server: its mcp_servers row, else its own kind, id and member.
func serverOf(e mcpconnectionattempts.FailingPerson) string {
	if id := e.BackingServerID(); id != "" {
		return id
	}
	if e.MemberID == "" {
		return e.ServerKind + ":" + e.ServerID
	}
	return e.ServerKind + ":" + e.ServerID + ":" + e.MemberID
}

// kindRank breaks ties, narrowest first; the exhaustive linter requires ScopePerson.
var kindRank = map[ScopeKind]int{ScopeAppVersion: 0, ScopeServer: 1, ScopeService: 2, ScopePerson: 3}

var ruleOf = map[ScopeKind]Rule{
	ScopeServer:     RuleSameServer,
	ScopeService:    RuleSameService,
	ScopeAppVersion: RuleSameAppVersion,
	ScopePerson:     RuleOnePerson,
}

// grouper holds one Group call's inputs, indexed.
type grouper struct {
	organizationID string
	episodes       []mcpconnectionattempts.FailingPerson
	ways           []way
	servers        []string
	scopes         [][]Scope
	served         map[servedKey]map[string]time.Time
}

func newGrouper(organizationID string, episodes []mcpconnectionattempts.FailingPerson, upstreams map[string]Upstream, served []mcpconnectionattempts.ServedByFamily) grouper {
	g := grouper{
		organizationID: organizationID,
		episodes:       episodes,
		ways:           make([]way, len(episodes)),
		servers:        make([]string, len(episodes)),
		scopes:         make([][]Scope, len(episodes)),
		served:         map[servedKey]map[string]time.Time{},
	}
	for i, e := range episodes {
		g.ways[i] = wayOf(e)
		g.servers[i] = serverOf(e)
		if e.ServerID == "" {
			continue
		}
		g.scopes[i] = append(g.scopes[i], Scope{Kind: ScopeServer, Key: g.servers[i]})
		if key := serviceOf(e, upstreams); key != "" {
			g.scopes[i] = append(g.scopes[i], Scope{Kind: ScopeService, Key: key})
		}
		if mcpconnectionattempts.NamedClient(e.ClientFamily) && e.ClientVersion != "" {
			g.scopes[i] = append(g.scopes[i], Scope{Kind: ScopeAppVersion, Key: e.ClientFamily + "@" + e.ClientVersion})
		}
	}
	for _, s := range served {
		k := servedKey{kind: s.ServerKind, id: s.ServerID, member: s.MemberID}
		if g.served[k] == nil {
			g.served[k] = map[string]time.Time{}
		}
		if s.LastServedAt.After(g.served[k][s.ClientFamily]) {
			g.served[k][s.ClientFamily] = s.LastServedAt
		}
	}
	return g
}

// candidate is a scope in one way, with counts kept as its episodes leave the pool.
type candidate struct {
	scope     Scope
	members   []int
	alive     int
	subjects  map[string]int
	servers   map[string]int
	dirty     bool
	qualifies bool
	otherApps bool
}

// Group applies the package rule; upstreams is keyed by mcp_servers id.
func Group(organizationID string, episodes []mcpconnectionattempts.FailingPerson, upstreams map[string]Upstream, served []mcpconnectionattempts.ServedByFamily, now time.Time) []Problem {
	g := newGrouper(organizationID, episodes, upstreams, served)
	taken := make([]bool, len(episodes))
	grouped := map[personWay]bool{}

	byWay := map[way][]int{}
	for i := range episodes {
		if len(g.scopes[i]) > 0 {
			byWay[g.ways[i]] = append(byWay[g.ways[i]], i)
		}
	}

	var out []Problem
	for w, idx := range byWay {
		for _, p := range g.groupWay(w, idx, taken) {
			for _, e := range p.Episodes {
				grouped[personWay{subject: e.SubjectURN, way: w}] = true
			}
			out = append(out, p)
		}
	}
	out = append(out, g.longFailures(taken, grouped, now)...)

	slices.SortFunc(out, func(a, b Problem) int {
		return cmp.Or(cmp.Compare(b.People, a.People), a.Since.Compare(b.Since), cmp.Compare(a.ID, b.ID))
	})
	return out
}

// groupWay picks the candidates of one way. Picks in one way never change another.
func (g grouper) groupWay(w way, idx []int, taken []bool) []Problem {
	cands := map[Scope]*candidate{}
	for _, i := range idx {
		for _, s := range g.scopes[i] {
			c := cands[s]
			if c == nil {
				c = &candidate{scope: s, members: nil, alive: 0, subjects: map[string]int{}, servers: map[string]int{}, dirty: true, qualifies: false, otherApps: false}
				cands[s] = c
			}
			c.members = append(c.members, i)
			c.alive++
			c.subjects[g.episodes[i].SubjectURN]++
			c.servers[g.servers[i]]++
		}
	}
	for s, c := range cands {
		if len(c.subjects) < MinPeople {
			delete(cands, s)
		}
	}

	var out []Problem
	for {
		var best *candidate
		for _, c := range cands {
			if c.dirty {
				g.evaluate(c, taken)
			}
			if c.qualifies && (best == nil || rank(c.scope, len(c.subjects), c.alive, best.scope, len(best.subjects), best.alive) < 0) {
				best = c
			}
		}
		if best == nil {
			return out
		}

		delete(cands, best.scope)
		picked := g.aliveMembers(best, taken)
		for _, i := range picked {
			taken[i] = true
			for _, s := range g.scopes[i] {
				c := cands[s]
				if c == nil {
					continue
				}
				c.alive--
				c.dirty = true
				if c.subjects[g.episodes[i].SubjectURN]--; c.subjects[g.episodes[i].SubjectURN] == 0 {
					delete(c.subjects, g.episodes[i].SubjectURN)
				}
				if c.servers[g.servers[i]]--; c.servers[g.servers[i]] == 0 {
					delete(c.servers, g.servers[i])
				}
				if len(c.subjects) < MinPeople {
					delete(cands, s)
				}
			}
		}
		otherApps := best.otherApps
		if best.scope.Kind != ScopeAppVersion {
			otherApps = g.otherAppsServed(picked)
		}
		out = append(out, g.problem(best.scope, w, picked, len(best.subjects), otherApps))
	}
}

func (g grouper) aliveMembers(c *candidate, taken []bool) []int {
	out := make([]int, 0, c.alive)
	for _, i := range c.members {
		if !taken[i] {
			out = append(out, i)
		}
	}
	return out
}

// evaluate refreshes whether c qualifies from its episodes still in the pool.
func (g grouper) evaluate(c *candidate, taken []bool) {
	c.dirty = false
	c.qualifies = len(c.subjects) >= MinPeople
	switch c.scope.Kind {
	case ScopeService:
		c.qualifies = c.qualifies && len(c.servers) >= MinServersPerService
	case ScopeAppVersion:
		c.otherApps = c.qualifies && g.otherAppsServed(g.aliveMembers(c, taken))
		c.qualifies = c.otherApps
	case ScopeServer, ScopePerson:
	}
}

// rank orders candidates: most people, most episodes, narrowest kind, then key.
func rank(a Scope, aPeople, aEpisodes int, b Scope, bPeople, bEpisodes int) int {
	return cmp.Or(
		cmp.Compare(bPeople, aPeople),
		cmp.Compare(bEpisodes, aEpisodes),
		cmp.Compare(kindRank[a.Kind], kindRank[b.Kind]),
		cmp.Compare(a.Key, b.Key),
	)
}

type personWay struct {
	subject string
	way     way
}

// longFailures makes the one-person problems from the episodes left.
func (g grouper) longFailures(taken []bool, grouped map[personWay]bool, now time.Time) []Problem {
	groups := map[personWay][]int{}
	for i, e := range g.episodes {
		k := personWay{subject: e.SubjectURN, way: g.ways[i]}
		if taken[i] || grouped[k] || now.Sub(e.OpenedAt) < OnePersonFor {
			continue
		}
		groups[k] = append(groups[k], i)
	}
	out := make([]Problem, 0, len(groups))
	for k, idx := range groups {
		out = append(out, g.problem(Scope{Kind: ScopePerson, Key: k.subject}, k.way, idx, 1, g.otherAppsServed(idx)))
	}
	return out
}

func (g grouper) problem(scope Scope, w way, idx []int, people int, otherApps bool) Problem {
	eps := make([]mcpconnectionattempts.FailingPerson, 0, len(idx))
	for _, i := range idx {
		eps = append(eps, g.episodes[i])
	}
	slices.SortFunc(eps, compareEpisodes)

	// Sorted by OpenedAt, so the first opened earliest.
	p := Problem{
		ID:              problemID(g.organizationID, scope, w),
		OrganizationID:  g.organizationID,
		Rule:            ruleOf[scope.Kind],
		Scope:           scope,
		Part:            w.part,
		WhoFixes:        w.who,
		Since:           eps[0].OpenedAt,
		LastFailedAt:    eps[0].LastFailureAt,
		People:          people,
		Episodes:        eps,
		SharedApp:       sharedApp(eps),
		OtherAppsServed: otherApps,
	}
	for _, e := range eps[1:] {
		if e.LastFailureAt.After(p.LastFailedAt) {
			p.LastFailedAt = e.LastFailureAt
		}
	}
	return p
}

// otherAppsServed matches servers exactly on kind, id and member; a gateway-level row has no member.
func (g grouper) otherAppsServed(idx []int) bool {
	families := map[string]bool{}
	var since time.Time
	for n, i := range idx {
		e := g.episodes[i]
		families[e.ClientFamily] = true
		if n == 0 || e.OpenedAt.Before(since) {
			since = e.OpenedAt
		}
	}
	for _, i := range idx {
		e := g.episodes[i]
		for family, at := range g.served[servedKey{kind: e.ServerKind, id: e.ServerID, member: e.MemberID}] {
			if mcpconnectionattempts.NamedClient(family) && !families[family] && at.After(since) {
				return true
			}
		}
	}
	return false
}

// serviceOf is the episode's configured upstream, empty when its server has none.
func serviceOf(e mcpconnectionattempts.FailingPerson, upstreams map[string]Upstream) string {
	id := e.BackingServerID()
	u, ok := upstreams[id]
	if id == "" || !ok {
		return ""
	}
	if e.Part == mcpconnectionattempts.PartServiceSignIn {
		if issuer := issuerKey(u.Issuer); issuer != "" {
			return issuer
		}
	}
	host, _ := hostAndPath(u.URL)
	return host
}

// issuerKey is lowercase host plus path: tenants of one identity provider share its host.
func issuerKey(raw string) string {
	host, path := hostAndPath(raw)
	if host == "" {
		return ""
	}
	return host + strings.TrimRight(path, "/")
}

func hostAndPath(raw string) (string, string) {
	if raw == "" {
		return "", ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", ""
	}
	return strings.TrimSuffix(strings.ToLower(parsed.Hostname()), "."), parsed.Path
}

func sharedApp(eps []mcpconnectionattempts.FailingPerson) App {
	first := eps[0]
	if !mcpconnectionattempts.NamedClient(first.ClientFamily) || first.ClientVersion == "" {
		return App{Family: "", Version: ""}
	}
	for _, e := range eps[1:] {
		if e.ClientFamily != first.ClientFamily || e.ClientVersion != first.ClientVersion {
			return App{Family: "", Version: ""}
		}
	}
	return App{Family: first.ClientFamily, Version: first.ClientVersion}
}

func problemID(organizationID string, scope Scope, w way) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{organizationID, string(scope.Kind), scope.Key, string(w.part), string(w.who)}, "\x00")))
	return hex.EncodeToString(sum[:16])
}

func compareEpisodes(a, b mcpconnectionattempts.FailingPerson) int {
	return cmp.Or(
		a.OpenedAt.Compare(b.OpenedAt),
		cmp.Compare(a.SubjectURN, b.SubjectURN),
		cmp.Compare(a.ServerKind, b.ServerKind),
		cmp.Compare(a.ServerID, b.ServerID),
		cmp.Compare(a.MemberID, b.MemberID),
		cmp.Compare(a.AppKey, b.AppKey),
		cmp.Compare(a.Part, b.Part),
		cmp.Compare(a.ClientFamily, b.ClientFamily),
		cmp.Compare(a.Reason, b.Reason),
		a.LastFailureAt.Compare(b.LastFailureAt),
		cmp.Compare(a.ProjectID.String(), b.ProjectID.String()),
		cmp.Compare(a.Step, b.Step),
	)
}
