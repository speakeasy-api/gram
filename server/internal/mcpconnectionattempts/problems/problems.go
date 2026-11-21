// Package problems groups the people who cannot connect right now into suspected
// problems: several people failing the same way on one server, service or app version,
// or one person failing the same way for a day.
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
	// MinPeople is how many distinct people a server, service or app version needs
	// failing the same way before it is a problem. Fewer can be a coincidence.
	MinPeople = 3

	// OnePersonFor is how long one person must fail the same way, counted from the
	// first failure of the run, before that alone is a problem.
	OnePersonFor = 24 * time.Hour

	// TriedWithin is how recently someone must have tried to count: a person who has
	// not tried for a day is no longer waiting on a fix.
	TriedWithin = 24 * time.Hour
)

// Rule is the rule a problem was listed by. The values are a contract with the dashboard.
type Rule string

const (
	// RuleThreePeopleSameServer is MinPeople people failing the same way on one server.
	RuleThreePeopleSameServer Rule = "three_people_same_server"

	// RuleThreePeopleSameService is MinPeople people failing the same way on servers
	// that share one configured upstream.
	RuleThreePeopleSameService Rule = "three_people_same_service"

	// RuleThreePeopleSameAppVersion is MinPeople people failing the same way on one app
	// version while other apps were served.
	RuleThreePeopleSameAppVersion Rule = "three_people_same_app_version"

	// RuleOnePerson24Hours is one person failing the same way for OnePersonFor.
	RuleOnePerson24Hours Rule = "one_person_24_hours"
)

// ScopeKind is what a problem is about. The values are a contract with the dashboard.
type ScopeKind string

