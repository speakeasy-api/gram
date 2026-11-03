// Package orgmemory serves the dashboard's read-only view of a project's
// Organizational Memory: the digest timeline, the sessions and memos each
// digest cites, and recent activity not yet rolled up.
//
// Summaries, memos and digests are already masked: summaries come from the
// redacted transcript, memos are redacted when saved, and digests are written
// only from those. Session titles are raw, so they are returned only to
// callers who also hold chat:read. Reads require memory:read on the project, the same grant the
// Platform MCP memory tools require, so people and agents see one corpus
// under one permission.
package orgmemory

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/org_memory/server"
	gen "github.com/speakeasy-api/gram/server/gen/org_memory"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orgmemory/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// recentWindow bounds how far back the not-yet-rolled-up view looks. Day
// digests close nightly, so anything older has normally been folded.
const recentWindow = 7 * 24 * time.Hour

// auditToolPrefix marks dashboard reads in the team memory audit trail, so
// recall counts can tell them apart from agent reads.
const auditToolPrefix = "dashboard."

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	auth   *auth.Auth
	authz  *authz.Engine
	audit  *audit.Logger
	flags  feature.Provider
	now    func() time.Time
}

var (
	_ gen.Service = (*Service)(nil)
	_ gen.Auther  = (*Service)(nil)
)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessionManager *sessions.Manager,
	authzEngine *authz.Engine,
	auditLogger *audit.Logger,
	flags feature.Provider,
) *Service {
	logger = logger.With(attr.SlogComponent("org-memory-api"))
	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/orgmemory"),
		logger: logger,
		db:     db,
		auth:   auth.New(logger, db, sessionManager, authzEngine),
		authz:  authzEngine,
		audit:  auditLogger,
		flags:  flags,
		now:    time.Now,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(mux, srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// authorize requires memory:read on the active project and the org-memory
// rollout. A disabled rollout reads as not found, so the
// dashboard hides the tab rather than showing an error.
func (s *Service) authorize(ctx context.Context) (*contextvalues.AuthContext, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	// Without RBAC enforcement every check passes, which would show the whole
	// project's memory to any member. Agent Sessions treats that case as
	// own-sessions-only, so this view, which spans every session, refuses it.
	enforce, err := s.authz.ShouldEnforce(ctx)
	if err != nil {
		return nil, err
	}
	if !enforce {
		return nil, oops.E(oops.CodeForbidden, nil, "organizational memory requires role-based access control")
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMemoryRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}
	enabled, err := chat.OrgMemoryFlag(ctx, s.db, s.flags, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "check organizational memory rollout").LogError(ctx, s.logger)
	}
	if !enabled {
		return nil, oops.E(oops.CodeNotFound, nil, "organizational memory is not enabled")
	}
	return authCtx, nil
}

// titlesVisible reports whether session titles may accompany summaries.
// Titles are raw, unmasked text, so they go only to callers who can already
// read the project's sessions with chat:read; memory:read alone gets
// summaries without titles, the same as the Platform MCP tools.
func (s *Service) titlesVisible(ctx context.Context, authCtx *contextvalues.AuthContext) (bool, error) {
	ok, err := s.authz.Evaluate(ctx, authz.ChatReadCheck(authCtx.ProjectID.String()))
	if err != nil {
		return false, oops.E(oops.CodeUnexpected, err, "check session title access").LogError(ctx, s.logger)
	}
	return ok, nil
}

// recordRead audits a dashboard read with the sources it showed, mirroring
// the Platform MCP tools so one trail covers every disclosure.
func (s *Service) recordRead(ctx context.Context, authCtx *contextvalues.AuthContext, method string, sources []string) error {
	// Every disclosure leaves an audit row: without a logger, refuse the read.
	if s.audit == nil {
		return oops.E(oops.CodeUnexpected, nil, "organizational memory audit logger is not configured").LogError(ctx, s.logger)
	}
	if err := s.audit.LogTeamMemoryRead(ctx, s.db, audit.LogTeamMemoryReadEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		ProjectSlug:      conv.PtrValOr(authCtx.ProjectSlug, ""),
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: nil,
		ActorSlug:        nil,
		Tool:             auditToolPrefix + method,
		Sources:          sources,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "audit organizational memory read").LogError(ctx, s.logger)
	}
	return nil
}

