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
	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/errgroup"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_connection_health"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/attemptlog"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/problems"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionhealth/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

const (
	// defaultProblemsDeadline bounds one problems request, well under its reads' own caps.
	defaultProblemsDeadline = 30 * time.Second

	// maxProblems bounds the problems one list returns.
	maxProblems = 100

	// maxProblemPeople bounds the people listed on one problem.
	maxProblemPeople = 50

	// maxFilterValues is the most values one Connection Logs filter takes.
	maxFilterValues = 50

	// problemBucket and problemChartSpan shape the failed attempts chart.
	problemBucket    = 10 * time.Minute
	problemChartSpan = 24 * time.Hour

	// problemFromSlack reaches the start of the attempt that opened the problem: an attempt runs at most this long before it counts as unfinished.
	problemFromSlack = mcpconnectionattempts.AttemptLiveWindow

	// maxFolded bounds the apps and servers folded into one person's row.
	maxFolded = 5

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

// readProblems groups the project's failures, or with issuerID the provider's attributed as Connection
// Logs attributes them, then keeps those on serverID when set. Callers without log:read get none.
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
	if issuerID != "" {
		var issuer repo.GetProviderIssuerRow
		if issuer, err = s.provider(ctx, c, issuerID); err != nil {
			return out, err
		}
		out.issuerID = issuer.ID
	}
	if out.loggingEnabled, err = s.loggingOn(ctx, c); err != nil || !out.loggingEnabled {
		return out, err
	}
	// A problem names other people.
	if c.ownSubjects != nil {
		return out, nil
	}
	if out.issuerID != uuid.Nil {
		if out.issuerServers, err = s.issuerServerIDs(ctx, c, out.issuerID); err != nil {
			return out, err
		}
	}

	// The start is when the organization's logs were last turned on, since nothing is recorded while they are off.
	recordedSince, err := s.logsOnSince(ctx, c)
	if err != nil {
		return out, err
	}
	all, err := problems.Read(ctx, s.ch, s.db, s.logsEnabled, problems.ReadScope{
		OrganizationID: c.organizationID, ProjectID: c.projectID, WholeOrganization: false,
		IssuerID: out.issuerID, IssuerServerIDs: out.issuerServers,
	}, recordedSince, now)
	switch {
	case errors.Is(err, mcpconnectionattempts.ErrLogsOff):
		out.loggingEnabled = false
		return out, nil
	case err != nil:
		return out, oops.E(oops.CodeUnexpected, err, "read suspected problems").LogError(ctx, s.logger)
	}
	for _, p := range all {
		if out.serverID == "" || peopleOn(p, out.serverID) > 0 {
			out.problems = append(out.problems, p)
		}
	}
	return out, nil
}

// logsOnSince is when the organization's logs were last turned on, zero when no row says so; nothing is recorded while they are off.
func (s *Service) logsOnSince(ctx context.Context, c caller) (time.Time, error) {
	at, err := repo.New(s.db).GetLogsOnSince(ctx, repo.GetLogsOnSinceParams{OrganizationID: c.organizationID, FeatureName: string(productfeatures.FeatureLogs)})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return time.Time{}, nil
	case err != nil:
		return time.Time{}, oops.E(oops.CodeUnexpected, err, "read when logs were turned on").LogError(ctx, s.logger)
	}
	return at.Time, nil
}

// pastDeadline turns any failure after the request's budget ran out into temporarily
// unavailable. Refusals keep their own code.
func (s *Service) pastDeadline(ctx context.Context, err error) error {
	if err == nil || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return err
	}
	var shareable *oops.ShareableError
	if errors.As(err, &shareable) && slices.Contains([]oops.Code{oops.CodeUnauthorized, oops.CodeForbidden, oops.CodeNotFound, oops.CodeBadRequest}, shareable.Code) {
		return err
	}
	return oops.E(oops.CodeUnavailable, err, "suspected problems took too long to read, try again in a minute").LogWarn(ctx, s.logger)
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

