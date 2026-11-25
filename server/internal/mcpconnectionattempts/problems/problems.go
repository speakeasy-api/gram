// Package problems groups the people who cannot connect right now into suspected problems.
//
// An episode is one person's ongoing failure on one connection, from
// mcpconnectionattempts.ReadFailingNow. Only people (users) count. The rule:
//  1. Each failure has a way: the part that broke and who has to fix its latest error.
//     The parts are the app's sign-in to the Speakeasy MCP server (inbound), and the
//     server's sign-in to the outside service and its request to it (outbound). Only
//     failures of the same way are grouped.
//  2. Within each way, a group is minPeople or more people whose failures share one of:
//     - a server: the Speakeasy MCP server, gateway or toolset the app connects to. A
//     direct server or gateway member is its mcp_servers row, anything else its own kind
//     and id (and member, when it has one);
//     - a service, only for outbound failures the provider fixes: what the server connects
//     out to (the remote session issuer for a service sign-in when the server has one,
//     else the remote URL host), on minServersPerService or more servers;
//     - an app version: a named client family and version, with its failures where
//     another named family worked after the first failure there. For an app sign-in that
//     is the front the app signs in at (the direct server, or the gateway whichever its
//     member), and worked means served; otherwise it is the server behind every route,
//     and worked means the outside service answered, any member counting for a failure
//     at a gateway itself. When it has any such failure, it also takes its failures on no
//     MCP server; failures without that evidence stay out.
//  3. Every group becomes a problem. A failure that fits several is counted in one: the
//     group with the most people is taken first (ties: app version, server, service, then
//     key), its failures are set aside, and the rest are grouped again until none is left.
//     Once a server is taken, its service is not a group in that way.
//  4. A failure in no group that opened onePersonFor or more before now is a problem for
//     that person, one per person and way.
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
	// minPeople is how many distinct people make a server, service or app version a problem.
	minPeople = 2

	// minServersPerService is how many servers must share an upstream for it to be a service problem.
	minServersPerService = 2

	// onePersonFor is how long one person must fail the same way to be a problem alone.
	onePersonFor = 24 * time.Hour

	// triedWithin is how recently a person must have last failed.
	triedWithin = 24 * time.Hour
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

	// RuleOnePerson is one person failing the same way for onePersonFor.
	RuleOnePerson Rule = "one_person"
)

// ScopeKind is what a problem is about. The values are a contract with the dashboard.
type ScopeKind string

const (
	// ScopeServer is one Speakeasy MCP server, gateway or toolset. Its key is the mcp_servers id, else kind:id.
	ScopeServer ScopeKind = "server"

	// ScopeService is what servers connect out to. Its key is the remote session issuer or the remote URL host.
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

	// OtherAppsWorked is whether another named family worked on one of its servers after its first failure there.
	OtherAppsWorked bool

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

// servedKey is where evidence is looked up: the front an app signs in at, or with server set the merged server.
type servedKey struct{ kind, id, server string }

// evidenceKey is the front for an app sign-in, shared by a gateway's members as in the parent's appSignedIn, else the merged server behind every route.
func evidenceKey(e mcpconnectionattempts.FailingPerson, part mcpconnectionattempts.Part) servedKey {
	if part == mcpconnectionattempts.PartAppSignIn {
		return servedKey{kind: e.ServerKind, id: e.ServerID, server: ""}
	}
	return servedKey{kind: "", id: "", server: ServerOf(e)}
}

// workedAt is when a family was last served and last answered on one server.
type workedAt struct{ served, answered time.Time }

// ServerOf is the episode's server: its mcp_servers row, else its own kind, id and member.
func ServerOf(e mcpconnectionattempts.FailingPerson) string {
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
		g.servers[i] = ServerOf(e)
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
		var row mcpconnectionattempts.FailingPerson
		row.ServerKind, row.ServerID, row.MemberID = s.ServerKind, s.ServerID, s.MemberID
		g.addWorked(evidenceKey(row, mcpconnectionattempts.PartAppSignIn), s)
		g.addWorked(evidenceKey(row, mcpconnectionattempts.PartServer), s)
		if s.OnGatewayMember() {
			// Any member answering shows the gateway itself works.
			row.MemberID = ""
			g.addWorked(evidenceKey(row, mcpconnectionattempts.PartServer), s)
		}
	}
	return g
}

