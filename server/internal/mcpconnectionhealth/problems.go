package mcpconnectionhealth

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_connection_health"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/attemptlog"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/problems"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	// problemsDeadline bounds one problems request, well under its reads' own caps.
	problemsDeadline = 30 * time.Second

	// maxProblemPeople bounds the people listed on one problem.
	maxProblemPeople = 50

	// maxFilterValues is the most values one Connection Logs filter takes.
	maxFilterValues = 50

	// problemBucket and problemChartSpan shape the failed attempts chart.
	problemBucket    = 10 * time.Minute
	problemChartSpan = 24 * time.Hour

	// problemFromSlack reaches attempts that started before the failure that opened the problem ended.
	problemFromSlack = attemptlog.FoldGap

	partSpeakeasy gen.SuspectedProblemPart = "speakeasy"
	partService   gen.SuspectedProblemPart = "service"
)

func partOf(p mcpconnectionattempts.Part) gen.SuspectedProblemPart {
	if p == mcpconnectionattempts.PartAppSignIn {
		return partSpeakeasy
	}
	return partService
}

// listed is the provider's problems as one request sees them.
type listed struct {
	loggingEnabled bool
	issuerID       uuid.UUID
	issuerServers  []string
	serverID       string
	problems       []problems.Problem
}

// readProblems reads the project's problems with a person on one of the provider's
// servers, and on serverID when set. Callers without log:read get none.
func (s *Service) readProblems(ctx context.Context, c caller, issuerID string, serverID *string, now time.Time) (listed, error) {
	var out listed
	issuer, err := s.provider(ctx, c, issuerID)
	if err != nil {
		return out, err
	}
	out.issuerID = issuer.ID
	if serverID != nil {
		parsed, err := uuid.Parse(*serverID)
		if err != nil {
			return out, oops.E(oops.CodeBadRequest, err, "invalid server id").LogWarn(ctx, s.logger)
		}
		out.serverID = parsed.String()
	}
	if out.issuerServers, err = s.issuerServerIDs(ctx, c, issuer.ID); err != nil {
		return out, err
	}
	if out.loggingEnabled, err = s.loggingOn(ctx, c); err != nil || !out.loggingEnabled {
		return out, err
	}
	// A problem names other people.
	if c.ownSubjects != nil {
		return out, nil
	}

	// Zero: a person who has not tried for a day no longer counts, so failures from before
	// served requests were recorded age out a day after that writer shipped.
	all, err := problems.Read(ctx, s.ch, s.db, s.logsEnabled, c.organizationID, c.projectID, time.Time{}, now)
	switch {
	case errors.Is(err, mcpconnectionattempts.ErrLogsOff):
		out.loggingEnabled = false
		return out, nil
	case err != nil:
		return out, oops.E(oops.CodeUnexpected, err, "read suspected problems").LogError(ctx, s.logger)
	}
	for _, p := range all {
		if slices.ContainsFunc(p.Episodes, func(e mcpconnectionattempts.FailingPerson) bool {
			return slices.Contains(out.issuerServers, e.ServerID)
		}) &&
			(out.serverID == "" || peopleOn(p, out.serverID) > 0) {
			out.problems = append(out.problems, p)
		}
	}
	return out, nil
}

// peopleOn counts the problem's people failing on a server, gateway member or toolset.
func peopleOn(p problems.Problem, serverID string) int {
	people := map[string]bool{}
	for _, e := range p.Episodes {
		if e.ServerID == serverID || e.MemberID == serverID {
			people[e.SubjectURN] = true
		}
	}
	return len(people)
}

func (s *Service) ListSuspectedProblems(ctx context.Context, payload *gen.ListSuspectedProblemsPayload) (*gen.ListSuspectedProblemsResult, error) {
	c, err := s.beginLogs(ctx)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, problemsDeadline)
	defer cancel()
	l, err := s.readProblems(ctx, c, payload.UserSessionIssuerID, payload.ServerID, s.now())
	if err != nil {
		return nil, err
	}
	result := &gen.ListSuspectedProblemsResult{LoggingEnabled: l.loggingEnabled, Problems: []*gen.SuspectedProblem{}}
	if payload.Part != nil {
		l.problems = slices.DeleteFunc(l.problems, func(p problems.Problem) bool { return partOf(p.Part) != *payload.Part })
	}
	if len(l.problems) == 0 {
		return result, nil
	}
	n, err := s.resolve(ctx, c, problemIDs(l.problems))
	if err != nil {
		return nil, err
	}
	for _, p := range l.problems {
		result.Problems = append(result.Problems, problemView(n, p, l.serverID))
	}
	return result, nil
}