func (s *Service) ListSuspectedProblems(ctx context.Context, payload *gen.ListSuspectedProblemsPayload) (res *gen.ListSuspectedProblemsResult, err error) {
	ctx, cancel := context.WithTimeout(ctx, s.problemsDeadline)
	defer cancel()
	defer func() { err = s.pastDeadline(ctx, err) }()
	c, err := s.beginLogs(ctx)
	if err != nil {
		return nil, err
	}
	l, err := s.readProblems(ctx, c, payload.UserSessionIssuerID, payload.ServerID, s.now())
	if err != nil {
		return nil, err
	}
	result := &gen.ListSuspectedProblemsResult{LoggingEnabled: l.loggingEnabled, ViewerScope: c.viewerScope(), Problems: []*gen.SuspectedProblem{}, ProblemsTotal: 0}
	if payload.Part != nil {
		l.problems = slices.DeleteFunc(l.problems, func(p problems.Problem) bool { return partOf(p.Part) != *payload.Part })
	}
	l.problems, result.ProblemsTotal = capProblems(l.problems, maxProblems)
	if len(l.problems) == 0 {
		return result, nil
	}
	ids := newCollector()
	for _, p := range l.problems {
		listedIDs(ids, p)
	}
	n, err := s.resolve(ctx, c, ids)
	if err != nil {
		return nil, err
	}
	for _, p := range l.problems {
		view, _ := problemView(n, p, l.serverID)
		result.Problems = append(result.Problems, view)
	}
	return result, nil
}

func (s *Service) GetSuspectedProblem(ctx context.Context, payload *gen.GetSuspectedProblemPayload) (res *gen.GetSuspectedProblemResult, err error) {
	ctx, cancel := context.WithTimeout(ctx, s.problemsDeadline)
	defer cancel()
	defer func() { err = s.pastDeadline(ctx, err) }()
	c, err := s.beginLogs(ctx)
	if err != nil {
		return nil, err
	}
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
	rows := detailRowsOf(p)
	ids := newCollector()
	listedIDs(ids, p)
	rows.collect(ids)
	n, err := s.resolve(ctx, c, ids)
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
		To:              now,
		OwnSubjects:     nil,
	}
	chart := scope
	chart.From = later(from, now.Add(-problemChartSpan))
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
		return nil, oops.E(oops.CodeUnexpected, err, "count suspected problem attempts").LogError(ctx, s.logger)
	}

	result.Found = true
	result.Problem = detailView(n, p, view, widened, rows, reasons, buckets, now)
	return result, nil
}

// capProblems lists what each person fixes last, keeps the first limit and counts them all.
func capProblems(ps []problems.Problem, limit int) ([]problems.Problem, int64) {
	slices.SortStableFunc(ps, func(a, b problems.Problem) int {
		return cmp.Compare(personFixes(a), personFixes(b))
	})
	return ps[:min(len(ps), limit)], int64(len(ps))
}

func personFixes(p problems.Problem) int {
	if p.WhoFixes == mcpconnectionattempts.WhoPerson {
		return 1
	}
	return 0
}