func (s *Service) GetOrgMemoryOverview(ctx context.Context, payload *gen.GetOrgMemoryOverviewPayload) (*gen.GetOrgMemoryOverviewResult, error) {
	authCtx, err := s.authorize(ctx)
	if err != nil {
		return nil, err
	}
	since := pgtype.Timestamptz{Time: s.now().Add(-time.Duration(payload.Days) * 24 * time.Hour), InfinityModifier: pgtype.Finite, Valid: true}
	q := repo.New(s.db)
	projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID

	remembered, err := q.CountRememberedSessions(ctx, repo.CountRememberedSessionsParams{ProjectID: projectID, OrganizationID: orgID, Since: since})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count remembered sessions").LogError(ctx, s.logger)
	}
	leftOut, err := q.CountLeftOutSessions(ctx, repo.CountLeftOutSessionsParams{ProjectID: projectID, OrganizationID: orgID, Since: since})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count left out sessions").LogError(ctx, s.logger)
	}
	memos, err := q.CountSavedMemos(ctx, repo.CountSavedMemosParams{ProjectID: projectID, OrganizationID: orgID, Since: since})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count memos").LogError(ctx, s.logger)
	}
	recalls, err := q.CountMemoryRecalls(ctx, repo.CountMemoryRecallsParams{OrganizationID: orgID, ProjectID: projectID.String(), Since: since})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count memory recalls").LogError(ctx, s.logger)
	}

	return &gen.GetOrgMemoryOverviewResult{
		Days:               payload.Days,
		SessionsRemembered: remembered,
		SessionsLeftOut:    leftOut,
		MemosSaved:         memos,
		Recalls:            recalls,
	}, nil
}

func (s *Service) ListOrgMemoryDigests(ctx context.Context, payload *gen.ListOrgMemoryDigestsPayload) (*gen.ListOrgMemoryDigestsResult, error) {
	authCtx, err := s.authorize(ctx)
	if err != nil {
		return nil, err
	}
	before := pgtype.Date{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	if payload.Before != nil {
		t, err := time.Parse(time.DateOnly, *payload.Before)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid before date")
		}
		before = pgtype.Date{Time: t, InfinityModifier: pgtype.Finite, Valid: true}
	}

	rows, err := repo.New(s.db).ListDigests(ctx, repo.ListDigestsParams{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		Level:          payload.Level,
		Before:         before,
		RowLimit:       conv.SafeInt32(payload.Limit + 1),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list digests").LogError(ctx, s.logger)
	}

	var nextBefore *string
	if len(rows) > payload.Limit {
		rows = rows[:payload.Limit]
		nextBefore = new(formatDate(rows[len(rows)-1].PeriodStart))
	}

	digests := make([]*gen.OrgMemoryDigest, 0, len(rows))
	sources := make([]string, 0, len(rows))
	for _, row := range rows {
		digests = append(digests, digestView(row.ID, row.Level, row.PeriodStart, row.PeriodEnd, row.Content, row.SessionCount, row.MemoCount, row.ChildCount, row.GeneratedAt))
		sources = append(sources, "digest:"+row.ID.String())
	}
	if err := s.recordRead(ctx, authCtx, "listDigests", sources); err != nil {
		return nil, err
	}
	return &gen.ListOrgMemoryDigestsResult{Digests: digests, NextBefore: nextBefore}, nil
}

func (s *Service) GetOrgMemoryDigest(ctx context.Context, payload *gen.GetOrgMemoryDigestPayload) (*gen.GetOrgMemoryDigestResult, error) {
	authCtx, err := s.authorize(ctx)
	if err != nil {
		return nil, err
	}
	showTitles, err := s.titlesVisible(ctx, authCtx)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid digest id")
	}
	q := repo.New(s.db)
	projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID

	row, err := q.GetDigest(ctx, repo.GetDigestParams{ID: id, ProjectID: projectID, OrganizationID: orgID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load digest").LogError(ctx, s.logger)
	}

	result := &gen.GetOrgMemoryDigestResult{
		Digest:   digestView(row.ID, row.Level, row.PeriodStart, row.PeriodEnd, row.Content, conv.SafeInt32(len(row.ChatIds)), conv.SafeInt32(len(row.MemoIds)), conv.SafeInt32(len(row.ChildDigestIds)), row.GeneratedAt),
		Children: []*gen.OrgMemoryDigest{},
		Sessions: []*gen.OrgMemorySession{},
		Memos:    []*gen.OrgMemoryMemo{},
	}
	sources := []string{"digest:" + row.ID.String()}

	if len(row.ChildDigestIds) > 0 {
		children, err := q.ListDigestsByID(ctx, repo.ListDigestsByIDParams{Ids: row.ChildDigestIds, ProjectID: projectID, OrganizationID: orgID})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "load child digests").LogError(ctx, s.logger)
		}
		for _, c := range children {
			result.Children = append(result.Children, digestView(c.ID, c.Level, c.PeriodStart, c.PeriodEnd, c.Content, c.SessionCount, c.MemoCount, c.ChildCount, c.GeneratedAt))
			sources = append(sources, "digest:"+c.ID.String())
		}
	}
	if len(row.ChatIds) > 0 {
		sessions, err := q.ListSessionsByID(ctx, repo.ListSessionsByIDParams{Ids: row.ChatIds, ProjectID: projectID, OrganizationID: orgID})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "load digest sessions").LogError(ctx, s.logger)
		}
		for _, c := range sessions {
			result.Sessions = append(result.Sessions, sessionView(c.ID, c.Title, c.Summary, c.SummaryGeneratedAt, showTitles))
			sources = append(sources, "chat:"+c.ID.String())
		}
	}
	if len(row.MemoIds) > 0 {
		memos, err := q.ListMemosByID(ctx, repo.ListMemosByIDParams{Ids: row.MemoIds, ProjectID: projectID, OrganizationID: orgID})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "load digest memos").LogError(ctx, s.logger)
		}
		for _, m := range memos {
			result.Memos = append(result.Memos, memoView(m.ID, m.ChatID, m.Content, m.AuthorUserID, m.AuthorAgentID, m.AuthorName, m.CreatedAt))
			sources = append(sources, "memo:"+m.ID.String())
		}
	}

	// Counts match what is shown: a source excluded or deleted since the
	// digest was written is neither listed nor counted.
	result.Digest.SessionCount = len(result.Sessions)
	result.Digest.MemoCount = len(result.Memos)

	if err := s.recordRead(ctx, authCtx, "getDigest", sources); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) ListOrgMemoryRecent(ctx context.Context, payload *gen.ListOrgMemoryRecentPayload) (*gen.ListOrgMemoryRecentResult, error) {
	authCtx, err := s.authorize(ctx)
	if err != nil {
		return nil, err
	}
	showTitles, err := s.titlesVisible(ctx, authCtx)
	if err != nil {
		return nil, err
	}
	since := pgtype.Timestamptz{Time: s.now().Add(-recentWindow), InfinityModifier: pgtype.Finite, Valid: true}
	q := repo.New(s.db)
	projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID

	sessions, err := q.ListRecentSessions(ctx, repo.ListRecentSessionsParams{ProjectID: projectID, OrganizationID: orgID, Since: since, RowLimit: conv.SafeInt32(payload.Limit)})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list recent sessions").LogError(ctx, s.logger)
	}
	memos, err := q.ListRecentMemos(ctx, repo.ListRecentMemosParams{ProjectID: projectID, OrganizationID: orgID, Since: since, RowLimit: conv.SafeInt32(payload.Limit)})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list recent memos").LogError(ctx, s.logger)
	}

	result := &gen.ListOrgMemoryRecentResult{
		Sessions: make([]*gen.OrgMemorySession, 0, len(sessions)),
		Memos:    make([]*gen.OrgMemoryMemo, 0, len(memos)),
	}
	sources := make([]string, 0, len(sessions)+len(memos))
	for _, c := range sessions {
		result.Sessions = append(result.Sessions, sessionView(c.ID, c.Title, c.Summary, c.SummaryGeneratedAt, showTitles))
		sources = append(sources, "chat:"+c.ID.String())
	}
	for _, m := range memos {
		result.Memos = append(result.Memos, memoView(m.ID, m.ChatID, m.Content, m.AuthorUserID, m.AuthorAgentID, m.AuthorName, m.CreatedAt))
		sources = append(sources, "memo:"+m.ID.String())
	}
	if err := s.recordRead(ctx, authCtx, "recent", sources); err != nil {
		return nil, err
	}
	return result, nil
}

