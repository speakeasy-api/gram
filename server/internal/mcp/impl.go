package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"

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
	"github.com/speakeasy-api/gram/internal/encryption"
	"github.com/speakeasy-api/gram/internal/middleware"
	"github.com/speakeasy-api/gram/internal/o11y"
	"github.com/speakeasy-api/gram/internal/oops"
)

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	repo   *repo.Queries
	auth   *auth.Auth
	enc    *encryption.Encryption
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, enc *encryption.Encryption) *Service {
	return &Service{
		tracer: otel.Tracer("github.com/speakeasy-api/gram/internal/mcp"),
		logger: logger,
		db:     db,
		repo:   repo.New(db),
		auth:   auth.New(logger, db, sessions),
		enc:    enc,
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

func (s *Service) Serve(ctx context.Context, payload *gen.ServePayload, r io.ReadCloser) (*gen.ServeResult, io.ReadCloser, error) {
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return r.Close()
	})

	// authorization check is done in handleBatch

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

	var noContent *gen.NoContent
	body, err := s.handleBatch(ctx, payload, batch)
	switch {
	case errors.As(err, &noContent):
		return nil, nil, err
	case err != nil:
		return nil, nil, NewErrorFromCause(batch[0].ID, err)
	}

	return &gen.ServeResult{
		ContentType: "application/json",
	}, io.NopCloser(bytes.NewReader(body)), nil

}

func (s *Service) handleBatch(ctx context.Context, payload *gen.ServePayload, batch batchedRawRequest) (json.RawMessage, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

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

func (s *Service) handleRequest(ctx context.Context, payload *gen.ServePayload, req *rawRequest) (json.RawMessage, error) {
	switch req.Method {
	case "ping":
		return handlePing(req.ID)
	case "initialize":
		return handleInitialize(ctx, payload, req)
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
