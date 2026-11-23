package mcpconnectionhealth

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

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

	// maxProblems bounds the problems one list returns.
	maxProblems = 100

	// maxProblemPeople bounds the people listed on one problem.
	maxProblemPeople = 50

	// maxFilterValues is the most values one Connection Logs filter takes.
	maxFilterValues = 50

	// problemBucket and problemChartSpan shape the failed attempts chart.
	problemBucket    = 10 * time.Minute
	problemChartSpan = 24 * time.Hour

	// problemFromSlack reaches attempts that started before the failure that opened the problem ended.
	problemFromSlack = attemptlog.FoldGap

	// kindAuthorizationServer is an attempt on a provider's shared authorization server; its server id is the provider.
	kindAuthorizationServer = "authorization_server"

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

// readProblems groups the failures on the provider's servers, gateways and authorization
// server in the project, then keeps those on serverID when set. Callers without log:read get none.
func (s *Service) readProblems(ctx context.Context, c caller, issuerID string, serverID *string, now time.Time) (listed, error) {
	var out listed
	if serverID != nil {
		parsed, err := uuid.Parse(*serverID)
		if err != nil {
			return out, oops.E(oops.CodeBadRequest, err, "invalid server id").LogWarn(ctx, s.logger)
		}
		out.serverID = parsed.String()
	}
	var err error
	if out.loggingEnabled, err = s.loggingOn(ctx, c); err != nil || !out.loggingEnabled {
		return out, err
	}
	// A problem names other people.
	if c.ownSubjects != nil {
		return out, nil
	}
	issuer, err := s.provider(ctx, c, issuerID)
	if err != nil {
		return out, err
	}
	out.issuerID = issuer.ID
	if out.issuerServers, err = s.issuerServerIDs(ctx, c, issuer.ID); err != nil {
		return out, err
	}

	ofProvider := func(e mcpconnectionattempts.FailingPerson) bool {
		return slices.Contains(out.issuerServers, e.ServerID) || (e.ServerKind == kindAuthorizationServer && e.ServerID == issuer.ID.String())
	}
	// Zero: a person who has not tried for a day no longer counts, so failures from before
	// served requests were recorded age out a day after that writer shipped.
	all, err := problems.Read(ctx, s.ch, s.db, s.logsEnabled, problems.ReadScope{
		OrganizationID: c.organizationID, ProjectID: c.projectID, WholeOrganization: false, Keep: ofProvider,
	}, time.Time{}, now)
	switch {
	case errors.Is(err, mcpconnectionattempts.ErrLogsOff):
		out.loggingEnabled = false
		return out, nil
	case err != nil:
		return out, s.problemsError(ctx, err, "read suspected problems")
	}
	for _, p := range all {
		if out.serverID == "" || peopleOn(p, out.serverID) > 0 {
			out.problems = append(out.problems, p)
		}
	}
	return out, nil
}

// problemsError reports a read past the deadline as temporarily unavailable, anything else as unexpected.
func (s *Service) problemsError(ctx context.Context, err error, what string) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return oops.E(oops.CodeUnavailable, err, "suspected problems took too long to read, try again in a minute").LogWarn(ctx, s.logger)
	}
	return oops.E(oops.CodeUnexpected, err, "%s", what).LogError(ctx, s.logger)
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
	result := &gen.ListSuspectedProblemsResult{LoggingEnabled: l.loggingEnabled, ViewerScope: c.viewerScope(), Problems: []*gen.SuspectedProblem{}, ProblemsTotal: 0}
	if payload.Part != nil {
		l.problems = slices.DeleteFunc(l.problems, func(p problems.Problem) bool { return partOf(p.Part) != *payload.Part })
	}
	l.problems, result.ProblemsTotal = capProblems(l.problems)
	if len(l.problems) == 0 {
		return result, nil
	}
	n, err := s.resolve(ctx, c, problemIDs(l.problems))
	if err != nil {
		return nil, err
	}
	for _, p := range l.problems {
		view, _ := problemView(n, p, l.serverID)
		result.Problems = append(result.Problems, view)
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
	result := &gen.GetSuspectedProblemResult{LoggingEnabled: l.loggingEnabled, ViewerScope: c.viewerScope(), Found: false, Problem: nil}
	i := slices.IndexFunc(l.problems, func(p problems.Problem) bool { return p.ID == payload.ID })
	if i < 0 {
		return result, nil
	}
	p := l.problems[i]
	n, err := s.resolve(ctx, c, problemIDs([]problems.Problem{p}))
	if err != nil {
		return nil, err
	}
	view, widened := problemView(n, p, l.serverID)
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
	chart := scope
	chart.From, chart.To = later(from, now.Add(-problemChartSpan)), now
	var reasons []attemptlog.FacetValue
	var buckets []attemptlog.TimeCount
	eg, egctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		var err error
		if reasons, err = attemptlog.CountByReason(egctx, s.ch, scope, filters, now); err != nil {
			return fmt.Errorf("count by reason: %w", err)
		}
		return nil
	})
	eg.Go(func() error {
		var err error
		if buckets, err = attemptlog.CountOver(egctx, s.ch, chart, filters, problemBucket, now); err != nil {
			return fmt.Errorf("count over time: %w", err)
		}
		return nil
	})
	if err := eg.Wait(); err != nil {
		return nil, s.problemsError(ctx, err, "count suspected problem attempts")
	}

	result.Found = true
	result.Problem = detailView(n, p, view, widened, reasons, buckets)
	return result, nil
}

