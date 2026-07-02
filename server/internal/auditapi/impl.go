package auditapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/auditlogs"
	srv "github.com/speakeasy-api/gram/server/gen/http/auditlogs/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

const listAuditLogsPageSize = 50

// Speakeasy staff actions in customer orgs (e.g. via the dev-tools org
// override) are shown under a collective label instead of the staff member's
// email. Logs viewed within the Speakeasy org itself are not masked.
const (
	speakeasyTeamOrganizationID = "5a25158b-24dc-4d49-b03d-e85acfbea59c"
	speakeasyTeamActorLabel     = "Speakeasy Team"
)

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	auth   *auth.Auth
	authz  *authz.Engine
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessions *sessions.Manager, authzEngine *authz.Engine) *Service {
	logger = logger.With(attr.SlogComponent("audit"))

	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/audit"),
		logger: logger,
		db:     db,
		auth:   auth.New(logger, db, sessions, authzEngine),
		authz:  authzEngine,
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

func (s *Service) List(ctx context.Context, payload *gen.ListPayload) (*gen.ListAuditLogsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, fmt.Errorf("require org read: %w", err)
	}

	projectID, err := s.resolveProjectID(ctx, authCtx.ActiveOrganizationID, conv.PtrValOrEmpty(payload.ProjectSlug, ""))
	if err != nil {
		return nil, err
	}

	params := repo.ListAuditLogsParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
		CursorSeq: pgtype.Int8{
			Int64: 0,
			Valid: false,
		},
		ActorID:     conv.PtrToPGTextEmpty(payload.ActorID),
		Action:      conv.PtrToPGTextEmpty(payload.Action),
		SubjectType: conv.PtrToPGTextEmpty(payload.SubjectType),
		SubjectID:   conv.PtrToPGTextEmpty(payload.SubjectID),
	}

	if payload.Cursor != nil && *payload.Cursor != "" {
		seq, err := decodeCursor(*payload.Cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").LogError(ctx, s.logger)
		}
		params.CursorSeq = pgtype.Int8{Int64: seq, Valid: true}
	}

	rows, err := repo.New(s.db).ListAuditLogs(ctx, params)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing audit logs").LogError(ctx, s.logger)
	}

	logs := make([]*gen.AuditLog, 0, min(len(rows), listAuditLogsPageSize))
	for _, row := range rows[:min(len(rows), listAuditLogsPageSize)] {
		log, err := toAuditLog(row)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error building audit log response").LogError(ctx, s.logger)
		}
		logs = append(logs, log)
	}

	var nextCursor *string
	if len(rows) > listAuditLogsPageSize {
		cursor := encodeCursor(rows[listAuditLogsPageSize-1].Seq, rows[listAuditLogsPageSize-1].ID.String())
		nextCursor = &cursor
		logs = logs[:listAuditLogsPageSize]
	}

	actorIDs := make([]string, 0, len(logs))
	for _, log := range logs {
		if log.ActorType == "user" {
			actorIDs = append(actorIDs, log.ActorID)
		}
	}
	speakeasyActors, err := s.speakeasyActorIDs(ctx, authCtx.ActiveOrganizationID, actorIDs)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error resolving audit actor identities").LogError(ctx, s.logger)
	}
	for _, log := range logs {
		if log.ActorType == "user" && speakeasyActors[log.ActorID] {
			log.ActorDisplayName = conv.PtrEmpty(speakeasyTeamActorLabel)
			log.ActorSlug = nil
		}
	}

	return &gen.ListAuditLogsResult{
		Logs:       logs,
		NextCursor: nextCursor,
	}, nil
}