func formatDate(d pgtype.Date) string {
	return d.Time.Format(time.DateOnly)
}

func formatTime(t pgtype.Timestamptz) string {
	return t.Time.UTC().Format(time.RFC3339)
}

func digestView(id uuid.UUID, level string, start, end pgtype.Date, content string, sessions, memos, children int32, generatedAt pgtype.Timestamptz) *gen.OrgMemoryDigest {
	return &gen.OrgMemoryDigest{
		ID:           id.String(),
		Level:        level,
		PeriodStart:  formatDate(start),
		PeriodEnd:    formatDate(end),
		Content:      content,
		SessionCount: int(sessions),
		MemoCount:    int(memos),
		ChildCount:   int(children),
		GeneratedAt:  formatTime(generatedAt),
	}
}

func sessionView(id uuid.UUID, title pgtype.Text, summary string, summarizedAt pgtype.Timestamptz, showTitle bool) *gen.OrgMemorySession {
	if !showTitle {
		title = pgtype.Text{String: "", Valid: false}
	}
	return &gen.OrgMemorySession{
		ID:           id.String(),
		Title:        conv.FromPGTextOrEmpty[string](title),
		Summary:      summary,
		SummarizedAt: formatTime(summarizedAt),
	}
}

func memoView(id uuid.UUID, chatID uuid.NullUUID, content string, authorUserID pgtype.Text, authorAgentID uuid.NullUUID, authorName string, createdAt pgtype.Timestamptz) *gen.OrgMemoryMemo {
	kind := "system"
	switch {
	case authorUserID.Valid:
		kind = "user"
	case authorAgentID.Valid:
		kind = "agent"
	}
	var chat *string
	if chatID.Valid {
		chat = new(chatID.UUID.String())
	}
	return &gen.OrgMemoryMemo{
		ID:         id.String(),
		Content:    content,
		ChatID:     chat,
		AuthorKind: kind,
		AuthorName: conv.PtrEmpty(authorName),
		CreatedAt:  formatTime(createdAt),
	}
}
