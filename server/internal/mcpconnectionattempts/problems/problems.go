// Package problems groups the people who cannot connect right now into suspected problems.
//
// An episode is one person's ongoing failure on one connection, from
// mcpconnectionattempts.ReadFailingNow. Only people (users) count. The rule:
//  1. Sort the failures by kind: which part of the connection broke, and who has to fix
//     the latest error. Only failures of the same kind are grouped.
//  2. Within each kind, a group is MinPeople or more people whose failures share one of:
//     - a server (inbound): the Speakeasy MCP server, gateway or toolset the app connects
//     to, as its mcp_servers row however reached;
//     - a service (outbound), only for service sign-in or server failures the provider
//     fixes: what the server connects out to (the remote session issuer for a service
//     sign-in when the server has one, else the remote URL host), on MinServersPerService
//     or more servers;
//     - an app version: a named client family and version, when another named family
//     worked on one of its servers after its first failure there (served for an app
//     sign-in, answered by the server otherwise).
//  3. Every group becomes a problem. A failure that fits several is counted in one: the
//     group with the most people is taken first (ties: app version, server, service, then
//     key), its failures are set aside, and the rest are grouped again until none is left.
//  4. A failure in no group that opened OnePersonFor or more before now is a problem for
//     that person, one per person and kind.
package problems

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/issuerurl"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// MinPeople is how many distinct people make a server, service or app version a problem.
	MinPeople = 2

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
	// ScopeServer is the inbound side: one Speakeasy MCP server, gateway or toolset. Its key is the mcp_servers id, else kind:id.
	ScopeServer ScopeKind = "server"

	// ScopeService is the outbound side: what servers connect out to. Its key is the remote session issuer or the remote URL host.
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
	// ID is a key of organization, scope and way, not an incident id: it changes when another scope wins.
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

	// OtherAppsServed is whether another named family worked on one of its servers after its first failure there.
	OtherAppsServed bool

	// OthersOnScope counts the people failing on its scope who are not in it, zero for a person.
	OthersOnScope int
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

// isPerson reports whether the subject is a user; keys, agents and workloads are not counted.
func isPerson(subject string) bool {
	s, err := urn.ParseSessionSubject(subject)
	return err == nil && s.Kind == urn.SessionSubjectKindUser
}

type servedKey struct{ kind, id, member string }

func servedKeyOf(e mcpconnectionattempts.FailingPerson) servedKey {
	return servedKey{kind: e.ServerKind, id: e.ServerID, member: e.MemberID}
}

// workedAt is when a family was last served and last answered on one server.
type workedAt struct{ served, answered time.Time }

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
	people         []bool
	ways           []way
	servers        []string
	scopes         [][]Scope
	onScope        map[Scope]map[string]struct{}
	worked         map[servedKey]map[string]workedAt
}

func newGrouper(organizationID string, episodes []mcpconnectionattempts.FailingPerson, upstreams map[string]Upstream, served []mcpconnectionattempts.ServedByFamily) grouper {
	g := grouper{
		organizationID: organizationID,
		episodes:       episodes,
		people:         make([]bool, len(episodes)),
		ways:           make([]way, len(episodes)),
		servers:        make([]string, len(episodes)),
		scopes:         make([][]Scope, len(episodes)),
		worked:         map[servedKey]map[string]workedAt{},
		onScope:        map[Scope]map[string]struct{}{},
	}
	for i, e := range episodes {
		g.people[i] = isPerson(e.SubjectURN)
		g.ways[i] = wayOf(e)
		g.servers[i] = serverOf(e)
		if g.people[i] {
			g.scopes[i] = scopesOf(e, g.ways[i], g.servers[i], upstreams)
			for _, sc := range g.scopes[i] {
				if g.onScope[sc] == nil {
					g.onScope[sc] = map[string]struct{}{}
				}
				g.onScope[sc][e.SubjectURN] = struct{}{}
			}
		}
	}
	for _, s := range served {
		k := servedKey{kind: s.ServerKind, id: s.ServerID, member: s.MemberID}
		if g.worked[k] == nil {
			g.worked[k] = map[string]workedAt{}
		}
		w := g.worked[k][s.ClientFamily]
		if s.LastServedAt.After(w.served) {
			w.served = s.LastServedAt
		}
		if s.LastAnsweredAt.After(w.answered) {
			w.answered = s.LastAnsweredAt
		}
		g.worked[k][s.ClientFamily] = w
	}
	return g
}