func (s *Service) GetSuspectedProblem(ctx context.Context, payload *gen.GetSuspectedProblemPayload) (*gen.GetSuspectedProblemResult, error) {
	c, err := s.beginLogs(ctx)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, problemsDeadline)
	defer cancel()
	now := s.now()
	l, err := s.readProblems(ctx, c, payload.UserSessionIssuerID, payload.ServerID, now)
	if err != nil {
		return nil, err
	}
	result := &gen.GetSuspectedProblemResult{LoggingEnabled: l.loggingEnabled, Found: false, Problem: nil}
	i := slices.IndexFunc(l.problems, func(p problems.Problem) bool { return p.ID == payload.ID })
	if i < 0 {
		return result, nil
	}
	p := l.problems[i]
	n, err := s.resolve(ctx, c, problemIDs([]problems.Problem{p}))
	if err != nil {
		return nil, err
	}
	view := problemView(n, p, l.serverID)
	filters := filtersOf(&gen.ConnectionAttemptFilters{
		Outcomes:    view.Filters.Outcomes,
		Steps:       nil,
		ServerIds:   view.Filters.ServerIds,
		MemberIds:   nil,
		People:      view.Filters.People,
		Networks:    nil,
		Apps:        nil,
		AppVersions: view.Filters.AppVersions,
		Reasons:     view.Filters.Reasons,
		Services:    nil,
	})
	from := p.Since.Add(-problemFromSlack)
	scope := attemptlog.Scope{
		OrganizationID:  c.organizationID,
		ProjectID:       c.projectID,
		IssuerID:        l.issuerID,
		IssuerServerIDs: l.issuerServers,
		ServerID:        "",
		AttemptID:       uuid.Nil,
		From:            from,
		To:              now.Add(detailLookahead),
		OwnSubjects:     nil,
	}
	counts, err := attemptlog.Count(ctx, s.ch, scope, filters, now)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count suspected problem attempts").LogError(ctx, s.logger)
	}
	chart := scope
	chart.From, chart.To = later(from, now.Add(-problemChartSpan)), now
	buckets, err := attemptlog.CountOver(ctx, s.ch, chart, filters, problemBucket, now)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count suspected problem attempts over time").LogError(ctx, s.logger)
	}

	result.Found = true
	result.Problem = detailView(n, p, view, counts, buckets)
	return result, nil
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// problemIDs collects the servers and people the problems name.
func problemIDs(ps []problems.Problem) *collector {
	ids := newCollector()
	for _, p := range ps {
		for _, e := range p.Episodes {
			ids.server(e.ServerID)
			ids.server(e.MemberID)
			ids.subject(e.SubjectURN)
		}
	}
	return ids
}

func problemView(n names, p problems.Problem, serverID string) *gen.SuspectedProblem {
	scope := &gen.SuspectedProblemScope{Kind: string(p.Scope.Kind), ID: p.Scope.Key, Name: scopeName(n, p)}
	view := &gen.SuspectedProblem{
		ID:                  p.ID,
		Rule:                gen.SuspectedProblemRule(p.Rule),
		Part:                partOf(p.Part),
		Scope:               scope,
		Since:               formatTime(p.Since),
		LastFailedAt:        formatTime(p.LastFailedAt),
		PeopleCount:         int64(p.People),
		PeopleOnServerCount: nil,
		Headline:            headline(p, scope.Name),
		Fact:                fact(n, p),
		Filters:             problemFilters(p),
	}
	if serverID != "" {
		view.PeopleOnServerCount = new(int64(peopleOn(p, serverID)))
	}
	return view
}

// scopeName is what the problem is about as the pages name it.
func scopeName(n names, p problems.Problem) string {
	key := p.Scope.Key
	switch p.Scope.Kind {
	case problems.ScopeServer:
		if name := serverKeyName(n, key); name != "" {
			return name
		}
		return "an MCP server"
	case problems.ScopeService:
		if u, err := url.Parse(key); err == nil && u.Hostname() != "" {
			return u.Hostname()
		}
		return key
	case problems.ScopeAppVersion:
		family, version, _ := strings.Cut(key, "@")
		return attemptlog.AppName(family) + " " + version
	case problems.ScopePerson:
		if person := n.person(key, false); person != nil && person.Name != nil {
			return *person.Name
		}
		return "Someone"
	}
	return key
}

// serverKeyName names a server scope key: an mcp_servers id, else kind:id[:member].
func serverKeyName(n names, key string) string {
	if _, err := uuid.Parse(key); err == nil {
		return conv.PtrValOr(n.serverName(key), "")
	}
	kind, rest, _ := strings.Cut(key, ":")
	id, _, _ := strings.Cut(rest, ":")
	if row, ok := n.lookup(id, serverKinds[kind]); ok {
		return row.Name
	}
	return ""
}