const (
	// ScopeServer is one server, gateway member or toolset. Its key is kind:id, plus
	// :member for a gateway member.
	ScopeServer ScopeKind = "server"

	// ScopeService is a configured upstream. Its key is the lowercase host.
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

// ServedByFamily is the latest request served on one server for one client family.
type ServedByFamily struct {
	// ServerKind is the kind of server.
	ServerKind string `ch:"server_kind"`

	// ServerID is the server, gateway or toolset.
	ServerID string `ch:"server_id"`

	// MemberID is the gateway member, empty for none.
	MemberID string `ch:"member_id"`

	// ClientFamily is the client family served.
	ClientFamily string `ch:"client_family"`

	// LastServedAt is when a request was last served.
	LastServedAt time.Time `ch:"last_served_at"`
}

// Problem is a suspected problem: people failing the same way that a rule groups.
type Problem struct {
	// ID is derived from the organization, scope and way, so it stays the same while
	// the problem exists.
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

	// SharedApp is the family and version when every episode has the same named family
	// and non-empty version, zero otherwise.
	SharedApp App

	// OtherAppsServed is whether a named client family none of its episodes failed with
	// was served on one of its servers after Since. Servers match on kind, id and member
	// exactly; a gateway-level served row has an empty member.
	OtherAppsServed bool
}

// way is how an episode failed: two episodes failed the same way when both are equal.
type way struct {
	part mcpconnectionattempts.Part
	who  mcpconnectionattempts.WhoFixes
}

func wayOf(e mcpconnectionattempts.FailingPerson) way {
	meaning, _ := mcpconnectionattempts.MeaningOf(e.Reason)
	return way{part: e.Part, who: meaning.WhoFixes}
}

type serverKey struct{ kind, id, member string }

func serverOf(e mcpconnectionattempts.FailingPerson) serverKey {
	return serverKey{kind: e.ServerKind, id: e.ServerID, member: e.MemberID}
}

func (k serverKey) String() string {
	if k.member == "" {
		return k.kind + ":" + k.id
	}
	return k.kind + ":" + k.id + ":" + k.member
}

// candidate is a scope with enough people failing one way to be picked.
type candidate struct {
	scope    Scope
	way      way
	episodes []int
	people   int
}

// kindRank breaks ties between candidates of equal size: the narrowest explanation first.
var kindRank = map[ScopeKind]int{ScopeAppVersion: 0, ScopeServer: 1, ScopeService: 2, ScopePerson: 3}

var ruleOf = map[ScopeKind]Rule{
	ScopeServer:     RuleThreePeopleSameServer,
	ScopeService:    RuleThreePeopleSameService,
	ScopeAppVersion: RuleThreePeopleSameAppVersion,
	ScopePerson:     RuleOnePerson24Hours,
}

// grouper holds one Group call's inputs, indexed.
type grouper struct {
	organizationID string
	episodes       []mcpconnectionattempts.FailingPerson
	ways           []way
	upstreams      map[string]Upstream
	served         map[serverKey]map[string]time.Time
}

// Group turns open episodes into suspected problems, each episode in at most one:
//  1. Each episode failed one way: its part and who fixes its latest reason.
//  2. Per way, a server, a service (an upstream shared by two or more servers) or an app
//     version (when another family was served on its servers since) with MinPeople
//     distinct people is a candidate.
//  3. The candidate with the most people is picked, ties by most episodes, then app
//     version, server, service, then key. Its episodes leave the pool and candidates are
//     rebuilt until none qualifies.
//  4. Episodes left that opened OnePersonFor before now are one problem per person and way.
//
// upstreams is keyed by mcp_servers id. Output is ordered by most people, then Since, then ID.
func Group(organizationID string, episodes []mcpconnectionattempts.FailingPerson, upstreams map[string]Upstream, served []ServedByFamily, now time.Time) []Problem {
	g := grouper{
		organizationID: organizationID,
		episodes:       episodes,
		ways:           make([]way, len(episodes)),
		upstreams:      upstreams,
		served:         map[serverKey]map[string]time.Time{},
	}
	for i, e := range episodes {
		g.ways[i] = wayOf(e)
	}
	for _, s := range served {
		k := serverKey{kind: s.ServerKind, id: s.ServerID, member: s.MemberID}
		if g.served[k] == nil {
			g.served[k] = map[string]time.Time{}
		}
		if s.LastServedAt.After(g.served[k][s.ClientFamily]) {
			g.served[k][s.ClientFamily] = s.LastServedAt
		}
	}

	pool := make([]bool, len(episodes))
	for i := range pool {
		pool[i] = true
	}

	var out []Problem
	for {
		best, ok := g.best(pool)
		if !ok {
			break
		}
		for _, i := range best.episodes {
			pool[i] = false
		}
		out = append(out, g.problem(best))
	}
	out = append(out, g.longFailures(pool, now)...)

	slices.SortFunc(out, func(a, b Problem) int {
		return cmp.Or(cmp.Compare(b.People, a.People), a.Since.Compare(b.Since), cmp.Compare(a.ID, b.ID))
	})
	return out
}

// best returns the qualifying candidate to pick from the episodes still in the pool.
func (g grouper) best(pool []bool) (candidate, bool) {
	groups := map[way]map[Scope][]int{}
	add := func(i int, s Scope) {
		w := g.ways[i]
		if groups[w] == nil {
			groups[w] = map[Scope][]int{}
		}
		groups[w][s] = append(groups[w][s], i)
	}
	for i, e := range g.episodes {
		if !pool[i] || e.ServerID == "" {
			continue
		}
		add(i, Scope{Kind: ScopeServer, Key: serverOf(e).String()})
		if host := g.serviceOf(e); host != "" {
			add(i, Scope{Kind: ScopeService, Key: host})
		}
		if mcpconnectionattempts.NamedClient(e.ClientFamily) && e.ClientVersion != "" {
			add(i, Scope{Kind: ScopeAppVersion, Key: e.ClientFamily + "@" + e.ClientVersion})
		}
	}

	var best candidate
	found := false
	for w, scopes := range groups {
		for s, idx := range scopes {
			c := candidate{scope: s, way: w, episodes: idx, people: g.people(idx)}
			if !g.qualifies(c) {
				continue
			}
			if !found || better(c, best) {
				best, found = c, true
			}
		}
	}
	return best, found
}

func (g grouper) qualifies(c candidate) bool {
	if c.people < MinPeople {
		return false
	}
	switch c.scope.Kind {
	case ScopeService:
		servers := map[serverKey]bool{}
		for _, i := range c.episodes {
			servers[serverOf(g.episodes[i])] = true
		}
		return len(servers) >= 2
	case ScopeAppVersion:
		return g.otherAppsServed(c.episodes)
	case ScopeServer, ScopePerson:
		return true
	}
	return false
}

func better(a, b candidate) bool {
	return cmp.Or(
		cmp.Compare(b.people, a.people),
		cmp.Compare(len(b.episodes), len(a.episodes)),
		cmp.Compare(kindRank[a.scope.Kind], kindRank[b.scope.Kind]),
		cmp.Compare(a.scope.Key, b.scope.Key),
		cmp.Compare(a.way.part, b.way.part),
		cmp.Compare(a.way.who, b.way.who),
	) < 0
}

// longFailures makes one problem per person and way from the pool's episodes that
// opened OnePersonFor or more before now.
func (g grouper) longFailures(pool []bool, now time.Time) []Problem {
	type personWay struct {
		subject string
		way     way
	}
	groups := map[personWay][]int{}
	var order []personWay
	for i, e := range g.episodes {
		if !pool[i] || now.Sub(e.OpenedAt) < OnePersonFor {
			continue
		}
		k := personWay{subject: e.SubjectURN, way: g.ways[i]}
		if groups[k] == nil {
			order = append(order, k)
		}
		groups[k] = append(groups[k], i)
	}
	out := make([]Problem, 0, len(order))
	for _, k := range order {
		idx := groups[k]
		out = append(out, g.problem(candidate{scope: Scope{Kind: ScopePerson, Key: k.subject}, way: k.way, episodes: idx, people: 1}))
	}
	return out
}

func (g grouper) problem(c candidate) Problem {
	eps := make([]mcpconnectionattempts.FailingPerson, 0, len(c.episodes))
	for _, i := range c.episodes {
		eps = append(eps, g.episodes[i])
	}
	slices.SortFunc(eps, compareEpisodes)

	// Sorted by OpenedAt, so the first opened earliest.
	p := Problem{
		ID:              problemID(g.organizationID, c.scope, c.way),
		OrganizationID:  g.organizationID,
		Rule:            ruleOf[c.scope.Kind],
		Scope:           c.scope,
		Part:            c.way.part,
		WhoFixes:        c.way.who,
		Since:           eps[0].OpenedAt,
		LastFailedAt:    eps[0].LastFailureAt,
		People:          g.people(c.episodes),
		Episodes:        eps,
		SharedApp:       sharedApp(eps),
		OtherAppsServed: g.otherAppsServed(c.episodes),
	}
	for _, e := range eps[1:] {
		if e.LastFailureAt.After(p.LastFailedAt) {
			p.LastFailedAt = e.LastFailureAt
		}
	}
	return p
}

func (g grouper) people(idx []int) int {
	subjects := map[string]bool{}
	for _, i := range idx {
		subjects[g.episodes[i].SubjectURN] = true
	}
	return len(subjects)
}

// otherAppsServed is whether a named family none of the episodes failed with was served
// on one of their servers after the earliest of them opened. An unknown or generic family
// proves nothing about which app was served.
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
		for family, at := range g.served[serverOf(g.episodes[i])] {
			if mcpconnectionattempts.NamedClient(family) && !families[family] && at.After(since) {
				return true
			}
		}
	}
	return false
}

// serviceOf is the episode's configured upstream host, empty when its server has none.
// A service sign-in is with the issuer when the server has one; everything else is with
// the URL.
func (g grouper) serviceOf(e mcpconnectionattempts.FailingPerson) string {
	var id string
	switch e.ServerKind {
	case mcpconnectionattempts.ServerKindMCP:
		id = e.ServerID
	case mcpconnectionattempts.ServerKindMeta:
		id = e.MemberID
	}
	u, ok := g.upstreams[id]
	if id == "" || !ok {
		return ""
	}
	if e.Part == mcpconnectionattempts.PartServiceSignIn {
		if issuer := hostOf(u.Issuer); issuer != "" {
			return issuer
		}
	}
	return hostOf(u.URL)
}

// hostOf is the lowercase host of a URL, without port, empty when it has none.
func hostOf(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
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
	)
}
