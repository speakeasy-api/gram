package mcpconnectionhealth

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_connection_health"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/attemptlog"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// detailLookahead covers attempts whose start is slightly ahead of this server's clock.
const detailLookahead = time.Hour

func filtersOf(f *gen.ConnectionAttemptFilters) attemptlog.Filters {
	var out attemptlog.Filters
	if f != nil {
		out = attemptlog.Filters{
			Outcomes:    stringValues(f.Outcomes),
			Steps:       stringValues(f.Steps),
			ServerIDs:   f.ServerIds,
			MemberIDs:   f.MemberIds,
			People:      f.People,
			Networks:    f.Networks,
			Apps:        f.Apps,
			AppVersions: f.AppVersions,
			Reasons:     f.Reasons,
			Services:    f.Services,
		}
	}
	return out
}

func (s *Service) ListAttempts(ctx context.Context, payload *gen.ListAttemptsPayload) (*gen.ListConnectionAttemptsResult, error) {
	c, err := s.beginLogs(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now()
	scope, err := s.attemptScope(ctx, c, rangeQuery{issuerID: payload.UserSessionIssuerID, serverID: payload.ServerID, from: payload.From, to: payload.To}, now)
	if err != nil {
		return nil, err
	}
	var after *attemptlog.Cursor
	if payload.Cursor != nil && *payload.Cursor != "" {
		cursor, err := attemptlog.DecodeCursor(*payload.Cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").LogWarn(ctx, s.logger)
		}
		after = &cursor
	}
	result := &gen.ListConnectionAttemptsResult{LoggingEnabled: false, ViewerScope: c.viewerScope(), Attempts: nil, NextCursor: nil}
	on, err := s.loggingOn(ctx, c)
	if err != nil || !on {
		return result, err
	}

	limit := conv.PtrValOr(payload.Limit, attemptlog.DefaultPageSize)
	rows, next, err := attemptlog.List(ctx, s.ch, scope, filtersOf(payload.Filters), after, limit, now)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list connection attempts").LogError(ctx, s.logger)
	}
	ids := newCollector()
	for _, row := range rows {
		ids.row(row)
	}
	n, err := s.resolve(ctx, c, ids)
	if err != nil {
		return nil, err
	}
	result.LoggingEnabled = true
	result.Attempts = make([]*gen.ConnectionAttemptRow, len(rows))
	for i, row := range rows {
		result.Attempts[i] = n.row(row)
	}
	if next != nil {
		result.NextCursor = new(next.Encode())
	}
	return result, nil
}

func (s *Service) GetAttemptCounts(ctx context.Context, payload *gen.GetAttemptCountsPayload) (*gen.ConnectionAttemptCountsResult, error) {
	c, err := s.beginLogs(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now()
	scope, err := s.attemptScope(ctx, c, rangeQuery{issuerID: payload.UserSessionIssuerID, serverID: payload.ServerID, from: payload.From, to: payload.To}, now)
	if err != nil {
		return nil, err
	}
	result := &gen.ConnectionAttemptCountsResult{LoggingEnabled: false, ViewerScope: c.viewerScope(), TotalAttempts: nil, Facets: nil, Truncated: nil}
	on, err := s.loggingOn(ctx, c)
	if err != nil || !on {
		return result, err
	}

	counts, err := attemptlog.Count(ctx, s.ch, scope, filtersOf(payload.Filters), now)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count connection attempts").LogError(ctx, s.logger)
	}
	ids := newCollector()
	for _, v := range counts.Facets[attemptlog.FacetServers] {
		ids.server(v.Value)
	}
	for _, v := range counts.Facets[attemptlog.FacetMembers] {
		ids.server(v.Value)
	}
	for _, v := range counts.Facets[attemptlog.FacetPeople] {
		ids.subject(v.Value)
	}
	n, err := s.resolve(ctx, c, ids)
	if err != nil {
		return nil, err
	}
	label := func(facet attemptlog.Facet) []*gen.ConnectionFacetValue {
		values := counts.Facets[facet]
		out := make([]*gen.ConnectionFacetValue, len(values))
		for i, v := range values {
			out[i] = &gen.ConnectionFacetValue{Value: v.Value, Label: n.facetLabel(facet, v.Value), Attempts: count(v.Attempts)}
		}
		return out
	}
	result.LoggingEnabled = true
	result.TotalAttempts = new(count(counts.Total))
	result.Truncated = new(counts.Truncated)
	result.Facets = &gen.ConnectionAttemptFacets{
		Outcomes:    label(attemptlog.FacetOutcomes),
		Steps:       label(attemptlog.FacetSteps),
		Servers:     label(attemptlog.FacetServers),
		Members:     label(attemptlog.FacetMembers),
		People:      label(attemptlog.FacetPeople),
		Networks:    label(attemptlog.FacetNetworks),
		Apps:        label(attemptlog.FacetApps),
		AppVersions: label(attemptlog.FacetAppVersions),
		Reasons:     label(attemptlog.FacetReasons),
		Services:    label(attemptlog.FacetServices),
	}
	return result, nil
}