// capProblems keeps the first maxProblems, already sorted most people first, and counts them all.
func capProblems(ps []problems.Problem) ([]problems.Problem, int64) {
	return ps[:min(len(ps), maxProblems)], int64(len(ps))
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

// problemView is the listed problem, and whether its filters had to leave a list out.
func problemView(n names, p problems.Problem, serverID string) (*gen.SuspectedProblem, bool) {
	scope := &gen.SuspectedProblemScope{Kind: string(p.Scope.Kind), ID: p.Scope.Key, Name: scopeName(n, p)}
	filters, widened := problemFilters(p)
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
		Filters:             filters,
	}
	if serverID != "" {
		view.PeopleOnServerCount = new(int64(peopleOn(p, serverID)))
	}
	return view, widened
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
		return hostOf(key)
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

// problemFilters are the Connection Logs filters for the problem's failed attempts, and
// whether a list over the filter's cap was left out, which widens the match.
func problemFilters(p problems.Problem) (*gen.SuspectedProblemFilters, bool) {
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
	widened := !allOnServer
	capped := func(values []string) []string {
		values = slices.DeleteFunc(values, func(v string) bool { return v == "" })
		if len(values) > maxFilterValues {
			widened = true
		}
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
	return out, widened
}

func appendNew(values []string, v string) []string {
	if slices.Contains(values, v) {
		return values
	}
	return append(values, v)
}

func detailView(n names, p problems.Problem, view *gen.SuspectedProblem, widened bool, reasons []attemptlog.FacetValue, buckets []attemptlog.TimeCount) *gen.SuspectedProblemDetail {
	d := &gen.SuspectedProblemDetail{
		Source:                 nil,
		FailedPer10Minutes:     make([]*gen.SuspectedProblemBucket, len(buckets)),
		LastConnectedAt:        nil,
		OtherFailuresOnService: int64(p.OthersOnScope),
		Messages:               make([]*gen.SuspectedProblemMessage, len(reasons)),
		People:                 []*gen.SuspectedProblemPerson{},
		PeopleTotal:            int64(p.People),
		Servers:                []*gen.SuspectedProblemServer{},
		CountsWidened:          widened,
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
	for i, v := range reasons {
		meaning, _ := mcpconnectionattempts.MeaningOf(v.Value)
		d.Messages[i] = &gen.SuspectedProblemMessage{
			Reason:       v.Value,
			Message:      meaning.WhatHappened,
			AdminMessage: conv.PtrEmpty(meaning.WhatHappenedForAdmin),
			Attempts:     count(v.Attempts),
		}
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

	d.People = peopleRows(n, p)
	type serverKey struct{ kind, id, member string }
	perServer := map[serverKey]uint64{}
	for _, e := range p.Episodes {
		if e.ReachedServer() {
			perServer[serverKey{e.ServerKind, e.ServerID, e.MemberID}] += e.Failures
		}
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

// peopleRows is one row per person, earliest failure first, with their apps and servers folded in.
func peopleRows(n names, p problems.Problem) []*gen.SuspectedProblemPerson {
	var out []*gen.SuspectedProblemPerson
	bySubject := map[string]*gen.SuspectedProblemPerson{}
	seen := map[string]bool{}
	// Episodes are sorted by when they opened, so a person's first row is their earliest failure.
	for _, e := range p.Episodes {
		row := bySubject[e.SubjectURN]
		if row == nil {
			if len(out) == maxProblemPeople {
				continue
			}
			row = &gen.SuspectedProblemPerson{
				Person: n.person(e.SubjectURN, false), Apps: []*gen.ConnectionApp{}, Servers: []*gen.SuspectedProblemPlace{},
				Attempts: 0, FirstFailedAt: formatTime(e.OpenedAt),
			}
			bySubject[e.SubjectURN] = row
			out = append(out, row)
		}
		row.Attempts += count(e.Failures)
		if app := e.SubjectURN + "|" + e.ClientFamily + "|" + e.ClientVersion + "|" + e.AppKey; !seen[app] {
			seen[app] = true
			row.Apps = append(row.Apps, appView(e.ClientFamily, e.ClientVersion, e.AppKey))
		}
		if place := e.SubjectURN + "|" + e.ServerKind + "|" + e.ServerID + "|" + e.MemberID; e.ReachedServer() && !seen[place] {
			if server := n.server(e.ServerID, e.ServerKind); server != nil {
				seen[place] = true
				row.Servers = append(row.Servers, &gen.SuspectedProblemPlace{Server: server, Member: n.member(e.MemberID)})
			}
		}
	}
	return out
}
