package businessmemory

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	pgvector_go "github.com/pgvector/pgvector-go"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/business_memories"
	srv "github.com/speakeasy-api/gram/server/gen/http/business_memories/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/businessmemory/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/killswitches/hostedinference"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type Service struct {
	tracer      trace.Tracer
	logger      *slog.Logger
	db          *pgxpool.Pool
	auth        *auth.Auth
	authz       *authz.Engine
	completions openrouter.CompletionClient
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
	completions openrouter.CompletionClient,
) *Service {
	logger = logger.With(attr.SlogComponent("business-memories-api"))
	return &Service{
		tracer:      tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/businessmemory"),
		logger:      logger,
		db:          db,
		auth:        auth.New(logger, db, sessionManager, authzEngine),
		authz:       authzEngine,
		completions: completions,
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

func (s *Service) requireOrgAdmin(ctx context.Context) (*contextvalues.AuthContext, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{
		Scope:        authz.ScopeOrgAdmin,
		ResourceKind: "",
		ResourceID:   authCtx.ActiveOrganizationID,
		Dimensions:   nil,
	}); err != nil {
		return nil, err
	}
	return authCtx, nil
}

func (s *Service) ListBusinessMemories(ctx context.Context, payload *gen.ListBusinessMemoriesPayload) (*gen.ListBusinessMemoriesResult, error) {
	authCtx, err := s.requireOrgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	cursorCreatedAt, cursorID, err := decodeCursor(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").LogError(ctx, s.logger)
	}

	rows, err := repo.New(s.db).ListBusinessMemories(ctx, repo.ListBusinessMemoriesParams{
		ProjectID:             conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:        authCtx.ActiveOrganizationID,
		CursorCreatedAt:       conv.PtrToPGTimestamptz(cursorCreatedAt),
		CursorID:              uuid.NullUUID{UUID: conv.PtrValOr(cursorID, uuid.Nil), Valid: cursorID != nil},
		ContentScope:          conv.PtrToPGText(payload.ContentScope),
		ContentScopeNamespace: conv.PtrToPGText(payload.ContentScopeNamespace),
		PageLimit:             conv.SafeInt32(payload.Limit + 1),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list business memories").LogError(ctx, s.logger)
	}

	hasNextPage := len(rows) > payload.Limit
	if hasNextPage {
		rows = rows[:payload.Limit]
	}

	memories := make([]*gen.BusinessMemory, 0, len(rows))
	for _, row := range rows {
		view, err := buildMemoryView(memoryRecord{
			ID:              row.ID,
			Body:            row.Body,
			MemoryType:      row.MemoryType,
			StructuralScope: row.StructuralScope,
			ContentScope:    row.ContentScope,
			EmbeddingModel:  row.EmbeddingModel,
			ExtractionModel: row.ExtractionModel,
			SourceChatID:    row.SourceChatID,
			SourceTurn:      row.SourceTurn,
			SourceAuthorID:  row.SourceAuthorID,
			ExtractedAt:     row.ExtractedAt,
			LifecycleState:  row.LifecycleState,
			Similarity:      nil,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "decode business memory").LogError(ctx, s.logger)
		}
		memories = append(memories, view)
	}

	var nextCursor *string
	if hasNextPage {
		last := rows[len(rows)-1]
		if last.CreatedAt.Valid {
			encoded := encodeCursor(last.CreatedAt.Time, last.ID)
			nextCursor = &encoded
		}
	}

	return &gen.ListBusinessMemoriesResult{Memories: memories, NextCursor: nextCursor}, nil
}

func (s *Service) ListBusinessMemoryContentScopes(ctx context.Context, _ *gen.ListBusinessMemoryContentScopesPayload) (*gen.ListBusinessMemoryContentScopesResult, error) {
	authCtx, err := s.requireOrgAdmin(ctx)
	if err != nil {
		return nil, err
	}

	queries := repo.New(s.db)
	params := repo.ListBusinessMemoryContentScopesParams{
		ProjectID:      conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID: authCtx.ActiveOrganizationID,
	}
	rows, err := queries.ListBusinessMemoryContentScopes(ctx, params)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list business memory content scopes").LogError(ctx, s.logger)
	}

	totalMemories, err := queries.CountBusinessMemories(ctx, repo.CountBusinessMemoriesParams(params))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count business memories").LogError(ctx, s.logger)
	}

	nodes := make([]*gen.BusinessMemoryContentScopeNode, 0, len(rows))
	for _, row := range rows {
		var parentScope *string
		if row.ParentScope.Valid {
			parentScope = &row.ParentScope.String
		}
		nodes = append(nodes, &gen.BusinessMemoryContentScopeNode{
			Scope:       row.Scope,
			ParentScope: parentScope,
			MemoryCount: row.MemoryCount,
		})
	}

	return &gen.ListBusinessMemoryContentScopesResult{
		Nodes:         nodes,
		TotalMemories: totalMemories,
	}, nil
}