// whoFixesOf is the API's name for who fixes a problem.
func whoFixesOf(w mcpconnectionattempts.WhoFixes) gen.SuspectedProblemWhoFixes {
	if w == mcpconnectionattempts.WhoNobody {
		return "unknown"
	}
	return gen.SuspectedProblemWhoFixes(w)
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// listedIDs collects what a listed problem names: its scope, and the first servers its fact names.
func listedIDs(ids *collector, p problems.Problem) {
	switch p.Scope.Kind {
	case problems.ScopePerson:
		ids.subject(p.Scope.Key)
	case problems.ScopeServer:
		_, rest, found := strings.Cut(p.Scope.Key, ":")
		if !found {
			rest = p.Scope.Key
		}
		id, _, _ := strings.Cut(rest, ":")
		ids.server(id)
	case problems.ScopeService, problems.ScopeAppVersion:
	}
	for _, e := range namedPlaces(p) {
		ids.server(e.ServerID)
		ids.server(e.MemberID)
	}
}

// problemView is the listed problem, and whether its filters had to leave a list out.
func problemView(n names, p problems.Problem, serverID string) (*gen.SuspectedProblem, bool) {
	scope := &gen.SuspectedProblemScope{Kind: string(p.Scope.Kind), ID: p.Scope.Key, Name: scopeName(n, p)}
	filters, widened := problemFilters(p)
	view := &gen.SuspectedProblem{
		ID:                  p.ID,
		Rule:                gen.SuspectedProblemRule(p.Rule),
		Part:                partOf(p.Part),
		WhoFixes:            new(whoFixesOf(p.WhoFixes)),
		Scope:               scope,
		Since:               formatTime(p.Since),
		LastFailedAt:        formatTime(p.LastFailedAt),
		PeopleCount:         int64(p.People),
		PeopleOnServerCount: nil,
		Headline:            headline(n, p, scope.Name),
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

// capSet keeps up to maxFilterValues distinct values and notes when there were more.
type capSet struct {
	values map[string]struct{}
	over   bool
}

func newCapSet() capSet { return capSet{values: map[string]struct{}{}, over: false} }

func (c *capSet) add(v string) {
	if v == "" || c.over {
		return
	}
	if _, ok := c.values[v]; ok {
		return
	}
	if len(c.values) == maxFilterValues {
		c.over, c.values = true, nil
		return
	}
	c.values[v] = struct{}{}
}

// list is the sorted values, nil when there were none or too many.
func (c *capSet) list() []string {
	if c.over || len(c.values) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(c.values))
}

// problemFilters are the Connection Logs filters for the problem's failed attempts, and
// whether a list over the filter's cap was left out, which widens the match.
func problemFilters(p problems.Problem) (*gen.SuspectedProblemFilters, bool) {
	servers, people, reasons := newCapSet(), newCapSet(), newCapSet()
	allOnServer := true
	for _, e := range p.Episodes {
		if e.ServerID == "" {
			allOnServer = false
		}
		servers.add(e.ServerID)
		people.add(e.SubjectURN)
		reasons.add(e.OpeningReason)
		reasons.add(e.Reason)
	}
	out := &gen.SuspectedProblemFilters{
		From:        formatTime(p.Since.Add(-problemFromSlack)),
		Outcomes:    []gen.ConnectionOutcome{gen.ConnectionOutcome(attemptlog.OutcomeFailed)},
		Steps:       nil,
		ServerIds:   nil,
		MemberIds:   nil,
		People:      people.list(),
		Networks:    nil,
		Apps:        nil,
		AppVersions: nil,
		Reasons:     reasons.list(),
		Services:    nil,
	}
	if allOnServer {
		out.ServerIds = servers.list()
	}
	if p.Rule == problems.RuleSameAppVersion {
		out.AppVersions = []string{p.Scope.Key}
	}
	return out, !allOnServer || servers.over || people.over || reasons.over
}

type serverKey struct{ kind, id, member string }

type appKey struct{ family, version, key string }

// personRow is one person's episodes folded together, before names.
type personRow struct {
	subject      string
	first        time.Time
	attempts     uint64
	apps         []appKey
	appsTotal    int
	servers      []serverKey
	serversTotal int
}

// detailRows are the detail's people and servers, capped, before names.
type detailRows struct {
	people       []*personRow
	servers      []serverKey
	attempts     map[serverKey]uint64
	serversTotal int
}

func detailRowsOf(p problems.Problem) detailRows {
	out := detailRows{people: nil, servers: nil, attempts: map[serverKey]uint64{}, serversTotal: 0}
	bySubject := map[string]*personRow{}
	seenApp, seenPlace := map[string]bool{}, map[string]bool{}
	// Episodes are sorted by when they opened, so a person's first episode is their earliest failure.
	for _, e := range p.Episodes {
		k := serverKey{e.ServerKind, e.ServerID, e.MemberID}
		if e.OnMCPServer() {
			out.attempts[k] += e.Failures
		}
		row := bySubject[e.SubjectURN]
		if row == nil {
			if len(out.people) == maxProblemPeople {
				continue
			}
			row = &personRow{subject: e.SubjectURN, first: e.OpenedAt, attempts: 0, apps: nil, appsTotal: 0, servers: nil, serversTotal: 0}
			bySubject[e.SubjectURN] = row
			out.people = append(out.people, row)
		}
		row.attempts += e.Failures
		// A server or service run keeps one key for every app, so fold each app it saw.
		for _, a := range e.Apps {
			if app := e.SubjectURN + "|" + a.Family + "|" + a.Version + "|" + a.Key; !seenApp[app] {
				seenApp[app] = true
				row.appsTotal++
				if len(row.apps) < maxFolded {
					row.apps = append(row.apps, appKey{a.Family, a.Version, a.Key})
				}
			}
		}
		// A run keeps at most MaxEpisodeApps apps, so its own total is a floor.
		row.appsTotal = max(row.appsTotal, e.AppsTotal)
		if place := e.SubjectURN + "|" + k.kind + "|" + k.id + "|" + k.member; e.OnMCPServer() && !seenPlace[place] {
			seenPlace[place] = true
			row.serversTotal++
			if len(row.servers) < maxFolded {
				row.servers = append(row.servers, k)
			}
		}
	}
	all := slices.SortedFunc(maps.Keys(out.attempts), func(a, b serverKey) int {
		return cmp.Or(cmp.Compare(out.attempts[b], out.attempts[a]), cmp.Compare(a.id, b.id), cmp.Compare(a.member, b.member))
	})
	out.serversTotal = len(all)
	out.servers = all[:min(len(all), maxProblemPeople)]
	return out
}

// collect adds the ids the rows name.
func (r detailRows) collect(ids *collector) {
	for _, row := range r.people {
		ids.subject(row.subject)
		for _, k := range row.servers {
			ids.server(k.id)
			ids.server(k.member)
		}
	}
	for _, k := range r.servers {
		ids.server(k.id)
		ids.server(k.member)
	}
}

func detailView(n names, p problems.Problem, view *gen.SuspectedProblem, widened bool, rows detailRows, reasons []attemptlog.FacetValue, buckets []attemptlog.TimeCount, asOf time.Time) *gen.SuspectedProblemDetail {
	d := &gen.SuspectedProblemDetail{
		Source:                 nil,
		FailedPer10Minutes:     make([]*gen.SuspectedProblemBucket, len(buckets)),
		LastConnectedAt:        nil,
		OtherFailuresOnService: int64(p.OthersOnScope),
		Messages:               make([]*gen.SuspectedProblemMessage, len(reasons)),
		People:                 make([]*gen.SuspectedProblemPerson, 0, len(rows.people)),
		PeopleTotal:            int64(p.People),
		Servers:                []*gen.SuspectedProblemServer{},
		ServersTotal:           int64(rows.serversTotal),
		AsOf:                   formatTime(asOf),
		CountsWidened:          widened,
		ID:                     view.ID,
		Rule:                   view.Rule,
		Part:                   view.Part,
		WhoFixes:               view.WhoFixes,
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

	for _, row := range rows.people {
		person := &gen.SuspectedProblemPerson{
			Person: n.person(row.subject, false), Apps: make([]*gen.ConnectionApp, len(row.apps)), AppsTotal: int64(row.appsTotal),
			Servers: []*gen.SuspectedProblemPlace{}, ServersTotal: int64(row.serversTotal),
			Attempts: count(row.attempts), FirstFailedAt: formatTime(row.first),
		}
		for i, a := range row.apps {
			person.Apps[i] = appView(a.family, a.version, a.key)
		}
		for _, k := range row.servers {
			if server := n.server(k.id, k.kind); server != nil {
				person.Servers = append(person.Servers, &gen.SuspectedProblemPlace{Server: server, Member: n.member(k.member)})
			}
		}
		d.People = append(d.People, person)
	}
	for _, k := range rows.servers {
		if server := n.server(k.id, k.kind); server != nil {
			d.Servers = append(d.Servers, &gen.SuspectedProblemServer{Server: server, Member: n.member(k.member), Attempts: count(rows.attempts[k])})
		}
	}
	return d
}