// episodeServerName names where an episode failed, empty when unknown.
func episodeServerName(n names, e mcpconnectionattempts.FailingPerson) string {
	if e.MemberID != "" {
		if name := n.serverName(e.MemberID); name != nil {
			return *name
		}
	}
	if row, ok := n.lookup(e.ServerID, serverKinds[e.ServerKind]); ok {
		return row.Name
	}
	return ""
}

func headline(p problems.Problem, scope string) string {
	verb := "can't connect"
	if p.Part == mcpconnectionattempts.PartAppSignIn {
		verb = "can't sign in"
	}
	people := fmt.Sprintf("%d people", p.People)
	switch p.Rule {
	case problems.RuleSameServer, problems.RuleSameService:
		target := " to " + scope
		return people + " " + verb + target
	case problems.RuleSameAppVersion:
		return people + " on " + scope + " " + verb
	case problems.RuleOnePerson:
		return scope + " " + verb
	}
	return people + " " + verb
}

// fact states only what the episodes establish, in the catalog's words where it has them.
func fact(n names, p problems.Problem) string {
	var out []string
	if p.Rule == problems.RuleOnePerson || p.Rule == problems.RuleSameAppVersion {
		if where := serversPhrase(n, p); where != "" {
			out = append(out, "On "+where+".")
		}
	}
	service := "the service"
	if p.Rule == problems.RuleSameService {
		service = p.Scope.Key
		if u, err := url.Parse(service); err == nil && u.Hostname() != "" {
			service = u.Hostname()
		}
	}
	switch p.Part {
	case mcpconnectionattempts.PartAppSignIn:
		out = append(out, "The app's sign-in to Speakeasy fails.")
	case mcpconnectionattempts.PartServiceSignIn:
		out = append(out, "Signing in to "+service+" fails.")
	case mcpconnectionattempts.PartServer:
		if p.Rule == problems.RuleSameService {
			out = append(out, "The service at "+service+" is not answering.")
		} else {
			out = append(out, "The server is not answering.")
		}
	}
	if reason := sharedReason(p); reason != "" {
		if meaning, ok := mcpconnectionattempts.MeaningOf(reason); ok && meaning.WhatHappenedForAdmin != "" {
			out = append(out, meaning.WhatHappenedForAdmin)
		}
	}
	if p.SharedApp.Family != "" && p.Rule != problems.RuleSameAppVersion {
		app := attemptlog.AppName(p.SharedApp.Family) + " " + p.SharedApp.Version
		if p.People == 1 {
			out = append(out, "Uses "+app+".")
		} else {
			out = append(out, "All use "+app+".")
		}
	}
	if p.OtherAppsServed {
		out = append(out, "Other apps worked there in this time.")
	}
	return strings.Join(out, " ")
}

func sharedReason(p problems.Problem) string {
	reason := p.Episodes[0].Reason
	for _, e := range p.Episodes[1:] {
		if e.Reason != reason {
			return ""
		}
	}
	return reason
}

// serversPhrase names the servers the problem's people fail on: "Linear", "Linear and GitHub", "Linear and 2 other servers".
func serversPhrase(n names, p problems.Problem) string {
	var named []string
	seen := map[string]bool{}
	for _, e := range p.Episodes {
		if !e.ReachedServer() {
			continue
		}
		key := e.ServerID + "/" + e.MemberID
		if seen[key] {
			continue
		}
		seen[key] = true
		name := episodeServerName(n, e)
		if name == "" {
			name = "an MCP server"
		}
		named = append(named, name)
	}
	switch len(named) {
	case 0:
		return ""
	case 1:
		return named[0]
	case 2:
		return named[0] + " and " + named[1]
	default:
		return fmt.Sprintf("%s and %d other servers", named[0], len(named)-1)
	}
}

// problemFilters are the Connection Logs filters for the problem's failed attempts. A
// list over the filter's cap is left out, which widens the match.
func problemFilters(p problems.Problem) *gen.SuspectedProblemFilters {
	var servers, people, reasons []string
	allOnServer := true
	for _, e := range p.Episodes {
		if e.ServerID == "" {
			allOnServer = false
		}
		servers = appendNew(servers, e.ServerID)
		people = appendNew(people, e.SubjectURN)
		reasons = appendNew(appendNew(reasons, e.OpeningReason), e.Reason)
	}
	capped := func(values []string) []string {
		values = slices.DeleteFunc(values, func(v string) bool { return v == "" })
		if len(values) == 0 || len(values) > maxFilterValues {
			return nil
		}
		slices.Sort(values)
		return values
	}
	out := &gen.SuspectedProblemFilters{
		From:        formatTime(p.Since.Add(-problemFromSlack)),
		Outcomes:    []gen.ConnectionOutcome{gen.ConnectionOutcome(attemptlog.OutcomeFailed)},
		Steps:       nil,
		ServerIds:   nil,
		MemberIds:   nil,
		People:      capped(people),
		Networks:    nil,
		Apps:        nil,
		AppVersions: nil,
		Reasons:     capped(reasons),
		Services:    nil,
	}
	if allOnServer {
		out.ServerIds = capped(servers)
	}
	if p.Rule == problems.RuleSameAppVersion {
		out.AppVersions = []string{p.Scope.Key}
	}
	return out
}

