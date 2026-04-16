package external

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/workos/workos-go/v6/pkg/webhooks"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/external"
	srv "github.com/speakeasy-api/gram/server/gen/http/external/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/temporal"
)

const maxWebhookBodySize = 1 << 20 // 1MB

type Service struct {
	tracer        trace.Tracer
	logger        *slog.Logger
	db            *pgxpool.Pool
	webhookClient *webhooks.Client
	temporalEnv   *temporal.Environment
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, webhookClient *webhooks.Client, temporalEnv *temporal.Environment) *Service {
	return &Service{
		tracer:        tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/external"),
		logger:        logger.With(attr.SlogComponent("external")),
		db:            db,
		webhookClient: webhookClient,
		temporalEnv:   temporalEnv,
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

func (s *Service) APIKeyAuth(ctx context.Context, key string, scheme *security.APIKeyScheme) (context.Context, error) {
	logger := s.logger

	switch scheme.Name {
	case constants.WorkOSSignatureSecurityScheme:
		// This will be validated within service methods because we need the request body
		return ctx, nil
	default:
		err := fmt.Errorf("unexpected security scheme: %s", scheme.Name)
		return ctx, oops.E(oops.CodeUnexpected, err, "unable to authorize request").Log(ctx, logger)
	}
}

// ReceiveWorkOSWebhook implements [external.Service].
func (s *Service) ReceiveWorkOSWebhook(ctx context.Context, payload *gen.ReceiveWorkOSWebhookPayload, body io.ReadCloser) (err error) {
	logger := s.logger
	defer o11y.NoLogDefer(func() error { return body.Close() })

	signature := conv.PtrValOrEmpty(payload.WorkosSignature, "")
	if signature == "" {
		return oops.C(oops.CodeUnauthorized)
	}

	bs, err := io.ReadAll(io.LimitReader(body, maxWebhookBodySize)) // Limit to 1MB
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "unable to read request body").Log(ctx, logger)
	}

	validated, err := s.webhookClient.ValidatePayload(signature, string(bs))
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "invalid WorkOS webhook signature").Log(ctx, logger)
	}

	var event struct {
		ID   string `json:"id"`
		Type string `json:"event"`
		Data struct {
			ID             string `json:"id"`
			Object         string `json:"object"`
			OrganizationID string `json:"organization_id"`
		} `json:"data"`
	}

	if err := json.Unmarshal([]byte(validated), &event); err != nil {
		return oops.E(oops.CodeBadRequest, err, "unable to parse workos webhook payload").Log(ctx, logger)
	}

	orgID := conv.Ternary(event.Data.Object == "organization", event.Data.ID, event.Data.OrganizationID)
	userID := conv.Ternary(event.Data.Object == "user", event.Data.ID, "")
	_ = userID

	if orgID == "" {
		if _, err := background.ExecuteProcessWorkOSEventsWorkflow(ctx, s.temporalEnv, background.ProcessWorkOSEventsParams{
			CursorOverride: "",
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to trigger workos event processing workflow").Log(ctx, logger)
		}
	}

	return nil
}
