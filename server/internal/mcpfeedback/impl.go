package mcpfeedback

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/mcp_feedback/server"
	gen "github.com/speakeasy-api/gram/server/gen/mcp_feedback"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpfeedback/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// feedbackWindowDays is how far back the counts and the tool ranking of a
	// server's feedback reach. A month is long enough to see whether a problem
	// keeps coming back after a fix.
	feedbackWindowDays = 30

	// topReportedTools is how many tools the ranking of most-named tools
	// holds. Agents write the names, so the ranking is capped; a server's
	// reports cluster on a handful of tools, and ten covers the ones worth
	// fixing first.
	topReportedTools = 10

	// maxFeedbackPageSize is the most reports one page returns, matching the
	// limit the API declares.
	maxFeedbackPageSize = 50
)

// Service is the management API for MCP server feedback: the settings of a
// server's feedback tool, and the reports agents sent through it.
type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	auth   *auth.Auth
	authz  *authz.Engine
	audit  *audit.Logger
}

var (
	_ gen.Service = (*Service)(nil)
	_ gen.Auther  = (*Service)(nil)
)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessionManager *sessions.Manager, authzEngine *authz.Engine, auditLogger *audit.Logger) *Service {
	logger = logger.With(attr.SlogComponent("mcpfeedback"))

	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/mcpfeedback"),
		logger: logger,
		db:     db,
		auth:   auth.New(logger, db, sessionManager, authzEngine),
		authz:  authzEngine,
		audit:  auditLogger,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) GetSettings(ctx context.Context, payload *gen.GetSettingsPayload) (*types.McpFeedbackSettings, error) {
	authCtx, logger, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}

	server, err := s.readableServer(ctx, logger, payload.McpServerID, *authCtx.ProjectID)
	if err != nil {
		return nil, err
	}

	queries := repo.New(s.db)
	config, err := queries.GetFeedbackConfig(ctx, repo.GetFeedbackConfigParams{McpServerID: server.ID, ProjectID: server.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return buildSettingsView(server.ID, Settings{Enabled: false, ToolName: "", ToolDescription: ""}, nil, nil), nil
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get mcp feedback settings").LogError(ctx, logger)
	}

	editor, err := s.settingsEditor(ctx, queries, server)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get mcp feedback settings editor").LogError(ctx, logger)
	}

	return buildSettingsView(server.ID, SettingsFromConfig(config), &config.UpdatedAt.Time, editor), nil
}