func (n names) facetLabel(facet attemptlog.Facet, value string) *string {
	switch facet {
	case attemptlog.FacetServers, attemptlog.FacetMembers:
		return n.serverName(value)
	case attemptlog.FacetPeople:
		if p := n.person(value, false); p != nil {
			return p.Name
		}
		return nil
	case attemptlog.FacetNetworks:
		return new(attemptlog.NetworkLabel(value))
	case attemptlog.FacetApps:
		return new(attemptlog.AppName(value))
	case attemptlog.FacetAppVersions:
		family, version, _ := strings.Cut(value, "@")
		return new(attemptlog.AppName(family) + " " + version)
	case attemptlog.FacetReasons:
		return conv.PtrEmpty(attemptlog.MessageOf(value))
	case attemptlog.FacetOutcomes, attemptlog.FacetSteps, attemptlog.FacetServices:
		return nil
	default:
		return nil
	}
}

func (s *Service) GetAttempt(ctx context.Context, payload *gen.GetAttemptPayload) (*gen.GetConnectionAttemptResult, error) {
	c, err := s.beginLogs(ctx)
	if err != nil {
		return nil, err
	}
	result := &gen.GetConnectionAttemptResult{LoggingEnabled: false, ViewerScope: c.viewerScope(), Found: nil, Attempt: nil}
	on, err := s.loggingOn(ctx, c)
	if err != nil || !on {
		return result, err
	}
	result.LoggingEnabled = true
	result.Found = new(false)

	id, err := uuid.Parse(payload.Reference)
	if err != nil || id == uuid.Nil || uuid.Validate(payload.Reference) != nil {
		return result, nil //nolint:nilerr // an unreadable reference matches nothing
	}
	now := s.now()
	scope := attemptlog.Scope{
		OrganizationID:  c.organizationID,
		ProjectID:       c.projectID,
		IssuerID:        uuid.Nil,
		IssuerServerIDs: nil,
		ServerID:        "",
		AttemptID:       id,
		From:            now.Add(-attemptlog.Retention),
		To:              now.Add(detailLookahead),
		OwnSubjects:     c.ownSubjects,
	}
	row, steps, err := attemptlog.Get(ctx, s.ch, scope, now)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get connection attempt").LogError(ctx, s.logger)
	}
	if row == nil {
		return result, nil
	}
	ids := newCollector()
	ids.row(*row)
	for _, step := range steps {
		ids.server(step.MemberID)
	}
	n, err := s.resolve(ctx, c, ids)
	if err != nil {
		return nil, err
	}

	view := n.row(*row)
	detail := &gen.ConnectionAttemptDetail{
		ID:                  view.ID,
		Time:                view.Time,
		FirstStartedAt:      view.FirstStartedAt,
		Count:               view.Count,
		Outcome:             view.Outcome,
		Step:                view.Step,
		Reason:              view.Reason,
		Message:             view.Message,
		TechnicalMessage:    view.TechnicalMessage,
		Source:              view.Source,
		Person:              n.person(row.Subject, true),
		Network:             view.Network,
		App:                 view.App,
		Server:              view.Server,
		Member:              view.Member,
		Service:             view.Service,
		StartedAt:           view.FirstStartedAt,
		UserSessionIssuerID: nil,
		TraceID:             conv.PtrEmpty(row.TraceID),
		Steps:               make([]*gen.ConnectionAttemptStep, len(steps)),
		Context:             &gen.ConnectionAttemptContext{GrantType: conv.PtrEmpty(row.GrantType), SubReason: nil, Retries: 0},
	}
	if len(row.IssuerIDs) == 1 {
		detail.UserSessionIssuerID = &row.IssuerIDs[0]
	}
	for i, step := range steps {
		detail.Context.Retries += int64(step.Retries)
		if step.Final && step.SubReason != "" && detail.Context.SubReason == nil {
			detail.Context.SubReason = &step.SubReason
		}
		detail.Steps[i] = &gen.ConnectionAttemptStep{
			At:               formatTime(step.At),
			Step:             gen.ConnectionStepGroup(step.Group),
			StoredStep:       step.StoredStep,
			Result:           step.Result,
			Final:            step.Final,
			Reason:           conv.PtrEmpty(step.Reason),
			Message:          conv.PtrEmpty(attemptlog.MessageOf(step.Reason)),
			TechnicalMessage: conv.PtrEmpty(attemptlog.TechnicalOf(step.Reason)),
			Member:           n.member(step.MemberID),
			Service:          conv.PtrEmpty(step.Service),
		}
	}
	if detail.Context.SubReason == nil {
		for _, step := range steps {
			if step.Reason == row.Reason && step.SubReason != "" {
				detail.Context.SubReason = &step.SubReason
			}
		}
	}
	result.Found = new(true)
	result.Attempt = detail
	return result, nil
}

func stringValues[T ~string](values []T) []string {
	if values == nil {
		return nil
	}
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return out
}