func (g grouper) addWorked(k servedKey, s mcpconnectionattempts.ServedByFamily) {
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

// scopesOf are the candidates an episode can join.
func scopesOf(e mcpconnectionattempts.FailingPerson, w way, server string, upstreams map[string]Upstream) []Scope {
	var out []Scope
	if e.OnMCPServer() {
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

	// people and picks are what it would take now; an app version takes only episodes with evidence.
	people int
	picks  []int
}

// Group applies the package rule, most people first, then earliest Since, then ID; upstreams is keyed by mcp_servers id.
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
				c = &candidate{scope: s, members: nil, alive: 0, subjects: map[string]int{}, servers: map[string]int{}, dirty: true, qualifies: false, people: 0, picks: nil}
				cands[s] = c
			}
			c.members = append(c.members, i)
			c.alive++
			c.subjects[g.episodes[i].SubjectURN]++
			if g.episodes[i].OnMCPServer() {
				c.servers[g.servers[i]]++
			}
		}
	}

	var out []Problem
	for {
		// People, servers and evidence only leave, so a candidate that fails never qualifies again.
		var best *candidate
		for s, c := range cands {
			if c.dirty {
				g.evaluate(c, w, taken)
			}
			if !c.qualifies {
				delete(cands, s)
				continue
			}
			if best == nil || rank(c.scope, c.people, best.scope, best.people) < 0 {
				best = c
			}
		}
		if best == nil {
			return out
		}

		delete(cands, best.scope)
		picked := best.picks
		for _, i := range picked {
			taken[i] = true
			for _, s := range g.scopes[i] {
				c := cands[s]
				if c == nil {
					continue
				}
				if best.scope.Kind == ScopeServer && s.Kind == ScopeService {
					// One outage is one problem: its service is not listed on the servers left.
					delete(cands, s)
					continue
				}
				c.alive--
				c.dirty = true
				if c.subjects[g.episodes[i].SubjectURN]--; c.subjects[g.episodes[i].SubjectURN] == 0 {
					delete(c.subjects, g.episodes[i].SubjectURN)
				}
				if g.episodes[i].OnMCPServer() {
					if c.servers[g.servers[i]]--; c.servers[g.servers[i]] == 0 {
						delete(c.servers, g.servers[i])
					}
				}
			}
		}
		otherApps := best.scope.Kind == ScopeAppVersion || g.otherAppsWorked(w.part, picked)
		out = append(out, g.problem(best.scope, w, picked, best.people, otherApps))
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
	c.picks, c.people = g.aliveMembers(c, taken), len(c.subjects)
	switch c.scope.Kind {
	case ScopeService:
		if len(c.servers) < minServersPerService {
			c.people = 0
		}
	case ScopeAppVersion:
		c.picks = g.withEvidence(w.part, c.picks)
		c.people = g.countPeople(c.picks)
	case ScopeServer, ScopePerson:
	}
	c.qualifies = c.people >= minPeople
}

func (g grouper) countPeople(idx []int) int {
	subjects := map[string]bool{}
	for _, i := range idx {
		subjects[g.episodes[i].SubjectURN] = true
	}
	return len(subjects)
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
		if !g.people[i] || taken[i] || now.Sub(e.OpenedAt) < onePersonFor {
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
		OtherAppsWorked: otherApps,
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

// otherAppsWorked is whether withEvidence keeps any of idx.
func (g grouper) otherAppsWorked(part mcpconnectionattempts.Part, idx []int) bool {
	return len(g.withEvidence(part, idx)) > 0
}

// withEvidence keeps the episodes on connections where a named family none of idx failed with worked after idx's first failure there, and, when any is kept, the episodes on no MCP server.
func (g grouper) withEvidence(part mcpconnectionattempts.Part, idx []int) []int {
	families := map[string]bool{}
	first := map[servedKey]time.Time{}
	for _, i := range idx {
		e := g.episodes[i]
		families[e.ClientFamily] = true
		if !e.OnMCPServer() {
			continue
		}
		k := evidenceKey(e, part)
		if at, ok := first[k]; !ok || e.OpenedAt.Before(at) {
			first[k] = e.OpenedAt
		}
	}
	worked := map[servedKey]bool{}
	for k, since := range first {
		for family, w := range g.worked[k] {
			at := w.answered
			if part == mcpconnectionattempts.PartAppSignIn {
				at = w.served
			}
			if mcpconnectionattempts.NamedClient(family) && !families[family] && at.After(since) {
				worked[k] = true
				break
			}
		}
	}
	if len(worked) == 0 {
		return nil
	}
	var out []int
	for _, i := range idx {
		if e := g.episodes[i]; !e.OnMCPServer() || worked[evidenceKey(e, part)] {
			out = append(out, i)
		}
	}
	return out
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