// speakeasyActorIDs returns which of the given user actor IDs belong to the
// Speakeasy org, so their identities can be masked in customer-facing feeds.
// It returns nil when the viewer is the Speakeasy org itself.
func (s *Service) speakeasyActorIDs(ctx context.Context, viewerOrganizationID string, actorIDs []string) (map[string]bool, error) {
	if viewerOrganizationID == speakeasyTeamOrganizationID || len(actorIDs) == 0 {
		return nil, nil
	}

	memberIDs, err := orgrepo.New(s.db).FilterOrganizationMemberUserIDs(ctx, orgrepo.FilterOrganizationMemberUserIDsParams{
		OrganizationID: speakeasyTeamOrganizationID,
		UserIds:        actorIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("filter speakeasy org members: %w", err)
	}

	members := make(map[string]bool, len(memberIDs))
	for _, id := range memberIDs {
		members[id] = true
	}

	return members, nil
}

func (s *Service) ListFacets(ctx context.Context, payload *gen.ListFacetsPayload) (*gen.ListAuditLogFacetsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, fmt.Errorf("require org read: %w", err)
	}

	projectID, err := s.resolveProjectID(ctx, authCtx.ActiveOrganizationID, conv.PtrValOrEmpty(payload.ProjectSlug, ""))
	if err != nil {
		return nil, err
	}

	queries := repo.New(s.db)
	actorRows, err := queries.ListAuditActorFacets(ctx, repo.ListAuditActorFacetsParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing audit actor facets").LogError(ctx, s.logger)
	}

	actionRows, err := queries.ListAuditActionFacets(ctx, repo.ListAuditActionFacetsParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing audit action facets").LogError(ctx, s.logger)
	}

	actors := toAuditActorFacetOptions(actorRows)

	// Facet values are actor IDs, so mask their display names the same way the
	// log feed does. Only user actors are candidates — other actor types keep
	// their labels even if their id collides with a staff user id.
	actorIDs := make([]string, 0, len(actorRows))
	for _, row := range actorRows {
		if row.IsUserActor {
			actorIDs = append(actorIDs, row.Value)
		}
	}
	speakeasyActors, err := s.speakeasyActorIDs(ctx, authCtx.ActiveOrganizationID, actorIDs)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error resolving audit actor identities").LogError(ctx, s.logger)
	}
	for _, actor := range actors {
		if speakeasyActors[actor.Value] {
			actor.DisplayName = speakeasyTeamActorLabel
		}
	}

	return &gen.ListAuditLogFacetsResult{
		Actors:  actors,
		Actions: toAuditActionFacetOptions(actionRows),
	}, nil
}

func (s *Service) resolveProjectID(ctx context.Context, organizationID string, projectSlug string) (uuid.NullUUID, error) {
	if projectSlug == "" {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
	}

	project, err := projectsrepo.New(s.db).GetProjectBySlug(ctx, projectsrepo.GetProjectBySlugParams{
		Slug:           projectSlug,
		OrganizationID: organizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return uuid.NullUUID{}, oops.C(oops.CodeNotFound)
	case err != nil:
		return uuid.NullUUID{}, oops.E(oops.CodeUnexpected, err, "error getting project by slug").LogError(ctx, s.logger, attr.SlogProjectSlug(projectSlug), attr.SlogOrganizationID(organizationID))
	default:
		return uuid.NullUUID{UUID: project.ID, Valid: true}, nil
	}
}

func encodeCursor(seq int64, id string) string {
	// currently, the id is included to ensure the cursor is unique by customer
	// and reduce predictability (hyrum's law).
	payload := fmt.Sprintf("%d:%s", seq, id)
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func decodeCursor(cursor string) (int64, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, fmt.Errorf("decode cursor: %w", err)
	}

	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid cursor format")
	}

	seq, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse cursor seq: %w", err)
	}

	return seq, nil
}

func toAuditLog(row repo.ListAuditLogsRow) (*gen.AuditLog, error) {
	var metadata map[string]any
	if len(row.Metadata) > 0 {
		metadata = make(map[string]any)
		if err := json.Unmarshal(row.Metadata, &metadata); err != nil {
			return nil, fmt.Errorf("unmarshal metadata: %w", err)
		}
	}

	return &gen.AuditLog{
		ID:                 row.ID.String(),
		ProjectID:          conv.FromNullableUUID(row.ProjectID),
		ProjectSlug:        conv.FromPGText[string](row.ProjectSlug),
		ActorID:            row.ActorID,
		ActorType:          row.ActorType,
		ActorDisplayName:   conv.FromPGText[string](row.ActorDisplayName),
		ActorSlug:          conv.FromPGText[string](row.ActorSlug),
		Action:             row.Action,
		SubjectID:          row.SubjectID,
		SubjectType:        row.SubjectType,
		SubjectDisplayName: conv.FromPGText[string](row.SubjectDisplayName),
		SubjectSlug:        conv.FromPGText[string](row.SubjectSlug),
		BeforeSnapshot:     row.BeforeSnapshot,
		AfterSnapshot:      row.AfterSnapshot,
		Metadata:           metadata,
		CreatedAt:          row.CreatedAt.Time.Format(time.RFC3339),
	}, nil
}

func toAuditActorFacetOptions(rows []repo.ListAuditActorFacetsRow) []*gen.AuditLogFacetOption {
	options := make([]*gen.AuditLogFacetOption, 0, len(rows))
	for _, row := range rows {
		options = append(options, &gen.AuditLogFacetOption{
			Value:       row.Value,
			DisplayName: row.DisplayName,
			Count:       row.Count,
		})
	}

	return options
}

func toAuditActionFacetOptions(rows []repo.ListAuditActionFacetsRow) []*gen.AuditLogFacetOption {
	options := make([]*gen.AuditLogFacetOption, 0, len(rows))
	for _, row := range rows {
		options = append(options, &gen.AuditLogFacetOption{
			Value:       row.Value,
			DisplayName: row.DisplayName,
			Count:       row.Count,
		})
	}

	return options
}
