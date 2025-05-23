package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/gen/http/mcp/server"
	gen "github.com/speakeasy-api/gram/gen/mcp"
	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/auth/repo"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/encryption"
	"github.com/speakeasy-api/gram/internal/middleware"
	"github.com/speakeasy-api/gram/internal/o11y"
	"github.com/speakeasy-api/gram/internal/oops"
	projects_repo "github.com/speakeasy-api/gram/internal/projects/repo"
	toolsets_repo "github.com/speakeasy-api/gram/internal/toolsets/repo"
)

type Service struct {
	tracer       trace.Tracer
	logger       *slog.Logger
	db           *pgxpool.Pool
	repo         *repo.Queries
	projectsRepo *projects_repo.Queries
	toolsetsRepo *toolsets_repo.Queries
	auth         *auth.Auth
	enc          *encryption.Encryption
}

type mcpInputs struct {
	projectID            uuid.UUID
	toolset              string
	environment          string
	environmentVariables json.RawMessage
	authenticated        bool
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, enc *encryption.Encryption) *Service {
	return &Service{
		tracer:       otel.Tracer("github.com/speakeasy-api/gram/internal/mcp"),
		logger:       logger,
		db:           db,
		repo:         repo.New(db),
		projectsRepo: projects_repo.New(db),
		toolsetsRepo: toolsets_repo.New(db),
		auth:         auth.New(logger, db, sessions),
		enc:          enc,
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

func (s *Service) ServePublic(ctx context.Context, payload *gen.ServePublicPayload, r io.ReadCloser) (*gen.ServePublicResult, io.ReadCloser, error) {
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return r.Close()
	})

	if payload.ApikeyToken != nil {
		var err error
		sc := security.APIKeyScheme{
			Name:           auth.KeySecurityScheme,
			RequiredScopes: []string{"consumer"},
			Scopes:         []string{},
		}
		ctx, err = s.auth.Authorize(ctx, *payload.ApikeyToken, &sc)
		if err != nil {
			return nil, nil, oops.C(oops.CodeUnauthorized)
		}
	}

	if payload.McpSlug == "" {
		return nil, nil, oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided")
	}

	toolset, err := s.toolsetsRepo.GetToolsetByMcpSlug(ctx, conv.ToPGText(payload.McpSlug))
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to get toolset for MCP server slug", slog.String("error", err.Error()))
		return nil, nil, oops.E(oops.CodeNotFound, err, "mcp server not found").Log(ctx, s.logger)
	}
	var defaultEnvironment string
	var authenticated bool
	if !toolset.McpIsPublic {
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		if !ok || authCtx == nil || authCtx.ProjectID == nil {
			return nil, nil, oops.C(oops.CodeUnauthorized)
		}
		// we'll only use a default environment if the mcp is authenticated
		authenticated = true
		defaultEnvironment = conv.PtrValOr(conv.FromPGText[string](toolset.DefaultEnvironmentSlug), "")
	}

	var batch batchedRawRequest
	err = json.NewDecoder(r).Decode(&batch)
	switch {
	case errors.Is(err, io.EOF):
		return nil, nil, nil
	case err != nil:
		return nil, nil, err
	}

	if len(batch) == 0 {
		return nil, nil, &gen.NoContent{Ack: true}
	}

	mcpInputs := &mcpInputs{
		projectID:            toolset.ProjectID,
		toolset:              toolset.Slug,
		environment:          defaultEnvironment,
		environmentVariables: nil,
		authenticated:        authenticated,
	}
	if payload.EnvironmentVariables != nil {
		mcpInputs.environmentVariables = json.RawMessage(*payload.EnvironmentVariables)
	}

	var noContent *gen.NoContent
	body, err := s.handleBatch(ctx, mcpInputs, batch)
	switch {
	case errors.As(err, &noContent):
		return nil, nil, err
	case err != nil:
		return nil, nil, NewErrorFromCause(batch[0].ID, err)
	}

	return &gen.ServePublicResult{
		ContentType: "application/json",
	}, io.NopCloser(bytes.NewReader(body)), nil
}

func (s *Service) ServeAuthenticated(ctx context.Context, payload *gen.ServeAuthenticatedPayload, r io.ReadCloser) (*gen.ServeAuthenticatedResult, io.ReadCloser, error) {
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return r.Close()
	})

	// authorization check
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, nil, oops.C(oops.CodeUnauthorized)
	}

	var batch batchedRawRequest
	err := json.NewDecoder(r).Decode(&batch)
	switch {
	case errors.Is(err, io.EOF):
		return nil, nil, nil
	case err != nil:
		return nil, nil, err
	}

	if len(batch) == 0 {
		return nil, nil, &gen.NoContent{Ack: true}
	}

	mcpInputs := &mcpInputs{
		projectID:            *authCtx.ProjectID,
		toolset:              *payload.Toolset,
		environment:          *payload.Environment,
		environmentVariables: nil,
		authenticated:        true,
	}
	if payload.EnvironmentVariables != nil {
		mcpInputs.environmentVariables = json.RawMessage(*payload.EnvironmentVariables)
	}

	var noContent *gen.NoContent
	body, err := s.handleBatch(ctx, mcpInputs, batch)
	switch {
	case errors.As(err, &noContent):
		return nil, nil, err
	case err != nil:
		return nil, nil, NewErrorFromCause(batch[0].ID, err)
	}

	return &gen.ServeAuthenticatedResult{
		ContentType: "application/json",
	}, io.NopCloser(bytes.NewReader(body)), nil

}

func (s *Service) handleBatch(ctx context.Context, payload *mcpInputs, batch batchedRawRequest) (json.RawMessage, error) {
	results := make([]json.RawMessage, 0, len(batch))
	for _, req := range batch {
		result, err := s.handleRequest(ctx, payload, req)
		var noContent *gen.NoContent
		switch {
		case errors.As(err, &noContent):
			return nil, err
		case err != nil:
			bs, merr := json.Marshal(NewErrorFromCause(req.ID, err))
			if merr != nil {
				return nil, merr
			}

			result = bs
		}

		results = append(results, result)
	}

	if len(results) == 1 {
		return results[0], nil
	} else {
		m, err := json.Marshal(results)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize results")
		}

		return m, nil
	}
}

func (s *Service) handleRequest(ctx context.Context, payload *mcpInputs, req *rawRequest) (json.RawMessage, error) {
	switch req.Method {
	case "ping":
		return handlePing(req.ID)
	case "initialize":
		return handleInitialize(req)
	case "notifications/initialized", "notifications/cancelled":
		return nil, &gen.NoContent{Ack: true}
	case "tools/list":
		return handleToolsList(ctx, s.logger, s.db, payload, req)
	case "tools/call":
		return handleToolsCall(ctx, s.tracer, s.logger, s.db, s.enc, payload, req)
	default:
		return nil, &rpcError{
			ID:      req.ID,
			Code:    methodNotFound,
			Message: fmt.Sprintf("%s: %s", req.Method, methodNotFound.UserMessage()),
			Data:    nil,
		}
	}
}