func (s *Service) UpdateSettings(ctx context.Context, payload *gen.UpdateSettingsPayload) (*types.McpFeedbackSettings, error) {
	authCtx, logger, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}

	serverID, err := uuid.Parse(payload.McpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server id")
	}

	settings := settingsFromForm(payload)
	if err := settings.Validate(); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "%s", err.Error())
	}

	// Checked before the transaction so a caller who may not write the server
	// never takes its row lock.
	if _, err := s.authorizedServer(ctx, logger, serverID, *authCtx.ProjectID, true); err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	server, err := mcpserversrepo.New(dbtx).LockMCPServerByIDAndProjectID(ctx, mcpserversrepo.LockMCPServerByIDAndProjectIDParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "lock mcp server").LogError(ctx, logger)
	}
	// The locked row is authoritative if the server's backing changed after
	// the check above.
	if err := s.authz.Require(ctx, authz.MCPServer(authz.MCPServerIDOf(mcpGrantResourceID(server)), authz.ProjectIDOf(server.ProjectID)).Write()); err != nil {
		return nil, err
	}
	if settings.Enabled && !builtWithSpeakeasy(server) {
		return nil, oops.E(oops.CodeInvalid, nil, "only MCP servers built from your API or functions can offer the feedback tool for now")
	}

	queries := repo.New(dbtx)
	before := Settings{Enabled: false, ToolName: "", ToolDescription: ""}
	var beforeUpdatedAt *time.Time
	existing, err := queries.GetFeedbackConfigForUpdate(ctx, repo.GetFeedbackConfigForUpdateParams{McpServerID: server.ID, ProjectID: server.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get existing mcp feedback settings").LogError(ctx, logger)
	default:
		before = SettingsFromConfig(existing)
		beforeUpdatedAt = &existing.UpdatedAt.Time
	}
	if payload.ExpectedVersion != nil && *payload.ExpectedVersion != settingsVersion(beforeUpdatedAt) {
		return nil, oops.E(oops.CodeConflict, nil, "these feedback settings were changed by someone else since you opened them; reload to see the change").LogInfo(ctx, logger)
	}

	updated, err := queries.UpsertFeedbackConfig(ctx, repo.UpsertFeedbackConfigParams{
		McpServerID:     server.ID,
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       server.ProjectID,
		Enabled:         settings.Enabled,
		ToolName:        conv.ToPGTextEmpty(settings.ToolName),
		ToolDescription: conv.ToPGTextEmpty(settings.ToolDescription),
		UpdatedByUserID: conv.ToPGTextEmpty(authCtx.UserID),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "save mcp feedback settings").LogError(ctx, logger)
	}

	view := buildSettingsView(server.ID, SettingsFromConfig(updated), &updated.UpdatedAt.Time, authCtx.Email)

	if err := s.audit.LogMcpServerFeedbackSettingsUpdate(ctx, dbtx, audit.LogMcpServerFeedbackSettingsUpdateEvent{
		OrganizationID:                    authCtx.ActiveOrganizationID,
		ProjectID:                         server.ProjectID,
		Actor:                             urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:                  authCtx.Email,
		ActorSlug:                         nil,
		McpServerURN:                      urn.NewMcpServer(server.ID),
		McpServerName:                     conv.FromPGTextOrEmpty[string](server.Name),
		McpServerSlug:                     conv.FromPGTextOrEmpty[string](server.Slug),
		McpFeedbackSettingsSnapshotBefore: buildSettingsView(server.ID, before, beforeUpdatedAt, nil),
		McpFeedbackSettingsSnapshotAfter:  buildSettingsView(server.ID, SettingsFromConfig(updated), &updated.UpdatedAt.Time, nil),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log mcp feedback settings update").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return view, nil
}

func (s *Service) List(ctx context.Context, payload *gen.ListPayload) (*gen.ListMcpFeedbackResult, error) {
	authCtx, logger, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}

	return s.list(ctx, logger, *authCtx.ProjectID, payload)
}

// ListForProject lists one server's feedback in a project the caller names
// directly rather than through the request's selected project, as the
// Platform MCP does. It runs the same validation and authorization checks as
// List, against the grants already loaded on ctx.
func (s *Service) ListForProject(ctx context.Context, projectID uuid.UUID, payload *gen.ListPayload) (*gen.ListMcpFeedbackResult, error) {
	return s.list(ctx, s.logger.With(attr.SlogProjectID(projectID.String())), projectID, payload)
}

func (s *Service) list(ctx context.Context, logger *slog.Logger, projectID uuid.UUID, payload *gen.ListPayload) (*gen.ListMcpFeedbackResult, error) {
	if payload.Limit < 1 || payload.Limit > maxFeedbackPageSize {
		return nil, oops.E(oops.CodeBadRequest, nil, "limit must be between 1 and %d", maxFeedbackPageSize)
	}

	severity := pgtype.Text{String: "", Valid: false}
	if payload.Severity != nil {
		parsed, ok := ParseSeverity(string(*payload.Severity))
		if !ok || parsed == SeverityUnspecified {
			return nil, oops.E(oops.CodeBadRequest, nil, "severity must be %s, %s or %s", SeverityBlocked, SeverityDegraded, SeverityCosmetic)
		}
		severity = conv.ToPGText(string(parsed))
	}

	toolName := pgtype.Text{String: "", Valid: false}
	if payload.ToolName != nil {
		toolName = conv.ToPGTextEmpty(strings.TrimSpace(*payload.ToolName))
	}

	cursorCreatedAt := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	cursorID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if payload.Cursor != nil {
		createdAt, id, err := decodeFeedbackCursor(*payload.Cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor")
		}
		cursorCreatedAt = conv.ToPGTimestamptz(createdAt)
		cursorID = uuid.NullUUID{UUID: id, Valid: true}
	}

	server, err := s.readableServer(ctx, logger, payload.McpServerID, projectID)
	if err != nil {
		return nil, err
	}

	queries := repo.New(s.db)

	counts, err := queries.CountFeedbackBySeverity(ctx, repo.CountFeedbackBySeverityParams{
		ProjectID:   server.ProjectID,
		McpServerID: server.ID,
		WindowDays:  feedbackWindowDays,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count mcp feedback").LogError(ctx, logger)
	}
	// The ranking covers the window the counts were taken over.
	windowStart, windowEnd := counts.WindowStart.Time.UTC(), counts.WindowEnd.Time.UTC()

	toolRows, err := queries.ListFeedbackToolCounts(ctx, repo.ListFeedbackToolCountsParams{
		ProjectID:   server.ProjectID,
		McpServerID: server.ID,
		WindowStart: conv.ToPGTimestamptz(windowStart),
		ToolLimit:   topReportedTools,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "rank tools in mcp feedback").LogError(ctx, logger)
	}

	rows, err := queries.ListFeedback(ctx, repo.ListFeedbackParams{
		ProjectID:       server.ProjectID,
		McpServerID:     server.ID,
		Severity:        severity,
		ToolName:        toolName,
		CursorCreatedAt: cursorCreatedAt,
		CursorID:        cursorID,
		PageLimit:       conv.SafeInt32(payload.Limit + 1),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list mcp feedback").LogError(ctx, logger)
	}

	var nextCursor *string
	if len(rows) > payload.Limit {
		rows = rows[:payload.Limit]
		last := rows[len(rows)-1]
		nextCursor = new(encodeFeedbackCursor(last.CreatedAt.Time, last.ID))
	}

	feedback := make([]*types.McpFeedback, len(rows))
	for i, row := range rows {
		feedback[i] = buildFeedbackView(row)
	}

	tools := make([]*gen.McpFeedbackToolCount, len(toolRows))
	for i, row := range toolRows {
		tools[i] = &gen.McpFeedbackToolCount{ToolName: row.ToolName, FeedbackCount: row.FeedbackCount, BlockedCount: row.BlockedCount}
	}

	return &gen.ListMcpFeedbackResult{
		Counts: &gen.McpFeedbackCounts{
			WindowStart: windowStart.Format(time.RFC3339Nano),
			WindowEnd:   windowEnd.Format(time.RFC3339Nano),
			Total:       counts.Total,
			Blocked:     counts.Blocked,
			Degraded:    counts.Degraded,
			Cosmetic:    counts.Cosmetic,
			Unspecified: counts.Unspecified,
		},
		Tools:      tools,
		Feedback:   feedback,
		NextCursor: nextCursor,
	}, nil
}

// caller is the authenticated caller of a project-scoped request.
func (s *Service) caller(ctx context.Context) (*contextvalues.AuthContext, *slog.Logger, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, s.logger, oops.C(oops.CodeUnauthorized)
	}

	return authCtx, s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String())), nil
}