// scopesOf are the candidates an episode can join: a service only when the provider has
// to fix a failure that involved it.
func scopesOf(e mcpconnectionattempts.FailingPerson, w way, server string, upstreams map[string]Upstream) []Scope {
	var out []Scope
	if e.ReachedServer() {
		out = append(out, Scope{Kind: ScopeServer, Key: server})
		if w.part != mcpconnectionattempts.PartAppSignIn && w.who == mcpconnectionattempts.WhoProvider {
			if key := serviceOf(e, upstreams); key != "" {
				out = append(out, Scope{Kind: ScopeService, Key: key})
			}
		}
	}
	if mcpconnectionattempts.NamedClient(e.ClientFamily) && e.ClientVersion != "" {
		out = append(out, Scope{Kind: ScopeAppVersion, Key: e.ClientFamily + "@" + e.ClientVersion})
	}
	return out
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
}

// Group applies the package rule; upstreams is keyed by mcp_servers id.
func Group(organizationID string, episodes []mcpconnectionattempts.FailingPerson, upstreams map[string]Upstream, served []mcpconnectionattempts.ServedByFamily, now time.Time) []Problem {
	g := newGrouper(organizationID, episodes, upstreams, served)
	taken := make([]bool, len(episodes))

	byWay := map[way][]int{}
	for i := range episodes {
		if len(g.scopes[i]) > 0 {
			byWay[g.ways[i]] = append(byWay[g.ways[i]], i)
		}
	}

	var out []Problem
	for w, idx := range byWay {
		out = append(out, g.groupWay(w, idx, taken)...)
	}
	out = append(out, g.longFailures(taken, now)...)

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
				c = &candidate{scope: s, members: nil, alive: 0, subjects: map[string]int{}, servers: map[string]int{}, dirty: true, qualifies: false}
				cands[s] = c
			}
			c.members = append(c.members, i)
			c.alive++
			c.subjects[g.episodes[i].SubjectURN]++
			if g.episodes[i].ReachedServer() {
				c.servers[g.servers[i]]++
			}
		}
	}

	var out []Problem
	for {
		// A candidate that fails can never qualify again: people and servers only leave,
		// and each server's first failure only moves later.
		var best *candidate
		for s, c := range cands {
			if c.dirty {
				g.evaluate(c, w, taken)
			}
			if !c.qualifies {
				delete(cands, s)
				continue
			}
			if best == nil || rank(c.scope, len(c.subjects), best.scope, len(best.subjects)) < 0 {
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
				if g.episodes[i].ReachedServer() {
					if c.servers[g.servers[i]]--; c.servers[g.servers[i]] == 0 {
						delete(c.servers, g.servers[i])
					}
				}
			}
		}
		otherApps := best.scope.Kind == ScopeAppVersion || g.otherAppsWorked(w.part, picked)
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
func (g grouper) evaluate(c *candidate, w way, taken []bool) {
	c.dirty = false
	c.qualifies = len(c.subjects) >= MinPeople
	switch c.scope.Kind {
	case ScopeService:
		c.qualifies = c.qualifies && len(c.servers) >= MinServersPerService
	case ScopeAppVersion:
		c.qualifies = c.qualifies && g.otherAppsWorked(w.part, g.aliveMembers(c, taken))
	case ScopeServer, ScopePerson:
	}
}

// rank orders candidates: most people, then narrowest kind, then key.
func rank(a Scope, aPeople int, b Scope, bPeople int) int {
	return cmp.Or(
		cmp.Compare(bPeople, aPeople),
		cmp.Compare(kindRank[a.Kind], kindRank[b.Kind]),
		cmp.Compare(a.Key, b.Key),
	)
}

type personWay struct {
	subject string
	way     way
}

// longFailures makes the one-person problems from the people's episodes left.
func (g grouper) longFailures(taken []bool, now time.Time) []Problem {
	groups := map[personWay][]int{}
	for i, e := range g.episodes {
		if !g.people[i] || taken[i] || now.Sub(e.OpenedAt) < OnePersonFor {
			continue
		}
		k := personWay{subject: e.SubjectURN, way: g.ways[i]}
		groups[k] = append(groups[k], i)
	}
	out := make([]Problem, 0, len(groups))
	for k, idx := range groups {
		out = append(out, g.problem(Scope{Kind: ScopePerson, Key: k.subject}, k.way, idx, 1, g.otherAppsWorked(k.way.part, idx)))
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
		OthersOnScope:   g.othersOn(scope, eps),
	}
	for _, e := range eps[1:] {
		if e.LastFailureAt.After(p.LastFailedAt) {
			p.LastFailedAt = e.LastFailureAt
		}
	}
	return p
}

// othersOn counts the people who could join scope in any way and are not among eps.
func (g grouper) othersOn(scope Scope, eps []mcpconnectionattempts.FailingPerson) int {
	if scope.Kind == ScopePerson {
		return 0
	}
	in := map[string]bool{}
	for _, e := range eps {
		in[e.SubjectURN] = true
	}
	others := 0
	for subject := range g.onScope[scope] {
		if !in[subject] {
			others++
		}
	}
	return others
}

// otherAppsWorked is whether a named family none of the episodes failed with worked on one
// of their servers (exact kind, id and member) after their first failure there. An app
// sign-in works when served; anything else only when the server answered.
func (g grouper) otherAppsWorked(part mcpconnectionattempts.Part, idx []int) bool {
	families := map[string]bool{}
	first := map[servedKey]time.Time{}
	for _, i := range idx {
		e := g.episodes[i]
		families[e.ClientFamily] = true
		if !e.ReachedServer() {
			continue
		}
		k := servedKeyOf(e)
		if at, ok := first[k]; !ok || e.OpenedAt.Before(at) {
			first[k] = e.OpenedAt
		}
	}
	for k, since := range first {
		for family, w := range g.worked[k] {
			at := w.answered
			if part == mcpconnectionattempts.PartAppSignIn {
				at = w.served
			}
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
	if e.Part == mcpconnectionattempts.PartServiceSignIn && u.Issuer != "" {
		if issuer, err := issuerurl.Parse(u.Issuer); err == nil {
			return issuer.String()
		}
	}
	return urlHost(u.URL)
}

// urlHost is the lowercase host of a URL, without port, empty when it has none.
func urlHost(raw string) string {
	parsed, err := url.Parse(raw)
	if raw == "" || err != nil {
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

// compareEpisodes orders by every field, so episodes that differ have a fixed order.
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
		cmp.Compare(a.ClientVersion, b.ClientVersion),
		cmp.Compare(a.Reason, b.Reason),
		cmp.Compare(a.OpeningReason, b.OpeningReason),
		cmp.Compare(a.Step, b.Step),
		cmp.Compare(a.ProviderHost, b.ProviderHost),
		a.LastFailureAt.Compare(b.LastFailureAt),
		a.LastWorkedAt.Compare(b.LastWorkedAt),
		cmp.Compare(a.Failures, b.Failures),
		cmp.Compare(a.ProjectID.String(), b.ProjectID.String()),
		a.ServerLastAnsweredAt.Compare(b.ServerLastAnsweredAt),
	)
}
