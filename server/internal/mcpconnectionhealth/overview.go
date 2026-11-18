package mcpconnectionhealth

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_connection_health"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/attemptlog"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionhealth/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
)

func (s *Service) GetSignInSummary(ctx context.Context, payload *gen.GetSignInSummaryPayload) (*gen.ConnectionSignInSummaryResult, error) {
	c, err := s.beginLogs(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now()
	scope, err := s.attemptScope(ctx, c, rangeQuery{issuerID: payload.UserSessionIssuerID, serverID: payload.ServerID, from: payload.From, to: payload.To}, now)
	if err != nil {
		return nil, err
	}
	result := &gen.ConnectionSignInSummaryResult{LoggingEnabled: false, ViewerScope: c.viewerScope(), Bucket: nil, Buckets: nil}
	on, err := s.loggingOn(ctx, c)
	if err != nil || !on {
		return result, err
	}

	bucket := attemptlog.DefaultBucket(scope.From, scope.To)
	if payload.Bucket != nil {
		bucket = attemptlog.Bucket(*payload.Bucket)
	}
	buckets, err := attemptlog.SignIns(ctx, s.ch, scope, bucket, now)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "summarize sign-ins").LogError(ctx, s.logger)
	}
	result.LoggingEnabled = true
	result.Bucket = new(string(bucket))
	result.Buckets = make([]*gen.ConnectionSignInBucket, len(buckets))
	for i, b := range buckets {
		result.Buckets[i] = &gen.ConnectionSignInBucket{
			Start:    formatTime(b.Start),
			SignIns:  &gen.ConnectionSignInCounts{Succeeded: count(b.SignInsSucceeded), DidNot: count(b.SignInsDidNot)},
			Renewals: &gen.ConnectionSignInCounts{Succeeded: count(b.RenewalsSucceeded), DidNot: count(b.RenewalsDidNot)},
		}
	}
	return result, nil
}

func (s *Service) ListDeniedClients(ctx context.Context, payload *gen.ListDeniedClientsPayload) (*gen.ConnectionDeniedClientsResult, error) {
	c, err := s.beginLogs(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now()
	scope, err := s.attemptScope(ctx, c, rangeQuery{issuerID: payload.UserSessionIssuerID, serverID: payload.ServerID, from: payload.From, to: payload.To}, now)
	if err != nil {
		return nil, err
	}
	result := &gen.ConnectionDeniedClientsResult{LoggingEnabled: false, ViewerScope: c.viewerScope(), Clients: nil}
	on, err := s.loggingOn(ctx, c)
	if err != nil || !on {
		return result, err
	}

	clients, err := attemptlog.Denied(ctx, s.ch, scope, now)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list denied clients").LogError(ctx, s.logger)
	}
	result.LoggingEnabled = true
	result.Clients = make([]*gen.ConnectionDeniedClient, len(clients))
	for i, client := range clients {
		result.Clients[i] = &gen.ConnectionDeniedClient{
			App:              appView(client.App, "", client.AppKey),
			Reason:           client.Reason,
			Message:          attemptlog.MessageOf(client.Reason),
			TechnicalMessage: conv.PtrEmpty(attemptlog.TechnicalOf(client.Reason)),
			Attempts:         count(client.Attempts),
			People:           count(client.People),
			Networks:         count(client.Networks),
			LastAt:           formatTime(client.LastAt),
		}
	}
	return result, nil
}

func (s *Service) GetSessionActivity(ctx context.Context, payload *gen.GetSessionActivityPayload) (*gen.ConnectionSessionActivityResult, error) {
	c, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.authz.Require(ctx, authz.Project(c.projectID).Read()); err != nil {
		return nil, err
	}
	now := s.now()
	from, to, err := s.parseRange(ctx, payload.From, payload.To, now)
	if err != nil {
		return nil, err
	}
	issuer, err := s.provider(ctx, c, payload.UserSessionIssuerID)
	if err != nil {
		return nil, err
	}

	queries := repo.New(s.db)
	project := uuid.NullUUID{UUID: c.projectID, Valid: true}
	firstDay := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	lastDay := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	if lastDay.Equal(to) {
		lastDay = lastDay.AddDate(0, 0, -1)
	}
	days, err := queries.CountActiveSessionsByDay(ctx, repo.CountActiveSessionsByDayParams{
		FromDay:        timestamptz(firstDay),
		LastDay:        timestamptz(lastDay),
		IssuerID:       issuer.ID,
		ProjectID:      project,
		OrganizationID: c.organizationID,
		ToDay:                timestamptz(lastDay.AddDate(0, 0, 1)),
		DashboardMintPattern: dashboardMintPattern,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count active sessions").LogError(ctx, s.logger)
	}
	activeNow, err := queries.CountActiveSessionsNow(ctx, repo.CountActiveSessionsNowParams{IssuerID: issuer.ID, ProjectID: project, OrganizationID: c.organizationID, DashboardMintPattern: dashboardMintPattern})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count active sessions now").LogError(ctx, s.logger)
	}
	averageFrom := now.Add(-averageSessionWindow)
	average, err := queries.AverageEndedSessionSeconds(ctx, repo.AverageEndedSessionSecondsParams{
		FromTime:       timestamptz(averageFrom),
		ToTime:         timestamptz(now.Add(clockSkewAllowance)),
		IssuerID:       issuer.ID,
		ProjectID:      project,
		OrganizationID:       c.organizationID,
		DashboardMintPattern: dashboardMintPattern,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "average session length").LogError(ctx, s.logger)
	}

	result := &gen.ConnectionSessionActivityResult{
		Days:                     make([]*gen.ConnectionSessionDay, len(days)),
		ActiveNow:                activeNow,
		AverageSessionSeconds:    nil,
		ConfiguredSessionSeconds: intervalSeconds(issuer.SessionDuration),
		AverageWindowFrom:        formatTime(averageFrom),
		AverageWindowTo:          formatTime(now),
	}
	if average >= 0 {
		result.AverageSessionSeconds = new(int64(average))
	}
	for i, day := range days {
		result.Days[i] = &gen.ConnectionSessionDay{Day: day.DayStart.Time.UTC().Format(time.DateOnly), ActiveSessions: day.ActiveSessions}
	}
	return result, nil
}

// dashboardMintPattern matches dashboard-minted sessions, which end with their access token.
var dashboardMintPattern = sessiontokens.DashboardMintRefreshTokenHashPrefix + ":%"

// averageSessionWindow is how far back the average session length looks. It is
// fixed so the readout does not swing with the chart's range.
const averageSessionWindow = 30 * 24 * time.Hour

// clockSkewAllowance covers a database clock slightly ahead of this server's. The
// query also stops at the database's own now, so nothing later is counted.
const clockSkewAllowance = time.Minute

func timestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, InfinityModifier: pgtype.Finite, Valid: true}
}

// intervalSeconds reads a session duration. Months count as 30 days, as Postgres does for epoch.
func intervalSeconds(i pgtype.Interval) int64 {
	const (
		secondsPerDay   = 24 * 60 * 60
		daysPerMonth    = 30
		microsPerSecond = 1_000_000
	)
	if !i.Valid {
		return 0
	}
	return i.Microseconds/microsPerSecond + int64(i.Days)*secondsPerDay + int64(i.Months)*daysPerMonth*secondsPerDay
}