func (s *Service) SearchBusinessMemories(ctx context.Context, payload *gen.SearchBusinessMemoriesPayload) (*gen.SearchBusinessMemoriesResult, error) {
	authCtx, err := s.requireOrgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	query := strings.TrimSpace(payload.Query)
	if query == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "query is required")
	}

	ctx, err = hostedinference.WithGovernedUserOrUnsupported(ctx, hostedinference.CallCategoryBusinessMemorySearchEmbedding, hostedinference.CallCategoryAPIKeyBusinessMemorySearch, hostedinference.CallCategoryNonOrdinarySessionMemorySearch)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "classify business memory search inference").LogError(ctx, s.logger)
	}
	vectors, err := s.completions.CreateEmbeddings(
		ctx,
		authCtx.ActiveOrganizationID,
		embeddingModel,
		[]string{query},
		openrouter.WithEmbeddingDimensions(embeddingDimensions),
		openrouter.WithEmbeddingKeyType(openrouter.KeyTypeInternal),
	)
	if err != nil {
		//nolint:wrapcheck // The mapper returns a fully wrapped, telemetry-safe boundary error.
		if mapped, ok := hostedinference.MapBoundaryError(ctx, s.logger, err); ok {
			return nil, mapped
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create business memory search embedding").LogError(ctx, s.logger)
	}
	if len(vectors) != 1 {
		return nil, oops.E(oops.CodeUnexpected, nil, "embedding response had %d vectors, expected 1", len(vectors)).LogError(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin business memory search").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	if err := enableFilteredVectorScan(ctx, dbtx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "configure business memory search").LogError(ctx, s.logger)
	}

	rows, err := repo.New(dbtx).SearchBusinessMemories(ctx, repo.SearchBusinessMemoriesParams{
		QueryEmbedding:        pgvector_go.NewHalfVector(vectors[0]),
		EmbeddingModel:        embeddingModel,
		ProjectID:             conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:        authCtx.ActiveOrganizationID,
		ContentScope:          conv.PtrToPGText(payload.ContentScope),
		ContentScopeNamespace: conv.PtrToPGText(payload.ContentScopeNamespace),
		ResultLimit:           conv.SafeInt32(payload.Limit),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "search business memories").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit business memory search").LogError(ctx, s.logger)
	}

	memories := make([]*gen.BusinessMemory, 0, len(rows))
	for _, row := range rows {
		similarity := row.Similarity
		view, err := buildMemoryView(memoryRecord{
			ID:              row.ID,
			Body:            row.Body,
			MemoryType:      row.MemoryType,
			StructuralScope: row.StructuralScope,
			ContentScope:    row.ContentScope,
			EmbeddingModel:  row.EmbeddingModel,
			ExtractionModel: row.ExtractionModel,
			SourceChatID:    row.SourceChatID,
			SourceTurn:      row.SourceTurn,
			SourceAuthorID:  row.SourceAuthorID,
			ExtractedAt:     row.ExtractedAt,
			LifecycleState:  row.LifecycleState,
			Similarity:      &similarity,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "decode business memory search result").LogError(ctx, s.logger)
		}
		memories = append(memories, view)
	}

	return &gen.SearchBusinessMemoriesResult{Memories: memories}, nil
}

type memoryRecord struct {
	ID              uuid.UUID
	Body            string
	MemoryType      string
	StructuralScope string
	ContentScope    []byte
	EmbeddingModel  string
	ExtractionModel string
	SourceChatID    uuid.NullUUID
	SourceTurn      pgtype.Int4
	SourceAuthorID  pgtype.Text
	ExtractedAt     pgtype.Timestamptz
	LifecycleState  string
	Similarity      *float64
}

func buildMemoryView(record memoryRecord) (*gen.BusinessMemory, error) {
	var contentScope []string
	if err := json.Unmarshal(record.ContentScope, &contentScope); err != nil {
		return nil, fmt.Errorf("unmarshal content scope: %w", err)
	}
	if contentScope == nil {
		contentScope = []string{}
	}

	sourceChatID := "unavailable"
	if record.SourceChatID.Valid {
		sourceChatID = record.SourceChatID.UUID.String()
	}
	var sourceTurn *int
	if record.SourceTurn.Valid {
		value := int(record.SourceTurn.Int32)
		sourceTurn = &value
	}
	var sourceAuthorID *string
	if record.SourceAuthorID.Valid {
		sourceAuthorID = &record.SourceAuthorID.String
	}

	extractedAt := ""
	if record.ExtractedAt.Valid {
		extractedAt = record.ExtractedAt.Time.UTC().Format(time.RFC3339Nano)
	}
	return &gen.BusinessMemory{
		ID:              record.ID.String(),
		Body:            record.Body,
		MemoryType:      record.MemoryType,
		StructuralScope: record.StructuralScope,
		ContentScope:    contentScope,
		EmbeddingModel:  record.EmbeddingModel,
		ExtractionModel: record.ExtractionModel,
		SourceChatID:    sourceChatID,
		SourceTurn:      sourceTurn,
		SourceAuthorID:  sourceAuthorID,
		ExtractedAt:     extractedAt,
		LifecycleState:  record.LifecycleState,
		Similarity:      record.Similarity,
	}, nil
}

func encodeCursor(createdAt time.Time, id uuid.UUID) string {
	payload := fmt.Sprintf("%s|%s", createdAt.UTC().Format(time.RFC3339Nano), id)
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func decodeCursor(cursor *string) (*time.Time, *uuid.UUID, error) {
	if cursor == nil || *cursor == "" {
		return nil, nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(*cursor)
	if err != nil {
		return nil, nil, fmt.Errorf("decode cursor: %w", err)
	}
	parts := strings.SplitN(string(decoded), "|", 2)
	if len(parts) != 2 {
		return nil, nil, fmt.Errorf("invalid cursor format")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return nil, nil, fmt.Errorf("parse cursor timestamp: %w", err)
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return nil, nil, fmt.Errorf("parse cursor id: %w", err)
	}
	return &createdAt, &id, nil
}