// readableServer loads the MCP server a read names and checks the caller may
// read it.
func (s *Service) readableServer(ctx context.Context, logger *slog.Logger, rawID string, projectID uuid.UUID) (mcpserversrepo.McpServer, error) {
	serverID, err := uuid.Parse(rawID)
	if err != nil {
		return mcpserversrepo.McpServer{}, oops.E(oops.CodeBadRequest, err, "invalid mcp server id")
	}

	return s.authorizedServer(ctx, logger, serverID, projectID, false)
}

// authorizedServer loads an MCP server and checks the caller may read it, or
// write it when write is set. A server that does not exist is reported as not
// found only to a caller who could read or write the project's servers, so the
// answer does not tell anyone else which ids exist.
func (s *Service) authorizedServer(ctx context.Context, logger *slog.Logger, serverID, projectID uuid.UUID, write bool) (mcpserversrepo.McpServer, error) {
	server, err := mcpserversrepo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        serverID,
		ProjectID: projectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		servers := authz.MCPServers(authz.ProjectIDOf(projectID))
		check := servers.Read()
		if write {
			check = servers.Write()
		}
		if err := s.authz.Require(ctx, check); err != nil {
			return mcpserversrepo.McpServer{}, err
		}
		return mcpserversrepo.McpServer{}, oops.E(oops.CodeNotFound, nil, "mcp server not found")
	case err != nil:
		return mcpserversrepo.McpServer{}, oops.E(oops.CodeUnexpected, err, "get mcp server").LogError(ctx, logger)
	}

	ref := authz.MCPServer(authz.MCPServerIDOf(mcpGrantResourceID(server)), authz.ProjectIDOf(server.ProjectID))
	check := ref.Read()
	if write {
		check = ref.Write()
	}
	if err := s.authz.Require(ctx, check); err != nil {
		return mcpserversrepo.McpServer{}, err
	}

	return server, nil
}

// settingsEditor is the email of the person who last changed a server's
// settings, or nil when there is no such person to name.
func (s *Service) settingsEditor(ctx context.Context, queries *repo.Queries, server mcpserversrepo.McpServer) (*string, error) {
	email, err := queries.GetFeedbackConfigEditor(ctx, repo.GetFeedbackConfigEditorParams{McpServerID: server.ID, ProjectID: server.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("get feedback settings editor: %w", err)
	}

	return conv.PtrEmpty(email), nil
}

// settingsFromForm reads the settings a caller sent. A name or description
// that only repeats the default is stored as unset, so the server keeps
// following the default if it changes.
func settingsFromForm(payload *gen.UpdateSettingsPayload) Settings {
	settings := Settings{
		Enabled:         payload.Enabled,
		ToolName:        strings.TrimSpace(conv.PtrValOr(payload.ToolName, "")),
		ToolDescription: strings.TrimSpace(conv.PtrValOr(payload.ToolDescription, "")),
	}
	if settings.ToolName == DefaultToolName {
		settings.ToolName = ""
	}
	if settings.ToolDescription == DefaultToolDescription {
		settings.ToolDescription = ""
	}

	return settings
}

// mcpGrantResourceID is the resource id mcp grants name for a stored MCP
// server: the backing toolset's id when it has one, else its own. The serving
// path and role-grant selectors use the same ids, so a grant scoped to one
// server applies here too.
func mcpGrantResourceID(server mcpserversrepo.McpServer) uuid.UUID {
	if server.ToolsetID.Valid {
		return server.ToolsetID.UUID
	}

	return server.ID
}

// builtWithSpeakeasy reports whether Speakeasy serves the server's tools
// itself, from an OpenAPI document or functions. Only those servers offer the
// feedback tool for now: a remote, tunneled or catalogued server belongs to a
// third party, and the gateway does not add tools to it.
func builtWithSpeakeasy(server mcpserversrepo.McpServer) bool {
	return server.ToolsetID.Valid
}