func appendNew(values []string, v string) []string {
	if slices.Contains(values, v) {
		return values
	}
	return append(values, v)
}

func detailView(n names, p problems.Problem, view *gen.SuspectedProblem, counts attemptlog.Counts, buckets []attemptlog.TimeCount) *gen.SuspectedProblemDetail {
	d := &gen.SuspectedProblemDetail{
		Source:                 nil,
		FailedPer10Minutes:     make([]*gen.SuspectedProblemBucket, len(buckets)),
		LastConnectedAt:        nil,
		OtherFailuresOnService: int64(p.OthersOnScope),
		Messages:               []*gen.SuspectedProblemMessage{},
		People:                 []*gen.SuspectedProblemPerson{},
		PeopleTotal:            int64(len(p.Episodes)),
		Servers:                []*gen.SuspectedProblemServer{},
		ID:                     view.ID,
		Rule:                   view.Rule,
		Part:                   view.Part,
		Scope:                  view.Scope,
		Since:                  view.Since,
		LastFailedAt:           view.LastFailedAt,
		PeopleCount:            view.PeopleCount,
		PeopleOnServerCount:    view.PeopleOnServerCount,
		Headline:               view.Headline,
		Fact:                   view.Fact,
		Filters:                view.Filters,
	}
	for i, b := range buckets {
		d.FailedPer10Minutes[i] = &gen.SuspectedProblemBucket{Start: formatTime(b.Start), Attempts: count(b.Attempts)}
	}

	var lastWorked time.Time
	var sources []attemptlog.Source
	for _, e := range p.Episodes {
		lastWorked = later(lastWorked, e.LastWorkedAt)
		if source := attemptlog.SourceOf(e.Reason, attemptlog.OutcomeFailed); !slices.Contains(sources, source) {
			sources = append(sources, source)
		}
	}
	if !lastWorked.IsZero() {
		d.LastConnectedAt = new(formatTime(lastWorked))
	}
	if len(sources) == 1 && sources[0] != "" {
		d.Source = new(gen.ConnectionFailureSource(sources[0]))
	}

	// The reasons facet ignores its own filter, so keep the problem's reasons only.
	for _, v := range counts.Facets[attemptlog.FacetReasons] {
		if view.Filters.Reasons != nil && !slices.Contains(view.Filters.Reasons, v.Value) {
			continue
		}
		meaning, _ := mcpconnectionattempts.MeaningOf(v.Value)
		d.Messages = append(d.Messages, &gen.SuspectedProblemMessage{
			Reason:       v.Value,
			Message:      meaning.WhatHappened,
			AdminMessage: conv.PtrEmpty(meaning.WhatHappenedForAdmin),
			Attempts:     count(v.Attempts),
		})
	}

	type serverKey struct{ kind, id, member string }
	perServer := map[serverKey]uint64{}
	for i, e := range p.Episodes {
		if e.ReachedServer() {
			perServer[serverKey{e.ServerKind, e.ServerID, e.MemberID}] += e.Failures
		}
		if i >= maxProblemPeople {
			continue
		}
		d.People = append(d.People, &gen.SuspectedProblemPerson{
			Person:        n.person(e.SubjectURN, false),
			App:           appView(e.ClientFamily, e.ClientVersion, e.AppKey),
			Server:        n.server(e.ServerID, e.ServerKind),
			Member:        n.member(e.MemberID),
			Attempts:      count(e.Failures),
			FirstFailedAt: formatTime(e.OpenedAt),
		})
	}
	for _, k := range slices.SortedFunc(maps.Keys(perServer), func(a, b serverKey) int {
		return cmp.Or(cmp.Compare(perServer[b], perServer[a]), cmp.Compare(a.id, b.id), cmp.Compare(a.member, b.member))
	}) {
		server := n.server(k.id, k.kind)
		if server == nil {
			continue
		}
		d.Servers = append(d.Servers, &gen.SuspectedProblemServer{Server: server, Member: n.member(k.member), Attempts: count(perServer[k])})
	}
	return d
}
