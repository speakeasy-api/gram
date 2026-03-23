package audit

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
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
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

const listAuditLogsPageSize = 50

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	auth   *auth.Auth
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessions *sessions.Manager) *Service {
	logger = logger.With(attr.SlogComponent("audit"))

	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/audit"),
		logger: logger,
		db:     db,
		auth:   auth.New(logger, db, sessions),
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

	params := repo.ListAuditLogsParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID: uuid.NullUUID{
			UUID:  uuid.Nil,
			Valid: false,
		},
		CursorSeq: pgtype.Int8{
			Int64: 0,
			Valid: false,
		},
	}

	projectSlug := conv.PtrValOrEmpty(payload.ProjectSlug, "")
	if projectSlug != "" {
		project, err := projectsrepo.New(s.db).GetProjectBySlug(ctx, projectsrepo.GetProjectBySlugParams{
			Slug:           projectSlug,
			OrganizationID: authCtx.ActiveOrganizationID,
		})
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil, oops.C(oops.CodeNotFound)
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "error getting project by slug").Log(ctx, s.logger, attr.SlogProjectSlug(projectSlug), attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
		default:
			params.ProjectID = uuid.NullUUID{UUID: project.ID, Valid: true}
		}
	}

	if payload.Cursor != nil && *payload.Cursor != "" {
		seq, err := decodeCursor(*payload.Cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").Log(ctx, s.logger)
		}
		params.CursorSeq = pgtype.Int8{Int64: seq, Valid: true}
	}

	rows, err := repo.New(s.db).ListAuditLogs(ctx, params)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing audit logs").Log(ctx, s.logger)
	}

	logs := make([]*gen.AuditLog, 0, min(len(rows), listAuditLogsPageSize))
	for _, row := range rows {
		log, err := toAuditLog(row)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error building audit log response").Log(ctx, s.logger)
		}
		logs = append(logs, log)
	}

	var nextCursor *string
	if len(rows) > listAuditLogsPageSize {
		cursor := encodeCursor(rows[listAuditLogsPageSize-1].Seq, rows[listAuditLogsPageSize-1].ID.String())
		nextCursor = &cursor
		logs = logs[:listAuditLogsPageSize]
	}

	return &gen.ListAuditLogsResult{
		Logs:       logs,
		NextCursor: nextCursor,
	}, nil
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

func toAuditLog(row repo.AuditLog) (*gen.AuditLog, error) {
	var metadata map[string]any
	if len(row.Metadata) > 0 {
		metadata = make(map[string]any)
		if err := json.Unmarshal(row.Metadata, &metadata); err != nil {
			return nil, fmt.Errorf("unmarshal metadata: %w", err)
		}
	}

	return &gen.AuditLog{
		ID:                 row.ID.String(),
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
